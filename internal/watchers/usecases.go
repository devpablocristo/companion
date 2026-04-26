// Package watchers implementa la observación proactiva del estado del negocio.
package watchers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/devpablocristo/core/concurrency/go/worker"
	"github.com/google/uuid"

	"github.com/devpablocristo/core/governance/go/reviewclient"
	domain "github.com/devpablocristo/companion/internal/watchers/usecases/domain"
)

// PymesClient port para consultar y ejecutar acciones en Pymes Core.
type PymesClient interface {
	GetStaleWorkOrders(ctx context.Context, orgID string, thresholdDays int) ([]domain.PymesItem, error)
	GetUnconfirmedAppointments(ctx context.Context, orgID string, hoursBefore int) ([]domain.PymesItem, error)
	GetLowStockItems(ctx context.Context, orgID string, thresholdUnits int) ([]domain.PymesItem, error)
	GetInactiveCustomers(ctx context.Context, orgID string, thresholdMonths int) ([]domain.PymesItem, error)
	GetRevenueComparison(ctx context.Context, orgID string) (*domain.RevenueComparison, error)
	SendWhatsAppTemplate(ctx context.Context, orgID, partyID, templateName string, params map[string]string) error
	SendWhatsAppText(ctx context.Context, orgID, partyID, body string) error
}

// ReviewGateway port para enviar solicitudes a Nexus Review.
type ReviewGateway interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body reviewclient.SubmitRequestBody) (reviewclient.SubmitResponse, error)
	GetRequest(ctx context.Context, id string) (reviewclient.RequestSummary, int, error)
}

// CreateWatcherInput es la entrada para crear un watcher.
type CreateWatcherInput struct {
	OrgID       string
	Name        string
	WatcherType domain.WatcherType
	Config      json.RawMessage
	Enabled     bool
}

// UpdateWatcherInput es la entrada para actualizar un watcher.
type UpdateWatcherInput struct {
	Name    *string
	Config  *json.RawMessage
	Enabled *bool
}

// ChatNotifier permite al watcher empujar alertas proactivas al chat del suscriptor.
type ChatNotifier interface {
	// NotifyAlert crea un mensaje de sistema en la conversación activa del suscriptor.
	// Si no hay conversación activa, crea una nueva tarea-chat con la alerta.
	NotifyAlert(ctx context.Context, orgID, message string) error
}

// Usecases contiene la lógica de negocio del módulo watchers.
type Usecases struct {
	repo     Repository
	pymes    PymesClient
	review   ReviewGateway
	notifier ChatNotifier // nil = sin notificaciones al chat
}

// NewUsecases crea los usecases del módulo watchers.
func NewUsecases(repo Repository, pymes PymesClient, review ReviewGateway) *Usecases {
	return &Usecases{repo: repo, pymes: pymes, review: review}
}

// SetNotifier inyecta el notificador de chat. Opcional.
func (uc *Usecases) SetNotifier(n ChatNotifier) {
	uc.notifier = n
}

// --- CRUD ---

// Create crea un nuevo watcher.
func (uc *Usecases) Create(ctx context.Context, input CreateWatcherInput) (domain.Watcher, error) {
	w := domain.Watcher{
		OrgID:       input.OrgID,
		Name:        input.Name,
		WatcherType: input.WatcherType,
		Config:      input.Config,
		Enabled:     input.Enabled,
	}
	return uc.repo.CreateWatcher(ctx, w)
}

// Get obtiene un watcher por ID.
func (uc *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.Watcher, error) {
	return uc.repo.GetWatcher(ctx, id)
}

// List lista watchers de una organización.
func (uc *Usecases) List(ctx context.Context, orgID string) ([]domain.Watcher, error) {
	return uc.repo.ListWatchers(ctx, orgID)
}

// Update actualiza un watcher.
func (uc *Usecases) Update(ctx context.Context, id uuid.UUID, input UpdateWatcherInput) (domain.Watcher, error) {
	w, err := uc.repo.GetWatcher(ctx, id)
	if err != nil {
		return domain.Watcher{}, fmt.Errorf("get watcher for update: %w", err)
	}
	if input.Name != nil {
		w.Name = *input.Name
	}
	if input.Config != nil {
		w.Config = *input.Config
	}
	if input.Enabled != nil {
		w.Enabled = *input.Enabled
	}
	return uc.repo.UpdateWatcher(ctx, w)
}

