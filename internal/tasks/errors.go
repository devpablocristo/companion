package tasks

import "github.com/devpablocristo/core/errors/go/domainerr"

// ErrInvalidTaskState indica que la tarea no admite la operación en su estado actual.
var ErrInvalidTaskState = domainerr.Conflict("invalid task state")
