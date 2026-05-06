package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	ai "github.com/devpablocristo/core/ai/go"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// PontiConnector adapter de Companion a Ponti (read-only piloto).
//
// La fuente de verdad del catálogo de capabilities es el manifest canónico
// publicado por Ponti (`ai.CapabilityManifest`). Acá se mantiene una copia
// hardcodeada del manifest mientras el discovery HTTP por request queda como
// fase 2 (decisión D.2 del plan). El test del paquete corre
// `ai.ValidateCapabilityManifest` para evitar drift.
type PontiConnector struct {
	client   *PontiClient
	manifest ai.CapabilityManifest
}

// NewPontiConnector crea el conector. Si client es nil el caller no debe
// registrarlo en el Registry.
func NewPontiConnector(client *PontiClient) *PontiConnector {
	return &PontiConnector{client: client, manifest: pontiInsightsManifest()}
}

func (p *PontiConnector) ID() string   { return "ponti" }
func (p *PontiConnector) Kind() string { return "ponti" }

// Capabilities expande cada tool del manifest canónico en un
// `domain.Capability` que el Registry y el runtime puedan consumir.
func (p *PontiConnector) Capabilities() []domain.Capability {
	out := make([]domain.Capability, 0, len(p.manifest.Tools))
	for _, tool := range p.manifest.Tools {
		out = append(out, capabilityFromTool(p.manifest, tool))
	}
	return out
}

func (p *PontiConnector) Validate(spec domain.ExecutionSpec) error {
	if spec.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	for _, tool := range p.manifest.Tools {
		if tool.Name == spec.Operation {
			return nil
		}
	}
	return fmt.Errorf("unknown ponti operation: %s", spec.Operation)
}

func (p *PontiConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	start := time.Now()

	var params struct {
		Limit           int    `json:"limit"`
		IncludeResolved bool   `json:"include_resolved"`
		InsightID       string `json:"insight_id"`
	}
	if len(spec.Payload) > 0 {
		if err := json.Unmarshal(spec.Payload, &params); err != nil {
			return domain.ExecutionResult{}, fmt.Errorf("parse payload: %w", err)
		}
	}

	var raw json.RawMessage
	var execErr error
	switch spec.Operation {
	case "ponti.insights.list":
		raw, execErr = p.client.ListInsights(ctx, spec.OrgID, params.Limit, params.IncludeResolved)
	case "ponti.insights.summary":
		raw, execErr = p.client.SummaryInsights(ctx, spec.OrgID)
	case "ponti.insights.explain":
		raw, execErr = p.client.ExplainInsight(ctx, spec.OrgID, params.InsightID)
	default:
		return domain.ExecutionResult{}, fmt.Errorf("unknown operation: %s", spec.Operation)
	}

	duration := time.Since(start).Milliseconds()
	status := domain.ExecSuccess
	var errMsg string
	if execErr != nil {
		status = domain.ExecFailure
		errMsg = execErr.Error()
	}

	if raw == nil {
		raw = json.RawMessage(`{}`)
	}

	evidence := map[string]any{
		"source_ref":  fmt.Sprintf("ponti.%s", spec.Operation),
		"captured_at": time.Now().UTC().Format(time.RFC3339),
		"actor":       spec.ActorID,
		"tenant":      spec.OrgID,
	}
	evidenceJSON, _ := json.Marshal(evidence)

	return domain.ExecutionResult{
		ID:              uuid.New(),
		ConnectorID:     spec.ConnectorID,
		OrgID:           spec.OrgID,
		ActorID:         spec.ActorID,
		Operation:       spec.Operation,
		Status:          status,
		ExternalRef:     fmt.Sprintf("ponti-%s", spec.Operation),
		Payload:         spec.Payload,
		ResultJSON:      raw,
		EvidenceJSON:    evidenceJSON,
		ErrorMessage:    errMsg,
		Retryable:       execErr != nil,
		DurationMS:      duration,
		IdempotencyKey:  spec.IdempotencyKey,
		TaskID:          spec.TaskID,
		GovernanceRequestID: spec.GovernanceRequestID,
		CreatedAt:       time.Now().UTC(),
	}, nil
}

