package governance_assist

import (
	"context"
	"net/http"
	"strings"

	"github.com/devpablocristo/core/http/go/httpjson"

	gadto "github.com/devpablocristo/companion/internal/governance_assist/handler/dto"
)

// proposerSurface es la superficie del Proposer expuesta al handler.
type proposerSurface interface {
	AnalyzeAndPropose(ctx context.Context) (analyzed, submitted int, errs []string, err error)
}

// contextualizerSurface es la superficie del Contextualizer expuesta al handler.
type contextualizerSurface interface {
	Explain(ctx context.Context, requestID string) (summary string, degraded bool, err error)
}

// Handler expone /companion/v1/governance-assist/* sobre Proposer + Contextualizer.
type Handler struct {
	proposer       proposerSurface
	contextualizer contextualizerSurface
}

func NewHandler(p proposerSurface, c contextualizerSurface) *Handler {
	return &Handler{proposer: p, contextualizer: c}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/governance-assist/propose", h.propose)
	mux.HandleFunc("GET /v1/governance-assist/explain/{request_id}", h.explain)
}

func (h *Handler) propose(w http.ResponseWriter, r *http.Request) {
	if h.proposer == nil {
		httpjson.WriteFlatError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "proposer not configured")
		return
	}
	analyzed, submitted, errs, err := h.proposer.AnalyzeAndPropose(r.Context())
	if err != nil {
		httpjson.WriteFlatInternalError(w, err, "analyze and propose failed")
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, gadto.ProposeResponse{
		PatternsAnalyzed:   analyzed,
		ProposalsSubmitted: submitted,
		Errors:             errs,
	})
}

func (h *Handler) explain(w http.ResponseWriter, r *http.Request) {
	if h.contextualizer == nil {
		httpjson.WriteFlatError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "contextualizer not configured")
		return
	}
	requestID := strings.TrimSpace(r.PathValue("request_id"))
	if requestID == "" {
		httpjson.WriteFlatError(w, http.StatusBadRequest, "VALIDATION", "request_id is required")
		return
	}
	summary, degraded, err := h.contextualizer.Explain(r.Context(), requestID)
	if err != nil {
		// Errores de fetch/upstream no son 500 hard; devolvemos summary fallback
		// con degraded=true para que la console igual muestre algo.
		httpjson.WriteJSON(w, http.StatusOK, gadto.ExplainResponse{
			RequestID: requestID,
			Summary:   "Resumen no disponible: " + err.Error(),
			Degraded:  true,
		})
		return
	}
	httpjson.WriteJSON(w, http.StatusOK, gadto.ExplainResponse{
		RequestID: requestID,
		Summary:   summary,
		Degraded:  degraded,
	})
}
