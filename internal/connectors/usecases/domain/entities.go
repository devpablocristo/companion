package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Connector configuración de un conector a sistema externo.
type Connector struct {
	ID         uuid.UUID
	OrgID      string
	Name       string
	Kind       string // pymes, whatsapp, slack, email, calendar, mock
	Enabled    bool
	ConfigJSON json.RawMessage // credenciales/config (sin secretos en claro)
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Capability capacidad que ofrece un conector.
type Capability struct {
	Operation      string         `json:"operation"` // send_whatsapp, create_purchase, etc.
	Mode           string         `json:"mode"`      // read o write
	SideEffect     bool           `json:"side_effect"`
	ReadOnly       bool           `json:"read_only"`
	RiskClass      string         `json:"risk_class,omitempty"`
	RequiresReview bool           `json:"requires_review"`
	InputSchema    map[string]any `json:"input_schema,omitempty"`
	EvidenceFields []string       `json:"evidence_fields,omitempty"`
}

// ExecutionSpec especificación de una ejecución en un conector.
type ExecutionSpec struct {
	ConnectorID     uuid.UUID
	OrgID           string
	ActorID         string
	Operation       string
	Payload         json.RawMessage
	IdempotencyKey  string
	TaskID          *uuid.UUID
	ReviewRequestID *uuid.UUID
}

// ExecutionResult resultado de una ejecución.
type ExecutionResult struct {
	ID              uuid.UUID
	ConnectorID     uuid.UUID
	OrgID           string
	ActorID         string
	Operation       string
	Status          string // success, failure, partial
	ExternalRef     string // referencia en el sistema externo
	Payload         json.RawMessage
	ResultJSON      json.RawMessage
	EvidenceJSON    json.RawMessage
	ErrorMessage    string
	Retryable       bool
	DurationMS      int64
	IdempotencyKey  string
	TaskID          *uuid.UUID
	ReviewRequestID *uuid.UUID
	CreatedAt       time.Time
}

// ExecutionStatus valores de estado de ejecución.
const (
	ExecSuccess = "success"
	ExecFailure = "failure"
	ExecPartial = "partial"
)

const (
	CapabilityModeRead  = "read"
	CapabilityModeWrite = "write"
)

// HasSideEffect mantiene compatibilidad con el contrato legacy y el contrato v1.
func (c Capability) HasSideEffect() bool {
	mode := strings.TrimSpace(strings.ToLower(c.Mode))
	return c.SideEffect || mode == CapabilityModeWrite || !c.ReadOnly && mode != CapabilityModeRead
}

// NeedsReview indica si Nexus debe aprobar/permitir antes de ejecutar.
func (c Capability) NeedsReview() bool {
	return c.RequiresReview || c.HasSideEffect()
}
