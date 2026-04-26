package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

type fakeConnectorRepo struct {
	connectors map[uuid.UUID]domain.Connector
	executions []domain.ExecutionResult
}

func (f *fakeConnectorRepo) SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	if f.connectors == nil {
		f.connectors = make(map[uuid.UUID]domain.Connector)
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.connectors[c.ID] = c
	return c, nil
}

func (f *fakeConnectorRepo) GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error) {
	c, ok := f.connectors[id]
	if !ok {
		return domain.Connector{}, ErrNotFound
	}
	return c, nil
}

func (f *fakeConnectorRepo) ListConnectors(ctx context.Context) ([]domain.Connector, error) {
	var out []domain.Connector
	for _, c := range f.connectors {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeConnectorRepo) UpdateConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	f.connectors[c.ID] = c
	return c, nil
}

func (f *fakeConnectorRepo) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	delete(f.connectors, id)
	return nil
}

func (f *fakeConnectorRepo) SaveExecution(ctx context.Context, r domain.ExecutionResult) error {
	f.executions = append(f.executions, r)
	return nil
}

func (f *fakeConnectorRepo) GetExecutionByIdempotency(ctx context.Context, taskID uuid.UUID, operation string, reviewRequestID *uuid.UUID, idempotencyKey string) (domain.ExecutionResult, error) {
	for _, execution := range f.executions {
		if execution.TaskID == nil || *execution.TaskID != taskID {
			continue
		}
		if execution.Operation != operation || execution.IdempotencyKey != idempotencyKey {
			continue
		}
		if reviewRequestID == nil && execution.ReviewRequestID == nil {
			return execution, nil
		}
		if reviewRequestID != nil && execution.ReviewRequestID != nil && *reviewRequestID == *execution.ReviewRequestID {
			return execution, nil
		}
	}
	return domain.ExecutionResult{}, ErrNotFound
}

func (f *fakeConnectorRepo) ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error) {
	var out []domain.ExecutionResult
	for _, execution := range f.executions {
		if execution.ConnectorID == connectorID {
			out = append(out, execution)
		}
	}
	return out, nil
}

type stubChecker struct {
	approved bool
	err      error
	calls    int
}

func (s *stubChecker) AuthorizeExecution(ctx context.Context, reviewRequestID uuid.UUID, orgID string) (bool, error) {
	s.calls++
	return s.approved, s.err
}

func TestUsecases_Execute_resolvesConnectorByKind(t *testing.T) {
	t.Parallel()
	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	reviewRequestID := uuid.New()

	result, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:     connectorID,
		Operation:       "mock.write",
		Payload:         json.RawMessage(`{"message":"hello"}`),
		ReviewRequestID: &reviewRequestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ConnectorID != connectorID {
		t.Fatalf("unexpected connector id %s", result.ConnectorID)
	}
	if len(repo.executions) != 1 {
		t.Fatalf("expected persisted execution, got %d", len(repo.executions))
	}
}

func TestUsecases_Execute_disabledConnector(t *testing.T) {
	t.Parallel()
	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: false,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connectorID,
		Operation:   "mock.echo",
		Payload:     json.RawMessage(`{}`),
	})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestUsecases_CapabilitiesExposeConnectorContractV1(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	caps := uc.Capabilities()
	if len(caps) != 1 {
		t.Fatalf("expected one connector, got %d", len(caps))
	}
	var writeCap domain.Capability
	for _, cap := range caps[0].Capabilities {
		if cap.Operation == "mock.write" {
			writeCap = cap
			break
		}
	}
	if writeCap.Operation == "" {
		t.Fatal("mock.write capability not found")
	}
	if writeCap.Mode != domain.CapabilityModeWrite {
		t.Fatalf("expected write mode, got %q", writeCap.Mode)
	}
	if !writeCap.RequiresReview || !writeCap.SideEffect {
		t.Fatalf("expected requires_review side-effect capability: %+v", writeCap)
	}
	if writeCap.RiskClass == "" {
		t.Fatal("expected risk_class")
	}
	if len(writeCap.InputSchema) == 0 || len(writeCap.EvidenceFields) == 0 {
		t.Fatalf("expected schema and evidence fields: %+v", writeCap)
	}
}

