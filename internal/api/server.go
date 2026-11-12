package api

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
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
	Config       *config.Config
	Logger       *slog.Logger
	DB           *database.DB
	Engine       *service.TaskEngine
	TaskRepo     repository.TaskRepository
	ApprovalRepo repository.ApprovalRepository
	AgentRepo    repository.AgentRepository
	// HostRepo enables the host API (laptop/phone hosts) when set.
	HostRepo repository.HostRepository
	// PolicyRepo stores the owner's permission policy; used with HostRepo.
	PolicyRepo repository.PolicyRepository
	// OutreachRepo enables campaigns and their people (cold mail, applications); nil leaves the routes off.
	OutreachRepo *repository.OutreachRepository
	// ProfileRepo holds the owner's settings (the manager's name).
	ProfileRepo repository.ProfileRepository
	// VoiceBrain is the fast model for voice chat; nil means every voice turn hands off to the worker.
	VoiceBrain      handlers.VoiceBrain
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

	// Hosts (ADR-074): when a live host can take plain work the cloud runner is not woken for it. The
	// manual Wake button keeps the raw controller: an explicit wake is always honoured.
	rawWakeup := deps.Wakeup
	var hostSvc *service.HostService
	if deps.HostRepo != nil && deps.TaskRepo != nil && deps.Config != nil && deps.Config.HostsEnabled {
		leaseSec := 60
		if deps.Config != nil && deps.Config.LeaseDurationSeconds > 0 {
			leaseSec = deps.Config.LeaseDurationSeconds
		}
		hostSvc = service.NewHostService(deps.HostRepo, deps.TaskRepo, time.Duration(leaseSec)*time.Second, deps.Logger)
		if deps.Wakeup != nil {
			deps.Wakeup = service.NewHostAwareWakeup(deps.Wakeup, hostSvc, deps.Logger)
		}
	}

	taskHandler := handlers.NewTaskHandler(deps.Engine, deps.TaskRepo, deps.Wakeup, scaleToZero)
	if deps.Orchestrator != nil {
		taskHandler.WithPlanReconciler(deps.Orchestrator)
		taskHandler.WithObjectives(deps.Orchestrator)
	}
	mux.Handle("POST /api/v1/tasks", userAuth(http.HandlerFunc(taskHandler.CreateTask)))
	mux.Handle("GET /api/v1/tasks", userAuth(http.HandlerFunc(taskHandler.ListTasks)))
	mux.Handle("GET /api/v1/tasks/{id}", userAuth(http.HandlerFunc(taskHandler.GetTask)))
	mux.Handle("POST /api/v1/tasks/{id}/cancel", userAuth(http.HandlerFunc(taskHandler.CancelTask)))
	mux.Handle("POST /api/v1/tasks/{id}/resume", userAuth(http.HandlerFunc(taskHandler.ResumeTask)))
	mux.Handle("DELETE /api/v1/tasks/{id}", userAuth(http.HandlerFunc(taskHandler.DeleteTask)))
	mux.Handle("GET /api/v1/tasks/{id}/events", userAuth(http.HandlerFunc(taskHandler.GetTaskEvents)))
	mux.Handle("GET /api/v1/tasks/{id}/attempts", userAuth(http.HandlerFunc(taskHandler.GetTaskAttempts)))

	approvalHandler := handlers.NewApprovalHandler(deps.Engine, deps.ApprovalRepo, deps.Wakeup)
	mux.Handle("GET /api/v1/approvals", userAuth(http.HandlerFunc(approvalHandler.ListApprovals)))
	mux.Handle("POST /api/v1/approvals/{id}/respond", userAuth(http.HandlerFunc(approvalHandler.RespondApproval)))
	mux.Handle("PATCH /api/v1/approvals/{id}", userAuth(http.HandlerFunc(approvalHandler.EditApproval)))
	mux.Handle("POST /api/v1/approvals/batch", userAuth(http.HandlerFunc(approvalHandler.BatchRespondApprovals)))

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
	mux.Handle("PUT /api/v1/artifacts/{id}/text", userAuth(http.HandlerFunc(artifactHandler.UpdateArtifactText)))
	mux.Handle("POST /api/v1/lists", userAuth(http.HandlerFunc(artifactHandler.CreateList)))

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
	var profiles handlers.ProfileStore
	if deps.ProfileRepo != nil {
		profiles = deps.ProfileRepo
	}
	var voiceCmds *service.VoiceCommands
	if deps.Engine != nil && deps.ApprovalRepo != nil {
		voiceCmds = &service.VoiceCommands{Tasks: deps.Engine, Approvals: voiceApprovals{deps.ApprovalRepo, deps.Engine}, Secret: []byte(deps.Config.SupabaseJWTSecret)}
	}

	jjChatHandler := handlers.NewJJChatHandler(deps.Engine, objectives, deps.Wakeup, scaleToZero).
		WithProfiles(profiles).
		WithThreads(deps.Engine).
		WithBrain(deps.VoiceBrain)
	if voiceCmds != nil {
		jjChatHandler.WithCommands(voiceCmds)
	}
	if hostSvc != nil {
		jjChatHandler.WithDeviceCheck(hostSvc.CanCover)
	}
	mux.Handle("POST /api/v1/jj/messages", userAuth(http.HandlerFunc(jjChatHandler.PostMessage)))
	mux.Handle("POST /api/v1/tasks/{id}/follow-ups", userAuth(http.HandlerFunc(jjChatHandler.PostFollowUp)))
	if profiles != nil {
		settingsHandler := handlers.NewSettingsHandler(profiles)
		mux.Handle("GET /api/v1/settings/manager-name", userAuth(http.HandlerFunc(settingsHandler.GetManagerName)))
		mux.Handle("PUT /api/v1/settings/manager-name", userAuth(http.HandlerFunc(settingsHandler.PutManagerName)))
	}
	spokenHandler := handlers.NewSpokenHandler(deps.Engine, &service.Summarizer{Brain: summaryBrain(deps.VoiceBrain)}, profiles)
	mux.Handle("GET /api/v1/tasks/{id}/spoken-summary", userAuth(http.HandlerFunc(spokenHandler.Get)))
	var vaultHandler *handlers.VaultHandler
	if deps.StorageService != nil && deps.ProfileRepo != nil {
		vaultHandler = handlers.NewVaultHandler(deps.StorageService, deps.ProfileRepo, deps.Config.MaxUploadBytes)
		mux.Handle("GET /api/v1/vault", userAuth(http.HandlerFunc(vaultHandler.List)))
		mux.Handle("POST /api/v1/vault", userAuth(http.HandlerFunc(vaultHandler.Add)))
		mux.Handle("GET /api/v1/vault/misses", userAuth(http.HandlerFunc(vaultHandler.ListMisses)))
		mux.Handle("POST /api/v1/vault/misses", userAuth(http.HandlerFunc(vaultHandler.AddMiss)))
		mux.Handle("DELETE /api/v1/vault/misses/{id}", userAuth(http.HandlerFunc(vaultHandler.ResolveMiss)))
	}
	// A nil client is fine here: its Enabled reports false, so every voice turn hands off.
	voiceHandler := handlers.NewVoiceHandler(deps.VoiceBrain, profiles)
	if voiceCmds != nil {
		voiceHandler.WithCommands(voiceCmds)
	}
	mux.Handle("POST /api/v1/voice/turn", userAuth(http.HandlerFunc(voiceHandler.PostTurn)))

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

	// Execution hosts (laptop, phone). Owner routes use the user JWT; host routes use a per-host token and
	// never touch the database directly.
	if hostSvc != nil {
		var policySvc *service.PolicyService
		if deps.PolicyRepo != nil {
			policySvc = service.NewPolicyService(deps.PolicyRepo)
		}
		hostHandler := handlers.NewHostHandler(hostSvc, deps.Engine, policySvc)
		mux.Handle("POST /api/v1/hosts", userAuth(http.HandlerFunc(hostHandler.RegisterHost)))
		mux.Handle("GET /api/v1/hosts", userAuth(http.HandlerFunc(hostHandler.ListHosts)))
		mux.Handle("PATCH /api/v1/hosts/{id}", userAuth(http.HandlerFunc(hostHandler.UpdateHost)))
		mux.Handle("DELETE /api/v1/hosts/{id}", userAuth(http.HandlerFunc(hostHandler.RevokeHost)))

		if policySvc != nil {
			permHandler := handlers.NewPermissionsHandler(policySvc)
			mux.Handle("GET /api/v1/permissions", userAuth(http.HandlerFunc(permHandler.Get)))
			mux.Handle("PUT /api/v1/permissions", userAuth(http.HandlerFunc(permHandler.Put)))
			mux.Handle("GET /api/v1/host/policy", middleware.AuthenticateHost(hostSvc)(http.HandlerFunc(permHandler.HostGet)))
		}

		hostAuth := middleware.AuthenticateHost(hostSvc)
		if vaultHandler != nil { // a host with the files capability can mirror documents and answer misses
			mux.Handle("GET /api/v1/host/vault", hostAuth(http.HandlerFunc(vaultHandler.List)))
			mux.Handle("POST /api/v1/host/vault", hostAuth(http.HandlerFunc(vaultHandler.Add)))
			mux.Handle("GET /api/v1/host/vault/misses", hostAuth(http.HandlerFunc(vaultHandler.ListMisses)))
			mux.Handle("DELETE /api/v1/host/vault/misses/{id}", hostAuth(http.HandlerFunc(vaultHandler.ResolveMiss)))
		}
		mux.Handle("POST /api/v1/host/heartbeat", hostAuth(http.HandlerFunc(hostHandler.Heartbeat)))
		mux.Handle("POST /api/v1/host/claim", hostAuth(http.HandlerFunc(hostHandler.Claim)))
		mux.Handle("POST /api/v1/host/tasks/{task_id}/heartbeat", hostAuth(http.HandlerFunc(hostHandler.TaskHeartbeat)))
		mux.Handle("POST /api/v1/host/tasks/{task_id}/approval", hostAuth(http.HandlerFunc(hostHandler.RequestApproval)))
		mux.Handle("POST /api/v1/host/tasks/{task_id}/complete", hostAuth(http.HandlerFunc(hostHandler.CompleteTask)))
		mux.Handle("POST /api/v1/host/tasks/{task_id}/fail", hostAuth(http.HandlerFunc(hostHandler.FailTask)))
	}

	// Outreach: campaigns and the people in them. Owner routes use the sign-in; a host's routes (below, inside the
	// host block) can add people, draft, claim the next send within the limits and report results, but never approve.
	if deps.OutreachRepo != nil {
		oh := handlers.NewOutreachHandler(deps.OutreachRepo)
		mux.Handle("POST /api/v1/outreach/campaigns", userAuth(http.HandlerFunc(oh.CreateCampaign)))
		mux.Handle("GET /api/v1/outreach/campaigns", userAuth(http.HandlerFunc(oh.ListCampaigns)))
		mux.Handle("PATCH /api/v1/outreach/campaigns/{id}", userAuth(http.HandlerFunc(oh.UpdateCampaign)))
		mux.Handle("GET /api/v1/outreach/campaigns/{id}/targets", userAuth(http.HandlerFunc(oh.ListTargets)))
		mux.Handle("POST /api/v1/outreach/campaigns/{id}/targets", userAuth(http.HandlerFunc(oh.AddTargets)))
		mux.Handle("POST /api/v1/outreach/campaigns/{id}/next", userAuth(http.HandlerFunc(oh.Next)))
		mux.Handle("GET /api/v1/outreach/campaigns/{id}/export.csv", userAuth(http.HandlerFunc(oh.Export)))
		mux.Handle("PATCH /api/v1/outreach/targets/{id}", userAuth(http.HandlerFunc(oh.UpdateTarget)))
		mux.Handle("POST /api/v1/outreach/replies", userAuth(http.HandlerFunc(oh.Reply)))
		if hostSvc != nil {
			ha := middleware.AuthenticateHost(hostSvc)
			mux.Handle("GET /api/v1/host/outreach/campaigns", ha(http.HandlerFunc(oh.ListCampaigns)))
			mux.Handle("GET /api/v1/host/outreach/campaigns/{id}/targets", ha(http.HandlerFunc(oh.ListTargets)))
			mux.Handle("POST /api/v1/host/outreach/campaigns/{id}/targets", ha(http.HandlerFunc(oh.AddTargets)))
			mux.Handle("POST /api/v1/host/outreach/campaigns/{id}/next", ha(http.HandlerFunc(oh.Next)))
			mux.Handle("PATCH /api/v1/host/outreach/targets/{id}", ha(http.HandlerFunc(oh.UpdateTarget)))
			mux.Handle("POST /api/v1/host/outreach/replies", ha(http.HandlerFunc(oh.Reply)))
		}
	}

	// Runner management endpoints
	runnerHandler := handlers.NewRunnerHandler(deps.InstanceMgr, rawWakeup, scaleToZero)
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

// summaryBrain adapts the fast voice model for summaries; nil stays nil so summaries fall back to the answer's own sentences.
func summaryBrain(b handlers.VoiceBrain) service.PlanBrain {
	if b == nil {
		return nil
	}
	if pb, ok := b.(service.PlanBrain); ok {
		return pb
	}
	return nil
}

// voiceApprovals joins the approval list (a repository) and the decision (the engine) for the voice commands.
type voiceApprovals struct {
	repo   repository.ApprovalRepository
	engine *service.TaskEngine
}

func (v voiceApprovals) ListApprovals(ctx context.Context, user uuid.UUID, st *domain.ApprovalStatus) ([]*domain.Approval, error) {
	return v.repo.ListApprovals(ctx, user, st)
}

func (v voiceApprovals) RespondApproval(ctx context.Context, id, user uuid.UUID, ok bool, reason string) (*domain.Approval, error) {
	return v.engine.RespondApproval(ctx, id, user, ok, reason)
}
