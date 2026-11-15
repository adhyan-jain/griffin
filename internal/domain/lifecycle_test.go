package domain

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/griffin/core/migrations"
)

func TestOrchestrationMoves(t *testing.T) {
	if !CanMoveOrchestration(OrchestrationStatusRunning, OrchestrationStatusCompleted) || CanMoveOrchestration(OrchestrationStatusCompleted, OrchestrationStatusRunning) {
		t.Fatal("CanMoveOrchestration wrong")
	}
	for _, s := range AllOrchestrationStatuses {
		for _, to := range orchMoves[s] {
			if to == s {
				t.Errorf("%s lists itself", s)
			}
		}
	}
}

// The database must accept every status the code can write. This is the check that was missing when
// BLOCKED_AWAITING_PROVIDER existed in Go but not in the task_status enum.
func TestDatabaseEnumCoversEveryTaskStatus(t *testing.T) {
	files, _ := fs.Glob(migrations.FS, "*.up.sql")
	var all strings.Builder
	for _, f := range files {
		b, _ := fs.ReadFile(migrations.FS, f)
		all.Write(b)
	}
	sql := all.String()
	for _, s := range AllTaskStatuses() {
		if !strings.Contains(sql, "'"+string(s)+"'") {
			t.Errorf("task status %s is not in any migration", s)
		}
	}
}