func TestUsecases_Execute_readOnlyDoesNotRequireReview(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	checker := &stubChecker{approved: false}
	uc := NewUsecases(repo, reg, checker)

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connectorID,
		Operation:   "mock.echo",
		Payload:     json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if checker.calls != 0 {
		t.Fatalf("expected read-only execution to skip review checker, got %d calls", checker.calls)
	}
}

func TestUsecases_Execute_sideEffectWithoutReviewDenied(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID: connectorID,
		Operation:   "mock.write",
		Payload:     json.RawMessage(`{"message":"hello"}`),
	})
	if !errors.Is(err, ErrUngated) {
		t.Fatalf("expected ErrUngated, got %v", err)
	}
}

func TestUsecases_Execute_validatesInputSchema(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	reviewRequestID := uuid.New()

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:     connectorID,
		Operation:       "mock.write",
		Payload:         json.RawMessage(`{}`),
		ReviewRequestID: &reviewRequestID,
	})
	if !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("expected ErrInvalidPayload, got %v", err)
	}
}

func TestUsecases_Execute_persistsSanitizedEvidence(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	reviewRequestID := uuid.New()
	taskID := uuid.New()

	result, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:     connectorID,
		OrgID:           "org-a",
		ActorID:         "actor-1",
		Operation:       "mock.write",
		Payload:         json.RawMessage(`{"message":"hello","api_key":"secret"}`),
		IdempotencyKey:  "idem-1",
		TaskID:          &taskID,
		ReviewRequestID: &reviewRequestID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OrgID != "org-a" || result.ActorID != "actor-1" {
		t.Fatalf("expected org/actor on result, got %+v", result)
	}
	if len(repo.executions) != 1 {
		t.Fatalf("expected persisted execution, got %d", len(repo.executions))
	}
	evidence := string(repo.executions[0].EvidenceJSON)
	if !strings.Contains(evidence, `"org_id":"org-a"`) {
		t.Fatalf("expected org evidence, got %s", evidence)
	}
	if strings.Contains(evidence, "secret") || !strings.Contains(evidence, `"api_key":"***"`) {
		t.Fatalf("expected sanitized evidence, got %s", evidence)
	}
}

func TestUsecases_Execute_reusesIdempotentExecution(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	reviewRequestID := uuid.New()
	taskID := uuid.New()
	spec := domain.ExecutionSpec{
		ConnectorID:     connectorID,
		Operation:       "mock.write",
		Payload:         json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey:  "idem-1",
		TaskID:          &taskID,
		ReviewRequestID: &reviewRequestID,
	}

	first, err := uc.Execute(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := uc.Execute(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same idempotent execution, got %s and %s", first.ID, second.ID)
	}
	if len(repo.executions) != 1 {
		t.Fatalf("expected one persisted execution, got %d", len(repo.executions))
	}
}

func TestUsecases_Execute_rejectsConnectorTenantMismatch(t *testing.T) {
	t.Parallel()

	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		OrgID:   "org-a",
		Name:    "Mock Connector",
		Kind:    "mock",
		Enabled: true,
	}
	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	reviewRequestID := uuid.New()

	_, err := uc.Execute(context.Background(), domain.ExecutionSpec{
		ConnectorID:     connectorID,
		OrgID:           "org-b",
		Operation:       "mock.write",
		Payload:         json.RawMessage(`{"message":"hello"}`),
		ReviewRequestID: &reviewRequestID,
	})
	if !IsForbidden(err) {
		t.Fatalf("expected forbidden tenant mismatch, got %v", err)
	}
}
