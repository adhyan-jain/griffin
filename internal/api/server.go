package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// Server coordinates HTTP routing, middleware chains, and lifecycle.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	logger     *slog.Logger
	engine     *service.TaskEngine
}

// ServerDependencies holds all required repositories and services for HTTP routing.
type ServerDependencies struct {
	Config          *config.Config
	Logger          *slog.Logger
	DB              *database.DB
	Engine          *service.TaskEngine
	TaskRepo        repository.TaskRepository
	ApprovalRepo    repository.ApprovalRepository
	AgentRepo       repository.AgentRepository
	ArtifactRepo    repository.ArtifactRepository
	StorageService  *storage.StorageService
	WorkerDefRepo   repository.WorkerDefinitionRepository
	OrchRepo        repository.OrchestrationRepository
	Orchestrator    *service.JJOrchestrator
	Factory         *service.AgentFactoryService
	ManagerRegistry *manager.Registry
	Wakeup          handlers.RunnerWakeup
	InstanceMgr     runner.InstanceManager
	// UserTx opens RLS-enforced per-request transactions. Required in production; when nil outside
	// production, repositories use whatever connection they were built with (tests/dev).
	UserTx *database.UserTxManager
}

// NewServer initializes and configures the HTTP server.
func NewServer(deps ServerDependencies) *Server {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	if deps.ManagerRegistry == nil {
		deps.ManagerRegistry = manager.NewDefaultRegistry()
	}
	if deps.WorkerDefRepo != nil && deps.Factory == nil {
		deps.Factory = service.NewAgentFactoryService(deps.WorkerDefRepo, deps.ManagerRegistry, deps.Logger)
	}
	if deps.OrchRepo != nil && deps.Orchestrator == nil && deps.Engine != nil && deps.TaskRepo != nil {
		deps.Orchestrator = service.NewJJOrchestrator(deps.Engine, deps.OrchRepo, deps.TaskRepo, deps.ManagerRegistry, deps.Factory, nil, deps.Logger)
	}

	mux := http.NewServeMux()

	// 1. Health & Readiness handlers (unauthenticated)
	healthHandler := handlers.NewHealthHandler(deps.DB)
	mux.HandleFunc("GET /healthz", healthHandler.Healthz)
	mux.HandleFunc("GET /readyz", healthHandler.Readyz)

	// Development-only login. Never registered in production, even if ENABLE_DEV_LOGIN is set
	// (config.Load also refuses that combination).
	if deps.DB != nil && deps.Config != nil && deps.Config.EnableDevLogin && !deps.Config.IsProduction() {
		authHandler := handlers.NewAuthHandler(deps.DB, deps.Config.SupabaseJWTSecret)
		mux.HandleFunc("POST /api/v1/auth/login", authHandler.Login)
	}

	// 2. User-authenticated route groups
	authOpts := middleware.AuthOptions{
		HMACSecret: deps.Config.SupabaseJWTSecret,
		Issuer:     deps.Config.SupabaseJWTIssuer,
	}
	if deps.Config.SupabaseJWKSURL != "" {
		authOpts.JWKS = middleware.NewJWKSCache(deps.Config.SupabaseJWKSURL, nil)
	}
	if deps.Config != nil {
		authOpts.AllowedEmails = deps.Config.AllowedUserEmails
		authOpts.AllowedUserIDs = deps.Config.AllowedUserIDs
		if len(deps.Config.AllowedUserIDs) == 0 {
			deps.Logger.Warn("ALLOWED_USER_IDS not set: operator is identified by email claim only")
		}
	}
	authenticate := middleware.AuthenticateUser(authOpts)
	// User routes: authenticate, then (when configured) run in an RLS-enforced transaction.
	var userScope func(http.Handler) http.Handler
	switch {
	case deps.UserTx != nil:
		userScope = middleware.RLSTransaction(deps.UserTx, deps.Logger)
	case deps.Config != nil && deps.Config.IsProduction():
		userScope = middleware.RLSTransaction(nil, deps.Logger) // fail closed: refuses every request
	default:
		userScope = func(h http.Handler) http.Handler { return h }
	}
	userAuth := func(h http.Handler) http.Handler { return authenticate(userScope(h)) }

	scaleToZero := false
	if deps.Config != nil {
		scaleToZero = deps.Config.ScaleToZeroEnabled
	}

	taskHandler := handlers.NewTaskHandler(deps.Engine, deps.TaskRepo, deps.Wakeup, scaleToZero)
	mux.Handle("POST /api/v1/tasks", userAuth(http.HandlerFunc(taskHandler.CreateTask)))
	mux.Handle("GET /api/v1/tasks", userAuth(http.HandlerFunc(taskHandler.ListTasks)))
	mux.Handle("GET /api/v1/tasks/{id}", userAuth(http.HandlerFunc(taskHandler.GetTask)))
	mux.Handle("POST /api/v1/tasks/{id}/cancel", userAuth(http.HandlerFunc(taskHandler.CancelTask)))
	mux.Handle("GET /api/v1/tasks/{id}/events", userAuth(http.HandlerFunc(taskHandler.GetTaskEvents)))
	mux.Handle("GET /api/v1/tasks/{id}/attempts", userAuth(http.HandlerFunc(taskHandler.GetTaskAttempts)))

	approvalHandler := handlers.NewApprovalHandler(deps.Engine, deps.ApprovalRepo, deps.Wakeup)
	mux.Handle("GET /api/v1/approvals", userAuth(http.HandlerFunc(approvalHandler.ListApprovals)))
	mux.Handle("POST /api/v1/approvals/{id}/respond", userAuth(http.HandlerFunc(approvalHandler.RespondApproval)))

	agentHandler := handlers.NewAgentHandler(deps.AgentRepo)
	mux.Handle("GET /api/v1/agents", userAuth(http.HandlerFunc(agentHandler.ListAgents)))
	mux.Handle("POST /api/v1/agents", userAuth(http.HandlerFunc(agentHandler.RegisterAgent)))

	artifactHandler := handlers.NewArtifactHandler(deps.ArtifactRepo, deps.StorageService, deps.TaskRepo, deps.Config.MaxUploadBytes).
		WithPresignExpiry(time.Duration(deps.Config.S3PresignExpiryMinutes) * time.Minute)
	mux.Handle("GET /api/v1/artifacts", userAuth(http.HandlerFunc(artifactHandler.ListArtifacts)))
	mux.Handle("GET /api/v1/artifacts/{id}", userAuth(http.HandlerFunc(artifactHandler.GetArtifact)))
	mux.Handle("POST /api/v1/artifacts", userAuth(http.HandlerFunc(artifactHandler.CreateArtifact)))
	mux.Handle("POST /api/v1/artifacts/upload", userAuth(http.HandlerFunc(artifactHandler.UploadArtifact)))
	mux.Handle("GET /api/v1/artifacts/{id}/download", userAuth(http.HandlerFunc(artifactHandler.DownloadArtifact)))
	mux.Handle("GET /api/v1/artifacts/{id}/presign", userAuth(http.HandlerFunc(artifactHandler.PresignArtifact)))
	mux.Handle("DELETE /api/v1/artifacts/{id}", userAuth(http.HandlerFunc(artifactHandler.DeleteArtifact)))

	// JJ Orchestrator & Manager routes
	if deps.Orchestrator != nil && deps.ManagerRegistry != nil {
		orchHandler := handlers.NewOrchestratorHandler(deps.Orchestrator, deps.ManagerRegistry, deps.Wakeup, scaleToZero)
		mux.Handle("POST /api/v1/orchestrator/requests", userAuth(http.HandlerFunc(orchHandler.SubmitRequest)))
		mux.Handle("GET /api/v1/orchestrator/requests/{id}", userAuth(http.HandlerFunc(orchHandler.GetRequest)))
		mux.Handle("GET /api/v1/orchestrator/plans/{id}", userAuth(http.HandlerFunc(orchHandler.GetPlan)))
		mux.Handle("GET /api/v1/managers", userAuth(http.HandlerFunc(orchHandler.ListManagers)))
		mux.Handle("GET /api/v1/managers/{id}", userAuth(http.HandlerFunc(orchHandler.GetManager)))
	}

	// JJ chat: conversations become one chat task; objectives go through the orchestrator.
	var objectives handlers.ObjectiveSubmitter
	if deps.Orchestrator != nil {
		objectives = deps.Orchestrator
	}
	jjChatHandler := handlers.NewJJChatHandler(deps.Engine, objectives, deps.Wakeup, scaleToZero)
	mux.Handle("POST /api/v1/jj/messages", userAuth(http.HandlerFunc(jjChatHandler.PostMessage)))

	// Worker Definition / Agent Factory routes
	if deps.Factory != nil {
		workerDefHandler := handlers.NewWorkerDefinitionHandler(deps.Factory)
		mux.Handle("GET /api/v1/workers/definitions", userAuth(http.HandlerFunc(workerDefHandler.ListDefinitions)))
		mux.Handle("GET /api/v1/workers/definitions/{id}", userAuth(http.HandlerFunc(workerDefHandler.GetDefinition)))
		mux.Handle("GET /api/v1/workers/definitions/{id}/versions", userAuth(http.HandlerFunc(workerDefHandler.GetDefinitionVersions)))
		mux.Handle("POST /api/v1/workers/definitions", userAuth(http.HandlerFunc(workerDefHandler.CreateDefinition)))
		mux.Handle("PUT /api/v1/workers/definitions/{id}", userAuth(http.HandlerFunc(workerDefHandler.UpdateDefinition)))
		mux.Handle("POST /api/v1/workers/definitions/{id}/toggle", userAuth(http.HandlerFunc(workerDefHandler.ToggleDefinition)))
	}

	// 3. Worker-authenticated route group & Scheduler route
	authenticateWorker := middleware.AuthenticateWorker(deps.Config.WorkerAPIKey)
	// Worker-key routes are the explicit system path (cross-tenant claim/heartbeat/tick).
	workerAuth := func(h http.Handler) http.Handler { return authenticateWorker(middleware.SystemScope()(h)) }
	workerHandler := handlers.NewWorkerHandler(deps.Engine, time.Duration(deps.Config.LeaseDurationSeconds)*time.Second)

	mux.Handle("POST /api/v1/workers/claim", workerAuth(http.HandlerFunc(workerHandler.ClaimTask)))
	mux.Handle("POST /api/v1/workers/{task_id}/heartbeat", workerAuth(http.HandlerFunc(workerHandler.Heartbeat)))
	mux.Handle("POST /api/v1/workers/{task_id}/complete", workerAuth(http.HandlerFunc(workerHandler.CompleteTask)))
	mux.Handle("POST /api/v1/workers/{task_id}/fail", workerAuth(http.HandlerFunc(workerHandler.FailTask)))

	schedulerHandler := handlers.NewSchedulerHandler(deps.Engine, deps.TaskRepo, deps.Wakeup)
	mux.Handle("POST /api/v1/scheduler/tick", workerAuth(http.HandlerFunc(schedulerHandler.Tick)))

	// Runner management endpoints
	runnerHandler := handlers.NewRunnerHandler(deps.InstanceMgr, deps.Wakeup, scaleToZero)
	mux.Handle("GET /api/v1/runner/status", userAuth(http.HandlerFunc(runnerHandler.GetStatus)))
	mux.Handle("POST /api/v1/runner/wake", userAuth(http.HandlerFunc(runnerHandler.WakeRunner)))
	// System status: evidence-derived component health for the authenticated user.
	var statusDB handlers.DBPinger
	var statusStore handlers.SystemStatusStore
	if deps.DB != nil {
		statusDB = deps.DB
		var q database.Querier = deps.DB.Pool
		if deps.UserTx != nil || (deps.Config != nil && deps.Config.IsProduction()) {
			q = database.NewScoped(deps.DB.Pool) // resolves to the request's RLS tx; denied otherwise
		}
		statusStore = handlers.NewPostgresSystemStatusStore(q)
	}
	var statusStorage handlers.StorageProber
	statusBucket := ""
	if deps.StorageService != nil && deps.StorageService.Provider() != nil {
		statusStorage = deps.StorageService.Provider()
		statusBucket = deps.StorageService.Bucket()
	}
	systemHandler := handlers.NewSystemHandler(statusDB, statusStore, statusStorage, statusBucket, deps.InstanceMgr, scaleToZero)
	mux.Handle("GET /api/v1/system/status", userAuth(http.HandlerFunc(systemHandler.GetStatus)))

	// Stopping the shared runner affects every tenant, so it is operator-only: worker key (constant-time
	// compare in AuthenticateWorker), never a user JWT. The browser must not hold this key.
	mux.Handle("POST /api/v1/runner/sleep", workerAuth(http.HandlerFunc(runnerHandler.SleepRunner)))

	// 4. Global middleware wrapping with CORS
	// Cap JSON bodies at 1 MiB; artifact routes enforce MaxUploadBytes themselves.
	handler := middleware.CORS(deps.Config.CORSAllowedOrigins)(
		middleware.BodyLimit(middleware.DefaultMaxBodyBytes, middleware.IsArtifactUploadRoute)(
			middleware.RequestID(
				middleware.Logger(deps.Logger)(
					middleware.Recovery(deps.Logger)(mux),
				),
			),
		),
	)

	addr := fmt.Sprintf(":%d", deps.Config.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Server{
		httpServer: srv,
		cfg:        deps.Config,
		logger:     deps.Logger,
		engine:     deps.Engine,
	}
}

// Handler returns the underlying http.Handler for testing.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Start begins listening and serving traffic.
func (s *Server) Start() error {
	s.logger.Info("Starting Griffin HTTP Server", "addr", s.httpServer.Addr, "env", s.cfg.Environment)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server failure: %w", err)
	}
	return nil
}

// Shutdown gracefully drains active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Gracefully shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}
