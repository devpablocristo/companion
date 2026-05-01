package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

type fakeConnectorRepo struct {
	connectors map[uuid.UUID]domain.Connector
	executions []domain.ExecutionResult
	locks      map[string]bool
	mu         sync.Mutex
}

func (f *fakeConnectorRepo) SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.connectors[id]
	if !ok {
		return domain.Connector{}, ErrNotFound
	}
	return c, nil
}

func (f *fakeConnectorRepo) ListConnectors(ctx context.Context) ([]domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.Connector
	for _, c := range f.connectors {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeConnectorRepo) UpdateConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connectors[c.ID] = c
	return c, nil
}

func (f *fakeConnectorRepo) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.connectors, id)
	return nil
}

func (f *fakeConnectorRepo) SaveExecution(ctx context.Context, r domain.ExecutionResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executions = append(f.executions, r)
	return nil
}

func (f *fakeConnectorRepo) AcquireExecutionLock(ctx context.Context, lockKey string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lockKey == "" {
		return true, nil
	}
	if f.locks == nil {
		f.locks = make(map[string]bool)
	}
	if f.locks[lockKey] {
		return false, nil
	}
	f.locks[lockKey] = true
	return true, nil
}

func (f *fakeConnectorRepo) ReleaseExecutionLock(ctx context.Context, lockKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lockKey == "" {
		return nil
	}
	delete(f.locks, lockKey)
	return nil
}

func (f *fakeConnectorRepo) GetExecutionByIdempotency(ctx context.Context, taskID uuid.UUID, operation string, reviewRequestID *uuid.UUID, idempotencyKey string) (domain.ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
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

type blockingConnector struct {
	started chan struct{}
	release chan struct{}

	startOnce sync.Once
	mu        sync.Mutex
	calls     int
}

func (b *blockingConnector) ID() string   { return "blocking" }
func (b *blockingConnector) Kind() string { return "blocking" }

func (b *blockingConnector) Capabilities() []domain.Capability {
	return []domain.Capability{
		{
			Operation:      "blocking.write",
			Mode:           domain.CapabilityModeWrite,
			SideEffect:     true,
			RequiresReview: true,
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"message"},
			},
		},
	}
}

func (b *blockingConnector) Validate(spec domain.ExecutionSpec) error {
	return nil
}

func (b *blockingConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	b.startOnce.Do(func() { close(b.started) })

	select {
	case <-ctx.Done():
		return domain.ExecutionResult{}, ctx.Err()
	case <-b.release:
	}

	resultJSON, err := json.Marshal(map[string]string{"status": "ok"})
	if err != nil {
		return domain.ExecutionResult{}, err
	}
	return domain.ExecutionResult{
		ID:              uuid.New(),
		ConnectorID:     spec.ConnectorID,
		OrgID:           spec.OrgID,
		ActorID:         spec.ActorID,
		Operation:       spec.Operation,
		Status:          domain.ExecSuccess,
		ExternalRef:     "blocking-" + uuid.New().String()[:8],
		Payload:         spec.Payload,
		ResultJSON:      json.RawMessage(resultJSON),
		DurationMS:      1,
		IdempotencyKey:  spec.IdempotencyKey,
		TaskID:          spec.TaskID,
		ReviewRequestID: spec.ReviewRequestID,
		CreatedAt:       time.Now().UTC(),
	}, nil
}

func (b *blockingConnector) CallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
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

	caps := uc.Capabilities(domain.CapabilityFilter{IncludeWrites: true})
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
	if writeCap.ID != "mock.write" || writeCap.Version == "" || writeCap.OwnerDomain != "mock" || writeCap.Product != "mock" {
		t.Fatalf("expected normalized manifest identity fields: %+v", writeCap)
	}
	if writeCap.TenantScope.Mode == "" || writeCap.AuthMode.Type == "" || !writeCap.ApprovalPolicy.Required {
		t.Fatalf("expected tenant/auth/approval defaults: %+v", writeCap)
	}
	if !writeCap.Idempotency.Required || len(writeCap.ErrorContract.TypedErrors) == 0 || !writeCap.Observability.EmitTrace {
		t.Fatalf("expected idempotency, typed errors and observability: %+v", writeCap)
	}
	if len(writeCap.InputSchema) == 0 || len(writeCap.EvidenceFields) == 0 {
		t.Fatalf("expected schema and evidence fields: %+v", writeCap)
	}
}

