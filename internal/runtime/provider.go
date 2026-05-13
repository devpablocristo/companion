// Package runtime implementa el control plane del empleado IA Companion.
// Orquesta LLM + tools + context para dar una sola voz al suscriptor.
package runtime

import (
	"strings"
	"time"

	coreai "github.com/devpablocristo/core/ai/go"
)

// Re-exportar tipos de core/ai/go para que el resto del runtime no importe core directamente.
type (
	LLMProvider  = coreai.Provider
	ChatRequest  = coreai.ChatRequest
	ChatResponse = coreai.ChatResponse
	LLMMessage   = coreai.Message
	LLMToolCall  = coreai.ToolCall
	ToolSchema   = coreai.Tool
)

// NewProvider crea el LLM provider usando la factory de core. Para Ollama se
// fuerza un timeout más generoso (600s) porque modelos locales chicos pueden
// tomar minutos al evaluar prompts grandes (sistema + memoria + ~25 tools).
// El default upstream es 120s, suficiente para anthropic/gemini pero no para
// ollama-on-CPU.
func NewProvider(provider, apiKey, model string) LLMProvider {
	if strings.EqualFold(strings.TrimSpace(provider), "ollama") {
		baseURL := strings.TrimSpace(apiKey)
		opts := []coreai.OllamaOption{coreai.WithOllamaTimeout(10 * time.Minute)}
		if model != "" {
			opts = append(opts, coreai.WithOllamaModel(model))
		}
		return coreai.NewOllama(baseURL, opts...)
	}
	return coreai.NewProvider(provider, apiKey, model)
}
