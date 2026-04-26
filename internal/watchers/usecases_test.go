package watchers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/devpablocristo/core/governance/go/reviewclient"
	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// --- fakes ---

type fakeWatcherRepo struct {
	watchers  map[uuid.UUID]domain.Watcher
	proposals []domain.Proposal
}

func newFakeRepo() *fakeWatcherRepo {
	return &fakeWatcherRepo{watchers: make(map[uuid.UUID]domain.Watcher)}
}

func (f *fakeWatcherRepo) CreateWatcher(_ context.Context, w domain.Watcher) (domain.Watcher, error) {
	w.ID = uuid.New()
	f.watchers[w.ID] = w
	return w, nil
}

func (f *fakeWatcherRepo) GetWatcher(_ context.Context, id uuid.UUID) (domain.Watcher, error) {
	w, ok := f.watchers[id]
	if !ok {
		return domain.Watcher{}, ErrNotFound
	}
	return w, nil
}

func (f *fakeWatcherRepo) ListWatchers(_ context.Context, orgID string) ([]domain.Watcher, error) {
	var out []domain.Watcher
	for _, w := range f.watchers {
		if orgID == "" || w.OrgID == orgID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (f *fakeWatcherRepo) ListEnabledOrgIDs(_ context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	for _, w := range f.watchers {
		if w.Enabled {
			seen[w.OrgID] = struct{}{}
		}
	}
	var out []string
	for orgID := range seen {
		out = append(out, orgID)
	}
	return out, nil
}

func (f *fakeWatcherRepo) UpdateWatcher(_ context.Context, w domain.Watcher) (domain.Watcher, error) {
	if _, ok := f.watchers[w.ID]; !ok {
		return domain.Watcher{}, ErrNotFound
	}
	f.watchers[w.ID] = w
	return w, nil
}

func (f *fakeWatcherRepo) DeleteWatcher(_ context.Context, id uuid.UUID) error {
	if _, ok := f.watchers[id]; !ok {
		return ErrNotFound
	}
	delete(f.watchers, id)
	return nil
}

func (f *fakeWatcherRepo) CreateProposal(_ context.Context, p domain.Proposal) (domain.Proposal, error) {
	p.ID = uuid.New()
	f.proposals = append(f.proposals, p)
	return p, nil
}

func (f *fakeWatcherRepo) UpdateProposal(_ context.Context, p domain.Proposal) error {
	for i, existing := range f.proposals {
		if existing.ID == p.ID {
			f.proposals[i] = p
			return nil
		}
	}
	return nil
}

func (f *fakeWatcherRepo) ListProposalsByWatcher(_ context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if p.WatcherID == watcherID {
			out = append(out, p)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeWatcherRepo) PendingProposals(_ context.Context, _ string) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if p.ExecutionStatus == domain.ProposalPending {
			out = append(out, p)
		}
	}
	return out, nil
}

// --- pymes fake ---

type fakePymes struct {
	staleItems []domain.PymesItem
	sendErr    error
	sendCalls  int
}

func (f *fakePymes) GetStaleWorkOrders(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return f.staleItems, nil
}

func (f *fakePymes) GetUnconfirmedAppointments(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return nil, nil
}

func (f *fakePymes) GetLowStockItems(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return nil, nil
}

func (f *fakePymes) GetInactiveCustomers(_ context.Context, _ string, _ int) ([]domain.PymesItem, error) {
	return nil, nil
}

func (f *fakePymes) GetRevenueComparison(_ context.Context, _ string) (*domain.RevenueComparison, error) {
	return &domain.RevenueComparison{CurrentMonth: 100, PreviousMonth: 100, DropPercent: 0}, nil
}

func (f *fakePymes) SendWhatsAppTemplate(_ context.Context, _, _, _ string, _ map[string]string) error {
	f.sendCalls++
	return f.sendErr
}

func (f *fakePymes) SendWhatsAppText(_ context.Context, _, _, _ string) error {
	f.sendCalls++
	return f.sendErr
}

// --- review fake ---

type fakeReview struct {
	decision string
}

func (f *fakeReview) SubmitRequest(_ context.Context, _ string, _ reviewclient.SubmitRequestBody) (reviewclient.SubmitResponse, error) {
	return reviewclient.SubmitResponse{
		RequestID: uuid.New().String(),
		Decision:  f.decision,
		Status:    f.decision,
	}, nil
}

func (f *fakeReview) GetRequest(_ context.Context, _ string) (reviewclient.RequestSummary, int, error) {
	return reviewclient.RequestSummary{Status: f.decision, Decision: f.decision}, 200, nil
}

// --- tests ---

func TestUsecases_Create(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakePymes{}, &fakeReview{decision: "allowed"})

	w, err := uc.Create(context.Background(), CreateWatcherInput{
		OrgID:       "org-1",
		Name:        "Stale Orders",
		WatcherType: domain.WatcherStaleWorkOrders,
		Config:      json.RawMessage(`{"threshold_days":5}`),
		Enabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.ID == uuid.Nil {
		t.Fatal("expected generated ID")
	}
	if w.Name != "Stale Orders" {
		t.Fatalf("unexpected name: %s", w.Name)
	}
}

func TestUsecases_UpdatePartialFields(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakePymes{}, &fakeReview{decision: "allowed"})

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Original", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{"threshold_units":10}`), Enabled: true,
	})

	newName := "Updated"
	disabled := false
	updated, err := uc.Update(context.Background(), w.ID, UpdateWatcherInput{
		Name:    &newName,
		Enabled: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Updated" {
		t.Fatalf("expected Updated, got %s", updated.Name)
	}
	if updated.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestUsecases_RunWatcher_DisabledReturnsError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakePymes{}, &fakeReview{decision: "allowed"})

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Disabled", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{}`), Enabled: false,
	})

	_, err := uc.RunWatcher(context.Background(), w.ID)
	if err == nil {
		t.Fatal("expected error for disabled watcher")
	}
}