// capabilityFromTool traduce un ai.CapabilityTool al modelo
// domain.Capability del Registry. La granularidad de Companion es por tool,
// la del manifest canónico es por paquete — esta función es el puente.
func capabilityFromTool(m ai.CapabilityManifest, tool ai.CapabilityTool) domain.Capability {
	requiresGovernance := false
	if tool.Governance != nil {
		requiresGovernance = tool.Governance.RequiresApproval
	}
	mode := domain.CapabilityModeRead
	sideEffectClass := domain.SideEffectClassRead
	readOnly := !tool.SideEffect && !strings.EqualFold(tool.Mode, ai.CapabilityModeWrite)
	if !readOnly {
		mode = domain.CapabilityModeWrite
		sideEffectClass = domain.SideEffectClassWrite
	}
	return domain.Capability{
		ID:              tool.Name,
		Version:         m.Version,
		Status:          domain.CapabilityStatusActive,
		OwnerDomain:     m.ID,
		PublishedFrom:   domain.CapabilityPublishedFromProduct,
		Product:         m.Product,
		Operation:       tool.Name,
		Mode:            mode,
		SideEffectClass: sideEffectClass,
		SideEffect:      tool.SideEffect,
		ReadOnly:        readOnly,
		RiskClass:       tool.RiskClass,
		TenantScope: domain.TenantScope{
			Mode:     domain.TenantScopeSingleTenant,
			Resolver: domain.TenantScopeResolverUser,
		},
		AuthMode:        domain.AuthMode{Type: "delegated_user"},
		RequiredRoles:   append([]string(nil), tool.RequiredRoles...),
		RequiredScopes:  []string{"companion:connectors:execute"},
		RequiredModules: append([]string(nil), tool.RequiredModules...),
		RequiresGovernance:  requiresGovernance,
		InputSchema:     tool.InputSchema,
		OutputSchema:    tool.OutputSchema,
		EvidenceFields:  append([]string(nil), tool.EvidenceFields...),
	}
}

// pontiInsightsManifest es la copia local del manifest canónico publicado
// por Ponti. El test del paquete valida que pasa ai.ValidateCapabilityManifest
// y que coincide en IDs/fields con la copia de Ponti.
//
// Cuando se implemente discovery dinámico (fase 2), esta función desaparece
// y el manifest llega vía GET /api/v1/capabilities a Ponti.
func pontiInsightsManifest() ai.CapabilityManifest {
	roles := []string{"ponti.insights.viewer"}
	modules := []string{"ponti", "insights"}

	return ai.CapabilityManifest{
		SchemaVersion: ai.CapabilityManifestSchemaVersion,
		ID:            "ponti.insights",
		Product:       "ponti",
		Version:       "1.0.0",
		TenantScope:   ai.CapabilityTenantScopeOrg,
		Name:          "Ponti Insights",
		Description:   "Read-only access to agricultural insights computed for the caller's tenant.",
		Agents: []ai.CapabilityAgentDescriptor{
			{
				Name:        "ponti_insights",
				Description: "Answers questions about active insights for the caller's tenant.",
			},
		},
		Tools: []ai.CapabilityTool{
			{
				Name:        "ponti.insights.list",
				Description: "Lists insights for the caller's tenant with optional filters.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
						"include_resolved": map[string]any{"type": "boolean"},
					},
				},
				OutputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{"type": "array"},
					},
					"required": []string{"items"},
				},
				EvidenceFields: []string{"source_ref", "captured_at"},
				CapabilityAuthz: ai.CapabilityAuthz{
					RequiredRoles:   roles,
					RequiredModules: modules,
				},
				CapabilityExecutor: ai.CapabilityExecutor{
					ExecutorRef: "ponti-backend.insights.list",
				},
			},
			{
				Name:        "ponti.insights.summary",
				Description: "Returns aggregate counts of insights by status and category for the tenant.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
				OutputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"summary":  map[string]any{"type": "object"},
						"evidence": map[string]any{"type": "object"},
					},
					"required": []string{"summary", "evidence"},
				},
				EvidenceFields: []string{"source_ref", "captured_at", "tenant_scope"},
				CapabilityAuthz: ai.CapabilityAuthz{
					RequiredRoles:   roles,
					RequiredModules: modules,
				},
				CapabilityExecutor: ai.CapabilityExecutor{
					ExecutorRef: "ponti-backend.insights.summary",
				},
			},
			{
				Name:        "ponti.insights.explain",
				Description: "Returns an insight together with its provenance and evidence.",
				Mode:        ai.CapabilityModeRead,
				SideEffect:  false,
				RiskClass:   ai.CapabilityRiskLow,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"insight_id": map[string]any{"type": "string", "format": "uuid"},
					},
					"required": []string{"insight_id"},
				},
				OutputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"insight":  map[string]any{"type": "object"},
						"evidence": map[string]any{"type": "object"},
					},
					"required": []string{"insight", "evidence"},
				},
				EvidenceFields: []string{"source_ref", "captured_at", "first_seen", "event_type", "entity"},
				CapabilityAuthz: ai.CapabilityAuthz{
					RequiredRoles:   roles,
					RequiredModules: modules,
				},
				CapabilityExecutor: ai.CapabilityExecutor{
					ExecutorRef: "ponti-backend.insights.explain",
				},
			},
		},
	}
}
