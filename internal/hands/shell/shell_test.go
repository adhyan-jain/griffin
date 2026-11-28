package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
)

type call struct {
	Cmd
	started bool
}

// fakeRunner records every process and answers from a script; nothing real runs.
type fakeRunner struct {
	calls     []call
	installed map[string]bool
	sessions  map[string]bool
	clients   string
	pane      string
	runResult Result
}

func (f *fakeRunner) Run(_ context.Context, c Cmd) (Result, error) {
	f.calls = append(f.calls, call{Cmd: c})
	if c.Name == "tmux" {
		switch c.Args[0] {
		case "has-session":
			if f.sessions[strings.TrimPrefix(c.Args[2], "=")] {
				return Result{}, nil
			}
			return Result{Exit: 1}, nil
		case "new-session":
			f.sessions[c.Args[3]] = true
		case "list-clients":
			return Result{Stdout: f.clients}, nil
		case "capture-pane":
			return Result{Stdout: f.pane}, nil
		}
		return Result{}, nil
	}
	return f.runResult, nil
}
func (f *fakeRunner) Start(c Cmd) error {
	f.calls = append(f.calls, call{Cmd: c, started: true})
	return nil
}
func (f *fakeRunner) LookPath(n string) (string, error) {
	if f.installed[n] {
		return "/usr/bin/" + n, nil
	}
	return "", os.ErrNotExist
}

func newFake() *fakeRunner {
	return &fakeRunner{installed: map[string]bool{"xdg-terminal-exec": true, "gtk-launch": true}, sessions: map[string]bool{}}
}

type auditLine struct {
	op      domain.ShellOp
	detail  string
	ok      bool
	session string
}

func svc(f *fakeRunner, allowShell bool, audit *[]auditLine) *Service {
	return &Service{Runner: f, AllowShell: allowShell, AppDirs: nil,
		SessionEnv: func(context.Context) []string { return []string{"WAYLAND_DISPLAY=wayland-1"} },
		Audit: func(op domain.ShellOp, session, detail string, ok bool, _ string) {
			*audit = append(*audit, auditLine{op, detail, ok, session})
		}}
}

func TestShellRunReportsExitAndTail(t *testing.T) {
	f := newFake()
	f.runResult = Result{Stdout: strings.Repeat("a", OutputCap+500) + "END", Stderr: "warn", Exit: 3}
	var au []auditLine
	out, err := svc(f, true, &au).Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpRun, Command: "ls", Cwd: "/tmp", TimeoutS: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Exit code 3") || !strings.HasSuffix(strings.Split(out, "--- stderr ---")[0], "END\n") || !strings.Contains(out, "warn") || !strings.Contains(out, "earlier output cut") {
		t.Fatalf("unexpected output: %.200s", out)
	}
	c := f.calls[0]
	if c.Name != "bash" || c.Args[0] != "-lc" || c.Args[1] != "ls" || c.Dir != "/tmp" || c.Env[0] != "WAYLAND_DISPLAY=wayland-1" {
		t.Fatalf("bad command: %+v", c)
	}
	if len(au) != 1 || au[0].op != domain.ShellOpRun || au[0].detail != "ls" || !au[0].ok {
		t.Fatalf("audit: %+v", au)
	}
}

func TestShellRunTimeoutAndRelativeCwd(t *testing.T) {
	f := newFake()
	f.runResult = Result{Stdout: "partial", TimedOut: true}
	var au []auditLine
	s := svc(f, true, &au)
	out, _ := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpRun, Command: "sleep 99", TimeoutS: 1})
	if !strings.Contains(out, "Timed out after 1s") || !strings.Contains(out, "partial") {
		t.Fatalf("got %q", out)
	}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpRun, Command: "ls", Cwd: "relative/dir", TimeoutS: 1}); err == nil {
		t.Fatal("a relative cwd must be refused")
	}
	if au[len(au)-1].ok {
		t.Fatal("a refused op must be audited as failed")
	}
}

func TestShellDisabledRefusesShellButNotApps(t *testing.T) {
	f := newFake()
	var au []auditLine
	s := svc(f, false, &au)
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpRun, Command: "ls", TimeoutS: 5}); err == nil {
		t.Fatal("shell must be refused when not switched on")
	}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalOpen, Session: "a"}); err == nil {
		t.Fatal("terminal must be refused when not switched on")
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing may run: %+v", f.calls)
	}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpAppList, Name: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalOpenCreatesSessionTypesAndOpensWindow(t *testing.T) {
	f := newFake()
	var au []auditLine
	out, err := svc(f, true, &au).Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalOpen, Session: "work", Cwd: "/srv", Command: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range f.calls {
		names = append(names, c.Name+" "+strings.Join(c.Args, " "))
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{"tmux new-session -d -s griffin-work -c /srv", "tmux send-keys -t =griffin-work: -l -- claude", "tmux send-keys -t =griffin-work: Enter", "/usr/bin/xdg-terminal-exec tmux attach -t =griffin-work"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if !strings.Contains(out, "xdg-terminal-exec") || !f.calls[len(f.calls)-1].started {
		t.Fatalf("window not opened: %s", out)
	}
}

func TestTerminalOpenExistingAttachedDoesNotStackWindows(t *testing.T) {
	f := newFake()
	f.sessions["griffin-work"] = true
	f.clients = "/dev/pts/3: griffin-work"
	var au []auditLine
	out, err := svc(f, true, &au).Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalOpen, Session: "work"})
	if err != nil || !strings.Contains(out, "already") {
		t.Fatalf("%v %q", err, out)
	}
	for _, c := range f.calls {
		if c.started {
			t.Fatal("no new window expected")
		}
	}
}

