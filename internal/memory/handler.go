package memory

import (
	"context"
	"net/http"
	"strings"

	"github.com/devpablocristo/core/http/go/httpjson"
	"github.com/google/uuid"

	"github.com/devpablocristo/companion/internal/memory/handler/dto"
	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

const (
	defaultListLimit = 50
)

type memoryUsecase interface {
	Upsert(ctx context.Context, in UpsertInput) (domain.MemoryEntry, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MemoryEntry, error)
	Find(ctx context.Context, q FindQuery) ([]domain.MemoryEntry, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// Handler HTTP adapter para memoria operativa.
type Handler struct {
	uc memoryUsecase
}

// NewHandler crea un nuevo handler de memoria.
func NewHandler(uc memoryUsecase) *Handler {
	return &Handler{uc: uc}
}

// Register registra las rutas de memoria en el mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/memory", h.upsert)
	mux.HandleFunc("GET /v1/memory/{id}", h.get)
	mux.HandleFunc("GET /v1/memory", h.find)
	mux.HandleFunc("DELETE /v1/memory/{id}", h.delete)
}

func (h *Handler) upsert(w http.ResponseWriter, r *http.Request) {
	var body dto.UpsertMemoryRequest
	if err := httpjson.DecodeJSON(r, &body); err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid json")
		return
	}
	if body.Kind == "" || body.ScopeType == "" || body.ScopeID == "" || body.Key == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "kind, scope_type, scope_id, and key are required")
		return
	}
	if !authorizeMemoryScope(r, domain.ScopeType(body.ScopeType), body.ScopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}

	entry, err := h.uc.Upsert(r.Context(), UpsertInput{
		Kind:        domain.MemoryKind(body.Kind),
		ScopeType:   domain.ScopeType(body.ScopeType),
		ScopeID:     body.ScopeID,
		Key:         body.Key,
		PayloadJSON: body.PayloadJSON,
		ContentText: body.ContentText,
		Version:     body.Version,
		TTLDays:     body.TTLDays,
	})
	if err != nil {
		if IsVersionConflict(err) {
			httpjson.WriteFlatError(w, http.StatusConflict, "VERSION_CONFLICT", "memory entry was modified by another process")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "upsert memory failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.EntryToResponse(entry))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	entry, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get memory failed")
		return
	}
	if !authorizeMemoryScope(r, entry.ScopeType, entry.ScopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.EntryToResponse(entry))
}

func (h *Handler) find(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scopeType := q.Get("scope_type")
	scopeID := q.Get("scope_id")
	if scopeType == "" || scopeID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "scope_type and scope_id are required")
		return
	}
	if !authorizeMemoryScope(r, domain.ScopeType(scopeType), scopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}

	entries, err := h.uc.Find(r.Context(), FindQuery{
		ScopeType: domain.ScopeType(scopeType),
		ScopeID:   scopeID,
		Kind:      domain.MemoryKind(q.Get("kind")),
		Limit:     defaultListLimit,
	})
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "find memory failed")
		return
	}

	out := make([]dto.MemoryResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, dto.EntryToResponse(e))
	}
	httpjson.WriteJSON(w, http.StatusOK, dto.MemoryListResponse{Entries: out})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "invalid id")
		return
	}
	entry, err := h.uc.Get(r.Context(), id)
	if err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "get memory failed")
		return
	}
	if !authorizeMemoryScope(r, entry.ScopeType, entry.ScopeID) {
		httpjson.WriteFlatError(w, http.StatusForbidden, "FORBIDDEN", "memory scope is not allowed for this principal")
		return
	}
	if err := h.uc.Delete(r.Context(), id); err != nil {
		if IsNotFound(err) {
			httpjson.WriteFlatError(w, http.StatusNotFound, "NOT_FOUND", "memory entry not found")
			return
		}
		httpjson.WriteFlatInternalError(w, err, "delete memory failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func authorizeMemoryScope(r *http.Request, scopeType domain.ScopeType, scopeID string) bool {
	orgID := strings.TrimSpace(r.Header.Get("X-Org-ID"))
	if orgID == "" {
		return true
	}

	scopeID = strings.TrimSpace(scopeID)
	switch scopeType {
	case domain.ScopeOrg:
		return scopeID == orgID
	case domain.ScopeUser:
		return scopeID != "" && scopeID == strings.TrimSpace(r.Header.Get("X-User-ID"))
	case domain.ScopeTask:
		return true
	default:
		return false
	}
}
