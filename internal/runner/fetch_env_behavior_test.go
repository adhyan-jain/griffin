package runner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runFetchEnv executes the real infra/helpers/griffin-fetch-env.sh with its /etc/griffin paths
// redirected to a temp dir and `aws` replaced by a stub that prints the given parameters.
func runFetchEnv(t *testing.T, params map[string]string) (combinedOut string, envFile string, err error) {
	t.Helper()
	// Not t.TempDir(): its path embeds the subtest name, whose parentheses/spaces would break the shell.
	dir, derr := os.MkdirTemp("", "fetchenv")
	if derr != nil {
		t.Fatal(derr)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	script, rerr := os.ReadFile("../../infra/helpers/griffin-fetch-env.sh")
	if rerr != nil {
		t.Fatal(rerr)
	}
	etc := filepath.Join(dir, "etc")
	if merr := os.MkdirAll(etc, 0o755); merr != nil {
		t.Fatal(merr)
	}
	patched := strings.ReplaceAll(string(script), "/etc/griffin", etc)
	// chown to root:griffin cannot work unprivileged in a test; keep the rest of the script intact.
	patched = strings.ReplaceAll(patched, `chown root:griffin "$TMP"`, `true`)
	sp := filepath.Join(dir, "fetch.sh")
	if werr := os.WriteFile(sp, []byte(patched), 0o755); werr != nil {
		t.Fatal(werr)
	}

	var lines strings.Builder
	for k, v := range params {
		lines.WriteString("/griffin/prod/worker/" + k + "\t" + v + "\n")
	}
	bin := filepath.Join(dir, "bin")
	if merr := os.MkdirAll(bin, 0o755); merr != nil {
		t.Fatal(merr)
	}
	stub := "#!/usr/bin/env bash\ncat <<'EOF'\n" + lines.String() + "EOF\n"
	if werr := os.WriteFile(filepath.Join(bin, "aws"), []byte(stub), 0o755); werr != nil {
		t.Fatal(werr)
	}

	cmd := exec.Command("bash", sp)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	b, _ := os.ReadFile(filepath.Join(etc, "worker.env"))
	return string(out), string(b), err
}

func TestFetchEnvBehavior(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	const secretDB = "postgresql://griffin_system.ref:SuperSecretPw@pooler.example.com:5432/postgres"
	base := map[string]string{
		"DATABASE_URL":      secretDB,
		"WORKER_API_KEY":    "worker-key-0123456789-0123456789-abcdef",
		"S3_BUCKET":         "griffin-artifacts-test",
		"SUPABASE_JWKS_URL": "https://ref.supabase.co/auth/v1/.well-known/jwks.json",
	}
	with := func(over map[string]string, drop ...string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		for _, k := range drop {
			delete(m, k)
		}
		return m
	}

	t.Run("asymmetric Supabase project (JWKS only, no JWT secret) is accepted", func(t *testing.T) {
		out, env, err := runFetchEnv(t, base)
		if err != nil {
			t.Fatalf("script failed: %v\n%s", err, out)
		}
		for _, want := range []string{"DATABASE_URL=", "SUPABASE_JWKS_URL=", "GRIFFIN_MODE=worker", "ENVIRONMENT=production", "DB_MAX_CONNS=5"} {
			if !strings.Contains(env, want) {
				t.Errorf("worker.env missing %s", want)
			}
		}
		if strings.Contains(out, "SuperSecretPw") {
			t.Error("script echoed a secret to its output")
		}
		if strings.Contains(env, "GEMINI_API_KEY") {
			t.Error("env must never enable a paid model fallback")
		}
	})
	t.Run("legacy JWT secret is still accepted", func(t *testing.T) {
		if out, _, err := runFetchEnv(t, with(map[string]string{"SUPABASE_JWT_SECRET": "legacy-secret-0123456789-0123456789-abcdef"}, "SUPABASE_JWKS_URL")); err != nil {
			t.Fatalf("legacy secret rejected: %v\n%s", err, out)
		}
	})
	t.Run("neither JWT secret nor JWKS URL fails closed", func(t *testing.T) {
		out, env, err := runFetchEnv(t, with(nil, "SUPABASE_JWKS_URL"))
		if err == nil || !strings.Contains(out, "SUPABASE_JWT_SECRET or SUPABASE_JWKS_URL") {
			t.Fatalf("expected fail-closed error, got err=%v out=%s", err, out)
		}
		if env != "" {
			t.Error("must not write worker.env when validation fails")
		}
	})
	for _, missing := range []string{"DATABASE_URL", "WORKER_API_KEY", "S3_BUCKET"} {
		t.Run("missing "+missing+" fails closed", func(t *testing.T) {
			out, env, err := runFetchEnv(t, with(nil, missing))
			if err == nil || !strings.Contains(out, missing) {
				t.Fatalf("expected failure naming %s, got err=%v out=%s", missing, err, out)
			}
			if env != "" {
				t.Error("must not write worker.env when validation fails")
			}
		})
	}
}
