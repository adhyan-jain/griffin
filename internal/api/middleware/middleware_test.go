package middleware_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
)

const hmacSecret = "unit-test-hmac-secret-0123456789-abcdefghij"

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func claims(over map[string]any) jwt.MapClaims {
	c := jwt.MapClaims{
		"sub":  uuid.NewString(),
		"role": "authenticated",
		"aud":  "authenticated",
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range over {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	return c
}

func hs(t *testing.T, c jwt.MapClaims, secret string) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(h http.Handler, token string) int {
	req := httptest.NewRequest("GET", "/x", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code
}

func TestAuthenticateUserHS256(t *testing.T) {
	h := middleware.AuthenticateUser(middleware.AuthOptions{HMACSecret: hmacSecret})(okHandler)

	if got := do(h, hs(t, claims(nil), hmacSecret)); got != 200 {
		t.Fatalf("valid token: want 200, got %d", got)
	}

	neg := map[string]string{
		"no token":         "",
		"wrong secret":     hs(t, claims(nil), "another-secret-another-secret-another"),
		"expired":          hs(t, claims(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}), hmacSecret),
		"no exp":           hs(t, claims(map[string]any{"exp": nil}), hmacSecret),
		"anon role":        hs(t, claims(map[string]any{"role": "anon"}), hmacSecret),
		"service_role":     hs(t, claims(map[string]any{"role": "service_role", "sub": nil}), hmacSecret),
		"wrong audience":   hs(t, claims(map[string]any{"aud": "somebody-else"}), hmacSecret),
		"missing audience": hs(t, claims(map[string]any{"aud": nil}), hmacSecret),
		"no subject":       hs(t, claims(map[string]any{"sub": nil}), hmacSecret),
		"non-uuid subject": hs(t, claims(map[string]any{"sub": "admin"}), hmacSecret),
		"garbage":          "not.a.jwt",
	}
	for name, tok := range neg {
		if got := do(h, tok); got != 401 {
			t.Errorf("%s: want 401, got %d", name, got)
		}
	}

	// alg=none must never be accepted
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims(nil)).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if got := do(h, none); got != 401 {
		t.Errorf("alg none: want 401, got %d", got)
	}
}

func TestAuthenticateUserIssuer(t *testing.T) {
	h := middleware.AuthenticateUser(middleware.AuthOptions{HMACSecret: hmacSecret, Issuer: "https://p.supabase.co/auth/v1"})(okHandler)
	if got := do(h, hs(t, claims(map[string]any{"iss": "https://p.supabase.co/auth/v1"}), hmacSecret)); got != 200 {
		t.Fatalf("matching issuer: got %d", got)
	}
	if got := do(h, hs(t, claims(map[string]any{"iss": "https://evil.example"}), hmacSecret)); got != 401 {
		t.Fatalf("wrong issuer: got %d", got)
	}
	if got := do(h, hs(t, claims(nil), hmacSecret)); got != 401 {
		t.Fatalf("missing issuer: got %d", got)
	}
}

func jwksServer(t *testing.T, key *ecdsa.PublicKey, kid string) *httptest.Server {
	t.Helper()
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	x, y := key.X.FillBytes(make([]byte, 32)), key.Y.FillBytes(make([]byte, 32))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{
			{"kty": "EC", "crv": "P-256", "kid": kid, "x": enc(x), "y": enc(y)},
		}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAuthenticateUserES256ViaJWKS(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := jwksServer(t, &priv.PublicKey, "kid-1")
	h := middleware.AuthenticateUser(middleware.AuthOptions{JWKS: middleware.NewJWKSCache(srv.URL, nil)})(okHandler)

	sign := func(kid string, key *ecdsa.PrivateKey) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims(nil))
		tok.Header["kid"] = kid
		s, err := tok.SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := do(h, sign("kid-1", priv)); got != 200 {
		t.Fatalf("valid ES256: got %d", got)
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if got := do(h, sign("kid-1", other)); got != 401 {
		t.Fatalf("ES256 signed by wrong key: got %d", got)
	}
	if got := do(h, sign("kid-unknown", priv)); got != 401 {
		t.Fatalf("unknown kid: got %d", got)
	}
	// HS256 must be rejected when only JWKS is configured (algorithm-confusion guard)
	if got := do(h, hs(t, claims(nil), hmacSecret)); got != 401 {
		t.Fatalf("HS256 with JWKS-only config: got %d", got)
	}
}

func TestAuthenticateWorker(t *testing.T) {
	key := "worker-key-worker-key-worker-key-1234"
	h := middleware.AuthenticateWorker(key)(okHandler)
	call := func(hdr, val string) int {
		req := httptest.NewRequest("POST", "/x", nil)
		if hdr != "" {
			req.Header.Set(hdr, val)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}
	if call("X-Worker-Key", key) != 200 {
		t.Error("valid key rejected")
	}
	if call("Authorization", "Bearer "+key) != 200 {
		t.Error("valid bearer key rejected")
	}
	for name, c := range map[string]int{
		"none":       call("", ""),
		"wrong":      call("X-Worker-Key", "nope"),
		"prefix":     call("X-Worker-Key", key[:len(key)-1]),
		"superstr":   call("X-Worker-Key", key+"x"),
		"empty bear": call("Authorization", "Bearer "),
	} {
		if c != 401 {
			t.Errorf("%s: want 401 got %d", name, c)
		}
	}

	// An empty configured key must reject everything, including an empty header.
	empty := middleware.AuthenticateWorker("")(okHandler)
	rr := httptest.NewRecorder()
	empty.ServeHTTP(rr, httptest.NewRequest("POST", "/x", nil))
	if rr.Code != 401 {
		t.Errorf("empty configured key accepted a request: %d", rr.Code)
	}
}

func corsDo(allowed []string, method, origin string) *httptest.ResponseRecorder {
	h := middleware.CORS(allowed)(okHandler)
	req := httptest.NewRequest(method, "/x", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestCORSAllowlist(t *testing.T) {
	allowed := []string{"https://app.example.com"}

	rr := corsDo(allowed, "OPTIONS", "https://app.example.com")
	if rr.Code != 204 || rr.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("allowed preflight: %d %v", rr.Code, rr.Header())
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials header must never be sent")
	}

	rr = corsDo(allowed, "GET", "https://app.example.com")
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatal("allowed origin not echoed on simple request")
	}

	for _, evil := range []string{"https://evil.example", "https://app.example.com.evil.io", "http://app.example.com", "null"} {
		rr = corsDo(allowed, "OPTIONS", evil)
		if rr.Code != 403 || rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("preflight from %q: %d %v", evil, rr.Code, rr.Header())
		}
		rr = corsDo(allowed, "GET", evil)
		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("GET from %q got ACAO", evil)
		}
	}

	rr = corsDo(allowed, "GET", "")
	if rr.Code != 200 || rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("no-origin request must pass without CORS headers, got %v", rr.Header())
	}
}

