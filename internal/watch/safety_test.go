package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafetyEngineOperatingWindow(t *testing.T) {
	eng := NewGlobalSafetyEngine(SafetyConfig{})

	loc, _ := time.LoadLocation("Asia/Kolkata")
	// Wednesday 10:00 IST -> inside
	inside := time.Date(2026, 11, 18, 10, 0, 0, 0, loc)
	assert.True(t, eng.OperatingWindowActive(inside))

	// Wednesday 08:00 IST -> before 09:30
	before := time.Date(2026, 11, 18, 8, 0, 0, 0, loc)
	assert.False(t, eng.OperatingWindowActive(before))

	// Wednesday 20:00 IST -> after 19:00
	after := time.Date(2026, 11, 18, 20, 0, 0, 0, loc)
	assert.False(t, eng.OperatingWindowActive(after))

	// Saturday 12:00 IST -> weekend
	weekend := time.Date(2026, 11, 21, 12, 0, 0, 0, loc)
	assert.False(t, eng.OperatingWindowActive(weekend))
}

func TestSafetyEngineCircuitBreaker(t *testing.T) {
	tmpDir := t.TempDir()
	notifier := &testNotifier{}
	eng := NewGlobalSafetyEngine(SafetyConfig{
		StateDir: tmpDir,
		Sender:   notifier,
	})

	ctx := context.Background()
	// Initially no breaker active
	locked, _ := eng.CheckCircuitBreaker("internship_tracker")
	assert.False(t, locked)

	// Detect challenge
	isChal, reason := eng.DetectSecurityChallenge("CAPTCHA required to proceed")
	assert.True(t, isChal)
	assert.Contains(t, reason, "CAPTCHA")

	// Trigger breaker
	eng.TriggerCircuitBreaker(ctx, "internship_tracker", reason)

	// Now locked
	locked, dur := eng.CheckCircuitBreaker("internship_tracker")
	assert.True(t, locked)
	assert.True(t, dur > 23*time.Hour)

	// Check active breakers list
	breakers := eng.ActiveBreakers()
	require.Len(t, breakers, 1)
	assert.Equal(t, "internship_tracker", breakers[0].Pipeline)
	assert.Equal(t, reason, breakers[0].Reason)

	// Notifier received alert
	require.Len(t, notifier.messages, 1)
	assert.Contains(t, notifier.messages[0].Title, "Pipeline Halt")
}

func TestSafetyEnginePersonalizedLineGrounding(t *testing.T) {
	eng := NewGlobalSafetyEngine(SafetyConfig{})

	// Line with explicit source URL
	line, ok := eng.ValidatePersonalizedLine("Loved your recent article on vector databases", "https://blog.example.com/vector-db")
	assert.True(t, ok)
	assert.Equal(t, "Loved your recent article on vector databases", line)

	// Line with embedded URL
	line, ok = eng.ValidatePersonalizedLine("Saw your post at https://linkedin.com/posts/123", "")
	assert.True(t, ok)
	assert.Equal(t, "Saw your post at https://linkedin.com/posts/123", line)

	// Line with NO source URL -> dropped!
	line, ok = eng.ValidatePersonalizedLine("I really like your company's mission and team culture.", "")
	assert.False(t, ok)
	assert.Empty(t, line)
}

func TestSafetyEngineResumeTruthfulness(t *testing.T) {
	eng := NewGlobalSafetyEngine(SafetyConfig{})

	// Valid roles
	err := eng.ValidateResumeTruthfulness("At RxGPT I worked as a Frontend Engineering Intern building UIs.")
	assert.NoError(t, err)

	err = eng.ValidateResumeTruthfulness("At Palni I served as an AI Engineer Intern fine-tuning LLMs.")
	assert.NoError(t, err)

	// False role: RxGPT as Full Stack Engineer
	err = eng.ValidateResumeTruthfulness("At RxGPT I was a Full Stack Engineer leading backend APIs.")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RxGPT must be specified as 'Frontend Engineering Intern'")

	// False role: Palni as Senior ML Scientist
	err = eng.ValidateResumeTruthfulness("At Palni I was a Senior ML Scientist designing architectures.")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Palni must be specified as 'AI Engineer Intern'")
}

func TestSafetyEngineDailyQuotasAndAuditLog(t *testing.T) {
	tmpState := t.TempDir()
	tmpDocs := t.TempDir()

	eng := NewGlobalSafetyEngine(SafetyConfig{
		StateDir:     tmpState,
		DocumentsDir: tmpDocs,
	})

	// Log 3 linkedin notes
	eng.AuditLog("linkedin_note", map[string]any{"to": "alumni1"})
	eng.AuditLog("linkedin_note", map[string]any{"to": "alumni2"})
	eng.AuditLog("linkedin_note", map[string]any{"to": "alumni3"})

	count := eng.TodayActionCount("linkedin_note")
	assert.Equal(t, 3, count)

	recent := eng.RecentAuditLogs(5)
	assert.Len(t, recent, 3)

	// Verify dual log files exist
	now := time.Now()
	todayStr := now.Format("2006-01-02")
	f1 := filepath.Join(tmpState, "logs", todayStr+".jsonl")
	f2 := filepath.Join(tmpDocs, "logs", todayStr+".jsonl")

	_, err1 := os.Stat(f1)
	_, err2 := os.Stat(f2)
	assert.NoError(t, err1)
	assert.NoError(t, err2)
}
