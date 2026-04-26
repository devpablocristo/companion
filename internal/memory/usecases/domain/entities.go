package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MemoryKind tipo de memoria operativa.
type MemoryKind string

const (
	MemoryTaskSummary    MemoryKind = "task_summary"
	MemoryTaskFacts      MemoryKind = "task_facts"
	MemoryPlaybook       MemoryKind = "playbook_snippet"
	MemoryUserPreference MemoryKind = "user_preference"
)

// ScopeType alcance de la entrada de memoria.
type ScopeType string

const (
	ScopeTask ScopeType = "task"
	ScopeOrg  ScopeType = "org"
	ScopeUser ScopeType = "user"
)

// MemoryEntry entrada de memoria operativa del compañero.
type MemoryEntry struct {
	ID          uuid.UUID
	Kind        MemoryKind
	ScopeType   ScopeType
	ScopeID     string
	Key         string
	PayloadJSON json.RawMessage
	ContentText string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   *time.Time
}

// DefaultRetentionDays retención por tipo de memoria.
func DefaultRetentionDays(kind MemoryKind) int {
	switch kind {
	case MemoryTaskSummary:
		return 90
	case MemoryTaskFacts:
		return 90
	case MemoryPlaybook:
		return 0 // sin expiración
	case MemoryUserPreference:
		return 0 // sin expiración
	default:
		return 90
	}
}
