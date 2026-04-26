package dto

import (
	"encoding/json"
	"time"

	domain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

// UpsertMemoryRequest petición para crear o actualizar memoria.
type UpsertMemoryRequest struct {
	Kind        string          `json:"kind"`
	ScopeType   string          `json:"scope_type"`
	ScopeID     string          `json:"scope_id"`
	Key         string          `json:"key"`
	PayloadJSON json.RawMessage `json:"payload_json,omitempty"`
	ContentText string          `json:"content_text,omitempty"`
	Version     int             `json:"version,omitempty"`
	TTLDays     int             `json:"ttl_days,omitempty"`
}

// MemoryResponse respuesta de una entrada de memoria.
type MemoryResponse struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	ScopeType   string          `json:"scope_type"`
	ScopeID     string          `json:"scope_id"`
	Key         string          `json:"key"`
	PayloadJSON json.RawMessage `json:"payload_json"`
	ContentText string          `json:"content_text"`
	Version     int             `json:"version"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	ExpiresAt   *string         `json:"expires_at,omitempty"`
}

// MemoryListResponse lista de entradas de memoria.
type MemoryListResponse struct {
	Entries []MemoryResponse `json:"entries"`
}

// EntryToResponse convierte entidad de dominio a DTO de respuesta.
func EntryToResponse(e domain.MemoryEntry) MemoryResponse {
	var expires *string
	if e.ExpiresAt != nil {
		s := e.ExpiresAt.UTC().Format(time.RFC3339)
		expires = &s
	}
	return MemoryResponse{
		ID:          e.ID.String(),
		Kind:        string(e.Kind),
		ScopeType:   string(e.ScopeType),
		ScopeID:     e.ScopeID,
		Key:         e.Key,
		PayloadJSON: e.PayloadJSON,
		ContentText: e.ContentText,
		Version:     e.Version,
		CreatedAt:   e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   e.UpdatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:   expires,
	}
}
