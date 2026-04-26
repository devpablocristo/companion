package memory

import (
	"errors"

	"github.com/devpablocristo/core/errors/go/domainerr"
)

var (
	ErrNotFound = domainerr.NotFound("not found")
	ErrVersionConflict = errors.New("memory version conflict")
)

// IsNotFound verifica si el error es de entrada no encontrada.
func IsNotFound(err error) bool {
	return domainerr.IsNotFound(err)
}

// IsVersionConflict verifica si el error es de conflicto de versión.
func IsVersionConflict(err error) bool {
	return errors.Is(err, ErrVersionConflict)
}
