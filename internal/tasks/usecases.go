package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/devpablocristo/core/concurrency/go/worker"
	"github.com/devpablocristo/core/errors/go/domainerr"
	"github.com/google/uuid"

	"github.com/devpablocristo/core/governance/go/reviewclient"
	connectordomain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

// Identidad del servicio Companion ante Review (documentado en README).
const (
	CompanionRequesterType     = "service"
	CompanionRequesterID       = "nexus_companion"
	CompanionRequesterName     = "Nexus Companion"
	ActionTypePropose          = "companion.propose"
	TaskActionInvestigate      = "investigate"
	TaskActionPropose          = "propose"
	TaskActionSyncReview       = "sync_review"
	TaskActionSetExecutionPlan = "set_execution_plan"
	TaskActionExecuteConnector = "execute_connector"
	TaskActionRetryExecution   = "retry_execution"
	TaskActionVerifyExecution  = "verify_execution"

	TaskArtifactConnectorExecution    = "connector_execution"
	TaskArtifactExecutionError        = "connector_execution_error"
	TaskArtifactExecutionVerification = "execution_verification"

	taskMemoryCurrentKey  = "current"
	taskMemoryKindFacts   = "task_facts"
	taskMemoryKindSummary = "task_summary"

	defaultReviewSyncInterval = 30 * time.Second
	maxReviewSyncBackoff      = 10 * time.Minute
)

type nexusGateway interface {
	SubmitRequest(ctx context.Context, idempotencyKey string, body reviewclient.SubmitRequestBody) (reviewclient.SubmitResponse, error)
	GetRequest(ctx context.Context, id string) (reviewclient.RequestSummary, int, error)
	ReportResult(ctx context.Context, id string, success bool, result map[string]any, durationMS int64, errorMessage string) (int, error)
}

type taskExecutor interface {
	GetConnector(ctx context.Context, id uuid.UUID) (connectordomain.Connector, error)
	Execute(ctx context.Context, spec connectordomain.ExecutionSpec) (connectordomain.ExecutionResult, error)
}

type taskMemoryWriter interface {
	UpsertTaskMemory(ctx context.Context, taskID uuid.UUID, kind, key string, contentText string, payload json.RawMessage) error
}

// ChatOrchestrator interfaz del runtime del compañero.
type ChatOrchestrator interface {
	Run(ctx context.Context, in OrchestratorInput) (OrchestratorResult, error)
}

// OrchestratorInput entrada para el runtime.
type OrchestratorInput struct {
	UserID   string
	OrgID    string
	Message  string
	Messages []domain.TaskMessage
}

// OrchestratorResult resultado del runtime.
type OrchestratorResult struct {
	Reply string
}

// Usecases lógica de tareas e integración con Nexus governance.
type Usecases struct {
	repo               Repository
	nexus              nexusGateway
	orchestrator       ChatOrchestrator // nil = sin LLM (solo persiste)
	executor           taskExecutor
	taskMemory         taskMemoryWriter
	reviewSyncInterval time.Duration
}

func NewUsecases(repo Repository, nexus nexusGateway) *Usecases {
	return &Usecases{
		repo:               repo,
		nexus:              nexus,
		reviewSyncInterval: defaultReviewSyncInterval,
	}
}

// SetOrchestrator inyecta el runtime del compañero. Opcional: si no se llama, Chat solo persiste.
func (u *Usecases) SetOrchestrator(o ChatOrchestrator) {
	u.orchestrator = o
}

func (u *Usecases) SetExecutor(executor taskExecutor) {
	u.executor = executor
}

func (u *Usecases) SetTaskMemory(writer taskMemoryWriter) {
	u.taskMemory = writer
}

func (u *Usecases) SetReviewSyncInterval(interval time.Duration) {
	if interval <= 0 {
		u.reviewSyncInterval = defaultReviewSyncInterval
		return
	}
	u.reviewSyncInterval = interval
}

type CreateTaskInput struct {
	OrgID       string
	Title       string
	Goal        string
	Priority    string
	CreatedBy   string
	AssignedTo  string
	Channel     string
	Summary     string
	ContextJSON json.RawMessage
}

func (u *Usecases) Create(ctx context.Context, in CreateTaskInput) (domain.Task, error) {
	if in.Title == "" {
		return domain.Task{}, fmt.Errorf("title is required")
	}
	t := domain.Task{
		Title:       in.Title,
		OrgID:       in.OrgID,
		Goal:        in.Goal,
		Status:      domain.TaskStatusNew,
		Priority:    in.Priority,
		CreatedBy:   in.CreatedBy,
		AssignedTo:  in.AssignedTo,
		Channel:     in.Channel,
		Summary:     in.Summary,
		ContextJSON: in.ContextJSON,
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if len(t.ContextJSON) == 0 {
		t.ContextJSON = json.RawMessage(`{}`)
	}
	out, err := u.repo.CreateTask(ctx, t)
	if err != nil {
		return domain.Task{}, err
	}
	u.syncTaskMemory(ctx, out.ID, "create")
	slog.Info("companion task created", "task_id", out.ID.String(), "title", out.Title, "created_by", out.CreatedBy)
	return out, nil
}

func (u *Usecases) List(ctx context.Context, limit int) ([]domain.Task, error) {
	return u.repo.ListTasks(ctx, limit)
}

func (u *Usecases) Get(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	return u.repo.GetTaskByID(ctx, id)
}

type LinkedReviewRequest struct {
	ActionID uuid.UUID                    `json:"action_id"`
	Request  *reviewclient.RequestSummary `json:"request,omitempty"`
}

type TaskDetail struct {
	Task                 domain.Task                 `json:"task"`
	Messages             []domain.TaskMessage        `json:"messages"`
	Actions              []domain.TaskAction         `json:"actions"`
	Artifacts            []domain.TaskArtifact       `json:"artifacts"`
	LinkedReviewRequests []LinkedReviewRequest       `json:"linked_review_requests"`
	ReviewSync           *domain.TaskReviewSyncState `json:"review_sync,omitempty"`
	ExecutionPlan        *domain.TaskExecutionPlan   `json:"execution_plan,omitempty"`
	ExecutionState       *domain.TaskExecutionState  `json:"execution_state,omitempty"`
}

func (u *Usecases) GetDetail(ctx context.Context, id uuid.UUID) (TaskDetail, error) {
	var out TaskDetail
	t, err := u.repo.GetTaskByID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Task = t
	out.Messages, err = u.repo.ListMessagesByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Actions, err = u.repo.ListActionsByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Artifacts, err = u.repo.ListArtifactsByTaskID(ctx, id)
	if err != nil {
		return out, err
	}
	state, stateErr := u.repo.GetReviewSyncState(ctx, id)
	if stateErr == nil {
		out.ReviewSync = &state
	} else if !domainerr.IsNotFound(stateErr) {
		return out, stateErr
	}
	plan, planErr := u.repo.GetExecutionPlan(ctx, id)
	if planErr == nil {
		out.ExecutionPlan = &plan
	} else if !domainerr.IsNotFound(planErr) {
		return out, planErr
	}
	executionState, executionStateErr := u.repo.GetExecutionState(ctx, id)
	if executionStateErr == nil {
		out.ExecutionState = &executionState
	} else if !domainerr.IsNotFound(executionStateErr) {
		return out, executionStateErr
	}
	seen := make(map[uuid.UUID]struct{})
	for _, a := range out.Actions {
		if a.ReviewRequestID == nil {
			continue
		}
		rid := *a.ReviewRequestID
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		sum, st, gErr := u.nexus.GetRequest(ctx, rid.String())
		lr := LinkedReviewRequest{ActionID: a.ID}
		if gErr != nil {
			slog.Error("review get request failed", "error", gErr, "request_id", rid)
			out.LinkedReviewRequests = append(out.LinkedReviewRequests, lr)
			continue
		}
		if st == 404 {
			out.LinkedReviewRequests = append(out.LinkedReviewRequests, lr)
			continue
		}
		lr.Request = &sum
		out.LinkedReviewRequests = append(out.LinkedReviewRequests, lr)
	}
	return out, nil
}

type AddMessageInput struct {
	AuthorType string
	AuthorID   string
	Body       string
}

func (u *Usecases) AddMessage(ctx context.Context, taskID uuid.UUID, in AddMessageInput) (domain.TaskMessage, error) {
	if in.Body == "" {
		return domain.TaskMessage{}, fmt.Errorf("body is required")
	}
	if _, err := u.repo.GetTaskByID(ctx, taskID); err != nil {
		return domain.TaskMessage{}, err
	}
	at := in.AuthorType
	if at == "" {
		at = "user"
	}
	return u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     taskID,
		AuthorType: at,
		AuthorID:   in.AuthorID,
		Body:       in.Body,
	})
}

