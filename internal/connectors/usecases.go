package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/connectors/registry"
	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// Repository port de persistencia para conectores y resultados de ejecución.
type Repository interface {
	SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error)
	GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error)
	ListConnectors(ctx context.Context) ([]domain.Connector, error)
	UpdateConnector(ctx context.Context, c domain.Connector) (domain.Connector, error)
	DeleteConnector(ctx context.Context, id uuid.UUID) error

	SaveExecution(ctx context.Context, r domain.ExecutionResult) error
	AcquireExecutionLock(ctx context.Context, lockKey string) (bool, error)
	ReleaseExecutionLock(ctx context.Context, lockKey string) error
	GetExecutionByIdempotency(ctx context.Context, taskID uuid.UUID, operation string, reviewRequestID *uuid.UUID, idempotencyKey string) (domain.ExecutionResult, error)
	ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error)
}

// ReviewChecker verifica que una ejecución tiene aprobación de Nexus y pertenece al tenant esperado.
type ReviewChecker interface {
	AuthorizeExecution(ctx context.Context, reviewRequestID uuid.UUID, orgID string) (bool, error)
}

// Usecases lógica de negocio de conectores.
type Usecases struct {
	repo     Repository
	registry *registry.Registry
	checker  ReviewChecker
}

// NewUsecases crea una nueva instancia de Usecases.
func NewUsecases(repo Repository, reg *registry.Registry, checker ReviewChecker) *Usecases {
	return &Usecases{
		repo:     repo,
		registry: reg,
		checker:  checker,
	}
}

// ListConnectors lista conectores registrados con su estado en DB.
func (uc *Usecases) ListConnectors(ctx context.Context) ([]domain.Connector, error) {
	conns, err := uc.repo.ListConnectors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list connectors: %w", err)
	}
	return conns, nil
}

// GetConnector obtiene un conector por ID.
func (uc *Usecases) GetConnector(ctx context.Context, id uuid.UUID) (domain.Connector, error) {
	conn, err := uc.repo.GetConnector(ctx, id)
	if err != nil {
		return domain.Connector{}, fmt.Errorf("get connector: %w", err)
	}
	return conn, nil
}

// SaveConnector crea o actualiza un conector.
func (uc *Usecases) SaveConnector(ctx context.Context, c domain.Connector) (domain.Connector, error) {
	if c.ID == uuid.Nil {
		return uc.repo.SaveConnector(ctx, c)
	}
	return uc.repo.UpdateConnector(ctx, c)
}

// DeleteConnector elimina un conector.
func (uc *Usecases) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	return uc.repo.DeleteConnector(ctx, id)
}

