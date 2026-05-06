package governance_assist

import (
	"context"
	"fmt"

	coreai "github.com/devpablocristo/core/ai/go"
	"github.com/devpablocristo/core/governance/go/governanceclient"
)

// Contextualizer arma summaries en lenguaje natural para approvers humanos.
// Lee el request en Nexus, lo pasa por LLM, devuelve summary breve.
//
// La console de Nexus invoca este endpoint como secondary call al renderizar
// una approval card. Si Companion no responde (timeout / down), la console
// muestra el request sin summary — Companion no es dependencia hard de Nexus.
type Contextualizer struct {
	governance *governanceclient.Client
	llm        coreai.Provider // puede ser nil → fallback determinístico
}

// NewContextualizer crea un Contextualizer. Si llm es nil, devuelve siempre
// summary fallback (degraded=true) sin llamar a un LLM.
func NewContextualizer(governance *governanceclient.Client, llm coreai.Provider) *Contextualizer {
	return &Contextualizer{governance: governance, llm: llm}
}

// Explain devuelve un summary natural-language para el request_id dado.
// Degraded=true indica que se devolvió el fallback (no hay LLM o falló).
func (c *Contextualizer) Explain(ctx context.Context, requestID string) (summary string, degraded bool, err error) {
	if requestID == "" {
		return "", true, fmt.Errorf("request_id is required")
	}
	req, st, err := c.governance.GetRequest(ctx, requestID)
	if err != nil {
		return "", true, fmt.Errorf("get request: %w", err)
	}
	if st == 404 {
		return "", true, fmt.Errorf("request not found")
	}
	fallback := buildFallbackSummary(req)

	if c.llm == nil {
		return fallback, true, nil
	}
	resp, err := c.llm.Chat(ctx, coreai.ChatRequest{
		SystemPrompt: contextualizerSystemPrompt,
		Messages:     []coreai.Message{{Role: "user", Content: buildUserMessage(req)}},
		MaxTokens:    300,
	})
	if err != nil || resp.Text == "" {
		return fallback, true, nil
	}
	return resp.Text, false, nil
}

func buildFallbackSummary(r governanceclient.RequestSummary) string {
	return fmt.Sprintf(
		"Resumen no disponible (modo fallback). %s pide %s sobre %s. Motivo: %s. Risk: %s.",
		r.RequesterID, r.ActionType, r.TargetResource, r.Reason, r.RiskLevel,
	)
}

func buildUserMessage(r governanceclient.RequestSummary) string {
	return fmt.Sprintf(
		"Requester: %s (%s)\nAcción: %s\nTarget: %s / %s\nMotivo: %s\nRisk: %s\nDecisión: %s (%s)",
		r.RequesterID, r.RequesterType,
		r.ActionType,
		r.TargetSystem, r.TargetResource,
		r.Reason,
		r.RiskLevel,
		r.Decision, r.DecisionReason,
	)
}

const contextualizerSystemPrompt = `Sos un asistente que ayuda a aprobadores humanos a decidir rápido sobre requests de governance.

Formato:
- Quién pide y qué pide (1 línea)
- Por qué se frenó (risk level + razón)
- Recomendación breve

Máximo 4 líneas. Español. Sin formato markdown.`
