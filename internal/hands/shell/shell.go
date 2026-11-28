// Package shell is the laptop host's hands for the owner's own computer: run a command, drive a visible tmux
// terminal, and open desktop apps. It runs as the owner, so the host opts in to it explicitly (HOST_SHELL=on) and
// every op is written to the host's audit log. Nothing here talks to the network.
package shell

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/desktop"
)

// OutputCap is the most characters of stdout, stderr or terminal text one result carries; the tail is kept.
const OutputCap = 20000

// Cmd is one process to run.
type Cmd struct {
	Name string
	Args []string
	Dir  string
	Env  []string // extra KEY=VALUE entries layered over the host's own environment
}

// Result is a finished process.
type Result struct {
	Stdout, Stderr string
	Exit           int
	TimedOut       bool
}

// Runner starts processes. ExecRunner is the real one; tests use a fake so no terminal or tmux is touched.
type Runner interface {
	// Run waits for the process (or the context) and returns what it printed. A non-zero exit is not an error.
	Run(ctx context.Context, c Cmd) (Result, error)
	// Start launches a process that outlives this call (a terminal window or an app).
	Start(c Cmd) error
	// LookPath says whether a program is installed.
	LookPath(name string) (string, error)
}

// AuditFunc records one op. detail never holds more than the command the owner can read back.
type AuditFunc func(op domain.ShellOp, session, detail string, ok bool, note string)

// Service runs shell, terminal and app ops.
type Service struct {
	Runner Runner
	// AllowShell is false on a host that only offers app launching: shell and terminal ops are refused.
	AllowShell bool
	// AppDirs are the folders searched for .desktop files.
	AppDirs []string
	// SessionEnv returns the graphical session's variables (Wayland, Hyprland, D-Bus); see DetectSessionEnv.
	SessionEnv func(ctx context.Context) []string
	Audit      AuditFunc
}

// NewService builds the service for this machine.
func NewService(allowShell bool, audit AuditFunc) *Service {
	r := ExecRunner{}
	s := &Service{Runner: r, AllowShell: allowShell, AppDirs: DefaultAppDirs(), Audit: audit}
	s.SessionEnv = func(ctx context.Context) []string { return DetectSessionEnv(ctx, r, os.Getenv) }
	return s
}

func (s *Service) audit(op domain.ShellOp, session, detail string, err error) {
	if s.Audit == nil {
		return
	}
	note := ""
	if err != nil {
		note = err.Error()
	}
	s.Audit(op, session, detail, err == nil, note)
}

// Do runs one action and returns the text to report back to the owner.
func (s *Service) Do(ctx context.Context, a *domain.ShellAction) (string, error) {
	if a.Op.RequiredCapability() == domain.HostCapShell && !s.AllowShell {
		return "", errors.New("shell access is not switched on for this host (set HOST_SHELL=on)")
	}
	var text, detail string
	var err error
	switch a.Op {
	case domain.ShellOpRun:
		detail = a.Command
		text, err = s.run(ctx, a)
	case domain.ShellOpTerminalOpen:
		detail = strings.TrimSpace(a.Command)
		text, err = s.terminalOpen(ctx, a)
	case domain.ShellOpTerminalSend:
		detail = a.Text
		text, err = s.terminalSend(ctx, a)
	case domain.ShellOpTerminalRead:
		text, err = s.terminalRead(ctx, a)
	case domain.ShellOpAppOpen:
		detail = a.Name
		text, err = s.appOpen(ctx, a)
	case domain.ShellOpAppList:
		detail = a.Name
		text, err = s.appList(a)
	default:
		return "", fmt.Errorf("unsupported shell op %q", a.Op)
	}
	s.audit(a.Op, a.Session, detail, err)
	return text, err
}

func (s *Service) env(ctx context.Context) []string {
	if s.SessionEnv == nil {
		return nil
	}
	return s.SessionEnv(ctx)
}

// ---- shell_run ----

