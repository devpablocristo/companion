package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/devpablocristo/core/config/go/envconfig"
	sharedpostgres "github.com/devpablocristo/core/databases/postgres/go"
	"github.com/devpablocristo/core/http/go/health"
	"github.com/devpablocristo/companion/internal/connectors"
	"github.com/devpablocristo/companion/internal/connectors/registry"
	"github.com/devpablocristo/companion/internal/memory"
	"github.com/devpablocristo/companion/internal/runtime"
	"github.com/devpablocristo/companion/internal/tasks"
	"github.com/devpablocristo/companion/internal/watchers"
	"github.com/devpablocristo/companion/internal/watchers/pymesclient"

	memdomain "github.com/devpablocristo/companion/internal/memory/usecases/domain"
)

type taskMemoryAdapter struct {
	uc *memory.Usecases
}

func (a taskMemoryAdapter) UpsertTaskMemory(ctx context.Context, taskID uuid.UUID, kind, key string, contentText string, payload json.RawMessage) error {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	_, err := a.uc.Upsert(ctx, memory.UpsertInput{
		Kind:        memdomain.MemoryKind(kind),
		ScopeType:   memdomain.ScopeTask,
		ScopeID:     taskID.String(),
		Key:         key,
		PayloadJSON: payload,
		ContentText: contentText,
	})
	return err
}

func nexusSyncInterval() time.Duration {
	return envconfig.Duration("COMPANION_NEXUS_SYNC_INTERVAL_SEC", 30*time.Second)
}

func watcherInterval() time.Duration {
	return envconfig.Duration("COMPANION_WATCHER_INTERVAL_SEC", 0)
}

// Config arranque del servicio Companion.
type Config struct {
	DatabaseURL    string
	APIKeys        string
	AuthIssuerURL  string
	AuthAudience   string
	NexusBaseURL   string
	NexusAPIKey    string
	PymesBaseURL   string
	PymesAPIKey    string
	LLMProvider    string
	LLMAPIKey      string
	LLMModel       string
	MigrationFiles fs.FS
}

// NewServer abre DB, migra, monta mux y auth.
func NewServer(cfg Config) (http.Handler, func(), error) {
	ctx := context.Background()

	db, err := sharedpostgres.OpenWithConfig(ctx, cfg.DatabaseURL, sharedpostgres.DefaultConfig("nexus-companion"))
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	if err := sharedpostgres.MigrateUp(ctx, db, "nexus-companion", cfg.MigrationFiles, "."); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	nexusGateway := newNexusGateway(cfg.NexusBaseURL, cfg.NexusAPIKey)
	rc := nexusGateway.client
	pymesClient := pymesclient.NewClient(cfg.PymesBaseURL, cfg.PymesAPIKey)

	// Connectors module
	connReg := registry.NewRegistry()
	connReg.Register(registry.NewMockConnector())
	if cfg.PymesBaseURL != "" {
		connReg.Register(registry.NewPymesConnector(pymesClient))
	}
	connRepo := connectors.NewPostgresRepository(db)
	reviewChecker := connectors.NewReviewCheckerAdapter(func(c context.Context, id uuid.UUID) (string, string, int, error) {
		return nexusGateway.GetRequestMeta(c, id.String())
	})
	connUC := connectors.NewUsecases(connRepo, connReg, reviewChecker)
	connHandler := connectors.NewHandler(connUC)

	repo := tasks.NewPostgresRepository(db)
	uc := tasks.NewUsecases(repo, nexusGateway)
	uc.SetReviewSyncInterval(nexusSyncInterval())
	uc.SetExecutor(connUC)
	h := tasks.NewHandler(uc)

	// Watchers module
	watcherRepo := watchers.NewPostgresRepository(db)
	watcherUC := watchers.NewUsecases(watcherRepo, pymesClient, rc)
	watcherHandler := watchers.NewHandler(watcherUC)

	// Memory module
	memRepo := memory.NewPostgresRepository(db)
	memUC := memory.NewUsecases(memRepo)
	memHandler := memory.NewHandler(memUC)
	uc.SetTaskMemory(taskMemoryAdapter{uc: memUC})

	// Runtime del compañero (LLM + tools + context)
	llmProvider := runtime.NewProvider(cfg.LLMProvider, cfg.LLMAPIKey, cfg.LLMModel)
	toolkit := runtime.NewToolKit(rc, memUC, watcherUC)
	contextPorts := runtime.ContextPorts{
		ReviewClient: rc,
		MemoryFind: func(c context.Context, st memdomain.ScopeType, sid string, k memdomain.MemoryKind, limit int) ([]memdomain.MemoryEntry, error) {
			return memUC.Find(c, memory.FindQuery{ScopeType: st, ScopeID: sid, Kind: k, Limit: limit})
		},
	}
	orchestrator := runtime.NewOrchestrator(llmProvider, toolkit, contextPorts)
	adapter := runtime.NewOrchestratorAdapter(orchestrator)
	uc.SetOrchestrator(adapter)
	// Watchers empujan alertas al chat del suscriptor
	watcherUC.SetNotifier(uc)
	slog.Info("companion runtime initialized", "llm_provider", cfg.LLMProvider)

	mux := http.NewServeMux()
	health.RegisterEndpoints(mux, func(c context.Context) error {
		return db.Ping(c)
	})
	h.Register(mux)
	watcherHandler.Register(mux)
	memHandler.Register(mux)
	connHandler.Register(mux)

	// Seed conectores por defecto
	if err := connUC.SeedDefaultConnectors(ctx); err != nil {
		slog.Error("seed default connectors", "error", err)
	}

	authMW, err := newAuthMiddleware(cfg.APIKeys, cfg.AuthIssuerURL, cfg.AuthAudience)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("create authenticator: %w", err)
	}

	cleanup := func() {
		db.Close()
	}
	if d := nexusSyncInterval(); d > 0 {
		syncCtx, syncCancel := context.WithCancel(context.Background())
		go uc.RunReviewSyncLoop(syncCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			syncCancel()
			prev()
		}
	}
	if d := watcherInterval(); d > 0 {
		watcherCtx, watcherCancel := context.WithCancel(context.Background())
		go watcherUC.RunWatcherLoop(watcherCtx, d, 50)
		prev := cleanup
		cleanup = func() {
			watcherCancel()
			prev()
		}
	}

	// Memory purge loop: limpia entradas expiradas cada hora
	{
		purgeCtx, purgeCancel := context.WithCancel(context.Background())
		go memUC.RunPurgeLoop(purgeCtx, 1*time.Hour)
		prev := cleanup
		cleanup = func() {
			purgeCancel()
			prev()
		}
	}

	return authMW(mux), cleanup, nil
}
