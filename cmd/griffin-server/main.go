package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/griffin/core/internal/hands/browser"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/provider/gemini"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/provider/antigravity"
	"github.com/griffin/core/internal/provider/claudecli"
	"github.com/griffin/core/internal/provider/router"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
	"github.com/griffin/core/internal/worker"
	"github.com/griffin/core/migrations"
)

func main() {
	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// 2. Structured logging
	var logLevel slog.Level
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	logger.Info("Initializing Griffin Core Engine",
		"version", "0.1.0-phase1",
		"env", cfg.Environment,
		"port", cfg.Port,
	)

	// 3. Database connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Everything started from main (workers, reaper, idle monitor, startup reconcile) is an explicit
	// system path. HTTP request contexts do NOT derive from this ctx, so user requests never inherit it.
	ctx = database.WithSystem(ctx)

	db, err := database.ConnectWithOptions(ctx, cfg.DatabaseURL, database.PoolOptions{
		MaxConns: int32(cfg.DBMaxConns),
		MinConns: int32(cfg.DBMinConns),
	})
	if err != nil {
		logger.Error("Failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("Connected to PostgreSQL successfully")

	// 4. Verify (read-only) that the schema is current. Migrations are applied explicitly with
	// `griffin-migrate up`, never by the server.
	if err := database.CheckSchema(ctx, db, migrations.FS); err != nil {
		logger.Error("Database schema check failed", "error", err)
		os.Exit(1)
	}
	logger.Info("Database schema verified up to date")

	// 5. Initialize Repositories
	// Repositories resolve their connection per context: user transaction (RLS) or explicit system
	// scope, never an implicit privileged default. Outside production, with no USER_DATABASE_URL,
	// they use the single development connection.
	var repoDB database.Querier = db.Pool
	if cfg.IsProduction() || cfg.UserDatabaseURL != "" {
		repoDB = database.NewScoped(db.Pool)
	} else {
		logger.Warn("USER_DATABASE_URL not set: row-level security is NOT enforced for user requests (development only)")
	}
	var userTx *database.UserTxManager
	if cfg.UserDatabaseURL != "" && (cfg.GriffinMode == "all" || cfg.GriffinMode == "api") {
		userDB, err := database.ConnectWithOptions(ctx, cfg.UserDatabaseURL, database.PoolOptions{
			MaxConns: int32(cfg.DBMaxConns),
			MinConns: int32(cfg.DBMinConns),
		})
		if err != nil {
			logger.Error("Failed to connect user (RLS) database pool", "error", err)
			os.Exit(1)
		}
		defer userDB.Close()
		if err := database.AssertRoleIsolation(ctx, userDB.Pool); err != nil {
			logger.Error("Refusing to serve user traffic", "error", err)
			os.Exit(1)
		}
		userTx = &database.UserTxManager{Pool: userDB.Pool}
		logger.Info("User database pool verified: NOSUPERUSER, NOBYPASSRLS")
	}
	taskRepo := repository.NewPostgresTaskRepository(repoDB)
	approvalRepo := repository.NewPostgresApprovalRepository(repoDB)
	agentRepo := repository.NewPostgresAgentRepository(repoDB)
	artifactRepo := repository.NewPostgresArtifactRepository(repoDB)
	workerDefRepo := repository.NewPostgresWorkerDefinitionRepository(repoDB)
	orchRepo := repository.NewPostgresOrchestrationRepository(repoDB)
	hostRepo := repository.NewPostgresHostRepository(repoDB)
	policyRepo := repository.NewPostgresPolicyRepository(repoDB)
	profileRepo := repository.NewPostgresProfileRepository(repoDB)
	outreachRepo := repository.NewOutreachRepository(repoDB)

	// 5.5 Initialize Object Storage Provider & Service (Phase 4)
	var storageProvider storage.Provider
	switch cfg.StorageProvider {
	case "s3":
		s3Prov, err := storage.NewS3StorageProvider(ctx, storage.S3ClientOptions{
			Region:         cfg.S3Region,
			Bucket:         cfg.S3Bucket,
			AWSProfile:     cfg.AWSProfile,
			Endpoint:       cfg.S3Endpoint,
			ForcePathStyle: cfg.S3ForcePathStyle,
		})
		if err != nil {
			logger.Error("Failed to initialize AWS S3 storage provider", "error", err)
			os.Exit(1)
		}
		storageProvider = s3Prov
		logger.Info("Initialized AWS S3 object storage provider",
			"bucket", cfg.S3Bucket,
			"region", cfg.S3Region,
			"profile", cfg.AWSProfile,
		)
	default:
		storageProvider = storage.NewMockStorageProvider()
		logger.Info("Initialized Mock in-memory storage provider")
	}
	storageService := storage.NewStorageService(storageProvider, artifactRepo, cfg.S3Bucket, logger)

	// 6. Initialize Task Engine, Manager Registry & Agent Factory
	engine := service.NewTaskEngine(taskRepo, approvalRepo, logger)
	mgrReg := manager.NewDefaultRegistry()
	agentFactory := service.NewAgentFactoryService(workerDefRepo, mgrReg, logger)
	if topic := cfg.NtfyTopic; topic != "" {
		engine.SetNotifier(&service.TaskNotifier{
			Sender: &notify.Ntfy{Server: cfg.NtfyServer, Topic: topic, Token: cfg.NtfyToken},
			Summ:   &service.Summarizer{Brain: summaryBrainFor(cfg)},
			AppURL: cfg.AppURL,
			ManagerName: func(ctx context.Context, userID uuid.UUID) string {
				name, _ := profileRepo.ManagerName(ctx, userID)
				return name
			},
			Logger: logger,
		})
		logger.Info("finish notifications are on", "server", cfg.NtfyServer)
	}
	jjOrchestrator := service.NewJJOrchestrator(engine, orchRepo, taskRepo, mgrReg, agentFactory, newPlanner(cfg, logger), logger)

	// 7. Initialize Wakeup Controller for scale-to-zero
	var instanceMgr runner.InstanceManager
	switch {
	case cfg.EC2InstanceID != "":
		ec2Mgr, err := runner.NewEC2InstanceManager(ctx, cfg.EC2InstanceID, cfg.AWSRegion)
		if err != nil {
			logger.Error("Failed to initialize EC2 instance manager", "error", err)
			os.Exit(1)
		}
		instanceMgr = ec2Mgr
	case cfg.IsProduction():
		// Production without an instance id would silently never wake the runner (config.Load
		// already rejects this for API mode); worker mode does not wake anything.
		instanceMgr = runner.NewMockInstanceManager(runner.RunnerStateRunning)
		logger.Warn("No EC2_INSTANCE_ID configured; runner wake-up is disabled in this process")
	default:
		instanceMgr = runner.NewMockInstanceManager(runner.RunnerStateRunning)
	}

	wakeupCtrl := runner.NewWakeupController(instanceMgr, runner.WakeupConfig{
		CooldownWindow: time.Duration(cfg.RunnerWakeupCooldownSeconds) * time.Second,
	}, logger)

	runWorkers := cfg.GriffinMode == "all" || cfg.GriffinMode == "worker"
	runAPI := cfg.GriffinMode == "all" || cfg.GriffinMode == "api"

	var workerPool *worker.WorkerPool

	if runWorkers {
		// 8. Background Lease Reaper
		reaper := worker.NewLeaseReaper(engine, time.Duration(cfg.ReaperIntervalSeconds)*time.Second, logger)
		// Keep old finished tasks lean: drop their activity history and shorten long answers that are
		// already kept as files. Runs at start and every 6 hours while the runner is up.
		if cfg.TaskCompactAfterDays > 0 {
			go func() {
				compact := func() {
					res, err := taskRepo.CompactFinishedTasks(ctx, time.Now().Add(-time.Duration(cfg.TaskCompactAfterDays)*24*time.Hour), 600)
					if err != nil {
						logger.Warn("Compacting old finished tasks failed", "error", err)
						return
					}
					if res.Events+res.Attempts+res.Trimmed > 0 {
						logger.Info("Compacted old finished tasks", "events_removed", res.Events, "attempts_removed", res.Attempts, "answers_trimmed", res.Trimmed)
					}
				}
				compact()
				t := time.NewTicker(6 * time.Hour)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						compact()
					}
				}
			}()
		}
		go func() {
			if err := reaper.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("Lease reaper stopped with error", "error", err)
			}
		}()

		// 9. Background AI Execution Pool
		var executor worker.TaskExecutor
		switch cfg.AIProvider {
		case "antigravity":
			agyExec, err := antigravity.NewAntigravityExecutor(antigravity.ExecutorConfig{
				BinaryPath:                cfg.AntigravityBinary,
				MaxConcurrent:             cfg.MaxConcurrentAntigravity,
				ReserveMB:                 cfg.AgyReserveMB,
				AgentMB:                   cfg.AgyAgentMB,
				LoadFactor:                cfg.AgyLoadFactor,
				ExecutionTimeout:          time.Duration(cfg.ExecutionTimeoutSeconds) * time.Second,
				GracefulTermination:       time.Duration(cfg.GracefulTerminationSeconds) * time.Second,
				WorkspaceRoot:             cfg.WorkspaceRoot,
				RetainWorkspacesOnFailure: cfg.RetainWorkspacesOnFailure,
				ToolsEnabled:              cfg.AgyToolsEnabled,
				ReadArgs:                  cfg.AgyReadArgs,
				ActArgs:                   cfg.AgyActArgs,
				Effort:                    cfg.AgyEffort,
				Logger:                    logger,
			})
			if err != nil {
				logger.Error("Failed to initialize Antigravity executor", "error", err)
				os.Exit(1)
			}
			// The router sends each step to the provider that suits it. Claude is optional: without it
			// (the default) every step runs on agy exactly as before.
			var claudeExec worker.TaskExecutor
			if cfg.ClaudeEnabled {
				ce, cerr := claudecli.New(claudecli.Config{
					BinaryPath:    cfg.ClaudeBinary,
					MaxConcurrent: cfg.MaxConcurrentClaude,
					Timeout:       time.Duration(cfg.ExecutionTimeoutSeconds) * time.Second,
					WorkspaceRoot: cfg.WorkspaceRoot,
					LeadModel:     cfg.ClaudeLeadModel,
					Effort:        cfg.ClaudeEffort,
					LeadBudgetUSD: cfg.ClaudeLeadBudgetUSD,
					BudgetCapUSD:  cfg.ClaudeBudgetCapUSD,
					Logger:        logger,
				})
				if cerr != nil {
					logger.Warn("CLAUDE_ENABLED is set but the claude CLI is unavailable; running every step on agy", "error", cerr)
				} else {
					claudeExec = ce
					logger.Info("Claude provider enabled for language-only steps", "binary", cfg.ClaudeBinary, "max_concurrent", cfg.MaxConcurrentClaude)
				}
			}
			executor = router.New(agyExec, claudeExec, logger)
			logger.Info("Initialized Antigravity AI execution provider",
				"binary", cfg.AntigravityBinary,
				"max_concurrent", cfg.MaxConcurrentAntigravity,
				"workspace_root", cfg.WorkspaceRoot,
			)
		default:
			executor = worker.NewMockExecutor()
			logger.Info("Initialized Mock AI execution provider")
		}

		workerPool = worker.NewWorkerPool()
		// One worker loop per agent the machine may run at once, so a growing queue can use all of them.
		workerLoops := cfg.MaxConcurrentWorkers
		if cfg.MaxConcurrentAntigravity > workerLoops {
			workerLoops = cfg.MaxConcurrentAntigravity
		}
		for i := 1; i <= workerLoops; i++ {
			workerID := fmt.Sprintf("griffin-worker-%d", i)
			w := worker.NewWorker(worker.Config{
				ID:                workerID,
				Engine:            engine,
				TaskRepo:          taskRepo,
				Executor:          executor,
				LeaseDuration:     time.Duration(cfg.LeaseDurationSeconds) * time.Second,
				HeartbeatInterval: time.Duration(cfg.HeartbeatIntervalSeconds) * time.Second,
				PollInterval:      1 * time.Second,
				Logger:            logger,
				Artifacts:         storageService,
				Playbooks:         storageService,
				OnTaskFinished: func(hookCtx context.Context, taskID uuid.UUID) {
					if err := jjOrchestrator.ReconcileByChildTask(hookCtx, taskID); err != nil {
						logger.Warn("Orchestration reconcile after task finish failed", "task_id", taskID, "error", err)
					}
				},
			})
			workerPool.Add(w)
			go func(workerInstance *worker.Worker) {
				if err := workerInstance.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("Worker loop exited with error", "worker_id", workerInstance.ID, "error", err)
				}
			}(w)
		}
		logger.Info("Spawned background task workers", "count", workerLoops, "provider", executor.Name())

		// If running in dedicated worker mode with scale-to-zero, enable IdleShutdownMonitor
		if cfg.GriffinMode == "worker" && cfg.ScaleToZeroEnabled {
			idleMonitor := runner.NewIdleShutdownMonitor(
				taskRepo,
				workerPool,
				runner.NewSystemStopper(logger),
				runner.IdleShutdownConfig{
					IdleTimeout:   time.Duration(cfg.IdleShutdownSeconds) * time.Second,
					CheckInterval: time.Duration(cfg.PreShutdownCheckIntervalSeconds) * time.Second,
				},
				logger,
			)
			go func() {
				if err := idleMonitor.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("Idle shutdown monitor stopped with error", "error", err)
				}
			}()
			logger.Info("Started EC2 IdleShutdownMonitor",
				"idle_timeout_seconds", cfg.IdleShutdownSeconds,
			)
		}
		// Reconcile in-flight orchestrations on startup
		_ = jjOrchestrator.ReconcileInFlight(ctx)
	}

	var server *api.Server
	if runAPI {
		// 10. HTTP API Server
		server = api.NewServer(api.ServerDependencies{
			Config:          cfg,
			Logger:          logger,
			DB:              db,
			Engine:          engine,
			TaskRepo:        taskRepo,
			ApprovalRepo:    approvalRepo,
			AgentRepo:       agentRepo,
			ArtifactRepo:    artifactRepo,
			StorageService:  storageService,
			WorkerDefRepo:   workerDefRepo,
			OrchRepo:        orchRepo,
			Orchestrator:    jjOrchestrator,
			Factory:         agentFactory,
			ManagerRegistry: mgrReg,
			Wakeup:          wakeupCtrl,
			HostRepo:        hostRepo,
			PolicyRepo:      policyRepo,
			ProfileRepo:     profileRepo,
			OutreachRepo:    outreachRepo,
			OutreachDrafter: outreachDrafter(cfg),
			DeviceDecider:   deviceDecider(cfg),
			VoiceBrain:      voiceBrain(cfg),
			InstanceMgr:     instanceMgr,
			UserTx:          userTx,
		})

		go func() {
			if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("HTTP server failed", "error", err)
				os.Exit(1)
			}
		}()
	}

	// 10. Graceful shutdown handler
	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdownSig
	logger.Info("Received shutdown signal", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	cancel() // Cancel background worker & reaper contexts

	if server != nil {
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Server forced shutdown", "error", err)
		}
	}

	logger.Info("Griffin Core Engine shutdown complete")
}

