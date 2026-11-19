package handlers

import (
	"encoding/json"
	"net/http"
)

// CapabilitiesHandler reports which integrations are wired in this process. It exposes readiness
// flags only, never configuration values or secrets.
func CapabilitiesHandler(caps map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": caps})
	})
}