func TestCORSNeverWildcard(t *testing.T) {
	for _, allowed := range [][]string{nil, {}, {"*"}} {
		for _, origin := range []string{"https://anything.example", ""} {
			rr := corsDo(allowed, "GET", origin)
			if v := rr.Header().Get("Access-Control-Allow-Origin"); v != "" {
				t.Errorf("allowlist %v origin %q produced ACAO %q", allowed, origin, v)
			}
			if v := rr.Header().Get("Access-Control-Allow-Credentials"); v != "" {
				t.Errorf("credentials header present: %q", v)
			}
		}
	}
}

// With open Supabase sign-ups a stranger can obtain a perfectly valid token. The operator
// allow-list must still reject them (403), case-insensitively accept the operator, and an empty
// list must keep the previous behaviour (used by tests/dev only; production requires the list).
func TestAuthenticateUserOperatorAllowList(t *testing.T) {
	h := middleware.AuthenticateUser(middleware.AuthOptions{HMACSecret: hmacSecret, AllowedEmails: []string{"Operator@Example.com"}})(okHandler)
	if got := do(h, hs(t, claims(map[string]any{"email": "operator@example.com"}), hmacSecret)); got != 200 {
		t.Fatalf("operator (case-insensitive): got %d", got)
	}
	if got := do(h, hs(t, claims(map[string]any{"email": "stranger@evil.example"}), hmacSecret)); got != 403 {
		t.Fatalf("stranger with a valid token: got %d, want 403", got)
	}
	if got := do(h, hs(t, claims(map[string]any{"email": ""}), hmacSecret)); got != 403 {
		t.Fatalf("token without email: got %d, want 403", got)
	}
	if got := do(h, hs(t, claims(nil), "wrong-secret-0123456789-abcdefghijklmnop")); got != 401 {
		t.Fatalf("forged token must stay 401: got %d", got)
	}
	open := middleware.AuthenticateUser(middleware.AuthOptions{HMACSecret: hmacSecret})(okHandler)
	if got := do(open, hs(t, claims(map[string]any{"email": "anyone@example.com"}), hmacSecret)); got != 200 {
		t.Fatalf("no allow-list configured: got %d", got)
	}
}
