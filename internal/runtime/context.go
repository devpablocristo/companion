package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devpablocristo/core/governance/go/reviewclient"

	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	taskdomain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// --- Identidad en context para tools ---

type identityKey struct{}

// Identity representa el usuario y organización del request actual.
type Identity struct {
	UserID string
	OrgID  string
}

// WithIdentity inyecta identidad en el context.
func WithIdentity(ctx context.Context, userID, orgID string) context.Context {
	return context.WithValue(ctx, identityKey{}, Identity{UserID: userID, OrgID: orgID})
}

// IdentityFromContext extrae la identidad del context.
func IdentityFromContext(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey{}).(Identity)
	return id
}

// ContextPorts interfaces que el context assembler necesita.
type ContextPorts struct {
	ReviewClient *reviewclient.Client
	MemoryFind   func(ctx context.Context, scopeType memdomain.ScopeType, scopeID string, kind memdomain.MemoryKind, limit int) ([]memdomain.MemoryEntry, error)
}

// AssembledContext contexto ensamblado para el LLM.
type AssembledContext struct {
	Summary string
	History []LLMMessage
}

// AssembleContext arma el contexto relevante para una conversación.
func AssembleContext(ctx context.Context, ports ContextPorts, userID, orgID string, messages []taskdomain.TaskMessage) AssembledContext {
	var parts []string

	// 1. Memoria del usuario (preferencias)
	if ports.MemoryFind != nil {
		userMem, err := ports.MemoryFind(ctx, memdomain.ScopeUser, userID, memdomain.MemoryUserPreference, 5)
		if err == nil && len(userMem) > 0 {
			var prefs []string
			for _, m := range userMem {
				if m.ContentText != "" {
					prefs = append(prefs, fmt.Sprintf("- %s: %s", m.Key, m.ContentText))
				}
			}
			if len(prefs) > 0 {
				parts = append(parts, "Preferencias del usuario:\n"+strings.Join(prefs, "\n"))
			}
		}

		// Memoria de la org (hechos del negocio)
		orgMem, err := ports.MemoryFind(ctx, memdomain.ScopeOrg, orgID, memdomain.MemoryPlaybook, 5)
		if err == nil && len(orgMem) > 0 {
			var facts []string
			for _, m := range orgMem {
				if m.ContentText != "" {
					facts = append(facts, "- "+m.ContentText)
				}
			}
			if len(facts) > 0 {
				parts = append(parts, "Hechos del negocio:\n"+strings.Join(facts, "\n"))
			}
		}
	}

	// 2. Aprobaciones pendientes
	if ports.ReviewClient != nil {
		st, raw, err := ports.ReviewClient.ListPendingApprovals(ctx)
		if err == nil && st == 200 && len(raw) > 0 {
			var approvals struct {
				Data []struct {
					ID         string `json:"id"`
					ActionType string `json:"action_type"`
					Reason     string `json:"reason"`
					RiskLevel  string `json:"risk_level"`
				} `json:"data"`
			}
			if jsonErr := json.Unmarshal(raw, &approvals); jsonErr == nil && len(approvals.Data) > 0 {
				var items []string
				for _, a := range approvals.Data {
					short := a.ID
					if len(short) > 8 {
						short = short[:8]
					}
					items = append(items, fmt.Sprintf("- [%s] %s (riesgo: %s, razón: %s)", short, a.ActionType, a.RiskLevel, a.Reason))
				}
				parts = append(parts, fmt.Sprintf("Aprobaciones pendientes (%d):\n%s", len(items), strings.Join(items, "\n")))
			}
		}
	}

	// 3. Historial de mensajes → formato LLM
	var history []LLMMessage
	limit := 20
	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}
	for _, m := range messages[start:] {
		role := "user"
		if m.AuthorType == "system" || m.AuthorType == "assistant" {
			role = "assistant"
		}
		history = append(history, LLMMessage{Role: role, Content: m.Body})
	}

	summary := ""
	if len(parts) > 0 {
		summary = strings.Join(parts, "\n\n")
	}

	return AssembledContext{
		Summary: summary,
		History: history,
	}
}