// ChatInput entrada para el endpoint de chat conversacional.
type ChatInput struct {
	TaskID  *uuid.UUID // nil = crear tarea nueva
	UserID  string
	OrgID   string
	Message string
	Channel string // "console", "api", etc.
}

// ChatResult resultado del chat.
type ChatResult struct {
	Task     domain.Task
	Messages []domain.TaskMessage
}

// Chat combina crear/reusar tarea + agregar mensaje del usuario.
// Es el endpoint principal para la interfaz conversacional del suscriptor.
func (u *Usecases) Chat(ctx context.Context, in ChatInput) (ChatResult, error) {
	if in.Message == "" {
		return ChatResult{}, fmt.Errorf("message is required")
	}

	var t domain.Task
	var err error

	if in.TaskID != nil {
		// Reusar tarea existente
		t, err = u.repo.GetTaskByID(ctx, *in.TaskID)
		if err != nil {
			return ChatResult{}, err
		}
	} else {
		// Crear tarea nueva con el primer mensaje como título
		title := in.Message
		if len(title) > 80 {
			title = title[:80]
		}
		channel := in.Channel
		if channel == "" {
			channel = "console"
		}
		t, err = u.repo.CreateTask(ctx, domain.Task{
			Title:     title,
			OrgID:     in.OrgID,
			Status:    domain.TaskStatusNew,
			Priority:  "normal",
			CreatedBy: in.UserID,
			Channel:   channel,
		})
		if err != nil {
			return ChatResult{}, fmt.Errorf("create chat task: %w", err)
		}
		slog.Info("companion chat started", "task_id", t.ID.String(), "user_id", in.UserID)
	}

	// Agregar mensaje del usuario
	_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     t.ID,
		AuthorType: "user",
		AuthorID:   in.UserID,
		Body:       in.Message,
	})
	if err != nil {
		return ChatResult{}, fmt.Errorf("insert chat message: %w", err)
	}

	// Si hay orchestrator, generar respuesta del compañero
	if u.orchestrator != nil {
		existingMsgs, listErr := u.repo.ListMessagesByTaskID(ctx, t.ID)
		if listErr != nil {
			slog.Error("chat list messages for orchestrator", "error", listErr)
		} else {
			orgID := in.OrgID
			if orgID == "" {
				orgID = t.CreatedBy // fallback si no viene en el request
			}
			result, runErr := u.orchestrator.Run(ctx, OrchestratorInput{
				UserID:   in.UserID,
				OrgID:    orgID,
				Message:  in.Message,
				Messages: existingMsgs,
			})
			if runErr != nil {
				slog.Error("orchestrator failed", "error", runErr)
			} else if result.Reply != "" {
				// Guardar respuesta del compañero como mensaje del sistema
				_, insertErr := u.repo.InsertMessage(ctx, domain.TaskMessage{
					TaskID:     t.ID,
					AuthorType: "system",
					AuthorID:   "nexus",
					Body:       result.Reply,
				})
				if insertErr != nil {
					slog.Error("insert orchestrator reply", "error", insertErr)
				}
			}
		}
	}

	// Devolver hilo completo (incluyendo respuesta del compañero si hubo)
	msgs, err := u.repo.ListMessagesByTaskID(ctx, t.ID)
	if err != nil {
		return ChatResult{}, fmt.Errorf("list chat messages: %w", err)
	}

	return ChatResult{Task: t, Messages: msgs}, nil
}

type InvestigateInput struct {
	Note string
}

func (u *Usecases) applyTaskEvent(ctx context.Context, t domain.Task, event string) (domain.Task, error) {
	to, err := companionTaskMachine().Transition(t.Status, event)
	if err != nil {
		return domain.Task{}, ErrInvalidTaskState
	}
	t.Status = to
	if to == domain.TaskStatusDone || to == domain.TaskStatusFailed {
		now := time.Now().UTC()
		t.ClosedAt = &now
	} else {
		t.ClosedAt = nil
	}
	return u.repo.UpdateTask(ctx, t)
}

func (u *Usecases) reviewSyncIntervalOrDefault() time.Duration {
	if u.reviewSyncInterval <= 0 {
		return defaultReviewSyncInterval
	}
	return u.reviewSyncInterval
}

func nextReviewSyncAt(now time.Time, interval time.Duration, consecutiveFailures int) time.Time {
	if interval <= 0 {
		interval = defaultReviewSyncInterval
	}
	if consecutiveFailures <= 0 {
		return now.Add(interval)
	}
	delay := interval
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxReviewSyncBackoff/2 {
			delay = maxReviewSyncBackoff
			break
		}
		delay *= 2
	}
	if delay > maxReviewSyncBackoff {
		delay = maxReviewSyncBackoff
	}
	return now.Add(delay)
}

func reviewSnapshotChanged(prev *domain.TaskReviewSyncState, next domain.TaskReviewSyncState) bool {
	if prev == nil {
		return next.ReviewRequestID != uuid.Nil ||
			next.LastReviewStatus != "" ||
			next.LastReviewHTTPStatus != 0 ||
			next.LastError != ""
	}
	return prev.ReviewRequestID != next.ReviewRequestID ||
		prev.LastReviewStatus != next.LastReviewStatus ||
		prev.LastReviewHTTPStatus != next.LastReviewHTTPStatus ||
		prev.LastError != next.LastError
}

func executionPlanChanged(prev *domain.TaskExecutionPlan, next domain.TaskExecutionPlan) bool {
	if prev == nil {
		return next.ConnectorID != uuid.Nil || next.Operation != "" || len(next.Payload) > 0 || next.IdempotencyKey != ""
	}
	return prev.ConnectorID != next.ConnectorID ||
		prev.Operation != next.Operation ||
		!bytes.Equal(prev.Payload, next.Payload) ||
		prev.IdempotencyKey != next.IdempotencyKey
}

func isApprovedReviewStatus(status string) bool {
	switch normalizeReviewStatus(status) {
	case "allowed", "approved", "executed":
		return true
	default:
		return false
	}
}