func TestUsecases_RunWatcher_StaleWorkOrders_AutoExecutes(t *testing.T) {
	t.Parallel()
	pymes := &fakePymes{
		staleItems: []domain.PymesItem{
			{ID: "wo-1", Type: "work_order", Name: "Orden atrasada", PartyID: "party-1"},
			{ID: "wo-2", Type: "work_order", Name: "Otra orden", PartyID: "party-2"},
		},
	}
	review := &fakeReview{decision: "allowed"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, pymes, review)

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Stale WO", WatcherType: domain.WatcherStaleWorkOrders,
		Config: json.RawMessage(`{"threshold_days":3}`), Enabled: true,
	})

	result, err := uc.RunWatcher(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Found != 2 {
		t.Fatalf("expected 2 found, got %d", result.Found)
	}
	if result.Proposed != 2 {
		t.Fatalf("expected 2 proposed, got %d", result.Proposed)
	}
	if result.Executed != 2 {
		t.Fatalf("expected 2 executed, got %d", result.Executed)
	}
	if pymes.sendCalls != 2 {
		t.Fatalf("expected 2 WhatsApp sends, got %d", pymes.sendCalls)
	}
	if len(repo.proposals) != 2 {
		t.Fatalf("expected 2 persisted proposals, got %d", len(repo.proposals))
	}
}

func TestUsecases_RunWatcher_DeniedSkipsExecution(t *testing.T) {
	t.Parallel()
	pymes := &fakePymes{
		staleItems: []domain.PymesItem{
			{ID: "wo-1", Type: "work_order", Name: "Denied order", PartyID: "party-1"},
		},
	}
	review := &fakeReview{decision: "denied"}
	repo := newFakeRepo()
	uc := NewUsecases(repo, pymes, review)

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "Denied WO", WatcherType: domain.WatcherStaleWorkOrders,
		Config: json.RawMessage(`{"threshold_days":3}`), Enabled: true,
	})

	result, err := uc.RunWatcher(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Executed != 0 {
		t.Fatalf("expected 0 executed when denied, got %d", result.Executed)
	}
	if pymes.sendCalls != 0 {
		t.Fatalf("expected 0 sends when denied, got %d", pymes.sendCalls)
	}
}

func TestUsecases_Delete(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	uc := NewUsecases(repo, &fakePymes{}, &fakeReview{})

	w, _ := uc.Create(context.Background(), CreateWatcherInput{
		OrgID: "org-1", Name: "To Delete", WatcherType: domain.WatcherLowStock,
		Config: json.RawMessage(`{}`), Enabled: true,
	})

	if err := uc.Delete(context.Background(), w.ID); err != nil {
		t.Fatal(err)
	}

	_, err := uc.Get(context.Background(), w.ID)
	if err == nil {
		t.Fatal("expected not found after delete")
	}
}
