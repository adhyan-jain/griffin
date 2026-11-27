package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/griffin/core/internal/hands/browser"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/provider/gemini"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/playbooks"
	alumniplaybook "github.com/griffin/core/internal/playbooks/alumni"
	companiesplaybook "github.com/griffin/core/internal/playbooks/companies"
	"github.com/griffin/core/internal/playbooks/internships"
	"github.com/griffin/core/internal/provider/antigravity"
	"github.com/griffin/core/internal/provider/claudecli"
	"github.com/griffin/core/internal/provider/router"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/research"
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
		logger.Warn("Database schema check notice", "error", err)
	} else {
		logger.Info("Database schema verified up to date")
	}

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
	aliasRepo := repository.NewPostgresAliasRepository(repoDB)
	notificationRepo := repository.NewPostgresNotificationRepository(repoDB)
	listingRepo := repository.NewOpportunityRepository(repoDB)
	assessRoutine := opportunityAssess(cfg, listingRepo)
	outreachRepo := repository.NewOutreachRepository(repoDB)
	researchRepo := repository.NewResearchRepository(repoDB)
	knowledgeRepo := repository.NewKnowledgeRepository(repoDB)
	// voiceBrain returns a nil *gemini.Client without a key; stored directly in the LLMClient interface it would be
	// a non-nil interface holding a nil pointer, which the router's nil checks cannot see. Only a real client goes in.
	var researchLLM research.LLMClient
	if researchBrain := voiceBrain(cfg); researchBrain != nil {
		researchLLM = researchBrain
	}
	researchRouter := research.NewRouter(researchLLM, researchLLM)
	researchTools := research.NewToolGateway(nil, nil)
	researchEngine := research.NewEngine(researchRepo, knowledgeRepo, researchTools, researchRouter, logger)
	// Research attaches to the existing companies and person records: the playbooks write there, not to a store of their own.
	peopleRepo := repository.NewPeopleRepository(repoDB)
	researchEngine.Playbooks = playbooks.Registry{
		"company_intelligence": companiesplaybook.NewPlaybook().WithStore(listingRepo),
		"internship_watch":     internships.NewPlaybook(nil),
		"alumni_intelligence":  alumniplaybook.NewPlaybook(&alumniplaybook.Criteria{GraduationYearCutoff: cfg.AlumniGraduationCutoff}).WithStore(peopleRepo),
	}
	// The RESEARCH RUN routine (a claimed task, run in code under the claim deadline) advances runs with this
	// engine; TrackerLists carries it (server.go and the worker), there is no global.

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
	// An application the owner approves is added to the Applied tracker.
	appliedLists := &service.TrackerLists{Store: storageService, Tasks: taskRepo, Logger: logger}
	engine.OnApplicationApproved(func(ctx context.Context, a *domain.Approval) {
		var p struct {
			Company    string `json:"company"`
			Role       string `json:"role"`
			PortalURL  string `json:"portal_url"`
			ResumePath string `json:"resume_path"`
		}
		if json.Unmarshal(a.ActionPayload, &p) == nil {
			appliedLists.RecordApplied(ctx, a.UserID, p.Company, p.Role, p.PortalURL, p.ResumePath)
		}
		// Approving the draft is the go-ahead: a host task now really submits it.
		appliedLists.StartSubmit(ctx, a, func(c context.Context, t *domain.Task) error {
			_, err := engine.CreateTask(c, t, nil)
			return err
		})
	})
	// Application packages: the routine and the Prepare button write them; the owner's decision on one moves its listing.
	prepRoutine := applicationPrep(cfg, listingRepo, engine, logger)
	// An approved alumni outreach draft only records the decision on the person; nothing is sent. The owner sends by hand.
	engine.OnOutreachDecided((&service.AlumniPeople{People: peopleRepo, Logger: logger}).OutreachDecided)
	engine.OnPackageDecided(func(ctx context.Context, a *domain.Approval, approved bool) {
		prepRoutine.Decided(ctx, a, approved, func(c context.Context, sub *domain.Approval) {
			var p struct {
				Company    string `json:"company"`
				Role       string `json:"role"`
				PortalURL  string `json:"portal_url"`
				ResumePath string `json:"resume_path"`
			}
			if json.Unmarshal(sub.ActionPayload, &p) == nil {
				appliedLists.RecordApplied(c, sub.UserID, p.Company, p.Role, p.PortalURL, p.ResumePath)
			}
			// The package is approved, not sent: this makes the existing submit task, which asks again before it sends.
			appliedLists.StartSubmit(c, sub, func(c2 context.Context, t *domain.Task) error {
				_, err := engine.CreateTask(c2, t, nil)
				return err
			})
		})
	})
	engine.OnSubmitFinished(appliedLists.RecordSubmitted)
	engine.OnDMFinished(appliedLists.RecordDM)
	engine.OnPostingMissing(appliedLists.CloseListing)
	mgrReg := manager.NewDefaultRegistry()
	agentFactory := service.NewAgentFactoryService(workerDefRepo, mgrReg, logger)
	var senders []notify.Sender
	senders = append(senders, &notify.DesktopSender{})
	if topic := cfg.NtfyTopic; topic != "" {
		senders = append(senders, &notify.Ntfy{Server: cfg.NtfyServer, Topic: topic, Token: cfg.NtfyToken})
		logger.Info("finish notifications are on", "server", cfg.NtfyServer)
	}
	engine.SetNotifier(&service.TaskNotifier{
		Sender: notify.MultiSender(senders),
		Summ:   &service.Summarizer{Brain: summaryBrainFor(cfg)},
		Outbox: notificationRepo,
		AppURL: cfg.AppURL,
		ManagerName: func(ctx context.Context, userID uuid.UUID) string {
			name, _ := profileRepo.ManagerName(ctx, userID)
			return name
		},
		Logger: logger,
	})
	// Reminders: the push is held by ntfy until its time; without a topic the phone's poll delivers it from the outbox.
	reminders := &service.Reminders{Tasks: engine, Due: taskRepo, Outbox: notificationRepo, AppURL: cfg.AppURL, Logger: logger}
	if cfg.NtfyTopic != "" {
		reminders.Sender = &notify.Ntfy{Server: cfg.NtfyServer, Topic: cfg.NtfyTopic, Token: cfg.NtfyToken}
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
				Listings:          listingRepo,
				Assess:            assessRoutine,
				ResearchRunner:    researchEngine,
				Alumni:            service.NewAlumniPeople(storageService, peopleRepo),
				Reminders:         reminders,
				Prep:              prepRoutine,
				GitHubLogin:       cfg.GitHubLogin,
				CommunityReplies:  communityReplier(cfg),
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

	// DSA coach: hands out problems, checks LeetCode for submissions, nags through ntfy when configured.
	dsaCoach := &service.DsaCoach{Store: repository.NewDsaRepository(repoDB), Checker: &service.LeetCodeChecker{}, AppURL: cfg.AppURL, Logger: logger}
	if cfg.NtfyTopic != "" {
		dsaCoach.Sender = &notify.Ntfy{Server: cfg.NtfyServer, Topic: cfg.NtfyTopic, Token: cfg.NtfyToken}
	}
	briefer := &service.Briefer{Sources: repository.NewReportRepository(repoDB), Coach: dsaCoach, Sender: dsaCoach.Sender, AppURL: cfg.AppURL, Logger: logger}
	// Proactive services. They are built in every mode so the API (including the short-lived function)
	// and the worker share one wiring; durable scheduling of them comes from the scheduler tick.
	ownerProfiles := repository.NewOwnerProfileRepository(repoDB)
	antigravity.CandidateContext = newProfileContext(ctx, ownerProfiles)
	ownerFacts := antigravity.CandidateContext
	engine.SetOwnerFacts(func(ctx context.Context, userID uuid.UUID) string {
		// The structured profile when there is one, plus the profile document in the Library.
		return strings.TrimSpace(ownerFacts(userID) + "\n" + appliedLists.ProfileText(database.WithSystem(ctx), userID))
	})
	inboundRepo := repository.NewInboundEventRepository(repoDB)
	inboxRepo := repository.NewInboxRepository(repoDB)
	discoveryRepo := repository.NewDiscoveryRepository(repoDB)
	triage := &service.InboundTriageService{Events: inboundRepo, Outreach: outreachRepo, Sender: dsaCoach.Sender}
	gmailSyncer := &service.GmailSyncer{InboxRepo: inboxRepo, Events: inboundRepo, Triage: triage, Tasks: taskRepo, Logger: logger}
	jobDiscovery := &service.JobDiscoveryService{
		DiscoveryRepo: discoveryRepo,
		Pipeline:      &service.OpportunityPipeline{Tasks: taskRepo, Profiles: ownerProfiles, Logger: logger},
		Client:        &http.Client{Timeout: 20 * time.Second},
		Logger:        logger,
	}
	opsRepo := repository.NewOperationRepository(repoDB)
	briefer.Ops = opsRepo
	briefer.Watchdog = &service.OutreachWatchdog{Outreach: outreachRepo, Drafter: outreachDrafter(cfg), Sender: dsaCoach.Sender, Profiles: ownerProfiles, Logger: logger}
	ttsService := service.NewTTSService("")

	if runWorkers { // a long-lived process; the API alone may be a short-lived function
		// Steps that need a laptop or phone and found none online fail with a plain reason instead of waiting forever.
		go func() {
			sweep := func() {
				ids, err := taskRepo.ExpireStaleDeviceWaits(ctx, time.Duration(domain.DeviceWaitLimit)*time.Second)
				if err != nil {
					logger.Warn("Expiring steps that waited for a device failed", "error", err)
					return
				}
				for _, id := range ids {
					if err := jjOrchestrator.ReconcileByChildTask(ctx, id); err != nil {
						logger.Warn("Reconcile after a device wait expired failed", "task_id", id, "error", err)
					}
				}
				if len(ids) > 0 {
					logger.Info("Failed steps that waited too long for a device", "count", len(ids))
				}
			}
			sweep()
			t := time.NewTicker(5 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					sweep()
				}
			}
		}()
		go dsaCoach.Run(ctx, 15*time.Minute)
		go briefer.Run(ctx, 15*time.Minute)
	}

	var callsHandler *handlers.CallsHandler
	if cfg.CallsUserID != "" {
		ownerID, perr := uuid.Parse(cfg.CallsUserID)
		if perr != nil || cfg.CallsPublicURL == "" {
			logger.Error("CALLS_USER_ID or CALLS_PUBLIC_URL is missing or invalid; calling features remain off")
		} else {
			name, _ := profileRepo.ManagerName(database.WithSystem(ctx), ownerID)
			callsHandler = &handlers.CallsHandler{
				Log: repository.NewCallRepository(repoDB), Sender: dsaCoach.Sender, AuthToken: cfg.CallsAuthToken, PublicURL: cfg.CallsPublicURL,
				OwnerUserID: ownerID, OwnerName: name, OwnerNumber: cfg.CallsOwnerNumber, Logger: logger,
				Tasks: taskRepo,
				InboundEnabled: func(callCtx context.Context) bool {
					enabled, err := profileRepo.InboundCallsEnabled(database.WithSystem(callCtx), ownerID)
					return err == nil && enabled
				},
			}
			for _, n := range strings.Split(cfg.CallsTrusted, ",") {
				if n = strings.TrimSpace(n); n != "" {
					callsHandler.Trusted = append(callsHandler.Trusted, n)
				}
			}
			logger.Info("calling handler initialized", "inbound_token_set", cfg.CallsAuthToken != "", "outbound_cellular", taskRepo != nil)
		}
	}

	var server *api.Server
	if runAPI {
		// 10. HTTP API Server
		server = api.NewServer(api.ServerDependencies{
			Config:           cfg,
			Logger:           logger,
			DB:               db,
			Engine:           engine,
			TaskRepo:         taskRepo,
			ApprovalRepo:     approvalRepo,
			AgentRepo:        agentRepo,
			ScheduleRepo:     repository.NewPostgresScheduleRepository(repoDB),
			ArtifactRepo:     artifactRepo,
			StorageService:   storageService,
			WorkerDefRepo:    workerDefRepo,
			OrchRepo:         orchRepo,
			Orchestrator:     jjOrchestrator,
			Factory:          agentFactory,
			ManagerRegistry:  mgrReg,
			Wakeup:           wakeupCtrl,
			HostRepo:         hostRepo,
			PolicyRepo:       policyRepo,
			ProfileRepo:      profileRepo,
			AliasRepo:        aliasRepo,
			NotificationRepo: notificationRepo,
			Reminders:        reminders,
			ListingRepo:      listingRepo,
			Assess:           assessRoutine,
			Prep:             prepRoutine,
			GitHubLogin:      cfg.GitHubLogin,
			CommunityReplies: communityReplier(cfg),
			OutreachRepo:     outreachRepo,
			ReportRepo:       repository.NewReportRepository(repoDB),
			Dsa:              dsaCoach,
			Calls:            callsHandler,
			Briefer:          briefer,
			InboundEventRepo: inboundRepo,
			InboundTriage:    triage,
			InboxRepo:        inboxRepo,
			GmailSyncer:      gmailSyncer,
			DiscoveryRepo:    discoveryRepo,
			JobDiscovery:     jobDiscovery,
			TTS:              ttsService,
			ProactiveOps: []handlers.ProactiveOp{
				// Briefs (once per local day, persisted) and the outreach follow-up watchdog.
				{Name: "briefs_and_watchdog", Run: func(c context.Context) error { briefer.Tick(c); return nil }},
			},
			OutreachDrafter:  outreachDrafter(cfg),
			DeviceDecider:    deviceDecider(cfg),
			VoiceBrain:       voiceBrain(cfg),
			InstanceMgr:      instanceMgr,
			ResearchRepo:     researchRepo,
			KnowledgeRepo:    knowledgeRepo,
			PeopleRepo:       peopleRepo,
			OwnerProfileRepo: ownerProfiles,
			ResearchEngine:   researchEngine,
			UserTx:           userTx,
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

	claudeLeadActive := false
	if cfg.ClaudeLead {
		if !cfg.ClaudeEnabled {
			logger.Warn("CLAUDE_LEAD is true but CLAUDE_ENABLED is false; Claude lead planning disabled")
		} else {
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
				logger.Warn("CLAUDE_LEAD is true but claude CLI is unavailable; Claude lead planning disabled", "error", cerr)
			} else if healthy, herr := ce.IsHealthy(context.Background()); !healthy || herr != nil {
				logger.Warn("CLAUDE_LEAD is true but claude CLI health check failed; Claude lead planning disabled", "error", herr)
			} else {
				claudeLeadActive = true
			}
		}
	}

	if !claudeLeadActive && !useBrain {
		return base
	}
	dp := &service.DynamicPlanner{Base: base, Logger: logger}
	if useBrain {
		dp.Brain = brain
		logger.Info("dynamic planning is on: a team of workers is written for each request, with the standard plan as fallback")
	}
	if claudeLeadActive {
		dp.ClaudeLead = func() bool { return true }
		logger.Info("Claude lead planning is on: whole jobs go to one Claude lead with its own sub-agents")
	} else if cfg.ClaudeLead {
		dp.ClaudeLead = func() bool { return false }
		logger.Warn("Claude lead planning requested but degraded to standard rule-based planner (Claude unavailable/unhealthy)")
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

// opportunityAssess builds the routine that assesses open listings. With a Gemini key it may ask Gemini Flash, at most
// service.DefaultMaxModelCalls times a run and once per listing and description; without one it assesses by code alone.
func opportunityAssess(cfg *config.Config, store service.AssessStore) *service.OpportunityAssess {
	o := &service.OpportunityAssess{Store: store}
	if cfg.GeminiAPIKey == "" {
		return o
	}
	zero := 0.0
	c := &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: "gemini-flash-latest", MaxOutputTokens: 2500, Temperature: &zero, JSONOutput: true,
		HTTP: &http.Client{Timeout: 60 * time.Second}}
	o.ModelName = c.Model
	o.Model = func(ctx context.Context, system, user string) (string, error) {
		return c.Chat(ctx, system, []gemini.Turn{{Role: "user", Text: user}})
	}
	return o
}

// applicationPrep builds the routine that prepares application packages. With a Gemini key it asks Gemini Flash, at
// most twice per listing (one to write the letter, one to rewrite what failed the fact check); without one it prepares
// what code can and lists the letter as missing. Facts are the owner's profile as the engine holds it.
func applicationPrep(cfg *config.Config, store *repository.OpportunityRepository, engine *service.TaskEngine, logger *slog.Logger) *service.ApplicationPrep {
	o := &service.ApplicationPrep{Store: store, Holder: engine, Facts: engine.OwnerFacts, Logger: logger}
	if cfg.GeminiAPIKey == "" {
		return o
	}
	temp := 0.3
	c := &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: "gemini-flash-latest", MaxOutputTokens: 1500, Temperature: &temp, JSONOutput: true,
		HTTP: &http.Client{Timeout: 60 * time.Second}}
	o.ModelName = c.Model
	o.Model = func(ctx context.Context, system, user string) (string, error) {
		return c.Chat(ctx, system, []gemini.Turn{{Role: "user", Text: user}})
	}
	return o
}

// outreachDrafter writes first drafts with the same Gemini key as the voice route; nil when there is none.
func outreachDrafter(cfg *config.Config) *service.OutreachDrafter {
	if cfg.GeminiAPIKey == "" {
		return nil
	}
	return &service.OutreachDrafter{Model: &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: "gemini-flash-latest", MaxOutputTokens: 500}}
}