func (s *Service) run(ctx context.Context, a *domain.ShellAction) (string, error) {
	dir, err := resolveDir(a.Cwd)
	if err != nil {
		return "", err
	}
	timeout := time.Duration(a.TimeoutS) * time.Second
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := s.Runner.Run(rctx, Cmd{Name: "bash", Args: []string{"-lc", a.Command}, Dir: dir, Env: s.env(ctx)})
	if err != nil {
		return "", fmt.Errorf("could not run the command: %w", err)
	}
	var b strings.Builder
	switch {
	case res.TimedOut:
		fmt.Fprintf(&b, "Timed out after %ds and was stopped.", a.TimeoutS)
	default:
		fmt.Fprintf(&b, "Exit code %d.", res.Exit)
	}
	if out := tail(res.Stdout, OutputCap); strings.TrimSpace(out) != "" {
		b.WriteString("\n--- stdout ---\n" + out)
	}
	if out := tail(res.Stderr, OutputCap); strings.TrimSpace(out) != "" {
		b.WriteString("\n--- stderr ---\n" + out)
	}
	return b.String(), nil
}

func resolveDir(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", nil
	}
	if cwd == "~" || strings.HasPrefix(cwd, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cwd = filepath.Join(home, strings.TrimPrefix(cwd, "~"))
	}
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd %q must be an absolute path or start with ~", cwd)
	}
	return cwd, nil
}

// tail keeps the last n characters.
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "[earlier output cut]\n" + string(r[len(r)-n:])
}

// ---- terminals ----

func tmuxName(session string) string { return "griffin-" + session }

// target names a session exactly (no prefix matching).
func tmuxTarget(session string) string { return "=" + tmuxName(session) + ":" }

func (s *Service) tmux(ctx context.Context, args ...string) (Result, error) {
	return s.Runner.Run(ctx, Cmd{Name: "tmux", Args: args, Env: s.env(ctx)})
}

func (s *Service) hasSession(ctx context.Context, session string) (bool, error) {
	res, err := s.tmux(ctx, "has-session", "-t", "="+tmuxName(session))
	if err != nil {
		return false, fmt.Errorf("tmux is not available: %w", err)
	}
	return res.Exit == 0, nil
}

// terminalCommand is how each terminal is told to run tmux attach.
func terminalCommand(bin, tmuxArgs0 string, attach []string) []string {
	switch filepath.Base(bin) {
	case "ghostty", "alacritty":
		return append([]string{"-e", tmuxArgs0}, attach...)
	default: // xdg-terminal-exec and kitty take the command directly
		return append([]string{tmuxArgs0}, attach...)
	}
}

func (s *Service) terminalOpen(ctx context.Context, a *domain.ShellAction) (string, error) {
	dir, err := resolveDir(a.Cwd)
	if err != nil {
		return "", err
	}
	exists, err := s.hasSession(ctx, a.Session)
	if err != nil {
		return "", err
	}
	name := tmuxName(a.Session)
	var b strings.Builder
	if !exists {
		args := []string{"new-session", "-d", "-s", name}
		if dir != "" {
			args = append(args, "-c", dir)
		}
		if res, err := s.tmux(ctx, args...); err != nil || res.Exit != 0 {
			return "", fmt.Errorf("could not create the tmux session: %s", firstNonEmpty(res.Stderr, errText(err)))
		}
		fmt.Fprintf(&b, "Created terminal %q.", a.Session)
	} else {
		fmt.Fprintf(&b, "Terminal %q already existed.", a.Session)
		if dir != "" {
			b.WriteString(" (cwd ignored for an existing terminal; cd in it if needed)")
		}
	}
	if cmd := strings.TrimSpace(a.Command); cmd != "" {
		if err := s.sendKeys(ctx, a.Session, cmd, true); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, " Typed: %s", truncate(cmd, 200))
	}
	// A visible window only when none is attached yet, so asking twice does not stack windows.
	if cl, err := s.tmux(ctx, "list-clients", "-t", "="+name); err == nil && cl.Exit == 0 && strings.TrimSpace(cl.Stdout) != "" {
		b.WriteString(" A window is already showing it.")
		return b.String(), nil
	}
	term, err := s.openWindow(ctx, name)
	if err != nil {
		return "", fmt.Errorf("%s, but no window could be opened: %w", b.String(), err)
	}
	fmt.Fprintf(&b, " Opened a window with %s.", term)
	return b.String(), nil
}

