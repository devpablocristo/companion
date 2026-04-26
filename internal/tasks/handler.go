package tasks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/devpablocristo/core/http/go/httpjson"
	"github.com/google/uuid"

	"github.com/devpablocristo/core/governance/go/reviewclient"
	tasksdto "github.com/devpablocristo/companion/internal/tasks/handler/dto"
	domain "github.com/devpablocristo/companion/internal/tasks/usecases/domain"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type taskUsecase interface {
	Create(ctx context.Context, in CreateTaskInput) (domain.Task, error)
	List(ctx context.Context, limit int) ([]domain.Task, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Task, error)
	GetDetail(ctx context.Context, id uuid.UUID) (TaskDetail, error)
	AddMessage(ctx context.Context, taskID uuid.UUID, in AddMessageInput) (domain.TaskMessage, error)
	Investigate(ctx context.Context, taskID uuid.UUID, in InvestigateInput) (domain.Task, error)
	Propose(ctx context.Context, taskID uuid.UUID, in ProposeInput) (domain.Task, domain.TaskAction, reviewclient.SubmitResponse, error)
	SetExecutionPlan(ctx context.Context, taskID uuid.UUID, in SetExecutionPlanInput) (domain.TaskExecutionPlan, error)
	ExecuteTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error)
	RetryTask(ctx context.Context, taskID uuid.UUID) (ExecuteTaskOutput, error)
	SyncTaskReview(ctx context.Context, taskID uuid.UUID) (domain.Task, error)
	Chat(ctx context.Context, in ChatInput) (ChatResult, error)
}

type Handler struct {
	uc taskUsecase
}

func NewHandler(uc taskUsecase) *Handler {
	return &Handler{uc: uc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/tasks", h.create)
	mux.HandleFunc("GET /v1/tasks", h.list)
	mux.HandleFunc("GET /v1/tasks/{id}", h.getByID)
	mux.HandleFunc("POST /v1/tasks/{id}/message", h.addMessage)
	mux.HandleFunc("POST /v1/tasks/{id}/investigate", h.investigate)
	mux.HandleFunc("POST /v1/tasks/{id}/propose", h.propose)
	mux.HandleFunc("PUT /v1/tasks/{id}/execution-plan", h.setExecutionPlan)
	mux.HandleFunc("POST /v1/tasks/{id}/execute", h.execute)
	mux.HandleFunc("POST /v1/tasks/{id}/retry", h.retry)
	mux.HandleFunc("POST /v1/tasks/{id}/sync", h.syncReview)
	mux.HandleFunc("POST /v1/chat", h.chat)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body tasksdto.CreateTaskRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Title == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "title is required")
		return
	}
	t, err := h.uc.Create(r.Context(), CreateTaskInput{
		OrgID:       principalOrgID(r),
		Title:       body.Title,
		Goal:        body.Goal,
		Priority:    body.Priority,
		CreatedBy:   body.CreatedBy,
		AssignedTo:  body.AssignedTo,
		Channel:     body.Channel,
		Summary:     body.Summary,
		ContextJSON: body.ContextJSON,
	})
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, tasksdto.TaskToResponse(t))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	limit := defaultListLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= maxListLimit {
			limit = n
		}
	}
	list, err := h.uc.List(r.Context(), limit)
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "list tasks failed")
		return
	}
	out := make([]tasksdto.TaskResponse, 0, len(list))
	for _, t := range list {
		if !canAccessTaskOrg(r, t) {
			continue
		}
		out = append(out, tasksdto.TaskToResponse(t))
	}
	httpjson.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksRead) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	detail, err := h.uc.GetDetail(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return
	}
	if !canAccessTaskOrg(r, detail.Task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return
	}
	resp := tasksdto.TaskDetailResponse{
		Task:                 tasksdto.TaskToResponse(detail.Task),
		Messages:             make([]tasksdto.MessageResponse, 0, len(detail.Messages)),
		Actions:              make([]tasksdto.ActionResponse, 0, len(detail.Actions)),
		Artifacts:            make([]tasksdto.ArtifactResponse, 0, len(detail.Artifacts)),
		LinkedReviewRequests: make([]tasksdto.LinkedReviewRequestResponse, 0, len(detail.LinkedReviewRequests)),
	}
	for _, m := range detail.Messages {
		resp.Messages = append(resp.Messages, tasksdto.MessageToResponse(m))
	}
	for _, a := range detail.Actions {
		resp.Actions = append(resp.Actions, tasksdto.ActionToResponse(a))
	}
	for _, ar := range detail.Artifacts {
		resp.Artifacts = append(resp.Artifacts, tasksdto.ArtifactToResponse(ar))
	}
	for _, lr := range detail.LinkedReviewRequests {
		resp.LinkedReviewRequests = append(resp.LinkedReviewRequests, tasksdto.LinkedReviewRequestResponse{
			ActionID: lr.ActionID.String(),
			Request:  lr.Request,
		})
	}
	if detail.ReviewSync != nil {
		resp.ReviewSync = tasksdto.ReviewSyncToResponse(*detail.ReviewSync)
	}
	if detail.ExecutionPlan != nil {
		resp.ExecutionPlan = tasksdto.ExecutionPlanToResponse(*detail.ExecutionPlan)
	}
	if detail.ExecutionState != nil {
		resp.ExecutionState = tasksdto.ExecutionStateToResponse(*detail.ExecutionState)
	}
	httpjson.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) addMessage(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.AddMessageRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	m, err := h.uc.AddMessage(r.Context(), id, AddMessageInput{
		AuthorType: body.AuthorType,
		AuthorID:   body.AuthorID,
		Body:       body.Body,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusCreated, tasksdto.MessageToResponse(m))
}

func (h *Handler) investigate(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body tasksdto.InvestigateRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
			return
		}
	}
	t, err := h.uc.Investigate(r.Context(), id, InvestigateInput{Note: body.Note})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "investigate failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskToResponse(t))
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.ProposeRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	t, action, sub, err := h.uc.Propose(r.Context(), id, ProposeInput{
		Note:           body.Note,
		TargetSystem:   body.TargetSystem,
		TargetResource: body.TargetResource,
		SessionID:      body.SessionID,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		if strings.HasPrefix(err.Error(), "review submit:") && t.ID != uuid.Nil {
			httpjson.WriteJSON(w, http.StatusBadGateway, map[string]any{
				"code":    "REVIEW_SUBMIT_FAILED",
				"message": "review request failed",
				"task":    tasksdto.TaskToResponse(t),
				"action":  tasksdto.ActionToResponse(action),
			})
			return
		}
		httpjson.WriteFlatInternalError(w, err, "propose failed")
		return
	}
	var pr tasksdto.ProposeResponse
	pr.Task = tasksdto.TaskToResponse(t)
	pr.Action = tasksdto.ActionToResponse(action)
	pr.ReviewSubmit.RequestID = sub.RequestID
	pr.ReviewSubmit.Decision = sub.Decision
	pr.ReviewSubmit.Status = sub.Status
	pr.ReviewSubmit.RiskLevel = sub.RiskLevel
	pr.ReviewSubmit.DecisionReason = sub.DecisionReason
	httpjson.WriteJSON(w, http.StatusOK, pr)
}

