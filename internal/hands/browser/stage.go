package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// Stage keeps a prepared script on this host between asking the owner and replaying it. The approval carries
// only the script's digest; the steps stay here, and are re-hashed on load, so a file that was changed after the
// owner approved fails to load instead of running.
type Stage struct{ Dir string }

type stagedFile struct {
	Goal   string               `json:"goal"`
	Digest string               `json:"digest"`
	Steps  []domain.BrowserStep `json:"steps"`
}

func (s Stage) path(taskID uuid.UUID) string { return filepath.Join(s.Dir, taskID.String()+".json") }

// Save records the script waiting for approval for a task, replacing any earlier one.
func (s Stage) Save(taskID uuid.UUID, goal string, steps []domain.BrowserStep) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	f := stagedFile{Goal: goal, Digest: domain.ScriptDigest(goal, steps), Steps: steps}
	b, _ := json.Marshal(f)
	if err := os.WriteFile(s.path(taskID), b, 0o600); err != nil {
		return "", err
	}
	return f.Digest, nil
}

// Load returns the task's staged script, verified against its digest. It returns (nil, "", nil) when there is none.
func (s Stage) Load(taskID uuid.UUID) (steps []domain.BrowserStep, digest string, err error) {
	b, err := os.ReadFile(s.path(taskID))
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var f stagedFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, "", fmt.Errorf("the staged script is unreadable: %w", err)
	}
	if domain.ScriptDigest(f.Goal, f.Steps) != f.Digest {
		return nil, "", fmt.Errorf("the staged script was changed after it was approved, so it will not run")
	}
	return f.Steps, f.Digest, nil
}

// Remove deletes the task's staged script.
func (s Stage) Remove(taskID uuid.UUID) { _ = os.Remove(s.path(taskID)) }
