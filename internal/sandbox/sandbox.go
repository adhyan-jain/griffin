// Package sandbox runs a command inside an OS sandbox (bubblewrap) that exposes only a scratch folder
// and an explicit allow-list. It is the enforcement layer PRD 7.16 puts BELOW the agent: it still holds
// when the agent's own permission prompts are off.
//
// SelfTest proves the sandbox is real on this machine by planting a canary file the agent must not be
// able to read. A host may only enable the permission-bypass flag after SelfTest passes (ADR-075).
package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrUnavailable means bubblewrap is missing or cannot create the sandbox here (for example user
// namespaces are disabled). Callers must treat this as "no sandbox", never as "sandbox not needed".
var ErrUnavailable = errors.New("sandbox unavailable")

// Config describes what the sandboxed process may see.
type Config struct {
	// Scratch is a per-task folder, mounted read-write and used as HOME and the working directory.
	Scratch string
	// ReadWrite are extra folders the owner allowed, mounted read-write.
	ReadWrite []string
	// ReadOnly are extra folders mounted read-only.
	ReadOnly []string
	// Network keeps network access. Off by default.
	Network bool
	// Bwrap is the bubblewrap binary; empty means look it up.
	Bwrap string
}

func (c Config) bwrap() (string, error) {
	if c.Bwrap != "" {
		return c.Bwrap, nil
	}
	p, err := exec.LookPath("bwrap")
	if err != nil {
		return "", fmt.Errorf("%w: bubblewrap (bwrap) is not installed", ErrUnavailable)
	}
	return p, nil
}

// systemMounts are the minimum read-only system paths a typical binary needs. Anything not listed is
// simply absent inside the sandbox, so a path such as ~/.ssh does not exist there.
var systemPaths = []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/lib32"}

var etcFiles = []string{
	"/etc/ld.so.cache", "/etc/ld.so.conf", "/etc/ld.so.conf.d", "/etc/resolv.conf", "/etc/hosts",
	"/etc/nsswitch.conf", "/etc/passwd", "/etc/group", "/etc/localtime", "/etc/ssl", "/etc/ca-certificates",
	"/etc/alternatives",
}

// Args builds the bubblewrap argument list (without the command). It validates every path.
func (c Config) Args() ([]string, error) {
	if c.Scratch == "" {
		return nil, errors.New("sandbox: a scratch folder is required")
	}
	scratch, err := cleanAbs(c.Scratch)
	if err != nil {
		return nil, err
	}
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--clearenv"}
	if c.Network {
		args = append(args, "--share-net")
	}
	for _, p := range systemPaths {
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err == nil {
				args = append(args, "--symlink", target, p)
			}
			continue
		}
		args = append(args, "--ro-bind", p, p)
	}
	for _, p := range etcFiles {
		if _, err := os.Stat(p); err == nil {
			args = append(args, "--ro-bind", p, p)
		}
	}
	args = append(args, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp")
	for _, p := range c.ReadOnly {
		abs, err := cleanAbs(p)
		if err != nil {
			return nil, err
		}
		args = append(args, "--ro-bind", abs, abs)
	}
	for _, p := range c.ReadWrite {
		abs, err := cleanAbs(p)
		if err != nil {
			return nil, err
		}
		args = append(args, "--bind", abs, abs)
	}
	args = append(args,
		"--bind", scratch, scratch,
		"--chdir", scratch,
		"--setenv", "HOME", scratch,
		"--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin",
		"--setenv", "TMPDIR", "/tmp",
	)
	return args, nil
}

func cleanAbs(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("sandbox: path %q must be absolute", p)
	}
	p = filepath.Clean(p)
	if p == "/" {
		return "", errors.New("sandbox: refusing to expose the whole filesystem")
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("sandbox: %w", err)
	}
	return p, nil
}

// Command returns a command that runs name inside the sandbox.
func Command(ctx context.Context, c Config, name string, args ...string) (*exec.Cmd, error) {
	bw, err := c.bwrap()
	if err != nil {
		return nil, err
	}
	base, err := c.Args()
	if err != nil {
		return nil, err
	}
	full := append(append(base, "--"), name)
	full = append(full, args...)
	return exec.CommandContext(ctx, bw, full...), nil
}

// SelfTest proves the sandbox on this machine. It checks that, from inside:
//  1. the sandbox starts and the scratch folder is writable;
//  2. a canary file planted outside the allow-list cannot be read (the check that matters);
//  3. a canary inside an allowed folder CAN be read (so check 2 is not passing because everything fails);
//  4. system paths are read-only.
//
// It returns ErrUnavailable (wrapped) when the sandbox cannot start at all.
func SelfTest(ctx context.Context, bwrap string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	root, err := os.MkdirTemp("", "griffin-sandbox-selftest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	scratch := filepath.Join(root, "scratch")
	allowed := filepath.Join(root, "allowed")
	forbidden := filepath.Join(root, "forbidden")
	for _, d := range []string{scratch, allowed, forbidden} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return err
		}
	}
	token := randomToken()
	secret := filepath.Join(forbidden, "canary")
	open := filepath.Join(allowed, "visible")
	if err := os.WriteFile(secret, []byte(token), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(open, []byte("ok"), 0o600); err != nil {
		return err
	}

	cfg := Config{Scratch: scratch, ReadOnly: []string{allowed}, Bwrap: bwrap}
	run := func(script string) (string, error) {
		cmd, err := Command(ctx, cfg, "/bin/sh", "-c", script)
		if err != nil {
			return "", err
		}
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// 1. it starts, and scratch is writable
	if out, err := run("echo probe > " + filepath.Join(scratch, "probe") + " && cat " + filepath.Join(scratch, "probe")); err != nil || !strings.Contains(out, "probe") {
		return fmt.Errorf("%w: cannot start a sandboxed shell (%v) %s", ErrUnavailable, err, firstLine(out))
	}
	// 2. the canary outside the allow-list must be unreadable (and must not even exist)
	if out, _ := run("cat " + secret + " 2>&1"); strings.Contains(out, token) {
		return errors.New("sandbox FAILED: a file outside the allow-list was readable from inside it")
	}
	if _, err := run("test -e " + secret); err == nil {
		return errors.New("sandbox FAILED: a path outside the allow-list exists inside it")
	}
	// 3. an allowed folder is readable
	if out, err := run("cat " + open); err != nil || !strings.Contains(out, "ok") {
		return errors.New("sandbox misconfigured: an allowed folder is not readable, so the canary result proves nothing")
	}
	// 4. system paths are read-only
	if _, err := run("echo x > /usr/griffin-selftest"); err == nil {
		return errors.New("sandbox FAILED: system paths are writable from inside it")
	}
	if _, err := os.Stat("/usr/griffin-selftest"); err == nil {
		return errors.New("sandbox FAILED: wrote to the real /usr")
	}
	return nil
}

// Available reports whether bwrap exists and can start a sandbox, without the full self-test.
func Available(ctx context.Context) (string, error) {
	bw, err := (Config{}).bwrap()
	if err != nil {
		return "", err
	}
	if err := exec.CommandContext(ctx, bw, "--ro-bind", "/", "/", "--unshare-all", "--die-with-parent", "true").Run(); err != nil {
		return bw, fmt.Errorf("%w: bubblewrap cannot create a sandbox here (%v); user namespaces may be disabled", ErrUnavailable, err)
	}
	return bw, nil
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "canary-" + hex.EncodeToString(b)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
