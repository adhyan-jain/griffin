package middleware

import (
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes caps JSON request bodies (1 MiB).
const DefaultMaxBodyBytes int64 = 1 << 20

// BodyLimit wraps r.Body with http.MaxBytesReader so oversized JSON bodies fail
// decoding (mapped to HTTP 413 by httputil.ErrorJSON). Requests for which skip
// returns true (e.g. artifact uploads, which enforce their own limit) are left
// untouched. max <= 0 uses DefaultMaxBodyBytes.
func BodyLimit(max int64, skip func(*http.Request) bool) func(http.Handler) http.Handler {
	if max <= 0 {
		max = DefaultMaxBodyBytes
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Body != http.NoBody && (skip == nil || !skip(r)) {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IsArtifactUploadRoute reports whether r targets a route that carries large
// bodies and enforces MaxUploadBytes itself.
func IsArtifactUploadRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	p := strings.TrimRight(r.URL.Path, "/")
	return p == "/api/v1/artifacts/upload" || p == "/api/v1/artifacts"
}
