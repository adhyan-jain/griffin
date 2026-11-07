package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

const hostKey contextKey = "host"

// HostAuthenticator resolves a host token to its host.
type HostAuthenticator interface {
	Authenticate(ctx context.Context, token string) (*domain.Host, error)
}

// AuthenticateHost authenticates a request by host token (Authorization: Bearer ghk_...) and puts the
// host in the context. Hosts are a system path: the lookup of a token cannot be tenant-scoped because
// the caller has no user session, so this middleware grants the system scope itself, after which every
// host handler MUST filter by the host's own user id (the repository methods it calls do). A token that
// is unknown or revoked yields the same 401, with no detail.
func AuthenticateHost(a HostAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimPrefix(h, "Bearer ")
			}
			if token == "" {
				token = r.Header.Get("X-Host-Key")
			}
			if a == nil || token == "" {
				unauthorizedResponse(w, "Invalid or missing host token")
				return
			}
			ctx := database.WithSystem(r.Context())
			host, err := a.Authenticate(ctx, token)
			if err != nil || host == nil {
				unauthorizedResponse(w, "Invalid or missing host token")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, hostKey, host)))
		})
	}
}

// GetHost returns the authenticated host, or nil outside a host-authenticated request.
func GetHost(ctx context.Context) *domain.Host {
	h, _ := ctx.Value(hostKey).(*domain.Host)
	return h
}
