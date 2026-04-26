package memory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

type fakeRepo struct {
	entries map[string]domain.MemoryEntry
}

func memoryKey(scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) string {
	return string(scopeType) + "|" + scopeID + "|" + string(kind) + "|" + key
}

func (f *fakeRepo) Upsert(ctx context.Context, e domain.MemoryEntry) (domain.MemoryEntry, error) {
	if f.entries == nil {
		f.entries = make(map[string]domain.MemoryEntry)
	}
	now := time.Now().UTC()
	k := memoryKey(e.ScopeType, e.ScopeID, e.Kind, e.Key)
	if e.Version == 0 {
		e.ID = uuid.New()
		e.Version = 1
		e.CreatedAt = now
		e.UpdatedAt = now
		f.entries[k] = e
		return e, nil
	}
	current := f.entries[k]
	e.CreatedAt = current.CreatedAt
	e.UpdatedAt = now
	e.Version = current.Version + 1
	f.entries[k] = e
	return e, nil
}

func (f *fakeRepo) Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error) {
	for _, entry := range f.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return domain.MemoryEntry{}, ErrNotFound
}

func (f *fakeRepo) GetByScopeKey(ctx context.Context, scopeType domain.ScopeType, scopeID string, kind domain.MemoryKind, key string) (domain.MemoryEntry, error) {
	if f.entries == nil {
		return domain.MemoryEntry{}, ErrNotFound
	}
	entry, ok := f.entries[memoryKey(scopeType, scopeID, kind, key)]
	if !ok {
		return domain.MemoryEntry{}, ErrNotFound
	}
	return entry, nil
}

func (f *fakeRepo) Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error) {
	return nil, nil
}

func (f *fakeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeRepo) PurgeExpired(ctx context.Context) (int64, error) {
	return 0, nil
}

func TestUsecases_Upsert_updatesExistingEntryByScopeKey(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	uc := NewUsecases(repo)

	created, err := uc.Upsert(context.Background(), UpsertInput{
		Kind:        domain.MemoryTaskSummary,
		ScopeType:   domain.ScopeTask,
		ScopeID:     "task-1",
		Key:         "current",
		ContentText: "initial",
		PayloadJSON: json.RawMessage(`{"status":"new"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := uc.Upsert(context.Background(), UpsertInput{
		Kind:        domain.MemoryTaskSummary,
		ScopeType:   domain.ScopeTask,
		ScopeID:     "task-1",
		Key:         "current",
		Version:     created.Version,
		ContentText: "updated",
		PayloadJSON: json.RawMessage(`{"status":"done"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected same ID, got %s != %s", updated.ID, created.ID)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("expected version %d, got %d", created.Version+1, updated.Version)
	}
	if updated.ContentText != "updated" {
		t.Fatalf("expected updated content, got %q", updated.ContentText)
	}
}
