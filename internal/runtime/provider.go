// Package runtime implementa el control plane del empleado IA Companion.
// Orquesta LLM + tools + context para dar una sola voz al suscriptor.
package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	coreai "github.com/devpablocristo/core/ai/go"
	"golang.org/x/oauth2/google"
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

// ProviderConfig agrupa la configuración para construir el LLM provider.
// Anthropic/Gemini/Ollama solo usan Provider/APIKey/Model. Vertex usa
// VertexProject + VertexLocation y se autentica vía ADC (no API key).
type ProviderConfig struct {
	Provider       string
	APIKey         string
	Model          string
	VertexProject  string
	VertexLocation string
}

// NewProvider crea el LLM provider. Para "vertex"/"vertex_ai" usa ADC del
// runtime (Cloud Run service account) — pymes-ai y ponti-ai siguen este
// mismo patrón. Para "ollama" se aplica un timeout extendido porque modelos
// locales sobre CPU pueden tardar minutos. Para el resto delega en
// coreai.NewProvider.
func NewProvider(cfg ProviderConfig) LLMProvider {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "vertex", "vertex_ai":
		return newVertexProvider(cfg)
	case "ollama":
		baseURL := strings.TrimSpace(cfg.APIKey)
		opts := []coreai.OllamaOption{coreai.WithOllamaTimeout(10 * time.Minute)}
		if cfg.Model != "" {
			opts = append(opts, coreai.WithOllamaModel(cfg.Model))
		}
		return coreai.NewOllama(baseURL, opts...)
	default:
		return coreai.NewProvider(cfg.Provider, cfg.APIKey, cfg.Model)
	}
}

func newVertexProvider(cfg ProviderConfig) LLMProvider {
	project := strings.TrimSpace(cfg.VertexProject)
	region := strings.TrimSpace(cfg.VertexLocation)
	if region == "" {
		region = "us-central1"
	}
	if project == "" {
		return coreai.NewEcho()
	}

	ts, err := google.DefaultTokenSource(context.Background(),
		"https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return coreai.NewEcho()
	}

	tokenSource := func(ctx context.Context) (string, error) {
		tok, err := ts.Token()
		if err != nil {
			return "", fmt.Errorf("vertex token: %w", err)
		}
		return tok.AccessToken, nil
	}

	opts := []coreai.VertexAIOption{}
	if cfg.Model != "" {
		opts = append(opts, coreai.WithVertexModel(cfg.Model))
	}
	return coreai.NewVertexAI(project, region, tokenSource, opts...)
}
