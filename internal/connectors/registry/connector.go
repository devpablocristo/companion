package registry

import (
	"context"

	domain "github.com/devpablocristo/companion/internal/connectors/usecases/domain"
)

// Connector interfaz que implementa cada conector a sistema externo.
type Connector interface {
	// ID identificador único del conector.
	ID() string
	// Kind tipo de conector (pymes, whatsapp, mock, etc.).
	Kind() string
	// Capabilities lista operaciones y su contrato v1 (mode, risk, review, schema, evidence).
	Capabilities() []domain.Capability
	// Validate verifica que la spec es válida para este conector.
	Validate(spec domain.ExecutionSpec) error
	// Execute ejecuta la operación. Solo debe llamarse tras aprobación de Review.
	Execute(ctx context.Context, spec domain.ExecutionSpec) (domain.ExecutionResult, error)
}

// Registry registro de conectores disponibles.
type Registry struct {
	connectors map[string]Connector
}

// NewRegistry crea un nuevo registro.
func NewRegistry() *Registry {
	return &Registry{connectors: make(map[string]Connector)}
}

// Register registra un conector.
func (r *Registry) Register(c Connector) {
	r.connectors[c.ID()] = c
}

// Get obtiene un conector por ID.
func (r *Registry) Get(id string) (Connector, bool) {
	c, ok := r.connectors[id]
	return c, ok
}

// List lista todos los conectores registrados.
func (r *Registry) List() []Connector {
	out := make([]Connector, 0, len(r.connectors))
	for _, c := range r.connectors {
		out = append(out, c)
	}
	return out
}
