package tasks

import (
	"testing"

	"github.com/devpablocristo/core/governance/go/governanceclient"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

func TestEventFromSubmitResponse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   string
	}{
		{"allowed", evGovernanceResolvedAllow},
		{" executed ", evGovernanceResolvedAllow},
		{"ALLOWED", evGovernanceResolvedAllow},
		{"denied", evGovernanceResolvedDeny},
		{"pending_approval", evGovernancePendingApproval},
	}
	for _, tc := range cases {
		ev, err := eventFromSubmitResponse(governanceclient.SubmitResponse{Status: tc.status})
		if err != nil || ev != tc.want {
			t.Fatalf("status %q: got %q %v want %q", tc.status, ev, err, tc.want)
		}
	}
	_, err := eventFromSubmitResponse(governanceclient.SubmitResponse{Status: "weird"})
	if err == nil {
		t.Fatal("expected error for unknown status")
	}
}

func TestEventFromGovernanceRequestStatus(t *testing.T) {
	t.Parallel()
	ev, ok := eventFromGovernanceRequestStatus("pending_approval")
	if ok || ev != "" {
		t.Fatalf("pending: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("evaluated")
	if ok || ev != "" {
		t.Fatalf("evaluated: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("approved")
	if !ok || ev != evGovernanceResolvedAllow {
		t.Fatalf("approved: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("rejected")
	if !ok || ev != evGovernanceResolvedDeny {
		t.Fatalf("rejected: got %q %v", ev, ok)
	}
	ev, ok = eventFromGovernanceRequestStatus("expired")
	if !ok || ev != evGovernanceResolvedDeny {
		t.Fatalf("expired: got %q %v", ev, ok)
	}
}

func TestEventFromGovernanceRequestStatusWithExecutionPlan(t *testing.T) {
	t.Parallel()
	ev, ok := eventFromGovernanceRequestStatusWithExecutionPlan("approved", true)
	if !ok || ev != evGovernanceResolvedAllowAwaitInput {
		t.Fatalf("approved with execution plan: got %q %v", ev, ok)
	}
}

func TestCompanionTaskFSM_investigateAndGovernance(t *testing.T) {
	t.Parallel()
	m := companionTaskMachine()
	to, err := m.Transition(domain.TaskStatusNew, evInvestigate)
	if err != nil || to != domain.TaskStatusInvestigating {
		t.Fatalf("investigate: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evInvestigate)
	if err != nil || to != domain.TaskStatusInvestigating {
		t.Fatalf("investigate idempotent: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evGovernancePendingApproval)
	if err != nil || to != domain.TaskStatusWaitingForApproval {
		t.Fatalf("pending: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusInvestigating, evGovernanceResolvedAllow)
	if err != nil || to != domain.TaskStatusDone {
		t.Fatalf("allow from investigating: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusWaitingForApproval, evGovernanceResolvedAllowAwaitInput)
	if err != nil || to != domain.TaskStatusWaitingForInput {
		t.Fatalf("allow awaiting input: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusWaitingForInput, evStartExecution)
	if err != nil || to != domain.TaskStatusExecuting {
		t.Fatalf("start execution: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusExecuting, evExecutionSucceeded)
	if err != nil || to != domain.TaskStatusVerifying {
		t.Fatalf("execution succeeded: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusVerifying, evExecutionVerified)
	if err != nil || to != domain.TaskStatusDone {
		t.Fatalf("execution verified: %q %v", to, err)
	}
	to, err = m.Transition(domain.TaskStatusFailed, evRetryExecution)
	if err != nil || to != domain.TaskStatusExecuting {
		t.Fatalf("retry execution: %q %v", to, err)
	}
}
