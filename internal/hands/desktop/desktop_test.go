package desktop

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopEnvironmentDetection(t *testing.T) {
	svc := NewService(slog.Default())
	require.NotNil(t, svc)
	assert.NotEmpty(t, svc.Env.OS)
	if svc.Env.OS == "linux" {
		assert.NotEmpty(t, svc.Env.Distro)
	}
}

func TestDesktopMediaExecutionOnLiveHost(t *testing.T) {
	svc := NewService(slog.Default())
	// Test querying status (safe read-only operation)
	res, err := svc.Media(context.Background(), MediaStatus, "spotify")
	if err == nil {
		assert.NotNil(t, res)
		assert.Equal(t, "status", res.Op)
		assert.NotEmpty(t, res.Message)
	}
}