func (u *Usecases) getExecutionPlan(ctx context.Context, taskID uuid.UUID) (*domain.TaskExecutionPlan, error) {
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		return &plan, nil
	}
	if domainerr.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

func (u *Usecases) getExecutionState(ctx context.Context, taskID uuid.UUID) (*domain.TaskExecutionState, error) {
	state, err := u.repo.GetExecutionState(ctx, taskID)
	if err == nil {
		return &state, nil
	}
	if domainerr.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}

type taskMemorySnapshot struct {
	Task           domain.Task
	ReviewSync     *domain.TaskReviewSyncState
	ExecutionPlan  *domain.TaskExecutionPlan
	ExecutionState *domain.TaskExecutionState
}

func (u *Usecases) loadTaskMemorySnapshot(ctx context.Context, taskID uuid.UUID) (taskMemorySnapshot, error) {
	task, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return taskMemorySnapshot{}, err
	}
	snapshot := taskMemorySnapshot{Task: task}

	reviewSync, err := u.repo.GetReviewSyncState(ctx, taskID)
	if err == nil {
		snapshot.ReviewSync = &reviewSync
		snapshot.Task.ReviewStatus = reviewSync.LastReviewStatus
		snapshot.Task.ReviewLastCheckedAt = &reviewSync.LastCheckedAt
		snapshot.Task.ReviewSyncError = reviewSync.LastError
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	executionPlan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		snapshot.ExecutionPlan = &executionPlan
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	executionState, err := u.repo.GetExecutionState(ctx, taskID)
	if err == nil {
		snapshot.ExecutionState = &executionState
	} else if !domainerr.IsNotFound(err) {
		return taskMemorySnapshot{}, err
	}

	return snapshot, nil
}

func nextTaskStep(snapshot taskMemorySnapshot) string {
	switch snapshot.Task.Status {
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		if snapshot.ExecutionPlan == nil {
			return "define execution plan and propose to review"
		}
		return "propose to review"
	case domain.TaskStatusWaitingForApproval:
		return "wait for review resolution or sync from review"
	case domain.TaskStatusWaitingForInput:
		if snapshot.ExecutionPlan != nil {
			return "execute the approved task manually"
		}
		return "provide the missing execution input"
	case domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return "observe execution and verification"
	case domain.TaskStatusFailed:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.Retryable && isApprovedReviewStatus(snapshot.Task.ReviewStatus) {
			return "inspect failure and retry execution"
		}
		if snapshot.Task.ReviewStatus == "rejected" || snapshot.Task.ReviewStatus == "denied" {
			return "inspect review decision and adjust the task"
		}
		return "inspect failure details"
	case domain.TaskStatusDone:
		return "closed"
	default:
		return "inspect task status"
	}
}

func buildTaskSummary(snapshot taskMemorySnapshot) string {
	title := strings.TrimSpace(snapshot.Task.Title)
	if title == "" {
		title = snapshot.Task.ID.String()
	}
	prefix := fmt.Sprintf("Task %q", title)

	switch snapshot.Task.Status {
	case domain.TaskStatusNew:
		return fmt.Sprintf("%s was created and is ready for investigation.", prefix)
	case domain.TaskStatusInvestigating:
		return fmt.Sprintf("%s is under investigation. Next step: %s.", prefix, nextTaskStep(snapshot))
	case domain.TaskStatusWaitingForApproval:
		if snapshot.ReviewSync != nil && snapshot.ReviewSync.ReviewRequestID != uuid.Nil {
			return fmt.Sprintf("%s is waiting for Review. Request %s is currently %s.", prefix, snapshot.ReviewSync.ReviewRequestID.String(), formatStatusForMemory(snapshot.ReviewSync.LastReviewStatus))
		}
		return fmt.Sprintf("%s is waiting for Review approval.", prefix)
	case domain.TaskStatusWaitingForInput:
		if snapshot.ExecutionPlan != nil {
			return fmt.Sprintf("%s is approved and ready for manual execution via %s.", prefix, snapshot.ExecutionPlan.Operation)
		}
		return fmt.Sprintf("%s is approved and waiting for additional input.", prefix)
	case domain.TaskStatusExecuting:
		return fmt.Sprintf("%s is executing the configured connector action.", prefix)
	case domain.TaskStatusVerifying:
		return fmt.Sprintf("%s finished execution and is being verified.", prefix)
	case domain.TaskStatusDone:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.VerificationResult.Status == domain.VerificationStatusVerified {
			return fmt.Sprintf("%s completed successfully and the latest execution was verified.", prefix)
		}
		if isApprovedReviewStatus(snapshot.Task.ReviewStatus) {
			return fmt.Sprintf("%s completed successfully after Review resolved %s.", prefix, formatStatusForMemory(snapshot.Task.ReviewStatus))
		}
		return fmt.Sprintf("%s completed successfully.", prefix)
	case domain.TaskStatusFailed:
		if snapshot.ExecutionState != nil && snapshot.ExecutionState.LastError != "" {
			if snapshot.ExecutionState.Retryable {
				return fmt.Sprintf("%s failed during execution. Retry is available. Last error: %s.", prefix, snapshot.ExecutionState.LastError)
			}
			return fmt.Sprintf("%s failed during execution. Last error: %s.", prefix, snapshot.ExecutionState.LastError)
		}
		if snapshot.Task.ReviewStatus != "" {
			return fmt.Sprintf("%s failed because Review resolved %s.", prefix, formatStatusForMemory(snapshot.Task.ReviewStatus))
		}
		return fmt.Sprintf("%s failed and needs operator attention.", prefix)
	default:
		return fmt.Sprintf("%s is in status %s.", prefix, formatStatusForMemory(snapshot.Task.Status))
	}
}

func formatStatusForMemory(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "unknown"
	}
	return strings.ReplaceAll(status, "_", " ")
}

