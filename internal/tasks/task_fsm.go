package tasks

import (
	"fmt"
	"strings"
	"sync"

	"github.com/devpablocristo/core/concurrency/go/fsm"

	"github.com/devpablocristo/core/governance/go/reviewclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// Eventos de transición de tarea (valores opacos para la FSM).
const (
	evInvestigate                   = "investigate"
	evReviewPendingApproval         = "review_pending_approval"
	evReviewResolvedAllow           = "review_resolved_allow"
	evReviewResolvedAllowAwaitInput = "review_resolved_allow_await_input"
	evReviewResolvedDeny            = "review_resolved_deny"
	evStartExecution                = "start_execution"
	evRetryExecution                = "retry_execution"
	evExecutionSucceeded            = "execution_succeeded"
	evExecutionVerified             = "execution_verified"
	evExecutionFailed               = "execution_failed"
)

func normalizeReviewStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

var companionTaskMachine = sync.OnceValue(buildCompanionTaskFSM)

func buildCompanionTaskFSM() *fsm.Machine[string, string] {
	return fsm.New([]fsm.Rule[string, string]{
		{From: domain.TaskStatusNew, Event: evInvestigate, To: domain.TaskStatusInvestigating},
		{From: domain.TaskStatusInvestigating, Event: evInvestigate, To: domain.TaskStatusInvestigating},

		{From: domain.TaskStatusNew, Event: evReviewPendingApproval, To: domain.TaskStatusWaitingForApproval},
		{From: domain.TaskStatusInvestigating, Event: evReviewPendingApproval, To: domain.TaskStatusWaitingForApproval},

		{From: domain.TaskStatusNew, Event: evReviewResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusInvestigating, Event: evReviewResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusWaitingForApproval, Event: evReviewResolvedAllow, To: domain.TaskStatusDone},
		{From: domain.TaskStatusNew, Event: evReviewResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},
		{From: domain.TaskStatusInvestigating, Event: evReviewResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},
		{From: domain.TaskStatusWaitingForApproval, Event: evReviewResolvedAllowAwaitInput, To: domain.TaskStatusWaitingForInput},

		{From: domain.TaskStatusNew, Event: evReviewResolvedDeny, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusInvestigating, Event: evReviewResolvedDeny, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusWaitingForApproval, Event: evReviewResolvedDeny, To: domain.TaskStatusFailed},

		{From: domain.TaskStatusWaitingForInput, Event: evStartExecution, To: domain.TaskStatusExecuting},
		{From: domain.TaskStatusFailed, Event: evRetryExecution, To: domain.TaskStatusExecuting},
		{From: domain.TaskStatusExecuting, Event: evExecutionSucceeded, To: domain.TaskStatusVerifying},
		{From: domain.TaskStatusVerifying, Event: evExecutionVerified, To: domain.TaskStatusDone},
		{From: domain.TaskStatusExecuting, Event: evExecutionFailed, To: domain.TaskStatusFailed},
		{From: domain.TaskStatusVerifying, Event: evExecutionFailed, To: domain.TaskStatusFailed},
	})
}

func eventFromSubmitResponse(sub reviewclient.SubmitResponse) (string, error) {
	return eventFromSubmitResponseWithExecutionPlan(sub, false)
}

func eventFromSubmitResponseWithExecutionPlan(sub reviewclient.SubmitResponse, hasExecutionPlan bool) (string, error) {
	s := normalizeReviewStatus(sub.Status)
	switch s {
	case "allowed", "approved", "executed":
		if hasExecutionPlan {
			return evReviewResolvedAllowAwaitInput, nil
		}
		return evReviewResolvedAllow, nil
	case "denied", "rejected":
		return evReviewResolvedDeny, nil
	case "pending_approval":
		return evReviewPendingApproval, nil
	default:
		return "", fmt.Errorf("unexpected review status after submit: %q", sub.Status)
	}
}

// eventFromReviewRequestStatus mapea estado HTTP de Review a evento FSM; apply=false = sin cambio.
func eventFromReviewRequestStatus(status string) (event string, apply bool) {
	return eventFromReviewRequestStatusWithExecutionPlan(status, false)
}

func eventFromReviewRequestStatusWithExecutionPlan(status string, hasExecutionPlan bool) (event string, apply bool) {
	s := normalizeReviewStatus(status)
	switch s {
	case "pending_approval", "pending", "evaluated":
		return "", false
	case "allowed", "approved", "executed":
		if hasExecutionPlan {
			return evReviewResolvedAllowAwaitInput, true
		}
		return evReviewResolvedAllow, true
	case "denied", "rejected", "expired", "failed", "cancelled":
		return evReviewResolvedDeny, true
	default:
		return "", false
	}
}
