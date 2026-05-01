package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	CompanionPrincipal    = "companion.employee_ai"
	DefaultProductSurface = "companion"
)

type AutonomyLevel string

const (
	AutonomyA0 AutonomyLevel = "A0"
	AutonomyA1 AutonomyLevel = "A1"
	AutonomyA2 AutonomyLevel = "A2"
	AutonomyA3 AutonomyLevel = "A3"
	AutonomyA4 AutonomyLevel = "A4"
	AutonomyA5 AutonomyLevel = "A5"
)

type IdentityChain struct {
	InitiatingUser      string `json:"initiating_user,omitempty"`
	Tenant              string `json:"tenant,omitempty"`
	ProductSurface      string `json:"product_surface,omitempty"`
	CompanionPrincipal  string `json:"companion_principal"`
	CapabilityPrincipal string `json:"capability_principal,omitempty"`
	ApprovalActor       string `json:"approval_actor,omitempty"`
}

type RunTrace struct {
	RunID           string           `json:"run_id"`
	IdentityChain   IdentityChain    `json:"identity_chain"`
	Intent          string           `json:"intent"`
	ProductSurface  string           `json:"product_surface"`
	AutonomyLevel   AutonomyLevel    `json:"autonomy_level"`
	GuardrailEvents []GuardrailEvent `json:"guardrail_events,omitempty"`
	ToolCalls       []ToolTrace      `json:"tool_calls,omitempty"`
	StartedAt       time.Time        `json:"started_at"`
	CompletedAt     time.Time        `json:"completed_at,omitempty"`
}

type ToolTrace struct {
	Name           string `json:"name"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	Allowed        bool   `json:"allowed"`
	DecisionReason string `json:"decision_reason,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	Error          string `json:"error,omitempty"`
}

type GuardrailEvent struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason"`
}

type AgentRoute struct {
	Intent       string        `json:"intent"`
	Product      string        `json:"product"`
	Autonomy     AutonomyLevel `json:"autonomy"`
	AllowedTools []string      `json:"allowed_tools"`
}

func BuildIdentityChain(userID, orgID, productSurface string) IdentityChain {
	productSurface = strings.TrimSpace(productSurface)
	if productSurface == "" {
		productSurface = DefaultProductSurface
	}
	return IdentityChain{
		InitiatingUser:     strings.TrimSpace(userID),
		Tenant:             strings.TrimSpace(orgID),
		ProductSurface:     productSurface,
		CompanionPrincipal: CompanionPrincipal,
	}
}

func RouteAgent(message, productSurface string, toolkit *ToolKit) AgentRoute {
	intent := classifyIntent(message)
	autonomy := AutonomyA2
	var tools []string
	if toolkit != nil {
		tools = make([]string, 0, len(toolkit.Schemas))
		for _, schema := range toolkit.Schemas {
			name := strings.TrimSpace(schema.Name)
			if name == "" {
				continue
			}
			tools = append(tools, name)
		}
	}
	if strings.TrimSpace(productSurface) == "" {
		productSurface = DefaultProductSurface
	}
	return AgentRoute{
		Intent:       intent,
		Product:      productSurface,
		Autonomy:     autonomy,
		AllowedTools: tools,
	}
}

func classifyIntent(message string) string {
	text := strings.ToLower(message)
	switch {
	case strings.Contains(text, "aprobar"), strings.Contains(text, "rechazar"), strings.Contains(text, "approval"):
		return "governance.review"
	case strings.Contains(text, "record"), strings.Contains(text, "memor"):
		return "memory"
	case strings.Contains(text, "alerta"), strings.Contains(text, "watcher"):
		return "operations.watchers"
	case strings.Contains(text, "política"), strings.Contains(text, "policy"):
		return "governance.policy"
	default:
		return "general.assist"
	}
}

func CheckPromptInjection(input string) *GuardrailEvent {
	normalized := strings.ToLower(input)
	suspicious := []string{
		"ignore previous instructions",
		"ignora las instrucciones anteriores",
		"olvida tus instrucciones",
		"reveal system prompt",
		"muestra el prompt",
		"exfiltrate",
	}
	for _, token := range suspicious {
		if strings.Contains(normalized, token) {
			return &GuardrailEvent{Type: "prompt_injection", Target: "message", Reason: "input contains instruction override pattern"}
		}
	}
	return nil
}

func ValidateToolPolicy(toolName string, args json.RawMessage, autonomy AutonomyLevel) *GuardrailEvent {
	if event := CheckPromptInjection(string(args)); event != nil {
		event.Target = "tool_args:" + toolName
		return event
	}
	if isApprovalTool(toolName) && autonomyRank(autonomy) < autonomyRank(AutonomyA4) {
		return &GuardrailEvent{Type: "excessive_agency", Target: toolName, Reason: "approval tools require explicit higher autonomy and human approval context"}
	}
	return nil
}

func isApprovalTool(toolName string) bool {
	return toolName == "approve_action" || toolName == "reject_action"
}

func autonomyRank(level AutonomyLevel) int {
	switch level {
	case AutonomyA0:
		return 0
	case AutonomyA1:
		return 1
	case AutonomyA2, "":
		return 2
	case AutonomyA3:
		return 3
	case AutonomyA4:
		return 4
	case AutonomyA5:
		return 5
	default:
		return 2
	}
}

func runtimeSummary(identity IdentityChain, route AgentRoute) string {
	return fmt.Sprintf(`- Identidad: %s.
- Tenant: %s.
- Usuario iniciador: %s.
- Superficie: %s.
- Intención clasificada: %s.
- Autonomía máxima efectiva: %s.
- Regla dura: podés decidir, recomendar y proponer; no ejecutes writes sensibles ni approvals como acción autónoma.
- Toda tool debe respetar tenant, permisos, trazas y guardrails.`,
		identity.CompanionPrincipal,
		emptyAsUnknown(identity.Tenant),
		emptyAsUnknown(identity.InitiatingUser),
		route.Product,
		route.Intent,
		route.Autonomy,
	)
}

func emptyAsUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