func TestTerminalFallbackToGhosttyUsesDashE(t *testing.T) {
	f := newFake()
	f.installed = map[string]bool{"ghostty": true}
	var au []auditLine
	if _, err := svc(f, true, &au).Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalOpen, Session: "w"}); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if last.Name != "/usr/bin/ghostty" || strings.Join(last.Args, " ") != "-e tmux attach -t =griffin-w" {
		t.Fatalf("%+v", last)
	}
}

func TestTerminalNoTerminalInstalled(t *testing.T) {
	f := newFake()
	f.installed = map[string]bool{}
	var au []auditLine
	if _, err := svc(f, true, &au).Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalOpen, Session: "w"}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestTerminalSendAndRead(t *testing.T) {
	f := newFake()
	f.sessions["griffin-w"] = true
	f.pane = "line1\nline2\n\n"
	var au []auditLine
	s := svc(f, true, &au)
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalSend, Session: "w", Text: "/rc"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls[len(f.calls)-2].Args, " "), "-l -- /rc") || f.calls[len(f.calls)-1].Args[len(f.calls[len(f.calls)-1].Args)-1] != "Enter" {
		t.Fatalf("send-keys wrong: %+v", f.calls)
	}
	out, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalRead, Session: "w", Lines: 30})
	if err != nil || !strings.HasSuffix(out, "line1\nline2") {
		t.Fatalf("%v %q", err, out)
	}
	if strings.Join(f.calls[len(f.calls)-1].Args, " ") != "capture-pane -p -J -t =griffin-w: -S -30" {
		t.Fatalf("%+v", f.calls[len(f.calls)-1])
	}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpTerminalSend, Session: "ghost", Text: "x"}); err == nil {
		t.Fatal("sending to a missing terminal must fail")
	}
}

func writeApp(t *testing.T, dir, id, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".desktop"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAppListAndOpen(t *testing.T) {
	dir := t.TempDir()
	writeApp(t, dir, "org.gnome.Nautilus", "[Desktop Entry]\nType=Application\nName=Files\nExec=nautilus --new-window %U\n")
	writeApp(t, dir, "firefox", "[Desktop Entry]\nType=Application\nName=Firefox\nExec=firefox %u\n")
	writeApp(t, dir, "hidden", "[Desktop Entry]\nType=Application\nName=Firefox Hidden\nNoDisplay=true\nExec=x\n")
	writeApp(t, dir, "hdfc-bank", "[Desktop Entry]\nType=Application\nName=Bank\nExec=x\n")
	f := newFake()
	var au []auditLine
	s := svc(f, true, &au)
	s.AppDirs = []string{dir}
	out, _ := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpAppList, Name: "fire"})
	if !strings.Contains(out, "Firefox | firefox") || strings.Contains(out, "Hidden") {
		t.Fatalf("%q", out)
	}
	out, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpAppOpen, Name: "files"})
	if err != nil || !strings.Contains(out, "Files") {
		t.Fatalf("%v %q", err, out)
	}
	last := f.calls[len(f.calls)-1]
	if last.Name != "/usr/bin/gtk-launch" || last.Args[0] != "org.gnome.Nautilus" || !last.started {
		t.Fatalf("%+v", last)
	}
	// hyprctl fallback strips field codes.
	f.installed = map[string]bool{"hyprctl": true}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpAppOpen, Name: "files"}); err != nil {
		t.Fatal(err)
	}
	last = f.calls[len(f.calls)-1]
	if strings.Join(last.Args, " ") != "dispatch exec nautilus --new-window" {
		t.Fatalf("%+v", last)
	}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpAppOpen, Name: "hdfc-bank"}); err == nil {
		t.Fatal("banking apps are blocked")
	}
	if _, err := s.Do(context.Background(), &domain.ShellAction{Op: domain.ShellOpAppOpen, Name: "nonexistent"}); err == nil {
		t.Fatal("unknown app must fail")
	}
}

func TestDetectSessionEnvReadsSystemd(t *testing.T) {
	f := newFake()
	f.runResult = Result{Stdout: "WAYLAND_DISPLAY=wayland-1\nHYPRLAND_INSTANCE_SIGNATURE=abc_123\nXDG_RUNTIME_DIR=/run/user/1000\nPATH=/x\n"}
	env := DetectSessionEnv(context.Background(), f, func(string) string { return "" })
	got := strings.Join(env, " ")
	for _, want := range []string{"WAYLAND_DISPLAY=wayland-1", "HYPRLAND_INSTANCE_SIGNATURE=abc_123", "XDG_RUNTIME_DIR=/run/user/1000"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "PATH") {
		t.Fatal("only session variables are taken")
	}
}
