package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/devpablocristo/core/governance/go/reviewclient"

	"github.com/devpablocristo/companion/internal/memory"
	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
	"github.com/devpablocristo/companion/internal/watchers"
)

// ToolHandler ejecuta un tool y devuelve resultado como string JSON.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// ToolKit contiene todas las herramientas del compañero.
type ToolKit struct {
	Schemas  []ToolSchema
	Handlers map[string]ToolHandler
}

// NewToolKit crea el kit de tools con las dependencias inyectadas.
func NewToolKit(rc *reviewclient.Client, memUC *memory.Usecases, watcherUC *watchers.Usecases) *ToolKit {
	tk := &ToolKit{
		Handlers: make(map[string]ToolHandler),
	}

	// --- get_overview: resumen de estado ---
	tk.add(ToolSchema{
		Name:        "get_overview",
		Description: "Obtiene un resumen del estado actual: aprobaciones pendientes y alertas activas.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		var parts []string

		// Aprobaciones pendientes
		if rc != nil {
			st, raw, err := rc.ListPendingApprovals(ctx)
			if err == nil && st == 200 {
				parts = append(parts, fmt.Sprintf("Aprobaciones pendientes (raw): %s", string(raw)))
			}
		}

		// Watchers activos
		if watcherUC != nil {
			wList, err := watcherUC.List(ctx, "")
			if err == nil {
				active := 0
				for _, w := range wList {
					if w.Enabled {
						active++
					}
				}
				parts = append(parts, fmt.Sprintf("Watchers activos: %d de %d configurados", active, len(wList)))
			}
		}

		if len(parts) == 0 {
			return `{"status": "sin datos disponibles"}`, nil
		}
		result := map[string]any{"overview": parts}
		b, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal overview result: %w", err)
		}
		return string(b), nil
	})

	// --- check_approvals: listar aprobaciones pendientes ---
	tk.add(ToolSchema{
		Name:        "check_approvals",
		Description: "Lista las aprobaciones pendientes que el usuario puede aprobar o rechazar.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if rc == nil {
			return `{"approvals": [], "message": "review no configurado"}`, nil
		}
		st, raw, err := rc.ListPendingApprovals(ctx)
		if err != nil {
			return "", fmt.Errorf("list approvals: %w", err)
		}
		if st != 200 {
			return fmt.Sprintf(`{"error": "review respondió con status %d"}`, st), nil
		}
		return string(raw), nil
	})

	if approvalToolsEnabled() {
		registerApprovalTools(tk, rc)
	}

	// --- list_policies ---
	tk.add(ToolSchema{
		Name:        "list_policies",
		Description: "Lista las reglas de gobernanza activas.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if rc == nil {
			return `{"policies": []}`, nil
		}
		st, raw, err := rc.ListPolicies(ctx)
		if err != nil {
			return "", fmt.Errorf("list policies: %w", err)
		}
		if st != 200 {
			return fmt.Sprintf(`{"error": "status %d"}`, st), nil
		}
		return string(raw), nil
	})

	// --- list_watchers ---
	tk.add(ToolSchema{
		Name:        "list_watchers",
		Description: "Lista las alertas automáticas configuradas (watchers).",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ json.RawMessage) (string, error) {
		if watcherUC == nil {
			return `{"watchers": []}`, nil
		}
		wList, err := watcherUC.List(ctx, "")
		if err != nil {
			return "", fmt.Errorf("list watchers: %w", err)
		}
		b, err := json.Marshal(map[string]any{"watchers": wList})
		if err != nil {
			return "", fmt.Errorf("marshal watchers: %w", err)
		}
		return string(b), nil
	})

	// --- remember ---
	tk.add(ToolSchema{
		Name:        "remember",
		Description: "Guarda un hecho o preferencia para recordar en el futuro.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":     map[string]any{"type": "string", "description": "Clave identificadora (ej: preferred_contact, business_hours)"},
				"content": map[string]any{"type": "string", "description": "Contenido a recordar"},
				"scope":   map[string]any{"type": "string", "description": "user o org", "enum": []string{"user", "org"}},
			},
			"required": []string{"key", "content"},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			Key     string `json:"key"`
			Content string `json:"content"`
			Scope   string `json:"scope"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if memUC == nil {
			return `{"error": "memory no configurado"}`, nil
		}
		id := IdentityFromContext(ctx)
		scope := memdomain.ScopeUser
		scopeID := id.UserID
		kind := memdomain.MemoryUserPreference
		if input.Scope == "org" {
			scope = memdomain.ScopeOrg
			scopeID = id.OrgID
			kind = memdomain.MemoryPlaybook
		}
		if scopeID == "" {
			scopeID = "default"
		}
		_, err := memUC.Upsert(ctx, memory.UpsertInput{
			Kind:        kind,
			ScopeType:   scope,
			ScopeID:     scopeID,
			Key:         input.Key,
			ContentText: input.Content,
		})
		if err != nil {
			return "", fmt.Errorf("remember: %w", err)
		}
		return `{"result": "guardado"}`, nil
	})

	// --- recall ---
	tk.add(ToolSchema{
		Name:        "recall",
		Description: "Busca en la memoria hechos o preferencias guardados previamente.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scope": map[string]any{"type": "string", "description": "user o org", "enum": []string{"user", "org"}},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			Scope string `json:"scope"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			input.Scope = "user"
		}
		if memUC == nil {
			return `{"memories": []}`, nil
		}
		id := IdentityFromContext(ctx)
		scope := memdomain.ScopeUser
		scopeID := id.UserID
		kind := memdomain.MemoryUserPreference
		if input.Scope == "org" {
			scope = memdomain.ScopeOrg
			scopeID = id.OrgID
			kind = memdomain.MemoryPlaybook
		}
		if scopeID == "" {
			scopeID = "default"
		}
		entries, err := memUC.Find(ctx, memory.FindQuery{
			ScopeType: scope,
			ScopeID:   scopeID,
			Kind:      kind,
			Limit:     10,
		})
		if err != nil {
			return "", fmt.Errorf("recall: %w", err)
		}
		type item struct {
			Key     string `json:"key"`
			Content string `json:"content"`
		}
		var items []item
		for _, e := range entries {
			items = append(items, item{Key: e.Key, Content: e.ContentText})
		}
		b, err := json.Marshal(map[string]any{"memories": items})
		if err != nil {
			return "", fmt.Errorf("marshal memories: %w", err)
		}
		return string(b), nil
	})

	return tk
}

