package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const maxToolRounds = 5

// Orchestrator coordina LLM + tools + context para producir la respuesta del compañero.
type Orchestrator struct {
	provider         LLMProvider
	toolkit          *ToolKit
	ports            ContextPorts
	traces           TraceRepository // opcional; nil = no persiste (uso en tests)
	defaultAutonomy  AutonomyLevel   // "" → A2 (default conservador)
}

// NewOrchestrator crea el orquestador del runtime.
func NewOrchestrator(provider LLMProvider, toolkit *ToolKit, ports ContextPorts) *Orchestrator {
	return &Orchestrator{
		provider: provider,
		toolkit:  toolkit,
		ports:    ports,
	}
}

// SetTraceRepository inyecta el repositorio de persistencia de traces. Opcional.
func (o *Orchestrator) SetTraceRepository(repo TraceRepository) {
	o.traces = repo
}

// SetDefaultAutonomy fija el nivel de autonomía por defecto del runtime.
// "" se trata como A2. Niveles fuera de A0..A5 se ignoran (queda A2).
func (o *Orchestrator) SetDefaultAutonomy(level AutonomyLevel) {
	o.defaultAutonomy = level
}

// RunInput entrada para ejecutar el orquestador.
type RunInput struct {
	UserID         string
	OrgID          string
	ProductSurface string
	Message        string
	Messages       []taskdomain.TaskMessage // hilo completo hasta ahora
	TaskID         *uuid.UUID               // opcional: vincula el trace a una task
}

// RunResult resultado del orquestador.
type RunResult struct {
	Reply string
	Trace RunTrace
}

// Run ejecuta el loop principal: context → LLM → tools → LLM → respuesta.
func (o *Orchestrator) Run(ctx context.Context, in RunInput) (RunResult, error) {
	productSurface := in.ProductSurface
	if productSurface == "" {
		productSurface = DefaultProductSurface
	}
	route := RouteAgent(in.Message, productSurface, o.toolkit, o.defaultAutonomy)
	trace := RunTrace{
		RunID:          uuid.NewString(),
		IdentityChain:  BuildIdentityChain(in.UserID, in.OrgID, productSurface),
		Intent:         route.Intent,
		ProductSurface: route.Product,
		AutonomyLevel:  route.Autonomy,
		StartedAt:      time.Now().UTC(),
	}
	if event := CheckPromptInjection(in.Message); event != nil {
		trace.GuardrailEvents = append(trace.GuardrailEvents, *event)
		trace.CompletedAt = time.Now().UTC()
		slog.Warn("runtime_guardrail_rejected", "run_id", trace.RunID, "type", event.Type, "reason", event.Reason)
		o.persistTrace(ctx, trace, in, "")
		return RunResult{
			Reply: "No puedo continuar con instrucciones que intentan modificar mis reglas internas. Si necesitás hacer una acción concreta, reformulá el pedido con el objetivo de negocio.",
			Trace: trace,
		}, nil
	}

	// 1. Ensamblar contexto
	assembled := AssembleContext(ctx, o.ports, in.UserID, in.OrgID, in.Messages)

	// 2. Construir mensajes para el LLM
	systemPrompt := SystemPrompt()
	systemPrompt += "\n\nRuntime control plane:\n" + runtimeSummary(trace.IdentityChain, route)
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
			result, fallbackErr := o.fallback(ctx, in)
			trace.CompletedAt = time.Now().UTC()
			result.Trace = trace
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			o.persistTrace(ctx, trace, in, errMsg)
			return result, fallbackErr
		}

		// Si no hay tool calls, tenemos la respuesta final
		if len(resp.ToolCalls) == 0 {
			reply := resp.Text
			if reply == "" {
				reply = "No pude generar una respuesta en este momento."
			}
			trace.CompletedAt = time.Now().UTC()
			o.persistTrace(ctx, trace, in, "")
			return RunResult{Reply: reply, Trace: trace}, nil
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
			toolStart := time.Now()
			if event := ValidateToolPolicy(tc.Name, tc.Args, trace.AutonomyLevel); event != nil {
				slog.Warn("tool_call_guardrail_rejected", "tool", tc.Name, "type", event.Type, "reason", event.Reason)
				trace.GuardrailEvents = append(trace.GuardrailEvents, *event)
				trace.ToolCalls = append(trace.ToolCalls, ToolTrace{
					Name:           tc.Name,
					ToolCallID:     tc.ID,
					Allowed:        false,
					DecisionReason: event.Reason,
					DurationMS:     time.Since(toolStart).Milliseconds(),
				})
				llmMessages = append(llmMessages, LLMMessage{
					Role:       "tool",
					Content:    fmt.Sprintf(`{"error":"tool call rejected: %s"}`, event.Reason),
					ToolCallID: tc.ID,
				})
				continue
			}

			// Inyectar identidad en context para que remember/recall usen IDs reales
			toolCtx, cancel := context.WithTimeout(WithIdentity(ctx, in.UserID, in.OrgID), 15*time.Second)
			result := o.toolkit.ExecuteTool(toolCtx, tc.Name, tc.Args)
			cancel()
			trace.ToolCalls = append(trace.ToolCalls, ToolTrace{
				Name:           tc.Name,
				ToolCallID:     tc.ID,
				Allowed:        true,
				DecisionReason: "allowed_by_runtime_policy",
				DurationMS:     time.Since(toolStart).Milliseconds(),
			})

			llmMessages = append(llmMessages, LLMMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Si llegamos acá, agotamos las rondas
	slog.Warn("orchestrator_max_rounds_reached", "rounds", maxToolRounds)
	result, err := o.fallback(ctx, in)
	result.Trace = trace
	result.Trace.CompletedAt = time.Now().UTC()
	o.persistTrace(ctx, result.Trace, in, "max_tool_rounds_exhausted")
	return result, err
}

// persistTrace guarda el trace si hay repo configurado. Falla en silencio (con log) para
// no bloquear la respuesta al usuario por un problema de persistencia.
func (o *Orchestrator) persistTrace(ctx context.Context, trace RunTrace, in RunInput, errMsg string) {
	if o.traces == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := o.traces.Save(saveCtx, trace, in.OrgID, in.UserID, in.TaskID, errMsg); err != nil {
		slog.Error("run_trace_persist_failed", "run_id", trace.RunID, "error", err)
	}
}

// fallback genera una respuesta determinista sin LLM.
// Sin LLM, Companion pierde riqueza pero no desaparece.
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