func buildTaskFactsPayload(snapshot taskMemorySnapshot, reason string) json.RawMessage {
	payload := map[string]any{
		"projection_reason":  reason,
		"task_id":            snapshot.Task.ID.String(),
		"title":              snapshot.Task.Title,
		"goal":               snapshot.Task.Goal,
		"status":             snapshot.Task.Status,
		"priority":           snapshot.Task.Priority,
		"created_by":         snapshot.Task.CreatedBy,
		"assigned_to":        snapshot.Task.AssignedTo,
		"channel":            snapshot.Task.Channel,
		"summary":            snapshot.Task.Summary,
		"next_step":          nextTaskStep(snapshot),
		"attention_required": snapshot.Task.Status == domain.TaskStatusWaitingForApproval || snapshot.Task.Status == domain.TaskStatusWaitingForInput || snapshot.Task.Status == domain.TaskStatusFailed,
		"updated_at":         snapshot.Task.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if snapshot.Task.CreatedAt.IsZero() {
		payload["created_at"] = ""
	} else {
		payload["created_at"] = snapshot.Task.CreatedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.ClosedAt != nil {
		payload["closed_at"] = snapshot.Task.ClosedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.ReviewStatus != "" {
		payload["review_status"] = snapshot.Task.ReviewStatus
	}
	if snapshot.Task.ReviewLastCheckedAt != nil {
		payload["review_last_checked_at"] = snapshot.Task.ReviewLastCheckedAt.UTC().Format(time.RFC3339)
	}
	if snapshot.Task.ReviewSyncError != "" {
		payload["review_sync_error"] = snapshot.Task.ReviewSyncError
	}
	if snapshot.ReviewSync != nil {
		payload["review"] = map[string]any{
			"review_request_id":    snapshot.ReviewSync.ReviewRequestID.String(),
			"status":               snapshot.ReviewSync.LastReviewStatus,
			"http_status":          snapshot.ReviewSync.LastReviewHTTPStatus,
			"last_checked_at":      snapshot.ReviewSync.LastCheckedAt.UTC().Format(time.RFC3339),
			"next_check_at":        snapshot.ReviewSync.NextCheckAt.UTC().Format(time.RFC3339),
			"consecutive_failures": snapshot.ReviewSync.ConsecutiveFailures,
			"last_error":           snapshot.ReviewSync.LastError,
		}
	}
	if snapshot.ExecutionPlan != nil {
		payload["execution_plan"] = map[string]any{
			"connector_id":    snapshot.ExecutionPlan.ConnectorID.String(),
			"operation":       snapshot.ExecutionPlan.Operation,
			"payload":         json.RawMessage(snapshot.ExecutionPlan.Payload),
			"idempotency_key": snapshot.ExecutionPlan.IdempotencyKey,
			"updated_at":      snapshot.ExecutionPlan.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	if snapshot.ExecutionState != nil {
		payload["execution"] = map[string]any{
			"last_execution_id":       snapshot.ExecutionState.LastExecutionID.String(),
			"last_execution_status":   snapshot.ExecutionState.LastExecutionStatus,
			"retryable":               snapshot.ExecutionState.Retryable,
			"retry_count":             snapshot.ExecutionState.RetryCount,
			"last_error":              snapshot.ExecutionState.LastError,
			"last_attempted_at":       snapshot.ExecutionState.LastAttemptedAt.UTC().Format(time.RFC3339),
			"verification_status":     snapshot.ExecutionState.VerificationResult.Status,
			"verification_summary":    snapshot.ExecutionState.VerificationResult.Summary,
			"verification_checked_at": snapshot.ExecutionState.VerificationResult.CheckedAt.UTC().Format(time.RFC3339),
		}
	}
	out, _ := json.Marshal(payload)
	return out
}

func (u *Usecases) syncTaskMemory(ctx context.Context, taskID uuid.UUID, reason string) {
	if u.taskMemory == nil {
		return
	}
	snapshot, err := u.loadTaskMemorySnapshot(ctx, taskID)
	if err != nil {
		slog.Warn("companion project task memory failed", "task_id", taskID.String(), "reason", reason, "error", err)
		return
	}
	summaryPayload, _ := json.Marshal(map[string]any{
		"projection_reason": reason,
		"status":            snapshot.Task.Status,
		"review_status":     snapshot.Task.ReviewStatus,
		"next_step":         nextTaskStep(snapshot),
	})
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindSummary, taskMemoryCurrentKey, buildTaskSummary(snapshot), summaryPayload); err != nil {
		slog.Warn("companion upsert task summary failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
	if err := u.taskMemory.UpsertTaskMemory(ctx, taskID, taskMemoryKindFacts, taskMemoryCurrentKey, "", buildTaskFactsPayload(snapshot, reason)); err != nil {
		slog.Warn("companion upsert task facts failed", "task_id", taskID.String(), "reason", reason, "error", err)
	}
}

func buildReviewSyncActionPayload(origin string, prev *domain.TaskReviewSyncState, next domain.TaskReviewSyncState, beforeStatus, afterStatus, event string) json.RawMessage {
	type syncSnapshot struct {
		ReviewRequestID string `json:"review_request_id,omitempty"`
		Status          string `json:"status,omitempty"`
		HTTPStatus      int    `json:"http_status,omitempty"`
		Error           string `json:"error,omitempty"`
	}
	payload := map[string]any{
		"origin":             origin,
		"task_status_before": beforeStatus,
		"task_status_after":  afterStatus,
	}
	if event != "" {
		payload["transition_event"] = event
	}
	current := syncSnapshot{
		Status:     next.LastReviewStatus,
		HTTPStatus: next.LastReviewHTTPStatus,
		Error:      next.LastError,
	}
	if next.ReviewRequestID != uuid.Nil {
		current.ReviewRequestID = next.ReviewRequestID.String()
	}
	payload["current"] = current
	if prev != nil {
		previous := syncSnapshot{
			Status:     prev.LastReviewStatus,
			HTTPStatus: prev.LastReviewHTTPStatus,
			Error:      prev.LastError,
		}
		if prev.ReviewRequestID != uuid.Nil {
			previous.ReviewRequestID = prev.ReviewRequestID.String()
		}
		payload["previous"] = previous
	}
	out, _ := json.Marshal(payload)
	return out
}

func (u *Usecases) latestReviewRequestIDForTask(ctx context.Context, taskID uuid.UUID, state *domain.TaskReviewSyncState) (uuid.UUID, error) {
	if state != nil && state.ReviewRequestID != uuid.Nil {
		return state.ReviewRequestID, nil
	}
	return u.repo.LatestProposeReviewRequestID(ctx, taskID)
}

func (u *Usecases) persistReviewSyncAction(ctx context.Context, taskID uuid.UUID, reviewRequestID uuid.UUID, origin string, prev *domain.TaskReviewSyncState, next domain.TaskReviewSyncState, beforeStatus, afterStatus, event string) {
	payload := buildReviewSyncActionPayload(origin, prev, next, beforeStatus, afterStatus, event)
	reviewRequestIDCopy := reviewRequestID
	if _, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:          taskID,
		ActionType:      TaskActionSyncReview,
		Payload:         payload,
		ReviewRequestID: &reviewRequestIDCopy,
	}); err != nil {
		slog.Warn("companion sync_review action failed", "task_id", taskID.String(), "review_request_id", reviewRequestID.String(), "error", err)
	}
}

func (u *Usecases) syncTaskWithReview(ctx context.Context, t domain.Task, origin string) (domain.Task, *domain.TaskReviewSyncState, error) {
	if t.Status != domain.TaskStatusWaitingForApproval {
		return t, nil, nil
	}

	var prevState *domain.TaskReviewSyncState
	currentState, err := u.repo.GetReviewSyncState(ctx, t.ID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.Task{}, nil, err
	}

	rid, err := u.latestReviewRequestIDForTask(ctx, t.ID, prevState)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return t, prevState, nil
		}
		return domain.Task{}, prevState, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskReviewSyncState{
		TaskID:          t.ID,
		ReviewRequestID: rid,
		LastCheckedAt:   now,
		NextCheckAt:     nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastReviewStatus = prevState.LastReviewStatus
		nextState.LastReviewHTTPStatus = prevState.LastReviewHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, st, gErr := u.nexus.GetRequest(ctx, rid.String())
	beforeStatus := t.Status
	appliedEvent := ""

	if gErr != nil {
		nextState.LastReviewHTTPStatus = st
		nextState.LastError = gErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertReviewSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if reviewSnapshotChanged(prevState, stateOut) {
			u.persistReviewSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "review_sync_error")
		}
		return domain.Task{}, &stateOut, fmt.Errorf("review get request: %w", gErr)
	}

	nextState.LastReviewHTTPStatus = st

	if st == http.StatusNotFound {
		nextState.LastError = "review request not found"
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upErr := u.repo.UpsertReviewSyncState(ctx, nextState)
		if upErr != nil {
			return domain.Task{}, prevState, upErr
		}
		if reviewSnapshotChanged(prevState, stateOut) {
			u.persistReviewSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
			u.syncTaskMemory(ctx, t.ID, "review_sync_not_found")
		}
		t.ReviewStatus = stateOut.LastReviewStatus
		t.ReviewLastCheckedAt = &stateOut.LastCheckedAt
		t.ReviewSyncError = stateOut.LastError
		return t, &stateOut, nil
	}
	if normalizedStatus := normalizeReviewStatus(sum.Status); normalizedStatus != "" {
		nextState.LastReviewStatus = normalizedStatus
	}

	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), 0)

	plan, planErr := u.getExecutionPlan(ctx, t.ID)
	if planErr != nil {
		return domain.Task{}, prevState, planErr
	}
	ev, apply := eventFromReviewRequestStatusWithExecutionPlan(sum.Status, plan != nil)
	if apply {
		appliedEvent = ev
		t, err = u.applyTaskEvent(ctx, t, ev)
		if err != nil {
			return domain.Task{}, prevState, err
		}
	}

	stateOut, upErr := u.repo.UpsertReviewSyncState(ctx, nextState)
	if upErr != nil {
		return domain.Task{}, prevState, upErr
	}
	if reviewSnapshotChanged(prevState, stateOut) || beforeStatus != t.Status {
		u.persistReviewSyncAction(ctx, t.ID, rid, origin, prevState, stateOut, beforeStatus, t.Status, appliedEvent)
		u.syncTaskMemory(ctx, t.ID, "review_sync")
	}
	t.ReviewStatus = stateOut.LastReviewStatus
	t.ReviewLastCheckedAt = &stateOut.LastCheckedAt
	t.ReviewSyncError = stateOut.LastError

	slog.Info("companion task synced from review",
		"task_id", t.ID.String(),
		"review_request_id", rid.String(),
		"review_status", stateOut.LastReviewStatus,
		"task_status", t.Status,
		"origin", origin,
	)
	return t, &stateOut, nil
}

func (u *Usecases) Investigate(ctx context.Context, taskID uuid.UUID, in InvestigateInput) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, err = u.applyTaskEvent(ctx, t, evInvestigate)
	if err != nil {
		return domain.Task{}, err
	}
	if in.Note != "" {
		_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
			TaskID:     taskID,
			AuthorType: "system",
			AuthorID:   "nexus_companion",
			Body:       in.Note,
		})
		if err != nil {
			return domain.Task{}, err
		}
	}
	u.syncTaskMemory(ctx, taskID, "investigate")
	return t, nil
}

