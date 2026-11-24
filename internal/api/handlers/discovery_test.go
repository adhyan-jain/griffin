package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func TestDefaultDiscoveryTargetsAreTheVerifiedSeeds(t *testing.T) {
	got := defaultDiscoveryTargets()
	require.Greater(t, len(got), 50)
	seen := map[string]bool{}
	for _, tg := range got {
		require.Contains(t, []domain.DiscoverySource{domain.SourceGreenhouse, domain.SourceLever, domain.SourceAshby}, tg.Source)
		seen[string(tg.Source)+"|"+tg.Company] = true
	}
	require.False(t, seen["greenhouse|modal"] && seen["lever|postman"] && seen["ashby|inkeep"] && len(got) == 3, "not the old hardcoded list")
}
