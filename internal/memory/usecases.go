package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/devpablocristo/core/concurrency/go/worker"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

// Repository port de persistencia para memoria operativa.
type Repository interface {
	Upsert(ctx context.Context, e domain.MemoryEntry) (domain.MemoryEntry, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error)
	GetByScopeKey(ctx context.Context, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) (domain.MemoryEntry, error)
	Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error)
	Delete(ctx context.Context, id uuid.UUID) error
	PurgeExpired(ctx context.Context) (int64, error)
}

// FindQuery filtros de búsqueda de memoria.
type FindQuery struct {
	ScopeType domain.ScopeType
	ScopeID   string
	Kind      domain.MemoryKind
	Limit     int
}

// UpsertInput datos para crear o actualizar una entrada de memoria.
type UpsertInput struct {
	Kind        domain.MemoryKind
	ScopeType   domain.ScopeType
	ScopeID     string
	Key         string
	PayloadJSON json.RawMessage
	ContentText string
	Version     int // 0 = insert, >0 = update con versión optimista
	TTLDays     int // 0 = usar default por kind
}

// Usecases lógica de negocio de memoria operativa.
type Usecases struct {
	repo Repository
}

// NewUsecases crea una nueva instancia de Usecases.
func NewUsecases(repo Repository) *Usecases {
	return &Usecases{repo: repo}
}

// Upsert crea o actualiza una entrada de memoria.
func (uc *Usecases) Upsert(ctx context.Context, in UpsertInput) (domain.MemoryEntry, error) {
	if in.ScopeType == "" || in.ScopeID == "" {
		return domain.MemoryEntry{}, fmt.Errorf("scope_type and scope_id are required")
	}
	if in.Kind == "" {
		return domain.MemoryEntry{}, fmt.Errorf("kind is required")
	}
	if in.Key == "" {
		return domain.MemoryEntry{}, fmt.Errorf("key is required")
	}

	ttl := in.TTLDays
	if ttl == 0 {
		ttl = domain.DefaultRetentionDays(in.Kind)
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().UTC().AddDate(0, 0, ttl)
		expiresAt = &t
	}

	if len(in.PayloadJSON) == 0 {
		in.PayloadJSON = json.RawMessage(`{}`)
	}

	entry := domain.MemoryEntry{
		Kind:        in.Kind,
		ScopeType:   in.ScopeType,
		ScopeID:     in.ScopeID,
		Key:         in.Key,
		PayloadJSON: in.PayloadJSON,
		ContentText: in.ContentText,
		Version:     in.Version,
		ExpiresAt:   expiresAt,
	}

	current, err := uc.repo.GetByScopeKey(ctx, in.ScopeType, in.ScopeID, in.Kind, in.Key)
	switch {
	case err == nil:
		if in.Version > 0 && current.Version != in.Version {
			return domain.MemoryEntry{}, ErrVersionConflict
		}
		entry.ID = current.ID
		entry.CreatedAt = current.CreatedAt
		if in.Version > 0 {
			entry.Version = in.Version
		} else {
			entry.Version = current.Version
		}
	case IsNotFound(err):
		if in.Version > 0 {
			return domain.MemoryEntry{}, ErrVersionConflict
		}
	case err != nil:
		return domain.MemoryEntry{}, fmt.Errorf("lookup memory entry: %w", err)
	}

	result, err := uc.repo.Upsert(ctx, entry)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("upsert memory: %w", err)
	}
	return result, nil
}

// Get obtiene una entrada de memoria por ID.
func (uc *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error) {
	entry, err := uc.repo.Get(ctx, id)
	if err != nil {
		return domain.MemoryEntry{}, fmt.Errorf("get memory: %w", err)
	}
	return entry, nil
}

// Find busca entradas de memoria por scope y kind.
func (uc *Usecases) Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	entries, err := uc.repo.Find(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("find memory: %w", err)
	}
	return entries, nil
}

// Delete elimina una entrada de memoria por ID.
func (uc *Usecases) Delete(ctx context.Context, id uuid.UUID) error {
	if err := uc.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	return nil
}

// RunPurgeLoop ejecuta purga periódica de entradas expiradas.
func (uc *Usecases) RunPurgeLoop(ctx context.Context, interval time.Duration) {
	worker.RunPeriodic(ctx, interval, "memory-purge", func(c context.Context) {
		purged, err := uc.repo.PurgeExpired(c)
		if err != nil {
			slog.Error("purge expired memory", "error", err)
			return
		}
		if purged > 0 {
			slog.Info("purged expired memory entries", "count", purged)
		}
	})
}
