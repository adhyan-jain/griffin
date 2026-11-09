// Command griffin-host runs on your laptop (and later your phone). It connects OUT to the Griffin API
// with a per-host token, announces that the device is online, claims the work it is allowed to do, runs
// it with the device's own agent login and reports back. It has no database credentials and opens no
// inbound port.
//
// Configuration (environment):
//
//	GRIFFIN_API_URL            API origin, e.g. https://api.example.com (https required except localhost)
//	GRIFFIN_HOST_TOKEN_FILE    path to a file holding the host token (preferred; keep it mode 0600)
//	GRIFFIN_HOST_TOKEN         the token itself (use the file form where you can)
//	HOST_CAPABILITIES          comma list, default "agent". Also backed: "messaging" (WhatsApp, when a browser or adb phone exists)
//	                           and "web" (search and page reading). See hands.go for HOST_WHATSAPP, and HOST_WEB=off to disable web.
//	BRAVE_SEARCH_API_KEY, SEARXNG_URL   reliable web search backends (DuckDuckGo is the keyless best-effort fallback)
//	HOST_EXECUTOR              "agy" (default) or "mock" (development)
//	AGY_BINARY                 path to agy (default "agy")
//	HOST_WORKSPACE_ROOT        scratch folders, one per task (default ~/.griffin-host/workspaces)
//	HOST_MAX_CONCURRENT        tasks at once (default 1)
//	HOST_EXEC_TIMEOUT_SECONDS  per-task limit (default 600)
//
// Flags: --check verifies the token, the API and the agent, prints what the owner has granted, and exits.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands"
	"github.com/griffin/core/internal/hands/web"
	"github.com/griffin/core/internal/hostagent"
	"github.com/griffin/core/internal/provider/antigravity"
	"github.com/griffin/core/internal/sandbox"
	"github.com/griffin/core/internal/worker"
)

// Version is set at build time: -ldflags "-X main.Version=..."
var Version = "dev"

// backedCapabilities are the capabilities this build can actually do. A host that advertised one it
// cannot back would be handed a step it would then answer as plain text, which is a false completion.
var backedCapabilities = map[string]bool{domain.HostCapAgent: true}

