package watch

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/storage"
)

func TestLocalTargetsSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "targets.json")

	content := []byte(`[{"repo": "kubernetes/kubernetes", "language": "Go"}]`)
	err := SaveTargetsToFile(path, content)
	require.NoError(t, err)

	loaded, err := LoadTargetsFromFile(path)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "kubernetes/kubernetes", loaded[0].Repo)
	assert.Equal(t, "Go", loaded[0].Language)
}

func TestResolveTargetsFallback(t *testing.T) {
	ctx := context.Background()
	// When file doesn't exist and S3 not provided, returns curated defaults
	targets, src, err := ResolveTargets(ctx, "", "", "/nonexistent/path/targets.json", storage.S3ClientOptions{})
	require.NoError(t, err)
	assert.Contains(t, src, "defaults")
	assert.NotEmpty(t, targets)
}
