package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	UserIDKey    contextKey = "user_id"
	UserEmailKey contextKey = "user_email"
	WorkerIDKey  contextKey = "worker_id"
)

// SupabaseClaims defines the standard JWT claims emitted by Supabase Auth.
type SupabaseClaims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// AuthOptions configures Supabase JWT verification.
type AuthOptions struct {
	HMACSecret string     // legacy HS256 JWT secret (optional if JWKS set)
	JWKS       *JWKSCache // asymmetric signing keys (optional if HMACSecret set)
	Issuer     string     // expected "iss" (optional)
}

// AuthenticateUser verifies Supabase JWTs and populates the authenticated user ID in context.
// Only HS256 (with HMACSecret) and ES256/RS256 (with JWKS) are accepted; "none" and any other
// algorithm are rejected. Tokens must carry role=authenticated and aud=authenticated, which
// excludes Supabase anon and service_role keys.
func AuthenticateUser(opts AuthOptions) func(http.Handler) http.Handler {
	secretBytes := []byte(opts.HMACSecret)

	keyFunc := func(t *jwt.Token) (any, error) {
		switch t.Method.Alg() {
		case "HS256":
			if len(secretBytes) == 0 {
				return nil, fmt.Errorf("HS256 not enabled")
			}
			return secretBytes, nil
		case "ES256", "RS256":
			if opts.JWKS == nil {
				return nil, fmt.Errorf("asymmetric tokens not enabled")
			}
			kid, _ := t.Header["kid"].(string)
			return opts.JWKS.Key(kid)
		}
		return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
	}
	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256", "ES256", "RS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithAudience("authenticated"),
	}
	if opts.Issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(opts.Issuer))
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				unauthorizedResponse(w, "Missing Authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				unauthorizedResponse(w, "Invalid Authorization header format. Expected 'Bearer <token>'")
				return
			}

			claims := &SupabaseClaims{}
			token, err := jwt.ParseWithClaims(parts[1], claims, keyFunc, parserOpts...)
			if err != nil || !token.Valid {
				unauthorizedResponse(w, "Invalid or expired authentication token")
				return
			}
			if claims.Role != "authenticated" {
				unauthorizedResponse(w, "Token role is not authenticated")
				return
			}

			sub := claims.Subject
			if sub == "" {
				unauthorizedResponse(w, "Token subject (user ID) is missing")
				return
			}

			userID, err := uuid.Parse(sub)
			if err != nil {
				unauthorizedResponse(w, "Invalid user ID format in token")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, UserEmailKey, claims.Email)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthenticateWorker protects background worker endpoints with a shared secret API key.
// The key is accepted via X-Worker-Key / X-Worker-API-Key / Bearer and compared in constant time.
func AuthenticateWorker(workerAPIKey string) func(http.Handler) http.Handler {
	want := []byte(workerAPIKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-Worker-Key")
			if apiKey == "" {
				apiKey = r.Header.Get("X-Worker-API-Key")
			}
			if apiKey == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					apiKey = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			if len(want) == 0 || apiKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), want) != 1 {
				unauthorizedResponse(w, "Invalid or missing worker API key")
				return
			}

			workerID := r.Header.Get("X-Worker-ID")
			if workerID == "" {
				workerID = "worker-remote"
			}

			ctx := context.WithValue(r.Context(), WorkerIDKey, workerID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID retrieves the authenticated user's UUID from context.
func GetUserID(ctx context.Context) (uuid.UUID, error) {
	if val, ok := ctx.Value(UserIDKey).(uuid.UUID); ok && val != uuid.Nil {
		return val, nil
	}
	return uuid.Nil, errors.New("no authenticated user in context")
}

// GetUserEmail retrieves the authenticated user's email from context.
func GetUserEmail(ctx context.Context) string {
	if val, ok := ctx.Value(UserEmailKey).(string); ok {
		return val
	}
	return ""
}

// GetWorkerID retrieves the worker ID from context.
func GetWorkerID(ctx context.Context) string {
	if val, ok := ctx.Value(WorkerIDKey).(string); ok {
		return val
	}
	return "worker-default"
}

func unauthorizedResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error": map[string]string{
			"code":    "UNAUTHORIZED",
			"message": message,
		},
	})
}