func main() {
	check := flag.Bool("check", false, "verify configuration, token, API and agent, then exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if args := flag.Args(); len(args) > 0 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err := runSubcommand(ctx, logger, args[0])
		stop()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(logger, *check); err != nil {
		logger.Error("griffin-host stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, check bool) error {
	apiURL := strings.TrimSpace(os.Getenv("GRIFFIN_API_URL"))
	if err := validateAPIURL(apiURL); err != nil {
		return err
	}
	token, err := loadToken(logger)
	if err != nil {
		return err
	}
	execKind := strings.ToLower(envOr("HOST_EXECUTOR", "agy"))
	// WhatsApp hands: the messaging capability is advertised only when a driver can back it.
	var wa *whatsappSetup
	if execKind == "agy" {
		if wa = newWhatsApp(logger); wa != nil {
			backedCapabilities[domain.HostCapMessaging] = true
		}
	}
	// Web search and page reading need nothing installed: the capability is backed unless switched off.
	var webSvc *hands.WebService
	if execKind == "agy" && strings.ToLower(envOr("HOST_WEB", "on")) != "off" {
		webSvc = &hands.WebService{Fetcher: web.NewFetcher(web.FetcherConfig{}), Searcher: web.SearcherFromEnv()}
		backedCapabilities[domain.HostCapWeb] = true
	}
	caps, err := parseCapabilities(envOr("HOST_CAPABILITIES", domain.HostCapAgent), execKind == "mock")
	if err != nil {
		return err
	}

	timeout := time.Duration(envInt("HOST_EXEC_TIMEOUT_SECONDS", 600)) * time.Second
	var exec worker.TaskExecutor
	switch execKind {
	case "mock":
		logger.Warn("using the mock executor: development only, results are fake")
		exec = worker.NewMockExecutor()
	case "agy":
		root := envOr("HOST_WORKSPACE_ROOT", defaultWorkspaceRoot())
		if err := os.MkdirAll(root, 0o700); err != nil {
			return fmt.Errorf("create workspace root: %w", err)
		}
		// Tools stay off in this build: the agent runs text-only exactly as on the cloud runner, and the
		// executor refuses permission-bypass flags. Hands (browser, shell, files) arrive with the sandbox
		// and the permissions gateway (PRD 7.16), not before.
		exec, err = antigravity.NewAntigravityExecutor(antigravity.ExecutorConfig{
			BinaryPath:       envOr("AGY_BINARY", "agy"),
			MaxConcurrent:    envInt("HOST_MAX_CONCURRENT", 1),
			ExecutionTimeout: timeout,
			WorkspaceRoot:    root,
			ToolsEnabled:     false,
			Logger:           logger,
		})
		if err != nil {
			return fmt.Errorf("agent executor: %w", err)
		}
	default:
		return fmt.Errorf("HOST_EXECUTOR must be agy or mock, got %q", execKind)
	}

	// With WhatsApp hands the executor routes: steps with a messaging action go through the permission
	// gateway to the drivers, everything else goes to the agent. The gateway is attached once the agent
	// exists, because the agent is where the owner's policy arrives.
	var handsExec *hands.Executor
	if wa != nil || webSvc != nil {
		handsExec = &hands.Executor{Agent: exec, Web: webSvc, Logger: logger}
		if wa != nil {
			handsExec.WhatsApp = wa.messenger
		}
		exec = handsExec
	}

	client := hostagent.NewClient(apiURL, token, nil)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if check {
		return runCheck(ctx, client, caps, exec)
	}

	agent, err := hostagent.New(hostagent.Config{
		Client:        client,
		Executor:      exec,
		Capabilities:  caps,
		Version:       Version,
		MaxConcurrent: envInt("HOST_MAX_CONCURRENT", 1),
		Logger:        logger,
	})
	if err != nil {
		return err
	}
	if handsExec != nil {
		if err := os.MkdirAll(stateDir(), 0o700); err != nil {
			return fmt.Errorf("create %s: %w", stateDir(), err)
		}
		audit, err := gateway.NewFileAudit(filepath.Join(stateDir(), "audit.log"))
		if err != nil {
			return err
		}
		defer audit.Close()
		handsExec.Gate = gateway.New(string(domain.HostKindLaptop), agent, audit)
		if wa != nil {
			logger.Info("WhatsApp hands enabled", "drivers", wa.messenger.Names())
		}
		if webSvc != nil {
			logger.Info("web search and page reading enabled", "search", webSvc.Searcher.Names())
		}
		logger.Info("outward actions are audited", "audit_log", filepath.Join(stateDir(), "audit.log"))
	}
	if handoverEnabled() {
		h, herr := newHandover(logger, agent)
		if herr != nil {
			return herr
		}
		h.start(ctx, logger)
	}
	logger.Info("griffin-host online", "version", Version, "api", apiURL, "capabilities", caps)
	err = agent.Run(ctx)
	if errors.Is(err, hostagent.ErrUnauthorized) {
		return fmt.Errorf("%w; register the host again from the app", err)
	}
	return err
}

func runCheck(ctx context.Context, c *hostagent.Client, caps []string, exec worker.TaskExecutor) error {
	rep, err := c.Heartbeat(ctx, caps, Version)
	if err != nil {
		return fmt.Errorf("API check failed: %w", err)
	}
	healthy, herr := exec.IsHealthy(ctx)
	fmt.Printf("API:            reachable, token accepted\n")
	fmt.Printf("Agent:          %s (healthy=%v)\n", exec.Name(), healthy)
	fmt.Printf("Advertised:     %s\n", strings.Join(caps, ", "))
	fmt.Printf("Granted:        %s\n", strings.Join(rep.Granted, ", "))
	fmt.Printf("Effective:      %s\n", strings.Join(rep.Effective, ", "))
	fmt.Printf("Paused:         %v\n", rep.Paused)
	if len(rep.Effective) == 0 {
		fmt.Println("Note: nothing is both granted and advertised, so this host will be given no work. Grant capabilities in the app.")
	}
	// The sandbox is the gate for ever turning the permission-bypass flag on (ADR-075). This build does
	// not use that flag, so the result is informational here; it tells you whether M3 can run on this machine.
	fmt.Printf("Permissions:    %s\n", policyStatus(ctx, c))
	fmt.Printf("Sandbox:        %s\n", sandboxStatus(ctx))
	if herr != nil {
		return fmt.Errorf("agent health check: %w", herr)
	}
	if !healthy {
		return errors.New("agent reports unhealthy (is it signed in? try `agy auth login`)")
	}
	return nil
}

// validateAPIURL requires https, except for a loopback address, because the token is a bearer secret.
func validateAPIURL(raw string) error {
	if raw == "" {
		return errors.New("GRIFFIN_API_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("GRIFFIN_API_URL is not a valid URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		h := u.Hostname()
		if h == "localhost" || h == "127.0.0.1" || h == "::1" {
			return nil
		}
	}
	return errors.New("GRIFFIN_API_URL must be https (plain http is allowed only for localhost): the host token is a bearer secret")
}

// loadToken reads the token from a file (preferred) or the environment. It warns when the file is
// readable by others. The token is never logged.
func loadToken(logger *slog.Logger) (string, error) {
	if path := strings.TrimSpace(os.Getenv("GRIFFIN_HOST_TOKEN_FILE")); path != "" {
		st, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		if st.Mode().Perm()&0o077 != 0 {
			logger.Warn("token file is readable by other users; run chmod 600 on it", "path", path, "mode", st.Mode().Perm().String())
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
		return "", errors.New("token file is empty")
	}
	if t := strings.TrimSpace(os.Getenv("GRIFFIN_HOST_TOKEN")); t != "" {
		return t, nil
	}
	return "", errors.New("set GRIFFIN_HOST_TOKEN_FILE (preferred) or GRIFFIN_HOST_TOKEN")
}

// parseCapabilities validates the advertised capabilities. Unless allowAll (mock executor), each must
// be one this build can back.
func parseCapabilities(raw string, allowAll bool) ([]string, error) {
	caps, err := domain.NormalizeHostCapabilities(strings.Split(raw, ","))
	if err != nil {
		return nil, err
	}
	if len(caps) == 0 {
		return nil, errors.New("HOST_CAPABILITIES is empty")
	}
	if !allowAll {
		for _, c := range caps {
			if !backedCapabilities[c] {
				return nil, fmt.Errorf("capability %q has no implementation in this build: advertising it would make Griffin hand this host steps it would answer as plain text. Remove it from HOST_CAPABILITIES", c)
			}
		}
	}
	return caps, nil
}

func defaultWorkspaceRoot() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".griffin-host", "workspaces")
	}
	return filepath.Join(os.TempDir(), "griffin-host-workspaces")
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// sandboxStatus runs the canary self-test and describes the result in one line.
func sandboxStatus(ctx context.Context) string {
	bw, err := sandbox.Available(ctx)
	if err != nil {
		return "NOT AVAILABLE (" + err.Error() + "). The bypass flag must stay off on this machine."
	}
	if err := sandbox.SelfTest(ctx, bw); err != nil {
		return "SELF-TEST FAILED: " + err.Error()
	}
	return "ok (bubblewrap; a file outside the allow-list is invisible from inside)"
}

// policyStatus fetches the owner's permission policy and summarizes it in one line.
func policyStatus(ctx context.Context, c *hostagent.Client) string {
	v, err := c.Policy(ctx)
	if err != nil {
		return "could not fetch (" + err.Error() + "). Until it can be fetched this host denies every outward action."
	}
	if v.Version == 0 {
		return fmt.Sprintf("none saved yet; the safe default applies (read and draft allowed, everything else asks). %d rules", len(v.Policy.Rules))
	}
	return fmt.Sprintf("version %d, default %s, %d rules", v.Version, v.Policy.Default, len(v.Policy.Rules))
}
