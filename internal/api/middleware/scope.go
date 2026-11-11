package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/griffin/core/internal/database"
)

// maxBufferedResponse bounds the in-memory response held until the request transaction commits.
const maxBufferedResponse = 32 << 20

// SystemScope marks the request as an explicitly authorized system path. It must only wrap
// handlers that are already protected by AuthenticateWorker.
func SystemScope() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(database.WithSystem(r.Context())))
		})
	}
}

// RLSTransaction runs the rest of the request inside one database transaction on the
// RLS-enforced role, with auth.uid() set to the authenticated user transaction-locally.
// Must run after AuthenticateUser. The response is held until COMMIT so a failed commit can
// never be reported to the client as success; the transaction is rolled back for any status
// >= 400 or panic. mgr == nil means user scope is not configured: requests are refused.
func RLSTransaction(mgr *database.UserTxManager, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if mgr == nil || mgr.Pool == nil {
				writeScopeError(w, http.StatusServiceUnavailable, "SCOPE_UNAVAILABLE", "user database scope is not configured")
				return
			}
			userID, err := GetUserID(r.Context())
			if err != nil {
				unauthorizedResponse(w, "Authentication required")
				return
			}
			tx, err := mgr.Begin(r.Context(), userID)
			if err != nil {
				logger.Error("failed to open user transaction", "error", err)
				writeScopeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "database unavailable")
				return
			}
			// Ensure user profile exists in profiles table so foreign keys referencing profiles(id) succeed.
			if email, _ := r.Context().Value(UserEmailKey).(string); email != "" {
				_, _ = tx.Exec(r.Context(),
					`INSERT INTO profiles (id, email, full_name, role) VALUES ($1, $2, 'Griffin Operator', 'OWNER') ON CONFLICT (id) DO NOTHING`,
					userID, email)
			}
			finished := false
			defer func() {
				if !finished { // panic or early return: never leave the tx (and its claims) open
					_ = tx.Rollback(r.Context())
				}
			}()

			buf := &bufferedWriter{header: http.Header{}, status: http.StatusOK}
			next.ServeHTTP(buf, r.WithContext(database.WithUserTx(r.Context(), tx)))

			if buf.overflow {
				finished = true
				_ = tx.Rollback(r.Context())
				writeScopeError(w, http.StatusInternalServerError, "RESPONSE_TOO_LARGE", "response too large")
				return
			}
			if buf.status >= http.StatusBadRequest {
				finished = true
				_ = tx.Rollback(r.Context())
			} else {
				finished = true
				if err := tx.Commit(r.Context()); err != nil {
					logger.Error("user transaction commit failed", "error", err)
					writeScopeError(w, http.StatusInternalServerError, "COMMIT_FAILED", "request could not be completed")
					return
				}
			}
			for k, v := range buf.header {
				w.Header()[k] = v
			}
			w.WriteHeader(buf.status)
			_, _ = w.Write(buf.body.Bytes())
		})
	}
}

type bufferedWriter struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	wrote    bool
	overflow bool
}

func (b *bufferedWriter) Header() http.Header { return b.header }
func (b *bufferedWriter) WriteHeader(code int) {
	if !b.wrote {
		b.status, b.wrote = code, true
	}
}
func (b *bufferedWriter) Write(p []byte) (int, error) {
	b.wrote = true
	if b.body.Len()+len(p) > maxBufferedResponse {
		b.overflow = true
		return 0, http.ErrBodyNotAllowed
	}
	return b.body.Write(p)
}

func writeScopeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]string{"code": code, "message": msg},
	})
}
