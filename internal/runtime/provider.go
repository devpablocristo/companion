// Package runtime implementa el cerebro del compañero Nexus.
// Orquesta LLM + tools + context para dar una sola voz al suscriptor.
package runtime

import (
	coreai "github.com/devpablocristo/core/ai/go"
)

// Re-exportar tipos de core/ai/go para que el resto del runtime no importe core directamente.
type (
	LLMProvider = coreai.Provider
	ChatRequest = coreai.ChatRequest
	ChatResponse = coreai.ChatResponse
	LLMMessage  = coreai.Message
	LLMToolCall = coreai.ToolCall
	ToolSchema  = coreai.Tool
)

// NewProvider crea el LLM provider usando la factory de core.
var NewProvider = coreai.NewProvider
