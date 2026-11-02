package antigravity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// WorkspaceManager provisions and cleans up isolated scratch directories for task runs.
type WorkspaceManager struct {
	rootDir         string
	retainOnFailure bool
}

// NewWorkspaceManager creates a WorkspaceManager rooted at rootDir.
func NewWorkspaceManager(rootDir string, retainOnFailure bool) (*WorkspaceManager, error) {
	if rootDir == "" {
		rootDir = filepath.Join(os.TempDir(), "griffin-workspaces")
	}

	cleanRoot := filepath.Clean(rootDir)
	if err := os.MkdirAll(cleanRoot, 0700); err != nil {
		return nil, fmt.Errorf("failed to initialize workspace root %s: %w", cleanRoot, err)
	}

	return &WorkspaceManager{
		rootDir:         cleanRoot,
		retainOnFailure: retainOnFailure,
	}, nil
}

// RootDir returns the root directory path.
func (m *WorkspaceManager) RootDir() string {
	return m.rootDir
}

// CreateWorkspace provisions an isolated scratch directory for a single task execution attempt.
func (m *WorkspaceManager) CreateWorkspace(task *domain.Task, execID uuid.UUID) (string, error) {
	subDirName := fmt.Sprintf("task-%s-%s", task.ID.String(), execID.String()[:8])
	targetPath := filepath.Join(m.rootDir, subDirName)

	// Validate against path traversal
	cleanTarget := filepath.Clean(targetPath)
	expectedPrefix := m.rootDir + string(filepath.Separator)
	if !strings.HasPrefix(cleanTarget, expectedPrefix) {
		return "", fmt.Errorf("%w: path %s escapes root %s", ErrWorkspacePathTraversal, cleanTarget, m.rootDir)
	}

	// Create directory with restricted 0700 permissions
	if err := os.MkdirAll(cleanTarget, 0700); err != nil {
		return "", fmt.Errorf("create workspace dir %s: %w", cleanTarget, err)
	}

	// Write task input payload if provided
	if len(task.InputPayload) > 0 {
		inputPath := filepath.Join(cleanTarget, "task_input.json")
		var pretty bytesOrRaw
		if err := json.Unmarshal(task.InputPayload, &pretty); err == nil {
			formatted, _ := json.MarshalIndent(pretty, "", "  ")
			_ = os.WriteFile(inputPath, formatted, 0600)
		} else {
			_ = os.WriteFile(inputPath, task.InputPayload, 0600)
		}
	}

	return cleanTarget, nil
}

// CleanupWorkspace removes the scratch directory unless retainOnFailure is enabled and failed is true.
func (m *WorkspaceManager) CleanupWorkspace(wsDir string, failed bool) error {
	if m.retainOnFailure && failed {
		return nil // Retain for debugging
	}

	// Safety check: ensure wsDir is within rootDir before removing
	cleanDir := filepath.Clean(wsDir)
	expectedPrefix := m.rootDir + string(filepath.Separator)
	if !strings.HasPrefix(cleanDir, expectedPrefix) {
		return fmt.Errorf("%w: refusing to delete directory %s outside root %s", ErrWorkspacePathTraversal, cleanDir, m.rootDir)
	}

	return os.RemoveAll(cleanDir)
}

type bytesOrRaw any