// Execute ejecuta una operación en un conector con gating obligatorio.
func (uc *Usecases) Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error) {
	config, err := uc.repo.GetConnector(ctx, spec.ConnectorID)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("get connector config: %w", err)
	}
	if !config.Enabled {
		return domain.ExecutionResult{}, ErrDisabled
	}
	if err := ensureConnectorOrg(config.OrgID, spec.OrgID); err != nil {
		return domain.ExecutionResult{}, err
	}

	// Obtener implementación del conector a partir del kind persistido.
	conn, ok := uc.registry.Get(config.Kind)
	if !ok {
		return domain.ExecutionResult{}, ErrNotFound
	}

	var capability domain.Capability
	operationKnown := false
	for _, cap := range conn.Capabilities() {
		if cap.Operation != spec.Operation {
			continue
		}
		operationKnown = true
		capability = cap
		break
	}
	if !operationKnown {
		return domain.ExecutionResult{}, ErrOperationUnknown
	}

	if spec.IdempotencyKey != "" && spec.TaskID != nil {
		existing, err := uc.repo.GetExecutionByIdempotency(ctx, *spec.TaskID, spec.Operation, spec.ReviewRequestID, spec.IdempotencyKey)
		if err == nil && existing.ID != uuid.Nil {
			return existing, nil
		}
		if err != nil && !IsNotFound(err) {
			return domain.ExecutionResult{}, fmt.Errorf("get execution by idempotency: %w", err)
		}
	}

	// Gating obligatorio: operations write/side-effect requieren approval/allow en Nexus.
	if capability.NeedsReview() && uc.checker == nil {
		return domain.ExecutionResult{}, ErrUngated
	}
	if capability.NeedsReview() && spec.ReviewRequestID != nil {
		approved, err := uc.checker.AuthorizeExecution(ctx, *spec.ReviewRequestID, spec.OrgID)
		if err != nil {
			slog.Error("check review approval", "error", err, "review_request_id", spec.ReviewRequestID)
			return domain.ExecutionResult{}, fmt.Errorf("check review approval: %w", err)
		}
		if !approved {
			return domain.ExecutionResult{}, ErrUngated
		}
	} else if capability.NeedsReview() && spec.ReviewRequestID == nil {
		return domain.ExecutionResult{}, ErrUngated
	}

	if err := validatePayloadSchema(spec.Payload, capability.InputSchema); err != nil {
		return domain.ExecutionResult{}, err
	}

	// Validar spec
	if err := conn.Validate(spec); err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("validate spec: %w", err)
	}

	lockKey := executionLockKey(spec)
	if lockKey != "" {
		acquired, err := uc.repo.AcquireExecutionLock(ctx, lockKey)
		if err != nil {
			return domain.ExecutionResult{}, err
		}
		if !acquired {
			return domain.ExecutionResult{}, fmt.Errorf("%w: execution already in progress", ErrConflict)
		}
		defer func() {
			if err := uc.repo.ReleaseExecutionLock(context.Background(), lockKey); err != nil {
				slog.Error("release execution lock", "error", err, "lock_key", lockKey)
			}
		}()

		existing, err := uc.repo.GetExecutionByIdempotency(ctx, *spec.TaskID, spec.Operation, spec.ReviewRequestID, spec.IdempotencyKey)
		if err == nil && existing.ID != uuid.Nil {
			return existing, nil
		}
		if err != nil && !IsNotFound(err) {
			return domain.ExecutionResult{}, fmt.Errorf("get execution by idempotency after lock: %w", err)
		}
	}

	// Ejecutar
	result, err := conn.Execute(ctx, spec)
	if err != nil {
		return domain.ExecutionResult{}, fmt.Errorf("execute connector %s: %w", conn.ID(), err)
	}
	result.OrgID = spec.OrgID
	result.ActorID = spec.ActorID
	result.IdempotencyKey = spec.IdempotencyKey
	if result.Payload == nil {
		result.Payload = spec.Payload
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	result.EvidenceJSON = buildExecutionEvidence(config, capability, spec, result)

	// Persistir resultado
	if saveErr := uc.repo.SaveExecution(ctx, result); saveErr != nil {
		if IsConflict(saveErr) && spec.IdempotencyKey != "" && spec.TaskID != nil {
			existing, err := uc.repo.GetExecutionByIdempotency(ctx, *spec.TaskID, spec.Operation, spec.ReviewRequestID, spec.IdempotencyKey)
			if err == nil && existing.ID != uuid.Nil {
				return existing, nil
			}
		}
		return domain.ExecutionResult{}, fmt.Errorf("save execution result: %w", saveErr)
	}

	return result, nil
}

// ListExecutions lista resultados de ejecución de un conector.
func (uc *Usecases) ListExecutions(ctx context.Context, connectorID uuid.UUID, limit int) ([]domain.ExecutionResult, error) {
	if limit <= 0 {
		limit = 50
	}
	return uc.repo.ListExecutions(ctx, connectorID, limit)
}

// Capabilities lista las capacidades de todos los conectores registrados.
func (uc *Usecases) Capabilities() []ConnectorCapabilities {
	var out []ConnectorCapabilities
	for _, c := range uc.registry.List() {
		out = append(out, ConnectorCapabilities{
			ID:           c.ID(),
			Kind:         c.Kind(),
			Capabilities: c.Capabilities(),
		})
	}
	return out
}

// ConnectorCapabilities agrupa capacidades por conector.
type ConnectorCapabilities struct {
	ID           string
	Kind         string
	Capabilities []domain.Capability
}

// ReviewCheckerAdapter adapta el reviewclient para verificar aprobaciones.
type ReviewCheckerAdapter struct {
	getRequest func(ctx context.Context, id uuid.UUID) (status string, orgID string, httpStatus int, err error)
}

// NewReviewCheckerAdapter crea un adaptador para verificar aprobaciones.
func NewReviewCheckerAdapter(getRequest func(ctx context.Context, id uuid.UUID) (string, string, int, error)) *ReviewCheckerAdapter {
	return &ReviewCheckerAdapter{getRequest: getRequest}
}

// AuthorizeExecution verifica si un request de Nexus fue aprobado y pertenece a la misma org.
func (a *ReviewCheckerAdapter) AuthorizeExecution(ctx context.Context, reviewRequestID uuid.UUID, orgID string) (bool, error) {
	status, reviewOrgID, _, err := a.getRequest(ctx, reviewRequestID)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(orgID) != "" && strings.TrimSpace(reviewOrgID) != "" && strings.TrimSpace(orgID) != strings.TrimSpace(reviewOrgID) {
		return false, ErrForbidden
	}
	// Estados que indican aprobación
	return status == "allowed" || status == "approved", nil
}