// voiceBrain is the fast chat model, or nil when no key is configured (every voice turn then hands off).
func voiceBrain(cfg *config.Config) *gemini.Client {
	if cfg.GeminiAPIKey == "" {
		return nil
	}
	return &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: cfg.GeminiModel}
}

// newPlanner is the standard planner, upgraded when asked: with CLAUDE_LEAD a whole job goes to one Claude lead
// that picks its own sub-agents; with DYNAMIC_PLANNER and a Gemini key a fast model writes a team of workers for
// each request. Both fall back to the standard plan on any failure.
func newPlanner(cfg *config.Config, logger *slog.Logger) service.Planner {
	base := service.NewRuleBasedPlanner()
	brain := voiceBrain(cfg)
	useBrain := cfg.DynamicPlanner && brain != nil && brain.Enabled()
	if !cfg.ClaudeLead && !useBrain {
		return base
	}
	dp := &service.DynamicPlanner{Base: base, Logger: logger}
	if useBrain {
		dp.Brain = brain
	}
	if cfg.ClaudeLead {
		dp.ClaudeLead = func() bool { return true }
		logger.Info("Claude lead planning is on: whole jobs go to one Claude lead with its own sub-agents")
	}
	if useBrain {
		logger.Info("dynamic planning is on: a team of workers is written for each request, with the standard plan as fallback")
	}
	return dp
}

// summaryBrainFor is the fast model for notification summaries, or nil without a key.
func summaryBrainFor(cfg *config.Config) service.PlanBrain {
	if b := voiceBrain(cfg); b != nil && b.Enabled() {
		return b
	}
	return nil
}

// outreachDrafter writes first drafts with the same Gemini key as the voice route; nil when there is none.
func outreachDrafter(cfg *config.Config) *service.OutreachDrafter {
	if cfg.GeminiAPIKey == "" {
		return nil
	}
	return &service.OutreachDrafter{Model: &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: "gemini-flash-latest", MaxOutputTokens: 500}}
}

// deviceDecider chooses phone screen steps with the server's Gemini key, so the key never reaches the phone.
func deviceDecider(cfg *config.Config) browser.Decider {
	if cfg.GeminiAPIKey == "" {
		return nil
	}
	return &browser.ModelDecider{Phone: true, Model: &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: "gemini-flash-latest", MaxOutputTokens: 700}}
}