// openWindow starts a visible terminal attached to the tmux session and says which one it used.
func (s *Service) openWindow(ctx context.Context, name string) (string, error) {
	attach := []string{"attach", "-t", "=" + name}
	for _, term := range []string{"xdg-terminal-exec", "ghostty", "alacritty", "kitty"} {
		bin, err := s.Runner.LookPath(term)
		if err != nil {
			continue
		}
		args := terminalCommand(bin, "tmux", attach)
		if err := s.Runner.Start(Cmd{Name: bin, Args: args, Env: s.env(ctx)}); err != nil {
			return "", fmt.Errorf("could not start %s: %w", term, err)
		}
		return term, nil
	}
	return "", errors.New("no terminal program found (looked for xdg-terminal-exec, ghostty, alacritty, kitty)")
}

func (s *Service) sendKeys(ctx context.Context, session, text string, enter bool) error {
	if res, err := s.tmux(ctx, "send-keys", "-t", tmuxTarget(session), "-l", "--", text); err != nil || res.Exit != 0 {
		return fmt.Errorf("could not type into the terminal: %s", firstNonEmpty(res.Stderr, errText(err)))
	}
	if enter {
		if res, err := s.tmux(ctx, "send-keys", "-t", tmuxTarget(session), "Enter"); err != nil || res.Exit != 0 {
			return fmt.Errorf("could not press Enter in the terminal: %s", firstNonEmpty(res.Stderr, errText(err)))
		}
	}
	return nil
}

func (s *Service) terminalSend(ctx context.Context, a *domain.ShellAction) (string, error) {
	exists, err := s.hasSession(ctx, a.Session)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("there is no terminal named %q; open it first", a.Session)
	}
	if err := s.sendKeys(ctx, a.Session, a.Text, !a.NoEnter); err != nil {
		return "", err
	}
	if a.NoEnter {
		return fmt.Sprintf("Typed into terminal %q (no Enter).", a.Session), nil
	}
	return fmt.Sprintf("Typed into terminal %q and pressed Enter.", a.Session), nil
}

func (s *Service) terminalRead(ctx context.Context, a *domain.ShellAction) (string, error) {
	exists, err := s.hasSession(ctx, a.Session)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("there is no terminal named %q", a.Session)
	}
	res, err := s.tmux(ctx, "capture-pane", "-p", "-J", "-t", tmuxTarget(a.Session), "-S", "-"+strconv.Itoa(a.Lines))
	if err != nil || res.Exit != 0 {
		return "", fmt.Errorf("could not read the terminal: %s", firstNonEmpty(res.Stderr, errText(err)))
	}
	out := strings.TrimRight(res.Stdout, " \n\t")
	if out == "" {
		return fmt.Sprintf("Terminal %q is empty.", a.Session), nil
	}
	return fmt.Sprintf("Last lines of terminal %q:\n%s", a.Session, tail(out, OutputCap)), nil
}

// ---- apps ----

// App is one launchable desktop entry.
type App struct {
	ID   string // the .desktop file name without the extension
	Name string
	Exec string
}

// DefaultAppDirs are the standard places .desktop files live.
func DefaultAppDirs() []string {
	dirs := []string{"/usr/share/applications", "/usr/local/share/applications", "/var/lib/flatpak/exports/share/applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local/share/applications"), filepath.Join(home, ".local/share/flatpak/exports/share/applications"))
	}
	return dirs
}