type ProposeInput struct {
	Note           string
	TargetSystem   string
	TargetResource string
	SessionID      string
}

func (u *Usecases) Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, reviewclient.SubmitResponse, error) {
	var zeroA domain.TaskAction
	var zeroSub reviewclient.SubmitResponse
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, err
	}
	switch t.Status {
	case domain.TaskStatusDone, domain.TaskStatusFailed:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	case domain.TaskStatusWaitingForApproval:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	case domain.TaskStatusNew, domain.TaskStatusInvestigating:
		// ok
	default:
		return domain.Task{}, zeroA, zeroSub, ErrInvalidTaskState
	}

	payload := map[string]any{
		"note": in.Note,
	}
	pj, _ := json.Marshal(payload)
	action, err := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:     taskID,
		ActionType: TaskActionPropose,
		Payload:    pj,
	})
	if err != nil {
		return domain.Task{}, zeroA, zeroSub, err
	}

	nexusMeta := map[string]any{
		"origin":      "companion",
		"task_id":     taskID.String(),
		"proposed_by": CompanionRequesterID,
		"human_owner": t.CreatedBy,
		"action_id":   action.ID.String(),
	}
	if in.SessionID != "" {
		nexusMeta["session_id"] = in.SessionID
	}
	params := map[string]any{"nexus": nexusMeta}

	ctxJSON := map[string]any{
		"task_title": t.Title,
		"task_goal":  t.Goal,
		"note":       in.Note,
	}
	ctxStr, _ := json.Marshal(ctxJSON)

	reason := t.Title
	if in.Note != "" {
		reason = t.Title + ": " + in.Note
	}

	idem := fmt.Sprintf("companion-propose-%s", action.ID.String())
	submitBody := reviewclient.SubmitRequestBody{
		RequesterType:  CompanionRequesterType,
		RequesterID:    CompanionRequesterID,
		RequesterName:  CompanionRequesterName,
		ActionType:     ActionTypePropose,
		TargetSystem:   in.TargetSystem,
		TargetResource: in.TargetResource,
		Params:         params,
		Reason:         reason,
		Context:        string(ctxStr),
	}

	submitOut, subErr := u.nexus.SubmitRequest(ctx, idem, submitBody)
	if subErr != nil {
		slog.Warn("companion propose review submit failed",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"error", subErr,
		)
		_ = u.repo.UpdateActionReviewResult(ctx, action.ID, nil, subErr.Error())
		t2, ge := u.repo.GetTaskByID(ctx, taskID)
		if ge != nil {
			return domain.Task{}, action, zeroSub, ge
		}
		return t2, action, zeroSub, fmt.Errorf("review submit: %w", subErr)
	}
	reqUUID, perr := uuid.Parse(submitOut.RequestID)
	if perr != nil {
		_ = u.repo.UpdateActionReviewResult(ctx, action.ID, nil, "invalid request_id from review")
		return domain.Task{}, action, zeroSub, fmt.Errorf("parse request_id: %w", perr)
	}
	if err := u.repo.UpdateActionReviewResult(ctx, action.ID, &reqUUID, ""); err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	now := time.Now().UTC()
	state, err := u.repo.UpsertReviewSyncState(ctx, domain.TaskReviewSyncState{
		TaskID:               taskID,
		ReviewRequestID:      reqUUID,
		LastReviewStatus:     normalizeReviewStatus(submitOut.Status),
		LastReviewHTTPStatus: http.StatusCreated,
		LastCheckedAt:        now,
		LastError:            "",
		ConsecutiveFailures:  0,
		NextCheckAt:          nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), 0),
	})
	if err != nil {
		return domain.Task{}, action, zeroSub, err
	}

	plan, planErr := u.getExecutionPlan(ctx, taskID)
	if planErr != nil {
		return domain.Task{}, action, zeroSub, planErr
	}
	ev, evErr := eventFromSubmitResponseWithExecutionPlan(submitOut, plan != nil)
	if evErr != nil {
		slog.Error("companion propose unexpected review status",
			"task_id", taskID.String(),
			"action_id", action.ID.String(),
			"review_status", submitOut.Status,
			"error", evErr,
		)
		return domain.Task{}, action, submitOut, evErr
	}
	t, err = u.applyTaskEvent(ctx, t, ev)
	if err != nil {
		return domain.Task{}, action, submitOut, err
	}
	t.ReviewStatus = state.LastReviewStatus
	t.ReviewLastCheckedAt = &state.LastCheckedAt
	t.ReviewSyncError = state.LastError
	action.ReviewRequestID = &reqUUID
	slog.Info("companion propose submitted to review",
		"task_id", taskID.String(),
		"action_id", action.ID.String(),
		"review_request_id", reqUUID.String(),
		"review_decision", submitOut.Decision,
		"review_status", submitOut.Status,
		"task_status", t.Status,
	)
	u.syncTaskMemory(ctx, taskID, "propose")
	return t, action, submitOut, nil
}