// Delete elimina un watcher.
func (uc *Usecases) Delete(ctx context.Context, id uuid.UUID) error {
	return uc.repo.DeleteWatcher(ctx, id)
}

// ListProposals lista propuestas de un watcher.
func (uc *Usecases) ListProposals(ctx context.Context, watcherID uuid.UUID, limit int) ([]domain.Proposal, error) {
	return uc.repo.ListProposalsByWatcher(ctx, watcherID, limit)
}

// --- Ejecución ---

// actionTypeForWatcher mapea tipo de watcher a action_type de Review.
func actionTypeForWatcher(wt domain.WatcherType) string {
	switch wt {
	case domain.WatcherStaleWorkOrders:
		return "work_order.delay_notify"
	case domain.WatcherUnconfirmedAppointments:
		return "notification.send"
	case domain.WatcherLowStock:
		return "notification.send"
	case domain.WatcherInactiveCustomers:
		return "vehicle.service_reminder"
	case domain.WatcherRevenueDrop:
		return "notification.send"
	default:
		return "notification.send"
	}
}

// RunWatcher ejecuta un watcher: consulta Pymes, crea propuestas, evalúa con Review, ejecuta si permite.
func (uc *Usecases) RunWatcher(ctx context.Context, watcherID uuid.UUID) (*domain.WatcherResult, error) {
	w, err := uc.repo.GetWatcher(ctx, watcherID)
	if err != nil {
		return nil, fmt.Errorf("get watcher: %w", err)
	}
	if !w.Enabled {
		return nil, ErrWatcherDisabled
	}

	items, err := uc.queryPymes(ctx, w)
	if err != nil {
		slog.Error("watcher query pymes failed", "watcher_id", w.ID, "error", err)
		return nil, fmt.Errorf("query pymes: %w", err)
	}

	result := &domain.WatcherResult{Found: len(items)}

	for _, item := range items {
		proposal, err := uc.processItem(ctx, w, item)
		if err != nil {
			slog.Warn("watcher process item failed", "watcher_id", w.ID, "item_id", item.ID, "error", err)
			continue
		}
		result.Proposed++
		if proposal.ExecutionStatus == domain.ProposalExecuted {
			result.Executed++
		}
	}

	// Actualizar último resultado
	now := time.Now().UTC()
	w.LastRunAt = &now
	resultJSON, _ := json.Marshal(result)
	w.LastResult = resultJSON
	if _, err := uc.repo.UpdateWatcher(ctx, w); err != nil {
		slog.Error("watcher update last run failed", "watcher_id", w.ID, "error", err)
	}

	// Notificar al chat si hubo hallazgos
	if uc.notifier != nil && result.Found > 0 {
		msg := fmt.Sprintf("Alerta de %s: encontré %d items", w.Name, result.Found)
		if result.Executed > 0 {
			msg += fmt.Sprintf(", %d ya se ejecutaron automáticamente", result.Executed)
		}
		if pending := result.Proposed - result.Executed; pending > 0 {
			msg += fmt.Sprintf(", %d esperan tu aprobación", pending)
		}
		msg += "."
		if err := uc.notifier.NotifyAlert(ctx, w.OrgID, msg); err != nil {
			slog.Error("watcher chat notification failed", "watcher_id", w.ID, "error", err)
		}
	}

	return result, nil
}