func (s *Service) apps() []App {
	seen := map[string]bool{}
	var out []App
	for _, d := range s.AppDirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".desktop")
			if seen[id] {
				continue
			}
			if app, ok := parseDesktop(filepath.Join(d, e.Name()), id); ok {
				seen[id] = true
				out = append(out, app)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func parseDesktop(path, id string) (App, bool) {
	f, err := os.Open(path)
	if err != nil {
		return App{}, false
	}
	defer f.Close()
	app := App{ID: id}
	inEntry, hidden, isApp := false, false, true
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "Name":
			app.Name = v
		case "Exec":
			app.Exec = v
		case "NoDisplay", "Hidden":
			if strings.EqualFold(v, "true") {
				hidden = true
			}
		case "Type":
			isApp = v == "Application"
		}
	}
	if hidden || !isApp || app.Name == "" {
		return App{}, false
	}
	return app, true
}

// matchApps ranks apps for a query: exact name or id first, then names or ids that contain it.
func matchApps(apps []App, query string) []App {
	q := strings.ToLower(strings.TrimSpace(query))
	var exact, partial []App
	for _, a := range apps {
		name, id := strings.ToLower(a.Name), strings.ToLower(a.ID)
		switch {
		case q == "" || name == q || id == q:
			exact = append(exact, a)
		case strings.Contains(name, q) || strings.Contains(id, q) || strings.Contains(strings.ToLower(a.Exec), q):
			partial = append(partial, a)
		}
	}
	if q == "" {
		return exact
	}
	return append(exact, partial...)
}

func (s *Service) appList(a *domain.ShellAction) (string, error) {
	ms := matchApps(s.apps(), a.Name)
	if len(ms) == 0 {
		return fmt.Sprintf("No desktop apps match %q.", a.Name), nil
	}
	const max = 25
	var b strings.Builder
	fmt.Fprintf(&b, "%d matching apps (name | id):", len(ms))
	for i, m := range ms {
		if i == max {
			fmt.Fprintf(&b, "\n... and %d more", len(ms)-max)
			break
		}
		fmt.Fprintf(&b, "\n%s | %s", m.Name, m.ID)
	}
	return b.String(), nil
}

func (s *Service) appOpen(ctx context.Context, a *domain.ShellAction) (string, error) {
	if desktop.IsPaymentTarget(a.Name) {
		return "", fmt.Errorf("launching or automating payment/banking application %q is strictly forbidden", a.Name)
	}
	ms := matchApps(s.apps(), a.Name)
	if len(ms) == 0 {
		return "", fmt.Errorf("no desktop app matches %q; list apps to see what is installed", a.Name)
	}
	app := ms[0]
	if desktop.IsPaymentTarget(app.Name) || desktop.IsPaymentTarget(app.ID) {
		return "", fmt.Errorf("launching or automating payment/banking application %q is strictly forbidden", app.Name)
	}
	env := s.env(ctx)
	if bin, err := s.Runner.LookPath("gtk-launch"); err == nil {
		if err := s.Runner.Start(Cmd{Name: bin, Args: []string{app.ID}, Env: env}); err != nil {
			return "", fmt.Errorf("could not launch %s: %w", app.Name, err)
		}
		return fmt.Sprintf("Launched %s (%s) with gtk-launch.", app.Name, app.ID), nil
	}
	if bin, err := s.Runner.LookPath("hyprctl"); err == nil {
		if err := s.Runner.Start(Cmd{Name: bin, Args: []string{"dispatch", "exec", stripFieldCodes(app.Exec)}, Env: env}); err != nil {
			return "", fmt.Errorf("could not launch %s: %w", app.Name, err)
		}
		return fmt.Sprintf("Launched %s (%s) with hyprctl.", app.Name, app.ID), nil
	}
	return "", errors.New("neither gtk-launch nor hyprctl is installed, so apps cannot be launched")
}