type SetExecutionPlanInput struct {
	ConnectorID    uuid.UUID
	Operation      string
	Payload        json.RawMessage
	IdempotencyKey string
}

func (u *Usecases) SetExecutionPlan(ctx context.Context, taskID uuid.UUID, in SetExecutionPlanInput) (domain.TaskExecutionPlan, error) {
	if in.ConnectorID == uuid.Nil {
		return domain.TaskExecutionPlan{}, fmt.Errorf("connector_id is required")
	}
	if in.Operation == "" {
		return domain.TaskExecutionPlan{}, fmt.Errorf("operation is required")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}
	switch t.Status {
	case domain.TaskStatusDone, domain.TaskStatusFailed, domain.TaskStatusExecuting, domain.TaskStatusVerifying:
		return domain.TaskExecutionPlan{}, ErrInvalidTaskState
	}

	if u.executor != nil {
		if _, err := u.executor.GetConnector(ctx, in.ConnectorID); err != nil {
			return domain.TaskExecutionPlan{}, fmt.Errorf("get connector: %w", err)
		}
	}

	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage(`{}`)
	}

	var prevPlan *domain.TaskExecutionPlan
	currentPlan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err == nil {
		currentCopy := currentPlan
		prevPlan = &currentCopy
	} else if !domainerr.IsNotFound(err) {
		return domain.TaskExecutionPlan{}, err
	}

	plan, err := u.repo.UpsertExecutionPlan(ctx, domain.TaskExecutionPlan{
		TaskID:         taskID,
		ConnectorID:    in.ConnectorID,
		Operation:      in.Operation,
		Payload:        in.Payload,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return domain.TaskExecutionPlan{}, err
	}

	if executionPlanChanged(prevPlan, plan) {
		payload, _ := json.Marshal(map[string]any{
			"connector_id":    plan.ConnectorID.String(),
			"operation":       plan.Operation,
			"payload":         json.RawMessage(plan.Payload),
			"idempotency_key": plan.IdempotencyKey,
		})
		if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
			TaskID:     taskID,
			ActionType: TaskActionSetExecutionPlan,
			Payload:    payload,
		}); insertErr != nil {
			slog.Warn("companion set execution plan action failed", "task_id", taskID.String(), "error", insertErr)
		}
	}
	u.syncTaskMemory(ctx, taskID, "set_execution_plan")

	return plan, nil
}

type ExecuteTaskOutput struct {
	Task           domain.Task
	Plan           domain.TaskExecutionPlan
	Execution      connectordomain.ExecutionResult
	ExecutionState domain.TaskExecutionState
}

func buildConnectorExecutionPayload(result connectordomain.ExecutionResult) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"id":              result.ID.String(),
		"connector_id":    result.ConnectorID.String(),
		"org_id":          result.OrgID,
		"actor_id":        result.ActorID,
		"operation":       result.Operation,
		"status":          result.Status,
		"external_ref":    result.ExternalRef,
		"payload":         json.RawMessage(result.Payload),
		"result":          json.RawMessage(result.ResultJSON),
		"evidence":        json.RawMessage(result.EvidenceJSON),
		"error_message":   result.ErrorMessage,
		"retryable":       result.Retryable,
		"duration_ms":     result.DurationMS,
		"idempotency_key": result.IdempotencyKey,
		"review_request_id": func() string {
			if result.ReviewRequestID != nil {
				return result.ReviewRequestID.String()
			}
			return ""
		}(),
	})
	return payload
}

func buildVerificationPayload(result connectordomain.ExecutionResult, verification domain.TaskVerificationResult) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"execution_id":        result.ID.String(),
		"execution_status":    result.Status,
		"verification_status": verification.Status,
		"summary":             verification.Summary,
		"checked_at":          verification.CheckedAt,
		"details":             json.RawMessage(verification.Details),
		"retryable":           result.Retryable,
	})
	return payload
}

func hasResultPayload(result json.RawMessage) bool {
	trimmed := bytes.TrimSpace(result)
	if len(trimmed) == 0 {
		return false
	}
	switch string(trimmed) {
	case "{}", "null", "[]":
		return false
	default:
		return true
	}
}

func hasVerificationEvidence(result connectordomain.ExecutionResult) bool {
	if strings.TrimSpace(result.ExternalRef) != "" {
		return true
	}
	return hasResultPayload(result.ResultJSON)
}

func verifyExecutionResult(result connectordomain.ExecutionResult) domain.TaskVerificationResult {
	checkedAt := time.Now().UTC()
	details, _ := json.Marshal(map[string]any{
		"execution_status":       result.Status,
		"external_ref_present":   strings.TrimSpace(result.ExternalRef) != "",
		"result_payload_present": hasResultPayload(result.ResultJSON),
		"retryable":              result.Retryable,
		"error_message":          result.ErrorMessage,
	})

	switch result.Status {
	case connectordomain.ExecSuccess:
		if hasVerificationEvidence(result) {
			return domain.TaskVerificationResult{
				Status:    domain.VerificationStatusVerified,
				Summary:   "connector execution verified from returned evidence",
				CheckedAt: checkedAt,
				Details:   details,
			}
		}
		return domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   "verification failed: connector returned no evidence",
			CheckedAt: checkedAt,
			Details:   details,
		}
	default:
		summary := "execution failed before verification"
		if result.ErrorMessage != "" {
			summary = result.ErrorMessage
		}
		return domain.TaskVerificationResult{
			Status:    domain.VerificationStatusFailed,
			Summary:   summary,
			CheckedAt: checkedAt,
			Details:   details,
		}
	}
}

func buildExecutionState(prev *domain.TaskExecutionState, taskID uuid.UUID, result connectordomain.ExecutionResult, verification domain.TaskVerificationResult, isRetry bool) domain.TaskExecutionState {
	retryCount := 0
	createdAt := time.Now().UTC()
	if prev != nil {
		retryCount = prev.RetryCount
		createdAt = prev.CreatedAt
	}
	if isRetry {
		retryCount++
	}
	lastError := result.ErrorMessage
	if lastError == "" && verification.Status == domain.VerificationStatusFailed {
		lastError = verification.Summary
	}
	retryable := result.Retryable
	if verification.Status == domain.VerificationStatusFailed {
		retryable = true
	}
	if verification.Status == domain.VerificationStatusVerified {
		retryable = false
		lastError = ""
	}
	return domain.TaskExecutionState{
		TaskID:              taskID,
		LastExecutionID:     result.ID,
		LastExecutionStatus: result.Status,
		Retryable:           retryable,
		RetryCount:          retryCount,
		LastError:           lastError,
		LastAttemptedAt:     result.CreatedAt,
		VerificationResult:  verification,
		CreatedAt:           createdAt,
	}
}

func defaultExecutionIdempotencyKey(taskID uuid.UUID, reviewRequestID *uuid.UUID) string {
	if reviewRequestID != nil && *reviewRequestID != uuid.Nil {
		return fmt.Sprintf("task-execute-%s-%s", taskID.String(), reviewRequestID.String())
	}
	return fmt.Sprintf("task-execute-%s", taskID.String())
}

func executionActorID(t domain.Task) string {
	if actor := strings.TrimSpace(t.AssignedTo); actor != "" {
		return actor
	}
	if actor := strings.TrimSpace(t.CreatedBy); actor != "" {
		return actor
	}
	return CompanionRequesterID
}