func (uc *Usecases) queryPymes(ctx context.Context, w domain.Watcher) ([]domain.PymesItem, error) {
	switch w.WatcherType {
	case domain.WatcherStaleWorkOrders:
		var cfg domain.StaleWorkOrdersConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdDays <= 0 {
			cfg.ThresholdDays = 3
		}
		return uc.pymes.GetStaleWorkOrders(ctx, w.OrgID, cfg.ThresholdDays)

	case domain.WatcherUnconfirmedAppointments:
		var cfg domain.UnconfirmedAppointmentsConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.HoursBeforeAppointment <= 0 {
			cfg.HoursBeforeAppointment = 24
		}
		return uc.pymes.GetUnconfirmedAppointments(ctx, w.OrgID, cfg.HoursBeforeAppointment)

	case domain.WatcherLowStock:
		var cfg domain.LowStockConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdUnits <= 0 {
			cfg.ThresholdUnits = 5
		}
		return uc.pymes.GetLowStockItems(ctx, w.OrgID, cfg.ThresholdUnits)

	case domain.WatcherInactiveCustomers:
		var cfg domain.InactiveCustomersConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdMonths <= 0 {
			cfg.ThresholdMonths = 6
		}
		return uc.pymes.GetInactiveCustomers(ctx, w.OrgID, cfg.ThresholdMonths)

	case domain.WatcherRevenueDrop:
		comparison, err := uc.pymes.GetRevenueComparison(ctx, w.OrgID)
		if err != nil {
			return nil, fmt.Errorf("get revenue comparison: %w", err)
		}
		var cfg domain.RevenueDropConfig
		if err := json.Unmarshal(w.Config, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if cfg.ThresholdPercent <= 0 {
			cfg.ThresholdPercent = 20
		}
		if comparison.DropPercent >= cfg.ThresholdPercent {
			meta, _ := json.Marshal(comparison)
			return []domain.PymesItem{{
				ID:       "revenue_alert",
				Type:     "revenue",
				Name:     fmt.Sprintf("Caida de %.1f%% en facturacion", comparison.DropPercent),
				Metadata: meta,
			}}, nil
		}
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown watcher type: %s", w.WatcherType)
	}
}

func (uc *Usecases) processItem(ctx context.Context, w domain.Watcher, item domain.PymesItem) (domain.Proposal, error) {
	actionType := actionTypeForWatcher(w.WatcherType)
	params, _ := json.Marshal(map[string]string{
		"item_id":   item.ID,
		"item_type": item.Type,
		"item_name": item.Name,
		"phone":     item.Phone,
		"party_id":  item.PartyID,
	})

	proposal := domain.Proposal{
		WatcherID:      w.ID,
		OrgID:          w.OrgID,
		ActionType:     actionType,
		TargetResource: item.ID,
		Params:         params,
		Reason:         fmt.Sprintf("Watcher %s detectó: %s", w.Name, item.Name),
	}

	proposal, err := uc.repo.CreateProposal(ctx, proposal)
	if err != nil {
		return proposal, fmt.Errorf("create proposal: %w", err)
	}

	// Consultar Review
	idempotencyKey := fmt.Sprintf("companion-watcher-%s-%s", w.ID, proposal.ID)
	reviewResp, err := uc.review.SubmitRequest(ctx, idempotencyKey, reviewclient.SubmitRequestBody{
		RequesterType:  "service",
		RequesterID:    "nexus_companion",
		RequesterName:  "Nexus Companion Watcher",
		ActionType:     actionType,
		TargetSystem:   "pymes",
		TargetResource: item.ID,
		Reason:         proposal.Reason,
	})
	if err != nil {
		slog.Error("watcher review submit failed", "proposal_id", proposal.ID, "error", err)
		return proposal, fmt.Errorf("submit review request: %w", err)
	}

	reviewID, _ := uuid.Parse(reviewResp.RequestID)
	if reviewID != uuid.Nil {
		proposal.ReviewRequestID = &reviewID
	}

	decision := reviewResp.Decision
	proposal.ReviewDecision = &decision

	switch {
	case decision == "allowed" || decision == "allow" || decision == "approved":
		// Ejecutar acción
		execErr := uc.executeAction(ctx, w, item)
		now := time.Now().UTC()
		proposal.ResolvedAt = &now
		if execErr != nil {
			proposal.ExecutionStatus = domain.ProposalFailed
			errMsg := execErr.Error()
			errJSON, _ := json.Marshal(map[string]string{"error": errMsg})
			proposal.ExecutionResult = errJSON
		} else {
			proposal.ExecutionStatus = domain.ProposalExecuted
			proposal.ExecutionResult = json.RawMessage(`{"status":"sent"}`)
		}

	case decision == "denied" || decision == "deny" || decision == "rejected":
		now := time.Now().UTC()
		proposal.ExecutionStatus = domain.ProposalSkipped
		proposal.ResolvedAt = &now

	default:
		// require_approval — queda pendiente
		proposal.ExecutionStatus = domain.ProposalPending
	}

	if err := uc.repo.UpdateProposal(ctx, proposal); err != nil {
		slog.Error("watcher update proposal failed", "proposal_id", proposal.ID, "error", err)
	}

	return proposal, nil
}

func (uc *Usecases) executeAction(ctx context.Context, w domain.Watcher, item domain.PymesItem) error {
	if item.PartyID == "" && item.Phone == "" {
		return fmt.Errorf("no contact info for item %s", item.ID)
	}

	message := fmt.Sprintf("Hola! Te contactamos desde el negocio: %s", item.Name)

	switch w.WatcherType {
	case domain.WatcherStaleWorkOrders:
		message = fmt.Sprintf("Hola! Te informamos que tu orden de trabajo esta en proceso. Lamentamos la demora y estamos trabajando en ello.")
	case domain.WatcherUnconfirmedAppointments:
		message = fmt.Sprintf("Hola! Te recordamos que tenes un turno agendado. Por favor, confirma tu asistencia.")
	case domain.WatcherInactiveCustomers:
		message = fmt.Sprintf("Hola! Hace tiempo que no nos visitas. Te esperamos!")
	case domain.WatcherLowStock, domain.WatcherRevenueDrop:
		// Estos notifican al dueño, no al cliente
		return uc.pymes.SendWhatsAppText(ctx, w.OrgID, item.PartyID, fmt.Sprintf("Alerta: %s", item.Name))
	}

	if item.PartyID != "" {
		return uc.pymes.SendWhatsAppText(ctx, w.OrgID, item.PartyID, message)
	}
	return nil
}

// RunAllEnabled ejecuta todos los watchers habilitados de una organización.
func (uc *Usecases) RunAllEnabled(ctx context.Context, orgID string) error {
	watchers, err := uc.repo.ListWatchers(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list watchers: %w", err)
	}
	for _, w := range watchers {
		if !w.Enabled {
			continue
		}
		if _, err := uc.RunWatcher(ctx, w.ID); err != nil {
			slog.Error("run watcher failed", "watcher_id", w.ID, "error", err)
		}
	}
	return nil
}

// RunWatcherLoop ejecuta watchers periódicamente en background para todas las orgs.
func (uc *Usecases) RunWatcherLoop(ctx context.Context, interval time.Duration, batchSize int) {
	worker.RunPeriodic(ctx, interval, "watcher-loop", func(tickCtx context.Context) {
		orgIDs, err := uc.repo.ListEnabledOrgIDs(tickCtx)
		if err != nil {
			slog.Error("watcher loop: list org ids failed", "error", err)
			return
		}
		for _, orgID := range orgIDs {
			if err := uc.RunAllEnabled(tickCtx, orgID); err != nil {
				slog.Error("watcher loop: run org failed", "org_id", orgID, "error", err)
			}
		}
	})
}

// SyncPendingProposals sincroniza propuestas pendientes con Review.
func (uc *Usecases) SyncPendingProposals(ctx context.Context, orgID string, limit int) {
	proposals, err := uc.repo.PendingProposals(ctx, orgID)
	if err != nil {
		slog.Error("sync pending proposals failed", "error", err)
		return
	}
	for i, p := range proposals {
		if i >= limit {
			break
		}
		if p.ReviewRequestID == nil {
			continue
		}
		summary, statusCode, err := uc.review.GetRequest(ctx, p.ReviewRequestID.String())
		if err != nil || statusCode == 404 {
			continue
		}
		status := summary.Status
		if status == "approved" || status == "allowed" || status == "rejected" || status == "denied" {
			decision := summary.Decision
			p.ReviewDecision = &decision
			now := time.Now().UTC()
			p.ResolvedAt = &now
			if status == "approved" || status == "allowed" {
				p.ExecutionStatus = domain.ProposalSkipped
				p.ExecutionResult = json.RawMessage(`{"status":"approved_not_executed","reason":"delayed execution is not implemented in sync loop"}`)
			} else {
				p.ExecutionStatus = domain.ProposalSkipped
			}
			if err := uc.repo.UpdateProposal(ctx, p); err != nil {
				slog.Error("sync update proposal failed", "proposal_id", p.ID, "error", err)
			}
		}
	}
}
