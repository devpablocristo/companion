package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	"github.com/devpablocristo/companion/internal/watchers/pymesclient"
)

// PymesConnector conector a Pymes Core API.
type PymesConnector struct {
	client *pymesclient.Client
}

// NewPymesConnector crea un conector a Pymes.
func NewPymesConnector(client *pymesclient.Client) *PymesConnector {
	return &PymesConnector{client: client}
}

func (p *PymesConnector) ID() string   { return "pymes" }
func (p *PymesConnector) Kind() string { return "pymes" }

func (p *PymesConnector) Capabilities() []domain.Capability {
	return []domain.Capability{
		{
			Operation:      "pymes.send_whatsapp_text",
			Mode:           domain.CapabilityModeWrite,
			SideEffect:     true,
			RiskClass:      "medium",
			RequiresReview: true,
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "party_id", "body"},
			},
			EvidenceFields: []string{"sent", "external_ref", "party_id"},
		},
		{
			Operation:      "pymes.send_whatsapp_template",
			Mode:           domain.CapabilityModeWrite,
			SideEffect:     true,
			RiskClass:      "medium",
			RequiresReview: true,
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id", "party_id", "template_name"},
			},
			EvidenceFields: []string{"sent", "external_ref", "party_id", "template_name"},
		},
		{
			Operation: "pymes.get_work_orders",
			Mode:      domain.CapabilityModeRead,
			ReadOnly:  true,
			RiskClass: "low",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation: "pymes.get_appointments",
			Mode:      domain.CapabilityModeRead,
			ReadOnly:  true,
			RiskClass: "low",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation: "pymes.get_low_stock",
			Mode:      domain.CapabilityModeRead,
			ReadOnly:  true,
			RiskClass: "low",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
		{
			Operation: "pymes.get_customers",
			Mode:      domain.CapabilityModeRead,
			ReadOnly:  true,
			RiskClass: "low",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"org_id"},
			},
			EvidenceFields: []string{"items"},
		},
	}
}

func (p *PymesConnector) Validate(spec domain.ExecutionSpec) error {
	if spec.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	return nil
}

func (p *PymesConnector) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	start := time.Now()

	var params struct {
		OrgID           string            `json:"org_id"`
		PartyID         string            `json:"party_id"`
		Body            string            `json:"body"`
		TemplateName    string            `json:"template_name"`
		Params          map[string]string `json:"params"`
		ThresholdDays   int               `json:"threshold_days"`
		ThresholdUnits  int               `json:"threshold_units"`
		ThresholdMonths int               `json:"threshold_months"`
	}
	if err := json.Unmarshal(spec.Payload, &params); err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("parse payload: %w", err)
	}

	var resultData any
	var execErr error

	switch spec.Operation {
	case "pymes.send_whatsapp_text":
		execErr = p.client.SendWhatsAppText(ctx, params.OrgID, params.PartyID, params.Body)
		resultData = map[string]string{"sent": "true"}

	case "pymes.send_whatsapp_template":
		execErr = p.client.SendWhatsAppTemplate(ctx, params.OrgID, params.PartyID, params.TemplateName, params.Params)
		resultData = map[string]string{"sent": "true"}

	case "pymes.get_work_orders":
		items, err := p.client.GetStaleWorkOrders(ctx, params.OrgID, params.ThresholdDays)
		execErr = err
		resultData = items

	case "pymes.get_appointments":
		items, err := p.client.GetUnconfirmedAppointments(ctx, params.OrgID, 24)
		execErr = err
		resultData = items

	case "pymes.get_low_stock":
		items, err := p.client.GetLowStockItems(ctx, params.OrgID, params.ThresholdUnits)
		execErr = err
		resultData = items

	case "pymes.get_customers":
		items, err := p.client.GetInactiveCustomers(ctx, params.OrgID, params.ThresholdMonths)
		execErr = err
		resultData = items

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

	resultJSON, _ := json.Marshal(resultData)

	return domain.ExecutionResult{
		ID:              uuid.New(),
		ConnectorID:     spec.ConnectorID,
		OrgID:           spec.OrgID,
		ActorID:         spec.ActorID,
		Operation:       spec.Operation,
		Status:          status,
		ExternalRef:     fmt.Sprintf("pymes-%s", spec.Operation),
		Payload:         spec.Payload,
		ResultJSON:      json.RawMessage(resultJSON),
		ErrorMessage:    errMsg,
		Retryable:       execErr != nil,
		DurationMS:      duration,
		IdempotencyKey:  spec.IdempotencyKey,
		TaskID:          spec.TaskID,
		ReviewRequestID: spec.ReviewRequestID,
		CreatedAt:       time.Now().UTC(),
	}, nil
}