func (u *Usecases) refreshReviewSnapshot(ctx context.Context, taskID uuid.UUID, origin string) (*domain.TaskReviewSyncState, error) {
	var prevState *domain.TaskReviewSyncState
	currentState, err := u.repo.GetReviewSyncState(ctx, taskID)
	if err == nil {
		stateCopy := currentState
		prevState = &stateCopy
	} else if !domainerr.IsNotFound(err) {
		return nil, err
	}

	reviewRequestID, err := u.latestReviewRequestIDForTask(ctx, taskID, prevState)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nextState := domain.TaskReviewSyncState{
		TaskID:          taskID,
		ReviewRequestID: reviewRequestID,
		LastCheckedAt:   now,
		NextCheckAt:     nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), 0),
	}
	if prevState != nil {
		nextState.CreatedAt = prevState.CreatedAt
		nextState.LastReviewStatus = prevState.LastReviewStatus
		nextState.LastReviewHTTPStatus = prevState.LastReviewHTTPStatus
		nextState.LastError = prevState.LastError
		nextState.ConsecutiveFailures = prevState.ConsecutiveFailures
	}

	sum, statusCode, getErr := u.nexus.GetRequest(ctx, reviewRequestID.String())
	if getErr != nil {
		nextState.LastReviewHTTPStatus = statusCode
		nextState.LastError = getErr.Error()
		nextState.ConsecutiveFailures++
		nextState.NextCheckAt = nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), nextState.ConsecutiveFailures)
		stateOut, upsertErr := u.repo.UpsertReviewSyncState(ctx, nextState)
		if upsertErr != nil {
			return nil, upsertErr
		}
		return &stateOut, fmt.Errorf("review get request: %w", getErr)
	}

	nextState.LastReviewHTTPStatus = statusCode
	nextState.LastReviewStatus = normalizeReviewStatus(sum.Status)
	nextState.LastError = ""
	nextState.ConsecutiveFailures = 0
	nextState.NextCheckAt = nextReviewSyncAt(now, u.reviewSyncIntervalOrDefault(), 0)

	stateOut, upsertErr := u.repo.UpsertReviewSyncState(ctx, nextState)
	if upsertErr != nil {
		return nil, upsertErr
	}
	if reviewSnapshotChanged(prevState, stateOut) {
		u.persistReviewSyncAction(ctx, taskID, reviewRequestID, origin, prevState, stateOut, "", "", "")
	}
	return &stateOut, nil
}

func (u *Usecases) runTaskExecution(ctx context.Context, t domain.Task, plan domain.TaskExecutionPlan, prevState *domain.TaskExecutionState, startEvent string) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput

	t, err := u.applyTaskEvent(ctx, t, startEvent)
	if err != nil {
		return out, err
	}

	var reviewRequestID *uuid.UUID
	if syncState, syncErr := u.repo.GetReviewSyncState(ctx, t.ID); syncErr == nil && syncState.ReviewRequestID != uuid.Nil {
		reviewRequestID = &syncState.ReviewRequestID
	}
	idempotencyKey := plan.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = defaultExecutionIdempotencyKey(t.ID, reviewRequestID)
	}

	result, execErr := u.executor.Execute(ctx, connectordomain.ExecutionSpec{
		ConnectorID:     plan.ConnectorID,
		OrgID:           t.OrgID,
		ActorID:         executionActorID(t),
		Operation:       plan.Operation,
		Payload:         plan.Payload,
		IdempotencyKey:  idempotencyKey,
		TaskID:          &t.ID,
		ReviewRequestID: reviewRequestID,
	})
	if execErr != nil {
		result = connectordomain.ExecutionResult{
			ID:              uuid.New(),
			ConnectorID:     plan.ConnectorID,
			OrgID:           t.OrgID,
			ActorID:         executionActorID(t),
			Operation:       plan.Operation,
			Status:          connectordomain.ExecFailure,
			Payload:         plan.Payload,
			ResultJSON:      json.RawMessage(`{}`),
			ErrorMessage:    execErr.Error(),
			Retryable:       true,
			IdempotencyKey:  idempotencyKey,
			TaskID:          &t.ID,
			ReviewRequestID: reviewRequestID,
			CreatedAt:       time.Now().UTC(),
		}
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	}
	u.reportExecutionToReview(ctx, reviewRequestID, result)

	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:          t.ID,
		ActionType:      TaskActionExecuteConnector,
		Payload:         buildConnectorExecutionPayload(result),
		ReviewRequestID: reviewRequestID,
		ErrorMessage:    result.ErrorMessage,
	}); insertErr != nil {
		slog.Warn("companion execute connector action failed", "task_id", t.ID.String(), "error", insertErr)
	}

	artifactKind := TaskArtifactConnectorExecution
	if result.Status != connectordomain.ExecSuccess {
		artifactKind = TaskArtifactExecutionError
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  t.ID,
		Kind:    artifactKind,
		URI:     result.ExternalRef,
		Payload: buildConnectorExecutionPayload(result),
	}); artifactErr != nil {
		slog.Warn("companion execute connector artifact failed", "task_id", t.ID.String(), "error", artifactErr)
	}

	verification := verifyExecutionResult(result)
	if _, verifyErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:          t.ID,
		ActionType:      TaskActionVerifyExecution,
		Payload:         buildVerificationPayload(result, verification),
		ReviewRequestID: reviewRequestID,
		ErrorMessage: func() string {
			if verification.Status == domain.VerificationStatusFailed {
				return verification.Summary
			}
			return ""
		}(),
	}); verifyErr != nil {
		slog.Warn("companion verify execution action failed", "task_id", t.ID.String(), "error", verifyErr)
	}
	if _, artifactErr := u.repo.InsertArtifact(ctx, domain.TaskArtifact{
		TaskID:  t.ID,
		Kind:    TaskArtifactExecutionVerification,
		URI:     result.ExternalRef,
		Payload: buildVerificationPayload(result, verification),
	}); artifactErr != nil {
		slog.Warn("companion verify execution artifact failed", "task_id", t.ID.String(), "error", artifactErr)
	}

	executionState, stateErr := u.repo.UpsertExecutionState(ctx, buildExecutionState(prevState, t.ID, result, verification, startEvent == evRetryExecution))
	if stateErr != nil {
		return out, stateErr
	}

	switch {
	case result.Status == connectordomain.ExecSuccess && verification.Status == domain.VerificationStatusVerified:
		t, err = u.applyTaskEvent(ctx, t, evExecutionSucceeded)
		if err != nil {
			return out, err
		}
		t, err = u.applyTaskEvent(ctx, t, evExecutionVerified)
		if err != nil {
			return out, err
		}
	case result.Status == connectordomain.ExecSuccess && verification.Status == domain.VerificationStatusFailed:
		t, err = u.applyTaskEvent(ctx, t, evExecutionSucceeded)
		if err != nil {
			return out, err
		}
		t, err = u.applyTaskEvent(ctx, t, evExecutionFailed)
		if err != nil {
			return out, err
		}
	default:
		t, err = u.applyTaskEvent(ctx, t, evExecutionFailed)
		if err != nil {
			return out, err
		}
	}

	t.ReviewStatus = normalizeReviewStatus(t.ReviewStatus)
	out.Task = t
	out.Plan = plan
	out.Execution = result
	out.ExecutionState = executionState
	u.syncTaskMemory(ctx, t.ID, "execution")
	return out, nil
}

