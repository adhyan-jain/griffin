package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireSandbox(t *testing.T) string {
	t.Helper()
	bw, err := Available(context.Background())
	if err != nil {
		t.Skipf("sandbox not usable on this machine: %v", err)
	}
	return bw
}

func TestSelfTestPassesWhereBubblewrapWorks(t *testing.T) {
	bw := requireSandbox(t)
	if err := SelfTest(context.Background(), bw); err != nil {
		t.Fatalf("self-test failed: %v", err)
	}
}

func TestAFileOutsideTheAllowListIsInvisibleAndTheScratchIsNot(t *testing.T) {
	bw := requireSandbox(t)
	root := t.TempDir()
	scratch, forbidden := filepath.Join(root, "s"), filepath.Join(root, "f")
	_ = os.Mkdir(scratch, 0o700)
	_ = os.Mkdir(forbidden, 0o700)
	secret := filepath.Join(forbidden, "ssh_key")
	_ = os.WriteFile(secret, []byte("PRIVATE"), 0o600)

	cmd, err := Command(context.Background(), Config{Scratch: scratch, Bwrap: bw}, "/bin/sh", "-c",
		"cat "+secret+" 2>&1; echo scratch-ok > "+filepath.Join(scratch, "out"))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "PRIVATE") {
		t.Fatalf("the secret leaked into the sandbox: %s", out)
	}
	if b, err := os.ReadFile(filepath.Join(scratch, "out")); err != nil || !strings.Contains(string(b), "scratch-ok") {
		t.Fatalf("the scratch folder must be writable and visible outside afterwards: %v", err)
	}
}

func TestNetworkIsOffByDefault(t *testing.T) {
	bw := requireSandbox(t)
	scratch := t.TempDir()
	cmd, _ := Command(context.Background(), Config{Scratch: scratch, Bwrap: bw}, "/bin/sh", "-c", "ls /sys/class/net 2>/dev/null | grep -v '^lo$' | head -1; echo done")
	out, _ := cmd.CombinedOutput()
	if strings.TrimSpace(string(out)) != "done" {
		t.Fatalf("no network interface other than loopback may exist by default, saw %q", out)
	}
}

func TestEnvironmentIsCleared(t *testing.T) {
	bw := requireSandbox(t)
	t.Setenv("GRIFFIN_HOST_TOKEN", "ghk_must_not_reach_the_agent")
	scratch := t.TempDir()
	cmd, _ := Command(context.Background(), Config{Scratch: scratch, Bwrap: bw}, "/bin/sh", "-c", "env")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "ghk_") || strings.Contains(string(out), "GRIFFIN_HOST_TOKEN") {
		t.Fatalf("the host token must never be visible to the sandboxed agent: %s", out)
	}
}

func TestConfigRefusesUnsafePaths(t *testing.T) {
	for name, c := range map[string]Config{
		"no scratch":         {},
		"relative scratch":   {Scratch: "relative/dir"},
		"whole filesystem":   {Scratch: t.TempDir(), ReadWrite: []string{"/"}},
		"missing path":       {Scratch: t.TempDir(), ReadOnly: []string{"/definitely/not/here"}},
		"relative allowlist": {Scratch: t.TempDir(), ReadWrite: []string{"home/me"}},
	} {
		if _, err := c.Args(); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestMissingBubblewrapIsReportedAsUnavailableNeverAsSafe(t *testing.T) {
	_, err := Command(context.Background(), Config{Scratch: t.TempDir(), Bwrap: ""}, "true")
	if err != nil && !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Available(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("with no bwrap on PATH Available must say unavailable, got %v", err)
	}
	if err := SelfTest(context.Background(), "/nonexistent/bwrap"); !errors.Is(err, ErrUnavailable) && err == nil {
		t.Fatal("a broken bwrap must never pass the self-test")
	}
}

// A "sandbox" that isolates nothing must FAIL the self-test: otherwise the check proves nothing.
func TestSelfTestCatchesASandboxThatIsolatesNothing(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "bwrap")
	// Skips every flag up to "--", then runs the command directly on the real filesystem.
	script := "#!/bin/sh\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	err := SelfTest(context.Background(), fake)
	if err == nil {
		t.Fatal("a fake sandbox that exposes the real filesystem passed the self-test")
	}
	if !strings.Contains(err.Error(), "FAILED") {
		t.Fatalf("expected the canary to catch it, got: %v", err)
	}
}
