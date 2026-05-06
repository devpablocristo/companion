package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/core/governance/go/governanceclient"
	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// makeWaitingForApprovalTask prepara una task en estado WaitingForApproval con
// governance_request_id y un sync state inicial para cubrir el path donde
// ExecuteTask sincroniza con governance antes de evaluar el gate.
func makeWaitingForApprovalTask(t *testing.T, repo *fakeRepo, governanceRequestID uuid.UUID) domain.Task {
	t.Helper()
	ctx := context.Background()
	uc := NewUsecases(repo, &stubGovernance{})
	task, err := uc.Create(ctx, CreateTaskInput{Title: "governance-gate"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForApproval
	task.GovernanceStatus = "pending"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repo.governanceSync[task.ID] = domain.TaskGovernanceSyncState{
		TaskID:           task.ID,
		GovernanceRequestID:  governanceRequestID,
		LastGovernanceStatus: "pending",
		LastCheckedAt:    now,
		NextCheckAt:      now.Add(time.Minute),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	repo.executionPlan[task.ID] = domain.TaskExecutionPlan{
		TaskID:         task.ID,
		ConnectorID:    uuid.New(),
		Operation:      "mock.write",
		Payload:        json.RawMessage(`{"x":1}`),
		IdempotencyKey: "k",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return task
}

func newGateTestRepo() *fakeRepo {
	return &fakeRepo{
		governanceSync:     make(map[uuid.UUID]domain.TaskGovernanceSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
}

// governanceGetter devuelve un governance summary con un status configurable, simulando
// la respuesta de Nexus al sincronizar.
func governanceGetter(status string) func(_ context.Context, id string) (governanceclient.RequestSummary, int, error) {
	return func(_ context.Context, id string) (governanceclient.RequestSummary, int, error) {
		return governanceclient.RequestSummary{
			ID:     id,
			Status: status,
		}, http.StatusOK, nil
	}
}

func TestExecuteTask_BlocksWhenGovernancePending_FlagOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	governanceRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, governanceRequestID)

	uc := NewUsecases(repo, &stubGovernance{getFn: governanceGetter("pending")})
	uc.SetExecutor(&stubExecutor{})
	// governanceGateEnforced = false (default)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error when governance is pending")
	}
	if !IsInvalidTaskState(err) {
		t.Fatalf("expected ErrInvalidTaskState (legacy), got %v", err)
	}
	if IsGovernanceNotApproved(err) {
		t.Fatal("with flag off, error should NOT be tagged as governance_not_approved")
	}
}

func TestExecuteTask_BlocksWhenGovernancePending_FlagOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	governanceRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, governanceRequestID)

	uc := NewUsecases(repo, &stubGovernance{getFn: governanceGetter("pending")})
	uc.SetExecutor(&stubExecutor{})
	uc.SetGovernanceGateEnforced(true)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error when governance is pending")
	}
	if !IsGovernanceNotApproved(err) {
		t.Fatalf("expected typed ErrGovernanceNotApproved when flag on, got %v", err)
	}
	// Backwards compat: IsInvalidTaskState debe seguir devolviendo true porque
	// ErrGovernanceNotApproved envuelve ErrInvalidTaskState.
	if !IsInvalidTaskState(err) {
		t.Fatal("expected legacy IsInvalidTaskState to remain true via wrap")
	}
	blocked, ok := AsGovernanceBlocked(err)
	if !ok {
		t.Fatal("expected AsGovernanceBlocked to extract detail")
	}
	if blocked.GovernanceStatus != "pending" {
		t.Fatalf("expected status=pending, got %q", blocked.GovernanceStatus)
	}
	if blocked.GovernanceRequestID != governanceRequestID.String() {
		t.Fatalf("expected governance_request_id %s, got %q", governanceRequestID, blocked.GovernanceRequestID)
	}
	if blocked.Reason != "execute" {
		t.Fatalf("expected reason=execute, got %q", blocked.Reason)
	}
}

func TestExecuteTask_BlocksWhenGovernanceDenied_FlagOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	governanceRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, governanceRequestID)

	uc := NewUsecases(repo, &stubGovernance{getFn: governanceGetter("denied")})
	uc.SetExecutor(&stubExecutor{})
	uc.SetGovernanceGateEnforced(true)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if !IsGovernanceNotApproved(err) {
		t.Fatalf("expected ErrGovernanceNotApproved for denied governance, got %v", err)
	}
	blocked, _ := AsGovernanceBlocked(err)
	if blocked.GovernanceStatus != "denied" {
		t.Fatalf("expected status=denied, got %q", blocked.GovernanceStatus)
	}
}

func TestExecuteTask_AllowsWhenGovernanceApproved_FlagOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	governanceRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, governanceRequestID)

	executed := false
	uc := NewUsecases(repo, &stubGovernance{getFn: governanceGetter("approved")})
	uc.SetExecutor(&stubExecutor{
		executeFn: func(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error) {
			executed = true
			return connectordomain.ExecutionResult{
				ID:              uuid.New(),
				ConnectorID:     spec.ConnectorID,
				Operation:       spec.Operation,
				Status:          connectordomain.ExecSuccess,
				ExternalRef:     "ref",
				Payload:         spec.Payload,
				ResultJSON:      json.RawMessage(`{"ok":true}`),
				TaskID:          spec.TaskID,
				GovernanceRequestID: spec.GovernanceRequestID,
				CreatedAt:       time.Now().UTC(),
			}, nil
		},
	})
	uc.SetGovernanceGateEnforced(true)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("expected approved execution to succeed, got %v", err)
	}
	if !executed {
		t.Fatal("expected executor to be called when governance is approved")
	}
}

// errorIsBoth verifica que un wrapped error mantenga la cadena correctamente.
func TestErrGovernanceNotApproved_WrapsInvalidTaskState(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrGovernanceNotApproved, ErrInvalidTaskState) {
		t.Fatal("ErrGovernanceNotApproved must wrap ErrInvalidTaskState for backwards compat")
	}
	blocked := &GovernanceBlockedError{GovernanceRequestID: "rid", GovernanceStatus: "pending", Reason: "execute"}
	if !errors.Is(blocked, ErrGovernanceNotApproved) {
		t.Fatal("GovernanceBlockedError must satisfy errors.Is(_, ErrGovernanceNotApproved)")
	}
	if !errors.Is(blocked, ErrInvalidTaskState) {
		t.Fatal("GovernanceBlockedError must transitively satisfy errors.Is(_, ErrInvalidTaskState)")
	}
}
