package antigravity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func TestWorkspaceManager_CreateAndCleanup(t *testing.T) {
	tempRoot := t.TempDir()

	mgr, err := NewWorkspaceManager(tempRoot, false)
	require.NoError(t, err)
	assert.Equal(t, tempRoot, mgr.RootDir())

	taskID := uuid.New()
	execID := uuid.New()
	task := &domain.Task{
		ID:           taskID,
		InputPayload: json.RawMessage(`{"prompt":"test input payload"}`),
	}

	wsDir, err := mgr.CreateWorkspace(task, execID)
	require.NoError(t, err)
	assert.DirExists(t, wsDir)

	// Verify permissions (0700)
	info, err := os.Stat(wsDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())

	// Verify task_input.json was created and readable
	inputPath := filepath.Join(wsDir, "task_input.json")
	assert.FileExists(t, inputPath)
	content, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "test input payload")

	// Cleanup on success
	err = mgr.CleanupWorkspace(wsDir, false)
	require.NoError(t, err)
	assert.NoDirExists(t, wsDir)
}

func TestWorkspaceManager_RetainOnFailure(t *testing.T) {
	tempRoot := t.TempDir()

	mgr, err := NewWorkspaceManager(tempRoot, true)
	require.NoError(t, err)

	task := &domain.Task{
		ID: uuid.New(),
	}
	execID := uuid.New()

	wsDir, err := mgr.CreateWorkspace(task, execID)
	require.NoError(t, err)

	// Cleanup with failed=true -> should NOT delete
	err = mgr.CleanupWorkspace(wsDir, true)
	require.NoError(t, err)
	assert.DirExists(t, wsDir, "Workspace should be retained when retainOnFailure=true and failed=true")

	// Cleanup with failed=false -> should delete
	err = mgr.CleanupWorkspace(wsDir, false)
	require.NoError(t, err)
	assert.NoDirExists(t, wsDir, "Workspace should be deleted on success even if retainOnFailure=true")
}

func TestWorkspaceManager_PathTraversalDefense(t *testing.T) {
	tempRoot := t.TempDir()

	mgr, err := NewWorkspaceManager(tempRoot, false)
	require.NoError(t, err)

	// Attempt to clean up an outside directory
	outsideDir := filepath.Dir(tempRoot)
	err = mgr.CleanupWorkspace(outsideDir, false)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrWorkspacePathTraversal)

	// Attempt cleanup with malicious relative path
	traversalPath := filepath.Join(tempRoot, "..", "some_other_dir")
	err = mgr.CleanupWorkspace(traversalPath, false)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrWorkspacePathTraversal)
}
