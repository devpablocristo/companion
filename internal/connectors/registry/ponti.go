package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// PontiConnector adapter de Companion a Ponti (read-only piloto).
//
// Las capabilities aquí espejan los manifests publicados por Ponti
// (ponti-backend/internal/capabilities). En fase 2 el descubrimiento será
// dinámico vía GET /api/v1/capabilities — por ahora se hardcodean para
// evitar discovery por request y mantener determinismo en eval/CI.
type PontiConnector struct {
	client *PontiClient
}

// NewPontiConnector crea el conector. Si client es nil el caller no debe
// registrarlo en el Registry.
func NewPontiConnector(client *PontiClient) *PontiConnector {
	return &PontiConnector{client: client}
}

func (p *PontiConnector) ID() string   { return "ponti" }
func (p *PontiConnector) Kind() string { return "ponti" }

func (p *PontiConnector) Capabilities() []domain.Capability {
	std := []string{"companion:connectors:execute"}
	mods := []string{"ponti", "insights"}
	roles := []string{"ponti.insights.viewer"}
	return []domain.Capability{
		{
			ID:               "ponti.insights.list",
			Version:          "1.0.0",
			Status:           "active",
			OwnerDomain:      "ponti.insights",
			PublishedFrom:    "product",
			Product:          "ponti",
			Operation:        "ponti.insights.list",
			Mode:             domain.CapabilityModeRead,
			SideEffectClass:  domain.SideEffectClassRead,
			ReadOnly:         true,
			RiskClass:        domain.RiskClassLow,
			TenantScope:      domain.TenantScope{Mode: domain.TenantScopeSingleTenant, Resolver: domain.TenantScopeResolverUser},
			AuthMode:         domain.AuthMode{Type: "delegated_user"},
			RequiredRoles:    roles,
			RequiredScopes:   std,
			RequiredModules:  mods,
			RequiresReview:   false,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit":            map[string]any{"type": "integer", "minimum": 1, "maximum": 200},
					"include_resolved": map[string]any{"type": "boolean"},
				},
			},
			EvidenceFields: []string{"source_ref", "captured_at"},
		},
		{
			ID:               "ponti.insights.summary",
			Version:          "1.0.0",
			Status:           "active",
			OwnerDomain:      "ponti.insights",
			PublishedFrom:    "product",
			Product:          "ponti",
			Operation:        "ponti.insights.summary",
			Mode:             domain.CapabilityModeRead,
			SideEffectClass:  domain.SideEffectClassRead,
			ReadOnly:         true,
			RiskClass:        domain.RiskClassLow,
			TenantScope:      domain.TenantScope{Mode: domain.TenantScopeSingleTenant, Resolver: domain.TenantScopeResolverUser},
			AuthMode:         domain.AuthMode{Type: "delegated_user"},
			RequiredRoles:    roles,
			RequiredScopes:   std,
			RequiredModules:  mods,
			RequiresReview:   false,
			InputSchema:      map[string]any{"type": "object"},
			EvidenceFields:   []string{"source_ref", "captured_at", "tenant_scope"},
		},
		{
			ID:               "ponti.insights.explain",
			Version:          "1.0.0",
			Status:           "active",
			OwnerDomain:      "ponti.insights",
			PublishedFrom:    "product",
			Product:          "ponti",
			Operation:        "ponti.insights.explain",
			Mode:             domain.CapabilityModeRead,
			SideEffectClass:  domain.SideEffectClassRead,
			ReadOnly:         true,
			RiskClass:        domain.RiskClassLow,
			TenantScope:      domain.TenantScope{Mode: domain.TenantScopeSingleTenant, Resolver: domain.TenantScopeResolverUser},
			AuthMode:         domain.AuthMode{Type: "delegated_user"},
			RequiredRoles:    roles,
			RequiredScopes:   std,
			RequiredModules:  mods,
			RequiresReview:   false,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"insight_id": map[string]any{"type": "string"},
				},
				"required": []string{"insight_id"},
			},
			EvidenceFields: []string{"source_ref", "captured_at", "first_seen", "event_type", "entity"},
		},
	}
}

func (p *PontiConnector) Validate(spec domain.ExecutionSpec) error {
	if spec.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	switch spec.Operation {
	case "ponti.insights.list", "ponti.insights.summary", "ponti.insights.explain":
		return nil
	default:
		return fmt.Errorf("unknown ponti operation: %s", spec.Operation)
	}
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

	// Evidence pack: source_refs, captured_at, actor — el manifest declara
	// estos campos como evidence_required. Los empaquetamos acá para que el
	// caller (orchestrator → run trace) tenga la cita lista.
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
		ReviewRequestID: spec.ReviewRequestID,
		CreatedAt:       time.Now().UTC(),
	}, nil
}
