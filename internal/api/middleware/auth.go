package middleware

import (
	"context"
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

// AuthenticateUser verifies Supabase JWTs and populates the authenticated user ID in context.
func AuthenticateUser(jwtSecret string) func(http.Handler) http.Handler {
	secretBytes := []byte(jwtSecret)

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

			tokenString := parts[1]

			// Parse and validate token
			claims := &SupabaseClaims{}
			token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return secretBytes, nil
			})

			if err != nil || !token.Valid {
				unauthorizedResponse(w, "Invalid or expired authentication token")
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
func AuthenticateWorker(workerAPIKey string) func(http.Handler) http.Handler {
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

			if apiKey == "" || apiKey != workerAPIKey {
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