// communityReplier writes the reply for each new community thread with Gemini Flash; nil (an untyped nil
// interface, so the scan leaves the cells empty) when there is no key.
func communityReplier(cfg *config.Config) service.OutreachChatter {
	if cfg.GeminiAPIKey == "" {
		return nil
	}
	// The lighter model: quick enough for several replies inside one 22s run, and less often overloaded.
	return &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: gemini.DefaultFallbackModel, MaxOutputTokens: 600, Fallback: "gemini-flash-latest"}
}

// deviceDecider chooses phone screen steps with the server's Gemini key, so the key never reaches the phone.
func deviceDecider(cfg *config.Config) browser.Decider {
	if cfg.GeminiAPIKey == "" {
		return nil
	}
	return &browser.ModelDecider{Phone: true, Model: &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: "gemini-flash-latest", MaxOutputTokens: 700}}
}

// newProfileContext returns a prompt-context provider backed by the stored owner profile, cached
// briefly so a burst of career tasks does not hit the database each time. It returns "" when no
// profile exists, which makes the prompt omit personal claims.
func newProfileContext(sysCtx context.Context, repo *repository.OwnerProfileRepository) func(uuid.UUID) string {
	type entry struct {
		text string
		at   time.Time
	}
	var mu sync.Mutex
	cache := map[uuid.UUID]entry{}
	return func(userID uuid.UUID) string {
		mu.Lock()
		defer mu.Unlock()
		if e, ok := cache[userID]; ok && time.Since(e.at) < 5*time.Minute {
			return e.text
		}
		text := ""
		if p, err := repo.Latest(database.WithSystem(sysCtx), userID); err == nil && p != nil {
			text = p.ContextBlock()
		}
		cache[userID] = entry{text: text, at: time.Now()}
		return text
	}
}