func (u *Usecases) reportExecutionToReview(ctx context.Context, reviewRequestID *uuid.UUID, result connectordomain.ExecutionResult) {
	if u.review == nil || reviewRequestID == nil || *reviewRequestID == uuid.Nil {
		return
	}
	success := result.Status == connectordomain.ExecSuccess
	var resultPayload map[string]any
	if len(result.ResultJSON) > 0 {
		if err := json.Unmarshal(result.ResultJSON, &resultPayload); err != nil {
			resultPayload = map[string]any{"raw": string(result.ResultJSON)}
		}
	}
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	resultPayload["connector_execution_id"] = result.ID.String()
	resultPayload["connector_id"] = result.ConnectorID.String()
	resultPayload["operation"] = result.Operation
	resultPayload["external_ref"] = result.ExternalRef
	resultPayload["org_id"] = result.OrgID
	resultPayload["actor_id"] = result.ActorID
	if len(result.EvidenceJSON) > 0 {
		resultPayload["evidence"] = json.RawMessage(result.EvidenceJSON)
	}
	status, err := u.nexus.ReportResult(ctx, reviewRequestID.String(), success, resultPayload, result.DurationMS, result.ErrorMessage)
	if err != nil || status >= http.StatusBadRequest {
		slog.Warn("report execution to review failed",
			"review_request_id", reviewRequestID.String(),
			"status", status,
			"error", err)
	}
}

func (u *Usecases) ExecuteTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, fmt.Errorf("execution plan is required")
		}
		return out, err
	}

	if t.Status == domain.TaskStatusWaitingForApproval {
		syncedTask, state, syncErr := u.syncTaskWithReview(ctx, t, "execute")
		if state != nil {
			syncedTask.ReviewStatus = state.LastReviewStatus
			syncedTask.ReviewLastCheckedAt = &state.LastCheckedAt
			syncedTask.ReviewSyncError = state.LastError
		}
		if syncErr != nil {
			return out, syncErr
		}
		t = syncedTask
	}

	if !isApprovedReviewStatus(t.ReviewStatus) || t.Status != domain.TaskStatusWaitingForInput {
		return out, ErrInvalidTaskState
	}

	prevState, stateErr := u.getExecutionState(ctx, taskID)
	if stateErr != nil {
		return out, stateErr
	}
	return u.runTaskExecution(ctx, t, plan, prevState, evStartExecution)
}

func (u *Usecases) RetryTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error) {
	var out ExecuteTaskOutput
	if u.executor == nil {
		return out, fmt.Errorf("task execution is not configured")
	}

	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return out, err
	}
	plan, err := u.repo.GetExecutionPlan(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, fmt.Errorf("execution plan is required")
		}
		return out, err
	}
	state, err := u.repo.GetExecutionState(ctx, taskID)
	if err != nil {
		if domainerr.IsNotFound(err) {
			return out, ErrInvalidTaskState
		}
		return out, err
	}
	if t.Status != domain.TaskStatusFailed || !state.Retryable {
		return out, ErrInvalidTaskState
	}

	snapshot, snapshotErr := u.refreshReviewSnapshot(ctx, taskID, "retry")
	if snapshotErr != nil {
		return out, snapshotErr
	}
	t.ReviewStatus = snapshot.LastReviewStatus
	t.ReviewLastCheckedAt = &snapshot.LastCheckedAt
	t.ReviewSyncError = snapshot.LastError
	if !isApprovedReviewStatus(snapshot.LastReviewStatus) {
		return out, ErrInvalidTaskState
	}

	payload, _ := json.Marshal(map[string]any{
		"retry_count_before":    state.RetryCount,
		"last_execution_status": state.LastExecutionStatus,
		"last_error":            state.LastError,
	})
	reviewRequestID := snapshot.ReviewRequestID
	if _, insertErr := u.repo.InsertAction(ctx, domain.TaskAction{
		TaskID:          taskID,
		ActionType:      TaskActionRetryExecution,
		Payload:         payload,
		ReviewRequestID: &reviewRequestID,
	}); insertErr != nil {
		slog.Warn("companion retry execution action failed", "task_id", taskID.String(), "error", insertErr)
	}

	return u.runTaskExecution(ctx, t, plan, &state, evRetryExecution)
}

// SyncTaskReview consulta Review y aplica transición si el request ya resolvió (tareas en espera).
func (u *Usecases) SyncTaskReview(ctx context.Context, taskID uuid.UUID) (domain.Task, error) {
	t, err := u.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	t, state, err := u.syncTaskWithReview(ctx, t, "manual")
	if state != nil {
		t.ReviewStatus = state.LastReviewStatus
		t.ReviewLastCheckedAt = &state.LastCheckedAt
		t.ReviewSyncError = state.LastError
	}
	if err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

// SyncPendingReviewTasks sincroniza un lote de tareas en waiting_for_approval.
func (u *Usecases) SyncPendingReviewTasks(ctx context.Context, limit int) {
	if limit <= 0 {
		limit = 50
	}
	list, err := u.repo.ListTasksPendingReviewSync(ctx, time.Now().UTC(), limit)
	if err != nil {
		slog.Error("companion sync list waiting tasks", "error", err)
		return
	}
	for _, item := range list {
		if _, _, sErr := u.syncTaskWithReview(ctx, item, "loop"); sErr != nil {
			slog.Warn("companion sync task failed", "task_id", item.ID.String(), "error", sErr)
		}
	}
}

// RunReviewSyncLoop ejecuta SyncPendingReviewTasks periódicamente hasta que ctx termina.
func (u *Usecases) RunReviewSyncLoop(ctx context.Context, interval time.Duration, batch int) {
	if batch <= 0 {
		return
	}
	worker.RunPeriodic(ctx, interval, "review-sync", func(c context.Context) {
		runCtx, cancel := context.WithTimeout(c, 2*time.Minute)
		u.SyncPendingReviewTasks(runCtx, batch)
		cancel()
	})
}

// ErrInvalidStatus para handlers.
func IsNotFound(err error) bool {
	return domainerr.IsNotFound(err)
}

// IsInvalidTaskState indica conflicto de estado (FSM / reglas de negocio).
func IsInvalidTaskState(err error) bool {
	return errors.Is(err, ErrInvalidTaskState)
}

// NotifyAlert implementa watchers.ChatNotifier.
// Crea una tarea-alerta y agrega el mensaje como sistema.
func (u *Usecases) NotifyAlert(ctx context.Context, orgID, message string) error {
	title := message
	if len(title) > 80 {
		title = title[:80]
	}
	t, err := u.repo.CreateTask(ctx, domain.Task{
		Title:     title,
		Status:    domain.TaskStatusNew,
		Priority:  "high",
		CreatedBy: orgID,
		Channel:   "watcher",
	})
	if err != nil {
		return fmt.Errorf("create alert task: %w", err)
	}
	_, err = u.repo.InsertMessage(ctx, domain.TaskMessage{
		TaskID:     t.ID,
		AuthorType: "system",
		AuthorID:   "nexus-watcher",
		Body:       message,
	})
	if err != nil {
		return fmt.Errorf("insert alert message: %w", err)
	}
	slog.Info("watcher alert pushed to chat", "task_id", t.ID, "org_id", orgID)
	return nil
}