func (h *Handler) syncReview(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	t, err := h.uc.SyncTaskReview(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "sync failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.TaskToResponse(t))
}

func (h *Handler) setExecutionPlan(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	var body tasksdto.SetExecutionPlanRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	connectorID, err := uuid.Parse(body.ConnectorID)
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid connector_id")
		return
	}
	plan, err := h.uc.SetExecutionPlan(r.Context(), id, SetExecutionPlanInput{
		ConnectorID:    connectorID,
		Operation:      body.Operation,
		Payload:        body.Payload,
		IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecutionPlanToResponse(plan))
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	out, err := h.uc.ExecuteTask(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecuteTaskResponse{
		Task:           tasksdto.TaskToResponse(out.Task),
		Plan:           *tasksdto.ExecutionPlanToResponse(out.Plan),
		Execution:      tasksdto.ExecutionResultToResponse(out.Execution),
		ExecutionState: tasksdto.ExecutionStateToResponse(out.ExecutionState),
	})
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	if !requireScope(w, r, scopeCompanionConnectorsExecute) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	if !h.authorizeTaskOrg(w, r, id) {
		return
	}
	out, err := h.uc.RetryTask(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		if IsInvalidTaskState(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "CONFLICT", "invalid task state")
			return
		}
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", err.Error())
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ExecuteTaskResponse{
		Task:           tasksdto.TaskToResponse(out.Task),
		Plan:           *tasksdto.ExecutionPlanToResponse(out.Plan),
		Execution:      tasksdto.ExecutionResultToResponse(out.Execution),
		ExecutionState: tasksdto.ExecutionStateToResponse(out.ExecutionState),
	})
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	if !requireScope(w, r, scopeCompanionTasksWrite) {
		return
	}
	var body tasksdto.ChatRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Message == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "message is required")
		return
	}

	var taskID *uuid.UUID
	if body.TaskID != "" {
		parsed, err := uuid.Parse(body.TaskID)
		if err != nil {
			httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid task_id")
			return
		}
		if !h.authorizeTaskOrg(w, r, parsed) {
			return
		}
		taskID = &parsed
	}

	// Extraer identidad: header X-User-ID / X-Org-ID (inyectados por el frontend o gateway)
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if userID == "" {
		userID = "subscriber"
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))

	result, err := h.uc.Chat(r.Context(), ChatInput{
		TaskID:  taskID,
		UserID:  userID,
		OrgID:   orgID,
		Message: body.Message,
		Channel: body.Channel,
	})
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "chat failed")
		return
	}

	msgs := make([]tasksdto.MessageResponse, 0, len(result.Messages))
	for _, m := range result.Messages {
		msgs = append(msgs, tasksdto.MessageToResponse(m))
	}
	httpjson.WriteJSON(w, http.StatusOK, tasksdto.ChatResponse{
		Task:     tasksdto.TaskToResponse(result.Task),
		Messages: msgs,
	})
}

func (h *Handler) authorizeTaskOrg(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	task, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
			return false
		}
		httpjson.WriteFlatInternalError(w, err, "get task failed")
		return false
	}
	if !canAccessTaskOrg(r, task) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "task org is not allowed for this principal")
		return false
	}
	return true
}