// SeedDefaultConnectors registra conectores por defecto en el registry y en DB.
func (uc *Usecases) SeedDefaultConnectors(ctx context.Context) error {
	for _, conn := range uc.registry.List() {
		existing, _ := uc.repo.ListConnectors(ctx)
		found := false
		for _, e := range existing {
			if e.Kind == conn.Kind() {
				found = true
				break
			}
		}
		if !found {
			capsJSON, mErr := json.Marshal(conn.Capabilities())
			if mErr != nil {
				slog.Error("seed connector marshal capabilities", "kind", conn.Kind(), "error", mErr)
				capsJSON = []byte(`[]`)
			}
			_, err := uc.repo.SaveConnector(ctx, domain.Connector{
				Name:       conn.Kind(),
				Kind:       conn.Kind(),
				Enabled:    true,
				ConfigJSON: json.RawMessage(`{}`),
			})
			if err != nil {
				slog.Error("seed connector", "kind", conn.Kind(), "error", err)
			} else {
				slog.Info("seeded connector", "kind", conn.Kind(), "capabilities", string(capsJSON))
			}
		}
	}
	return nil
}

// ignore para compilación
var _ = time.Now

func validatePayloadSchema(payload json.RawMessage, schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	if typ, ok := schema["type"].(string); ok && typ != "" && typ != "object" {
		return fmt.Errorf("%w: input_schema must describe an object", ErrInvalidPayload)
	}

	var data map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &data); err != nil {
			return fmt.Errorf("%w: payload must be a JSON object", ErrInvalidPayload)
		}
	}
	if data == nil {
		data = make(map[string]any)
	}

	required, ok := requiredSchemaKeys(schema["required"])
	if !ok {
		if _, exists := schema["required"]; exists {
			return fmt.Errorf("%w: input_schema.required must be an array", ErrInvalidPayload)
		}
		return nil
	}
	for _, key := range required {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := data[key]; !exists {
			return fmt.Errorf("%w: missing required field %q", ErrInvalidPayload, key)
		}
	}
	return nil
}

func requiredSchemaKeys(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case nil:
		return nil, false
	case []any:
		keys := make([]string, 0, len(values))
		for _, item := range values {
			keys = append(keys, fmt.Sprint(item))
		}
		return keys, true
	case []string:
		return values, true
	default:
		return nil, false
	}
}

func ensureConnectorOrg(connectorOrgID, specOrgID string) error {
	connectorOrgID = strings.TrimSpace(connectorOrgID)
	specOrgID = strings.TrimSpace(specOrgID)
	if connectorOrgID == "" || specOrgID == "" || connectorOrgID == specOrgID {
		return nil
	}
	return ErrForbidden
}

func executionLockKey(spec domain.ExecutionSpec) string {
	if spec.TaskID == nil || strings.TrimSpace(spec.IdempotencyKey) == "" {
		return ""
	}
	reviewID := "none"
	if spec.ReviewRequestID != nil {
		reviewID = spec.ReviewRequestID.String()
	}
	return fmt.Sprintf("connector-execution:%s:%s:%s:%s", spec.TaskID.String(), spec.Operation, reviewID, strings.TrimSpace(spec.IdempotencyKey))
}

func buildExecutionEvidence(config domain.Connector, capability domain.Capability, spec domain.ExecutionSpec, result domain.ExecutionResult) json.RawMessage {
	evidence := map[string]any{
		"actor_id":          strings.TrimSpace(spec.ActorID),
		"org_id":            strings.TrimSpace(spec.OrgID),
		"connector_id":      spec.ConnectorID.String(),
		"connector_kind":    config.Kind,
		"operation":         spec.Operation,
		"mode":              capability.Mode,
		"side_effect":       capability.HasSideEffect(),
		"risk_class":        capability.RiskClass,
		"payload":           sanitizeJSONPayload(spec.Payload),
		"result":            sanitizeJSONPayload(result.ResultJSON),
		"external_ref":      result.ExternalRef,
		"status":            result.Status,
		"error_message":     result.ErrorMessage,
		"duration_ms":       result.DurationMS,
		"idempotency_key":   spec.IdempotencyKey,
		"created_at":        result.CreatedAt.UTC().Format(time.RFC3339Nano),
		"verification":      "unsigned",
		"attestation_ready": true,
	}
	if spec.TaskID != nil {
		evidence["task_id"] = spec.TaskID.String()
	}
	if spec.ReviewRequestID != nil {
		evidence["review_request_id"] = spec.ReviewRequestID.String()
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func sanitizeJSONPayload(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "[unparseable]"
	}
	return sanitizeValue(value)
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = "***"
				continue
			}
			out[key] = sanitizeValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeValue(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	for _, token := range []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "private_key", "client_secret"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}
