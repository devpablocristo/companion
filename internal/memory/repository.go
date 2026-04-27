package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	sharedpostgres "github.com/devpablocristo/core/databases/postgres/go"
	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

// PostgresRepository implementación PostgreSQL del repositorio de memoria.
type PostgresRepository struct {
	db *sharedpostgres.DB
}

// NewPostgresRepository crea un nuevo repositorio de memoria.
func NewPostgresRepository(db *sharedpostgres.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

const selectMemory = `
	SELECT id, kind, scope_type, scope_id, key, payload_json, content_text,
	       version, created_at, updated_at, expires_at
	FROM companion_memory_entries`

// Upsert crea o actualiza una entrada de memoria con versión optimista.
func (r *PostgresRepository) Upsert(ctx context.Context, e domain.MemoryEntry) (domain.MemoryEntry, error) {
	now := time.Now().UTC()

	if e.Version == 0 {
		// Insert nuevo
		e.ID = uuid.New()
		e.Version = 1
		e.CreatedAt = now
		e.UpdatedAt = now

		_, err := r.db.Pool().Exec(ctx, `
			INSERT INTO companion_memory_entries
				(id, kind, scope_type, scope_id, key, payload_json, content_text, version, created_at, updated_at, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		`, e.ID, e.Kind, e.ScopeType, e.ScopeID, e.Key, e.PayloadJSON, e.ContentText,
			e.Version, e.CreatedAt, e.UpdatedAt, e.ExpiresAt)
		if err != nil {
			return domain.MemoryEntry{}, fmt.Errorf("insert memory: %w", err)
		}
		return e, nil
	}

	// Update con versión optimista
	newVersion := e.Version + 1
	tag, err := r.db.Pool().Exec(ctx, `
		UPDATE companion_memory_entries
		SET payload_json = $3, content_text = $4, version = $5, updated_at = $6, expires_at = $7
		WHERE id = $1 AND version = $2
	`, e.ID, e.Version, e.PayloadJSON, e.ContentText, newVersion, now, e.ExpiresAt)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("update memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.MemoryEntry{}, ErrVersionConflict
	}
	e.Version = newVersion
	e.UpdatedAt = now
	return e, nil
}

// Get obtiene una entrada de memoria por ID.
func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error) {
	row := r.db.Pool().QueryRow(ctx, selectMemory+` WHERE id = $1`, id)
	entry, err := scanMemoryEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MemoryEntry{}, ErrNotFound
		}
		return domain.MemoryEntry{}, fmt.Errorf("get memory: %w", err)
	}
	return entry, nil
}

// GetByScopeKey obtiene una entrada de memoria por scope, kind y key.
func (r *PostgresRepository) GetByScopeKey(ctx context.Context, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) (domain.MemoryEntry, error) {
	row := r.db.Pool().QueryRow(ctx, selectMemory+` WHERE scope_type = $1 AND scope_id = $2 AND kind = $3 AND key = $4`, scopeType, scopeID, kind, key)
	entry, err := scanMemoryEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MemoryEntry{}, ErrNotFound
		}
		return domain.MemoryEntry{}, fmt.Errorf("get memory by scope key: %w", err)
	}
	return entry, nil
}

// Find busca entradas de memoria por scope y kind.
func (r *PostgresRepository) Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}

	query := selectMemory + ` WHERE scope_type = $1 AND scope_id = $2`
	args := []any{q.ScopeType, q.ScopeID}

	if q.Kind != "" {
		query += ` AND kind = $3`
		args = append(args, q.Kind)
		query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d`, len(args)+1)
		args = append(args, q.Limit)
	} else {
		query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d`, len(args)+1)
		args = append(args, q.Limit)
	}

	rows, err := r.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find memory: %w", err)
	}
	defer rows.Close()

	var out []domain.MemoryEntry
	for rows.Next() {
		entry, err := scanMemoryEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// Delete elimina una entrada de memoria.
func (r *PostgresRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Pool().Exec(ctx, `DELETE FROM companion_memory_entries WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpired elimina entradas expiradas.
func (r *PostgresRepository) PurgeExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Pool().Exec(ctx, `
		DELETE FROM companion_memory_entries WHERE expires_at IS NOT NULL AND expires_at < $1
	`, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("purge expired: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountByScope devuelve cuántas entradas vivas existen en (scope_type, scope_id).
// Excluye entradas expiradas (expires_at < now) porque PurgeExpired las va a
// drenar en el próximo loop; contarlas inflaría el quota artificialmente.
func (r *PostgresRepository) CountByScope(ctx context.Context, scopeType domain.ScopeType, scopeID string) (int, error) {
	var n int
	err := r.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*) FROM companion_memory_entries
		WHERE scope_type = $1 AND scope_id = $2
		  AND (expires_at IS NULL OR expires_at > $3)
	`, scopeType, scopeID, time.Now().UTC()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count memory by scope: %w", err)
	}
	return n, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemoryEntry(row rowScanner) (domain.MemoryEntry, error) {
	var e domain.MemoryEntry
	var payloadRaw []byte
	var expiresAt *time.Time

	err := row.Scan(
		&e.ID, &e.Kind, &e.ScopeType, &e.ScopeID, &e.Key,
		&payloadRaw, &e.ContentText, &e.Version,
		&e.CreatedAt, &e.UpdatedAt, &expiresAt,
	)
	if err != nil {
		return domain.MemoryEntry{}, err
	}
	if payloadRaw != nil {
		e.PayloadJSON = json.RawMessage(payloadRaw)
	}
	e.ExpiresAt = expiresAt
	return e, nil
}
