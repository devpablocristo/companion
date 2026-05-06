package tasks

import (
	"errors"
	"fmt"

	"github.com/devpablocristo/core/errors/go/domainerr"
)

// ErrInvalidTaskState indica que la tarea no admite la operación en su estado actual.
var ErrInvalidTaskState = domainerr.Conflict("invalid task state")

// ErrReviewSubmit indica que la propuesta a Nexus falló al enviarse. Se
// envuelve con %w para que callers usen errors.Is en vez de string match.
var ErrReviewSubmit = errors.New("review submit failed")

// ErrReviewNotApproved indica que una capability con requires_review=true
// fue invocada sin que la review en Nexus esté en estado aprobado. Es un
// caso especial de invalid state, distinguible para que el handler lo mapee
// a HTTP 412 (precondition_failed) con detalle del review_request_id y status.
//
// Wraps ErrInvalidTaskState para mantener compatibilidad con callers que
// usan IsInvalidTaskState (no rompe el handler legacy ni los tests viejos).
var ErrReviewNotApproved = fmt.Errorf("review not approved: %w", ErrInvalidTaskState)

// ReviewBlockedError contiene el contexto de un bloqueo por review no aprobada.
// El handler lo extrae para incluir review_request_id y review_status en el body.
type ReviewBlockedError struct {
	ReviewRequestID string
	ReviewStatus    string
	Reason          string
}

func (e *ReviewBlockedError) Error() string {
	if e.ReviewRequestID == "" {
		return fmt.Sprintf("%s (status=%s)", ErrReviewNotApproved.Error(), e.ReviewStatus)
	}
	return fmt.Sprintf("%s (review_request_id=%s status=%s)", ErrReviewNotApproved.Error(), e.ReviewRequestID, e.ReviewStatus)
}

// Unwrap permite errors.Is(err, ErrReviewNotApproved) y, transitivamente,
// errors.Is(err, ErrInvalidTaskState) para no romper callers legacy.
func (e *ReviewBlockedError) Unwrap() error {
	return ErrReviewNotApproved
}

// IsReviewNotApproved indica que la operación está bloqueada por una review
// no aprobada en Nexus Governance.
func IsReviewNotApproved(err error) bool {
	return errors.Is(err, ErrReviewNotApproved)
}

// AsReviewBlocked devuelve el detalle estructurado si el error es un bloqueo
// por review. Útil para que el handler construya el body con context.
func AsReviewBlocked(err error) (*ReviewBlockedError, bool) {
	var blocked *ReviewBlockedError
	if errors.As(err, &blocked) {
		return blocked, true
	}
	return nil, false
}