func TestUsecases_CapabilitiesFiltersWritesByDefault(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	caps := uc.Capabilities(domain.CapabilityFilter{
		Scopes: []string{"companion:connectors:execute"},
	})
	if len(caps) != 1 {
		t.Fatalf("expected one connector, got %d", len(caps))
	}
	if len(caps[0].Capabilities) != 1 {
		t.Fatalf("expected only read-only capability, got %+v", caps[0].Capabilities)
	}
	if caps[0].Capabilities[0].Operation != "mock.echo" {
		t.Fatalf("expected mock.echo, got %s", caps[0].Capabilities[0].Operation)
	}
}

func TestUsecases_CapabilitiesFiltersByPermissionsWhenAuthContextExists(t *testing.T) {
	t.Parallel()

	reg := registry.NewRegistry()
	reg.Register(registry.NewMockConnector())
	uc := NewUsecases(&fakeConnectorRepo{}, reg, &stubChecker{})

	caps := uc.Capabilities(domain.CapabilityFilter{
		IncludeWrites:      true,
		EnforcePermissions: true,
	})
	if len(caps) != 0 {
		t.Fatalf("expected no capabilities without required scopes, got %+v", caps)
	}

	caps = uc.Capabilities(domain.CapabilityFilter{
		Scopes:             []string{"companion:connectors:execute"},
		IncludeWrites:      true,
		EnforcePermissions: true,
	})
	if len(caps) != 1 || len(caps[0].Capabilities) != 2 {
		t.Fatalf("expected scoped discovery to expose mock capabilities, got %+v", caps)
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

func TestUsecases_Execute_conflictsConcurrentIdempotentExecution(t *testing.T) {
	repo := &fakeConnectorRepo{connectors: make(map[uuid.UUID]domain.Connector)}
	connectorID := uuid.New()
	repo.connectors[connectorID] = domain.Connector{
		ID:      connectorID,
		Name:    "Blocking Connector",
		Kind:    "blocking",
		Enabled: true,
	}
	conn := &blockingConnector{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	reg := registry.NewRegistry()
	reg.Register(conn)
	uc := NewUsecases(repo, reg, &stubChecker{approved: true})
	reviewRequestID := uuid.New()
	taskID := uuid.New()
	spec := domain.ExecutionSpec{
		ConnectorID:     connectorID,
		Operation:       "blocking.write",
		Payload:         json.RawMessage(`{"message":"hello"}`),
		IdempotencyKey:  "idem-concurrent",
		TaskID:          &taskID,
		ReviewRequestID: &reviewRequestID,
	}

	type executionResult struct {
		result domain.ExecutionResult
		err    error
	}
	done := make(chan executionResult, 1)
	go func() {
		result, err := uc.Execute(context.Background(), spec)
		done <- executionResult{result: result, err: err}
	}()

	<-conn.started
	_, err := uc.Execute(context.Background(), spec)
	if !IsConflict(err) {
		t.Fatalf("expected concurrent idempotent execution conflict, got %v", err)
	}
	if calls := conn.CallCount(); calls != 1 {
		t.Fatalf("expected only first execution to reach connector, got %d calls", calls)
	}

	close(conn.release)
	first := <-done
	if first.err != nil {
		t.Fatal(first.err)
	}
	again, err := uc.Execute(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.result.ID {
		t.Fatalf("expected stored idempotent result %s, got %s", first.result.ID, again.ID)
	}
	if calls := conn.CallCount(); calls != 1 {
		t.Fatalf("expected replay to skip connector, got %d calls", calls)
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
