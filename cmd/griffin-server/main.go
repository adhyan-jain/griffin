package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/provider/antigravity"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/worker"
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

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("Failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("Connected to PostgreSQL successfully")

	// 4. Run database migrations
	if err := database.RunMigrations(ctx, db, "migrations"); err != nil {
		logger.Error("Failed to apply database migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("Database migrations verified and up to date")

	// 5. Initialize Repositories
	taskRepo := repository.NewPostgresTaskRepository(db.Pool)
	approvalRepo := repository.NewPostgresApprovalRepository(db.Pool)
	agentRepo := repository.NewPostgresAgentRepository(db.Pool)
	artifactRepo := repository.NewPostgresArtifactRepository(db.Pool)

	// 6. Initialize Task Engine
	engine := service.NewTaskEngine(taskRepo, approvalRepo, logger)

	// 7. Initialize Wakeup Controller for scale-to-zero
	var instanceMgr runner.InstanceManager
	if cfg.EC2InstanceID != "" {
		ec2Mgr, err := runner.NewEC2InstanceManager(cfg.EC2InstanceID, cfg.AWSRegion)
		if err != nil {
			logger.Error("Failed to initialize EC2 instance manager", "error", err)
			os.Exit(1)
		}
		instanceMgr = ec2Mgr
	} else {
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
				ExecutionTimeout:          time.Duration(cfg.ExecutionTimeoutSeconds) * time.Second,
				GracefulTermination:       time.Duration(cfg.GracefulTerminationSeconds) * time.Second,
				WorkspaceRoot:             cfg.WorkspaceRoot,
				RetainWorkspacesOnFailure: cfg.RetainWorkspacesOnFailure,
				Logger:                    logger,
			})
			if err != nil {
				logger.Error("Failed to initialize Antigravity executor", "error", err)
				os.Exit(1)
			}
			executor = agyExec
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
		for i := 1; i <= cfg.MaxConcurrentWorkers; i++ {
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
			})
			workerPool.Add(w)
			go func(workerInstance *worker.Worker) {
				if err := workerInstance.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("Worker loop exited with error", "worker_id", workerInstance.ID, "error", err)
				}
			}(w)
		}
		logger.Info("Spawned background task workers", "count", cfg.MaxConcurrentWorkers, "provider", executor.Name())

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
	}

	var server *api.Server
	if runAPI {
		// 10. HTTP API Server
		server = api.NewServer(api.ServerDependencies{
			Config:       cfg,
			Logger:       logger,
			DB:           db,
			Engine:       engine,
			TaskRepo:     taskRepo,
			ApprovalRepo: approvalRepo,
			AgentRepo:    agentRepo,
			ArtifactRepo: artifactRepo,
			Wakeup:       wakeupCtrl,
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
