package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery handles panics cleanly by logging stack traces and returning a 500 error.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					reqID := GetRequestID(r.Context())
					stack := string(debug.Stack())

					logger.Error("Panic recovered in HTTP handler",
						"panic", fmt.Sprintf("%v", rec),
						"request_id", reqID,
						"path", r.URL.Path,
						"stack", stack,
					)

					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"success": false,
						"error": map[string]string{
							"code":    "INTERNAL_PANIC",
							"message": "A critical server error occurred",
						},
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
