package tests

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func policyOf(out map[string]any) (map[string]any, int) {
	d := unwrap(out)
	p, _ := d["policy"].(map[string]any)
	v, _ := d["version"].(float64)
	return p, int(v)
}

func putPolicy(t *testing.T, e *hostEnv, auth string, version int, p map[string]any) (int, map[string]any) {
	t.Helper()
	return e.do(t, "PUT", "/api/v1/permissions", auth, map[string]any{"policy": p, "version": version})
}

func TestPermissionsStartAtTheSafeDefault(t *testing.T) {
	e := setupHostEnv(t)
	code, out := e.do(t, "GET", "/api/v1/permissions", e.auth, nil)
	if code != http.StatusOK {
		t.Fatalf("get: %d %v", code, out)
	}
	p, v := policyOf(out)
	if v != 0 || p["default"] != "ask" {
		t.Fatalf("no saved policy => version 0 and default ask, got v=%d %v", v, p)
	}
	rules, _ := p["rules"].([]any)
	if len(rules) != 2 { // read and draft are auto; everything else asks
		t.Fatalf("the default policy auto-allows only read and draft, got %v", rules)
	}
}

func TestPolicyVersioningStopsTwoDevicesOverwritingEachOther(t *testing.T) {
	e := setupHostEnv(t)
	base := map[string]any{"default": "ask", "rules": []any{map[string]any{"class": "read", "mode": "auto"}}}

	// First save must name version 0.
	if code, out := putPolicy(t, e, e.auth, 0, base); code != http.StatusOK {
		t.Fatalf("first save: %d %v", code, out)
	}
	_, v := policyOf(func() map[string]any { _, o := e.do(t, "GET", "/api/v1/permissions", e.auth, nil); return o }())
	if v != 1 {
		t.Fatalf("version after first save must be 1, got %d", v)
	}
	// A second device that still thinks nothing is saved must be refused, not overwrite.
	if code, _ := putPolicy(t, e, e.auth, 0, base); code != http.StatusConflict {
		t.Fatalf("a stale write (version 0) must be 409, got %d", code)
	}
	// The up-to-date device succeeds and bumps the version.
	if code, _ := putPolicy(t, e, e.auth, 1, base); code != http.StatusOK {
		t.Fatalf("up-to-date write must succeed, got %d", code)
	}
	if code, _ := putPolicy(t, e, e.auth, 1, base); code != http.StatusConflict {
		t.Fatal("reusing the old version must conflict")
	}
}

func TestInvalidPoliciesAreRefusedAndStoreNothing(t *testing.T) {
	e := setupHostEnv(t)
	tooLong := time.Now().Add(9 * time.Hour).UTC().Format(time.RFC3339)
	bad := []map[string]any{
		{"default": "yolo", "rules": []any{}},
		{"rules": []any{map[string]any{"mode": "auto"}}},                               // narrows nothing
		{"rules": []any{map[string]any{"folder": "/home/me/../root", "mode": "auto"}}}, // traversal
		{"rules": []any{map[string]any{"folder": "/", "mode": "auto"}}},                // everything
		{"rules": []any{map[string]any{"class": "teleport", "mode": "auto"}}},          // unknown class
		{"rules": []any{}, "full_until": tooLong},                                      // Full beyond 8 hours
	}
	for i, p := range bad {
		if code, out := putPolicy(t, e, e.auth, 0, p); code != http.StatusBadRequest {
			t.Errorf("case %d must be 400, got %d %v", i, code, out)
		}
	}
	_, v := policyOf(func() map[string]any { _, o := e.do(t, "GET", "/api/v1/permissions", e.auth, nil); return o }())
	if v != 0 {
		t.Fatalf("a refused policy must store nothing, version is %d", v)
	}
}

func TestHostsReadThePolicyButCannotChangeIt(t *testing.T) {
	e := setupHostEnv(t)
	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")

	read := func() (map[string]any, int) {
		code, out := e.do(t, "GET", "/api/v1/host/policy", laptop, nil)
		if code != http.StatusOK {
			t.Fatalf("host policy: %d %v", code, out)
		}
		return policyOf(out)
	}
	if _, v := read(); v != 0 {
		t.Fatalf("version 0 before the owner saves, got %d", v)
	}
	body := map[string]any{"default": "off", "rules": []any{map[string]any{"class": "read", "mode": "auto"}}}
	if code, _ := putPolicy(t, e, e.auth, 0, body); code != http.StatusOK {
		t.Fatal("owner save failed")
	}
	p, v := read()
	if v != 1 || p["default"] != "off" {
		t.Fatalf("the host must see the owner's policy, got v=%d %v", v, p)
	}

	// The heartbeat tells the host the version, so it refetches only on change.
	code, out := e.do(t, "POST", "/api/v1/host/heartbeat", laptop, map[string]any{"capabilities": []string{"agent"}})
	if code != http.StatusOK || unwrap(out)["policy_version"] != float64(1) {
		t.Fatalf("heartbeat must carry policy_version=1: %d %v", code, out)
	}

	// A host cannot write: the route does not accept a host token.
	raw, _ := json.Marshal(map[string]any{"policy": map[string]any{"default": "auto"}, "version": 1})
	_ = raw
	if code, _ := e.do(t, "PUT", "/api/v1/permissions", laptop, map[string]any{"policy": map[string]any{"default": "auto"}, "version": 1}); code != http.StatusUnauthorized {
		t.Fatalf("a host token must not be able to write the policy, got %d", code)
	}
	// And the owner's JWT is not a host token.
	if code, _ := e.do(t, "GET", "/api/v1/host/policy", e.auth, nil); code != http.StatusUnauthorized {
		t.Fatalf("a user JWT must not work on the host policy route, got %d", code)
	}
}

func TestPolicyRoutesAreOffWithHostsOff(t *testing.T) {
	e := setupHostEnv(t)
	e.Config.HostsEnabled = false
	// Rebuild a server with hosts off: the routes must not exist (404), nothing half-enabled.
	ts := newServerWithHostsFlag(t, e, false)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/permissions", nil)
	req.Header.Set("Authorization", e.auth)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("with HOSTS_ENABLED=false the permissions routes must not exist, got %d", resp.StatusCode)
	}
}
