package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const maxToolRounds = 5

// Orchestrator coordina LLM + tools + context para producir la respuesta del compañero.
type Orchestrator struct {
	provider LLMProvider
	toolkit  *ToolKit
	ports    ContextPorts
}

// NewOrchestrator crea el orquestador del runtime.
func NewOrchestrator(provider LLMProvider, toolkit *ToolKit, ports ContextPorts) *Orchestrator {
	return &Orchestrator{
		provider: provider,
		toolkit:  toolkit,
		ports:    ports,
	}
}

// RunInput entrada para ejecutar el orquestador.
type RunInput struct {
	UserID   string
	OrgID    string
	Message  string
	Messages []taskdomain.TaskMessage // hilo completo hasta ahora
}

// RunResult resultado del orquestador.
type RunResult struct {
	Reply string
}

// Run ejecuta el loop principal: context → LLM → tools → LLM → respuesta.
func (o *Orchestrator) Run(ctx context.Context, in RunInput) (RunResult, error) {
	// 1. Ensamblar contexto
	assembled := AssembleContext(ctx, o.ports, in.UserID, in.OrgID, in.Messages)

	// 2. Construir mensajes para el LLM
	systemPrompt := SystemPrompt()
	if assembled.Summary != "" {
		systemPrompt += "\n\nContexto actual:\n" + assembled.Summary
	}

	llmMessages := make([]LLMMessage, 0, len(assembled.History)+1)
	llmMessages = append(llmMessages, assembled.History...)
	llmMessages = append(llmMessages, LLMMessage{Role: "user", Content: in.Message})

	// 3. Loop de tool calling (máximo maxToolRounds rondas)
	for round := 0; round < maxToolRounds; round++ {
		resp, err := o.provider.Chat(ctx, ChatRequest{
			SystemPrompt: systemPrompt,
			Messages:     llmMessages,
			Tools:        o.toolkit.Schemas,
			MaxTokens:    1024,
		})
		if err != nil {
			slog.Error("llm_chat_failed", "round", round, "error", err)
			// Fallback determinista: intentar con echo provider
			return o.fallback(ctx, in)
		}

		// Si no hay tool calls, tenemos la respuesta final
		if len(resp.ToolCalls) == 0 {
			reply := resp.Text
			if reply == "" {
				reply = "No pude generar una respuesta en este momento."
			}
			return RunResult{Reply: reply}, nil
		}

		// Hay tool calls: ejecutar y agregar resultados
		// Agregar mensaje del asistente con tool calls
		llmMessages = append(llmMessages, LLMMessage{
			Role:      "assistant",
			Content:   resp.Text,
			ToolCalls: resp.ToolCalls,
		})

		// Ejecutar cada tool y agregar resultado
		for _, tc := range resp.ToolCalls {
			slog.Info("tool_call", "tool", tc.Name, "round", round)
			if err := ValidateToolCallSafety(tc.Name, tc.Args); err != nil {
				slog.Warn("tool_call_rejected", "tool", tc.Name, "error", err)
				llmMessages = append(llmMessages, LLMMessage{
					Role:       "tool",
					Content:    fmt.Sprintf(`{"error":"tool call rejected: %s"}`, err.Error()),
					ToolCallID: tc.ID,
				})
				continue
			}

			// Inyectar identidad en context para que remember/recall usen IDs reales
			toolCtx := WithIdentity(ctx, in.UserID, in.OrgID)
			result := o.toolkit.ExecuteTool(toolCtx, tc.Name, tc.Args)

			llmMessages = append(llmMessages, LLMMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Si llegamos acá, agotamos las rondas
	slog.Warn("orchestrator_max_rounds_reached", "rounds", maxToolRounds)
	return o.fallback(ctx, in)
}

// fallback genera una respuesta determinista sin LLM.
// GPT señaló este punto: sin LLM, Nexus pierde riqueza pero no desaparece.
func (o *Orchestrator) fallback(ctx context.Context, in RunInput) (RunResult, error) {
	assembled := AssembleContext(ctx, o.ports, in.UserID, in.OrgID, in.Messages)

	reply := "Estoy con capacidad limitada en este momento."
	if assembled.Summary != "" {
		reply += "\n\nEsto es lo que sé ahora:\n" + assembled.Summary
	}
	reply += "\n\nPodés aprobar o rechazar desde el inbox, o preguntarme de nuevo en un momento."

	return RunResult{Reply: reply}, nil
}

// FallbackReply genera respuesta determinista directamente (para cuando el provider no está configurado).
func FallbackReply(overview string) string {
	if overview == "" {
		return "Estoy disponible. ¿En qué te puedo ayudar?"
	}
	return fmt.Sprintf("Estado actual:\n%s\n\n¿Qué necesitás?", overview)
}

// ValidateToolCallSafety regla dura en código: validaciones que no dependen del prompt.
// Retorna error si la tool call viola una regla no negociable.
func ValidateToolCallSafety(toolName string, args json.RawMessage) error {
	switch toolName {
	case "approve_action", "reject_action":
		// Regla dura: debe tener approval_id
		var input struct {
			ApprovalID string `json:"approval_id"`
		}
		if err := json.Unmarshal(args, &input); err != nil || input.ApprovalID == "" {
			return fmt.Errorf("approval_id requerido para %s", toolName)
		}
	}
	return nil
}