// stripFieldCodes removes the %f %u style placeholders of a .desktop Exec line.
func stripFieldCodes(exec string) string {
	var parts []string
	for _, f := range strings.Fields(exec) {
		if len(f) == 2 && f[0] == '%' {
			continue
		}
		parts = append(parts, f)
	}
	return strings.Join(parts, " ")
}

// ---- session environment ----

// sessionVars are the graphical-session variables a systemd user service does not inherit by itself.
var sessionVars = []string{"WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "HYPRLAND_INSTANCE_SIGNATURE", "DISPLAY",
	"DBUS_SESSION_BUS_ADDRESS", "XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "XDG_SESSION_DESKTOP"}

// DetectSessionEnv finds the variables a visible window needs. Order of trust: the service's own environment,
// then `systemctl --user show-environment` (where the compositor exports them), then the runtime directory
// itself (the newest hypr/<signature> folder and the first wayland-N socket).
func DetectSessionEnv(ctx context.Context, r Runner, getenv func(string) string) []string {
	have := map[string]string{}
	for _, k := range sessionVars {
		if v := getenv(k); v != "" {
			have[k] = v
		}
	}
	if res, err := r.Run(ctx, Cmd{Name: "systemctl", Args: []string{"--user", "show-environment"}}); err == nil && res.Exit == 0 {
		for _, line := range strings.Split(res.Stdout, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			for _, want := range sessionVars {
				if k == want && have[k] == "" {
					have[k] = strings.Trim(v, "'")
				}
			}
		}
	}
	if have["XDG_RUNTIME_DIR"] == "" {
		have["XDG_RUNTIME_DIR"] = "/run/user/" + strconv.Itoa(os.Getuid())
	}
	rt := have["XDG_RUNTIME_DIR"]
	if have["HYPRLAND_INSTANCE_SIGNATURE"] == "" {
		if dirs, err := os.ReadDir(filepath.Join(rt, "hypr")); err == nil {
			var newest string
			var newestT time.Time
			for _, d := range dirs {
				if info, err := d.Info(); err == nil && d.IsDir() && info.ModTime().After(newestT) {
					newest, newestT = d.Name(), info.ModTime()
				}
			}
			if newest != "" {
				have["HYPRLAND_INSTANCE_SIGNATURE"] = newest
			}
		}
	}
	if have["WAYLAND_DISPLAY"] == "" {
		if socks, _ := filepath.Glob(filepath.Join(rt, "wayland-[0-9]*")); len(socks) > 0 {
			sort.Strings(socks)
			for _, sk := range socks {
				if !strings.HasSuffix(sk, ".lock") {
					have["WAYLAND_DISPLAY"] = filepath.Base(sk)
					break
				}
			}
		}
	}
	if have["DBUS_SESSION_BUS_ADDRESS"] == "" {
		if _, err := os.Stat(filepath.Join(rt, "bus")); err == nil {
			have["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + filepath.Join(rt, "bus")
		}
	}
	var out []string
	for _, k := range sessionVars {
		if v := have[k]; v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// ---- real runner ----

// ExecRunner runs real processes.
type ExecRunner struct{}

func mergedEnv(extra []string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	keys := map[string]bool{}
	for _, e := range extra {
		k, _, _ := strings.Cut(e, "=")
		keys[k] = true
	}
	var env []string
	for _, e := range os.Environ() {
		if k, _, _ := strings.Cut(e, "="); !keys[k] {
			env = append(env, e)
		}
	}
	return append(env, extra...)
}

// Run implements Runner. The process gets its own group so a timeout stops everything it started.
func (ExecRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = mergedEnv(c.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() != nil {
		res.TimedOut, res.Exit = true, -1
		return res, nil
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.Exit = ee.ExitCode()
	default:
		return res, err
	}
	return res, nil
}

// Start implements Runner: the process is detached and reaped in the background.
func (ExecRunner) Start(c Cmd) error {
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = mergedEnv(c.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// LookPath implements Runner.
func (ExecRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return "unknown error"
}
