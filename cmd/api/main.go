package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/devpablocristo/core/http/go/httpserver"
	sharedobservability "github.com/devpablocristo/core/observability/go"
	"github.com/devpablocristo/companion/migrations"
	"github.com/devpablocristo/companion/wire"
)

func main() {
	logger := sharedobservability.NewJSONLogger("companion")
	addr := os.Getenv("PORT")
	if addr == "" {
		addr = "8080"
	}
	if addr[0] != ':' {
		addr = ":" + addr
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	nexusBase := os.Getenv("NEXUS_BASE_URL")
	if nexusBase == "" {
		logger.Error("NEXUS_BASE_URL is required")
		os.Exit(1)
	}
	nexusKey := os.Getenv("NEXUS_API_KEY")
	if nexusKey == "" {
		logger.Error("NEXUS_API_KEY is required")
		os.Exit(1)
	}
	apiKeys := os.Getenv("NEXUS_API_KEYS")
	if apiKeys == "" {
		logger.Error("NEXUS_API_KEYS is required")
		os.Exit(1)
	}

	cfg := wire.Config{
		DatabaseURL:    databaseURL,
		APIKeys:        apiKeys,
		AuthIssuerURL:  os.Getenv("NEXUS_AUTH_ISSUER_URL"),
		AuthAudience:   os.Getenv("NEXUS_AUTH_AUDIENCE"),
		NexusBaseURL:   nexusBase,
		NexusAPIKey:    nexusKey,
		PymesBaseURL:   os.Getenv("PYMES_BASE_URL"),
		PymesAPIKey:    os.Getenv("PYMES_API_KEY"),
		LLMProvider:    os.Getenv("NEXUS_LLM_PROVIDER"),
		LLMAPIKey:      os.Getenv("NEXUS_LLM_API_KEY"),
		LLMModel:       os.Getenv("NEXUS_LLM_MODEL"),
		MigrationFiles: migrations.Files,
	}

	handler, cleanup, err := wire.NewServer(cfg)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	const maxBodySize = 1 << 20
	limitedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		handler.ServeHTTP(w, r)
	})

	metrics := sharedobservability.NewMetrics(sharedobservability.DefaultMetricsConfig("companion"))
	appHandler := sharedobservability.WithMetricsEndpoint(limitedHandler, metrics.Handler())
	securedHandler := httpserver.SecurityMiddleware(
		httpserver.SecurityConfigFromEnv("NEXUS"),
		sharedobservability.MiddlewareWithMetrics(logger, metrics, appHandler),
	)
	server := httpserver.New(addr, securedHandler)

	logger.Info("http server listening", "addr", addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := httpserver.Serve(ctx, server, logger); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
