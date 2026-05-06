package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/core/governance/go/reviewclient"
	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// makeWaitingForApprovalTask prepara una task en estado WaitingForApproval con
// review_request_id y un sync state inicial para cubrir el path donde
// ExecuteTask sincroniza con governance antes de evaluar el gate.
func makeWaitingForApprovalTask(t *testing.T, repo *fakeRepo, reviewRequestID uuid.UUID) domain.Task {
	t.Helper()
	ctx := context.Background()
	uc := NewUsecases(repo, &stubReview{})
	task, err := uc.Create(ctx, CreateTaskInput{Title: "review-gate"})
	if err != nil {
		t.Fatal(err)
	}
	task.Status = domain.TaskStatusWaitingForApproval
	task.ReviewStatus = "pending"
	task, err = repo.UpdateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	repo.reviewSync[task.ID] = domain.TaskReviewSyncState{
		TaskID:           task.ID,
		ReviewRequestID:  reviewRequestID,
		LastReviewStatus: "pending",
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
		reviewSync:     make(map[uuid.UUID]domain.TaskReviewSyncState),
		executionPlan:  make(map[uuid.UUID]domain.TaskExecutionPlan),
		executionState: make(map[uuid.UUID]domain.TaskExecutionState),
	}
}

// reviewGetter devuelve un review summary con un status configurable, simulando
// la respuesta de Nexus al sincronizar.
func reviewGetter(status string) func(_ context.Context, id string) (reviewclient.RequestSummary, int, error) {
	return func(_ context.Context, id string) (reviewclient.RequestSummary, int, error) {
		return reviewclient.RequestSummary{
			ID:     id,
			Status: status,
		}, http.StatusOK, nil
	}
}

func TestExecuteTask_BlocksWhenReviewPending_FlagOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	reviewRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, reviewRequestID)

	uc := NewUsecases(repo, &stubReview{getFn: reviewGetter("pending")})
	uc.SetExecutor(&stubExecutor{})
	// reviewGateEnforced = false (default)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error when review is pending")
	}
	if !IsInvalidTaskState(err) {
		t.Fatalf("expected ErrInvalidTaskState (legacy), got %v", err)
	}
	if IsReviewNotApproved(err) {
		t.Fatal("with flag off, error should NOT be tagged as review_not_approved")
	}
}

func TestExecuteTask_BlocksWhenReviewPending_FlagOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	reviewRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, reviewRequestID)

	uc := NewUsecases(repo, &stubReview{getFn: reviewGetter("pending")})
	uc.SetExecutor(&stubExecutor{})
	uc.SetReviewGateEnforced(true)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err == nil {
		t.Fatal("expected error when review is pending")
	}
	if !IsReviewNotApproved(err) {
		t.Fatalf("expected typed ErrReviewNotApproved when flag on, got %v", err)
	}
	// Backwards compat: IsInvalidTaskState debe seguir devolviendo true porque
	// ErrReviewNotApproved envuelve ErrInvalidTaskState.
	if !IsInvalidTaskState(err) {
		t.Fatal("expected legacy IsInvalidTaskState to remain true via wrap")
	}
	blocked, ok := AsReviewBlocked(err)
	if !ok {
		t.Fatal("expected AsReviewBlocked to extract detail")
	}
	if blocked.ReviewStatus != "pending" {
		t.Fatalf("expected status=pending, got %q", blocked.ReviewStatus)
	}
	if blocked.ReviewRequestID != reviewRequestID.String() {
		t.Fatalf("expected review_request_id %s, got %q", reviewRequestID, blocked.ReviewRequestID)
	}
	if blocked.Reason != "execute" {
		t.Fatalf("expected reason=execute, got %q", blocked.Reason)
	}
}

func TestExecuteTask_BlocksWhenReviewDenied_FlagOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	reviewRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, reviewRequestID)

	uc := NewUsecases(repo, &stubReview{getFn: reviewGetter("denied")})
	uc.SetExecutor(&stubExecutor{})
	uc.SetReviewGateEnforced(true)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if !IsReviewNotApproved(err) {
		t.Fatalf("expected ErrReviewNotApproved for denied review, got %v", err)
	}
	blocked, _ := AsReviewBlocked(err)
	if blocked.ReviewStatus != "denied" {
		t.Fatalf("expected status=denied, got %q", blocked.ReviewStatus)
	}
}

func TestExecuteTask_AllowsWhenReviewApproved_FlagOn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	repo := newGateTestRepo()
	reviewRequestID := uuid.New()
	task := makeWaitingForApprovalTask(t, repo, reviewRequestID)

	executed := false
	uc := NewUsecases(repo, &stubReview{getFn: reviewGetter("approved")})
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
				ReviewRequestID: spec.ReviewRequestID,
				CreatedAt:       time.Now().UTC(),
			}, nil
		},
	})
	uc.SetReviewGateEnforced(true)

	_, err := uc.ExecuteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("expected approved execution to succeed, got %v", err)
	}
	if !executed {
		t.Fatal("expected executor to be called when review is approved")
	}
}

// errorIsBoth verifica que un wrapped error mantenga la cadena correctamente.
func TestErrReviewNotApproved_WrapsInvalidTaskState(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrReviewNotApproved, ErrInvalidTaskState) {
		t.Fatal("ErrReviewNotApproved must wrap ErrInvalidTaskState for backwards compat")
	}
	blocked := &ReviewBlockedError{ReviewRequestID: "rid", ReviewStatus: "pending", Reason: "execute"}
	if !errors.Is(blocked, ErrReviewNotApproved) {
		t.Fatal("ReviewBlockedError must satisfy errors.Is(_, ErrReviewNotApproved)")
	}
	if !errors.Is(blocked, ErrInvalidTaskState) {
		t.Fatal("ReviewBlockedError must transitively satisfy errors.Is(_, ErrInvalidTaskState)")
	}
}