func approvalToolsEnabled() bool {
	return strings.EqualFold(os.Getenv("NEXUS_COMPANION_ENABLE_APPROVAL_TOOLS"), "true")
}

func registerApprovalTools(tk *ToolKit, rc *reviewclient.Client) {
	// --- approve_action ---
	tk.add(ToolSchema{
		Name:        "approve_action",
		Description: "Aprueba una solicitud pendiente por su ID.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"approval_id": map[string]any{"type": "string", "description": "ID de la aprobación"},
				"note":        map[string]any{"type": "string", "description": "Nota opcional"},
			},
			"required": []string{"approval_id"},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			ApprovalID string `json:"approval_id"`
			Note       string `json:"note"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if rc == nil {
			return `{"error": "review no configurado"}`, nil
		}
		body := map[string]string{"decided_by": "nexus-companion", "note": input.Note}
		st, raw, err := rc.Approve(ctx, input.ApprovalID, body)
		if err != nil {
			return "", fmt.Errorf("approve: %w", err)
		}
		if st >= 400 {
			return fmt.Sprintf(`{"error": "approve falló", "status": %d, "detail": %q}`, st, reviewclient.ParseErrorBody(raw)), nil
		}
		return `{"result": "aprobado"}`, nil
	})

	// --- reject_action ---
	tk.add(ToolSchema{
		Name:        "reject_action",
		Description: "Rechaza una solicitud pendiente por su ID.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"approval_id": map[string]any{"type": "string", "description": "ID de la aprobación"},
				"note":        map[string]any{"type": "string", "description": "Nota opcional"},
			},
			"required": []string{"approval_id"},
		},
	}, func(ctx context.Context, args json.RawMessage) (string, error) {
		var input struct {
			ApprovalID string `json:"approval_id"`
			Note       string `json:"note"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
		if rc == nil {
			return `{"error": "review no configurado"}`, nil
		}
		body := map[string]string{"decided_by": "nexus-companion", "note": input.Note}
		st, raw, err := rc.Reject(ctx, input.ApprovalID, body)
		if err != nil {
			return "", fmt.Errorf("reject: %w", err)
		}
		if st >= 400 {
			return fmt.Sprintf(`{"error": "reject falló", "status": %d, "detail": %q}`, st, reviewclient.ParseErrorBody(raw)), nil
		}
		return `{"result": "rechazado"}`, nil
	})
}

func (tk *ToolKit) add(schema ToolSchema, handler ToolHandler) {
	tk.Schemas = append(tk.Schemas, schema)
	tk.Handlers[schema.Name] = handler
}

// ExecuteTool ejecuta un tool por nombre. Regla dura: loguea pero nunca expone errores internos.
func (tk *ToolKit) ExecuteTool(ctx context.Context, name string, args json.RawMessage) string {
	handler, ok := tk.Handlers[name]
	if !ok {
		return fmt.Sprintf(`{"error": "tool %q no reconocido"}`, name)
	}
	result, err := handler(ctx, args)
	if err != nil {
		slog.Error("tool_execution_failed", "tool", name, "error", err)
		// Regla dura: no exponer error interno al LLM
		return fmt.Sprintf(`{"error": "no se pudo ejecutar %s en este momento"}`, name)
	}
	return result
}
