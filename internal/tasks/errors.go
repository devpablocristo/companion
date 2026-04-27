package tasks

import (
	"errors"

	"github.com/devpablocristo/core/errors/go/domainerr"
)

// ErrInvalidTaskState indica que la tarea no admite la operación en su estado actual.
var ErrInvalidTaskState = domainerr.Conflict("invalid task state")

// ErrReviewSubmit indica que la propuesta a Nexus falló al enviarse. Se
// envuelve con %w para que callers usen errors.Is en vez de string match.
var ErrReviewSubmit = errors.New("review submit failed")
