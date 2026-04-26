package runtime

import (
	"context"

	"github.com/devpablocristo/companion/internal/tasks"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// OrchestratorAdapter adapta el runtime.Orchestrator a la interfaz tasks.ChatOrchestrator.
type OrchestratorAdapter struct {
	orch *Orchestrator
}

// NewOrchestratorAdapter crea el adapter.
func NewOrchestratorAdapter(orch *Orchestrator) *OrchestratorAdapter {
	return &OrchestratorAdapter{orch: orch}
}

// Run implementa tasks.ChatOrchestrator.
func (a *OrchestratorAdapter) Run(ctx context.Context, in tasks.OrchestratorInput) (tasks.OrchestratorResult, error) {
	result, err := a.orch.Run(ctx, RunInput{
		UserID:   in.UserID,
		OrgID:    in.OrgID,
		Message:  in.Message,
		Messages: convertMessages(in.Messages),
	})
	if err != nil {
		return tasks.OrchestratorResult{}, err
	}
	return tasks.OrchestratorResult{Reply: result.Reply}, nil
}

func convertMessages(msgs []taskdomain.TaskMessage) []taskdomain.TaskMessage {
	// Mismo tipo, solo pasa directo — el adapter existe para desacoplar packages
	return msgs
}
