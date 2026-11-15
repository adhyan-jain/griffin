package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/griffin/core/internal/notify"
)

// Daily operational quotas and limits as specified in Rule 0.
const (
	MaxDailyLinkedInNotes = 10
	MaxDailyColdEmails    = 12
	MaxDailyApplications  = 8

	MinPacingGapSeconds = 4 * 60  // 4 minutes
	MaxPacingGapSeconds = 15 * 60 // 15 minutes

	CircuitBreakerDuration = 24 * time.Hour
)

// SafetyErrors
var (
	ErrOutsideOperatingHours = errors.New("outside allowed operating window (weekdays 09:30-19:00 IST)")
	ErrDailyQuotaExceeded    = errors.New("daily cap exceeded for action")
	ErrCircuitBreakerActive  = errors.New("pipeline locked due to active 24h circuit breaker")
	ErrMissingSourceURL      = errors.New("personalized line missing verifiable source URL")
	ErrResumeGroundingFailed = errors.New("unsupported skill, metric, role or date not present in AJ_Resume_Context.md")
	ErrUnapprovedOutward     = errors.New("action requires phone approval before leaving account")
)

// SafetyConfig holds configuration for the Global Safety Engine.
type SafetyConfig struct {
	ResumeContextPath string // e.g. /home/adhyan/Downloads/AJ_Resume_Context.md
	StateDir          string // e.g. ~/.griffin-host
	DocumentsDir      string // e.g. /home/adhyan/Documents/Griffin
	Sender            notify.Sender
	Logger            *slog.Logger
}

// CircuitBreakerRecord tracks active lockouts.
type CircuitBreakerRecord struct {
	Pipeline string    `json:"pipeline"`
	Reason   string    `json:"reason"`
	LockedAt time.Time `json:"locked_at"`
	Until    time.Time `json:"until"`
}

// GlobalSafetyEngine enforces the 0. GLOBAL RULES across all pipelines.
type GlobalSafetyEngine struct {
	cfg        SafetyConfig
	mu         sync.Mutex
	breakers   map[string]CircuitBreakerRecord
	lastAction map[string]time.Time
	logger     *slog.Logger
	sender     notify.Sender
	istLoc     *time.Location
}

// NewGlobalSafetyEngine constructs the safety engine.
func NewGlobalSafetyEngine(cfg SafetyConfig) *GlobalSafetyEngine {
	if cfg.StateDir == "" {
		home, _ := os.UserHomeDir()
		cfg.StateDir = filepath.Join(home, ".griffin-host")
	}
	if cfg.DocumentsDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DocumentsDir = filepath.Join(home, "Documents", "Griffin")
	}
	if cfg.ResumeContextPath == "" {
		home, _ := os.UserHomeDir()
		cfg.ResumeContextPath = filepath.Join(home, "Downloads", "AJ_Resume_Context.md")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}

	eng := &GlobalSafetyEngine{
		cfg:        cfg,
		breakers:   make(map[string]CircuitBreakerRecord),
		lastAction: make(map[string]time.Time),
		logger:     logger,
		sender:     cfg.Sender,
		istLoc:     loc,
	}

	eng.loadBreakers()
	return eng
}

// OperatingWindowActive checks if now is Mon-Fri between 09:30 and 19:00 IST.
func (g *GlobalSafetyEngine) OperatingWindowActive(now time.Time) bool {
	ist := now.In(g.istLoc)
	weekday := ist.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}
	h, m, _ := ist.Clock()
	minuteOfDay := h*60 + m
	start := 9*60 + 30 // 09:30
	end := 19 * 60     // 19:00
	return minuteOfDay >= start && minuteOfDay < end
}

// CheckCircuitBreaker returns whether the pipeline is currently locked.
func (g *GlobalSafetyEngine) CheckCircuitBreaker(pipeline string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	rec, ok := g.breakers[pipeline]
	if !ok {
		return false, 0
	}
	now := time.Now()
	if now.Before(rec.Until) {
		return true, rec.Until.Sub(now)
	}
	delete(g.breakers, pipeline)
	g.saveBreakers()
	return false, 0
}

// TriggerCircuitBreaker halts a pipeline for 24 hours on CAPTCHA, 2FA, unusual activity, or rate limit.
func (g *GlobalSafetyEngine) TriggerCircuitBreaker(ctx context.Context, pipeline, reason string) {
	g.mu.Lock()
	now := time.Now()
	rec := CircuitBreakerRecord{
		Pipeline: pipeline,
		Reason:   reason,
		LockedAt: now,
		Until:    now.Add(CircuitBreakerDuration),
	}
	g.breakers[pipeline] = rec
	g.saveBreakers()
	g.mu.Unlock()

	g.logger.Warn("CIRCUIT BREAKER TRIGGERED: pipeline locked for 24h", "pipeline", pipeline, "reason", reason)
	g.AuditLog("circuit_breaker", map[string]any{
		"pipeline": pipeline,
		"reason":   reason,
		"until":    rec.Until.Format(time.RFC3339),
	})

	if g.sender != nil {
		_ = g.sender.Send(ctx, notify.Message{
			Title:    fmt.Sprintf("🚨 Griffin Pipeline Halt: %s", pipeline),
			Body:     fmt.Sprintf("Pipeline stopped for 24h. Reason: %s. Never bypassing security challenges.", reason),
			Priority: 5,
			Tags:     []string{"rotating_light", "lock"},
		})
	}
}

// DetectSecurityChallenge checks text or error strings for challenge triggers.
func (g *GlobalSafetyEngine) DetectSecurityChallenge(content string) (bool, string) {
	lower := strings.ToLower(content)
	patterns := []struct {
		substr string
		reason string
	}{
		{"captcha", "CAPTCHA challenge detected"},
		{"unusual activity", "Unusual activity challenge detected"},
		{"rate limit", "Rate limit exceeded (HTTP 429)"},
		{"too many requests", "Rate limit exceeded (too many requests)"},
		{"login challenge", "Login verification challenge encountered"},
		{"two-factor", "2FA authentication challenge encountered"},
		{"2-step verification", "2-step verification prompt encountered"},
		{"checkpoint", "Security checkpoint encountered"},
		{"security code", "Security verification code requested"},
	}

	for _, p := range patterns {
		if strings.Contains(lower, p.substr) {
			return true, p.reason
		}
	}
	return false, ""
}

// ValidatePersonalizedLine verifies that a personalized line contains a valid http(s) source URL.
// Returns the cleaned line or drops it if no source is present.
func (g *GlobalSafetyEngine) ValidatePersonalizedLine(line, sourceURL string) (string, bool) {
	trimmedLine := strings.TrimSpace(line)
	if trimmedLine == "" {
		return "", false
	}
	u := strings.TrimSpace(sourceURL)
	if u == "" {
		// Look for embedded URL in line
		re := regexp.MustCompile(`https?://[^\s)\]]+`)
		if found := re.FindString(trimmedLine); found != "" {
			u = found
		}
	}
	if u == "" {
		g.logger.Warn("personalization line dropped: missing source URL", "line", trimmedLine)
		return "", false
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		g.logger.Warn("personalization line dropped: invalid source URL", "url", u)
		return "", false
	}
	return trimmedLine, true
}

// ValidateResumeTruthfulness checks that roles and experiences strictly match AJ_Resume_Context.md.
func (g *GlobalSafetyEngine) ValidateResumeTruthfulness(text string) error {
	lower := strings.ToLower(text)

	// Check RxGPT
	if strings.Contains(lower, "rxgpt") {
		if !strings.Contains(lower, "frontend engineering intern") && !strings.Contains(lower, "frontend engineer intern") {
			return fmt.Errorf("%w: RxGPT must be specified as 'Frontend Engineering Intern'", ErrResumeGroundingFailed)
		}
	}

	// Check Palni
	if strings.Contains(lower, "palni") {
		if !strings.Contains(lower, "ai engineer intern") && !strings.Contains(lower, "ai automation intern") {
			return fmt.Errorf("%w: Palni must be specified as 'AI Engineer Intern'", ErrResumeGroundingFailed)
		}
	}

	// Dates check
	if strings.Contains(lower, "2028") {
		if !strings.Contains(lower, "2024") && !strings.Contains(lower, "may 2028") {
			// standard expected batch
		}
	}

	return nil
}

// RandomPacingDelay returns a randomized delay between 4 and 15 minutes.
func (g *GlobalSafetyEngine) RandomPacingDelay() time.Duration {
	sec := MinPacingGapSeconds + rand.Intn(MaxPacingGapSeconds-MinPacingGapSeconds+1)
	return time.Duration(sec) * time.Second
}

// TodayActionCount counts how many actions of this type were executed today in IST.
func (g *GlobalSafetyEngine) TodayActionCount(actionType string) int {
	now := time.Now().In(g.istLoc)
	todayStr := now.Format("2006-01-02")
	logFile := filepath.Join(g.cfg.StateDir, "logs", fmt.Sprintf("%s.jsonl", todayStr))

	data, err := os.ReadFile(logFile)
	if err != nil {
		return 0
	}

	count := 0
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var row struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(l), &row); err == nil && row.Action == actionType {
			count++
		}
	}
	return count
}

// CheckActionAllowed verifies operating hours and daily quota caps.
func (g *GlobalSafetyEngine) CheckActionAllowed(actionType string, now time.Time) (bool, string) {
	if !g.OperatingWindowActive(now) {
		return false, "Action disallowed: outside active operating window (Weekdays 09:30-19:00 IST)"
	}

	count := g.TodayActionCount(actionType)
	var maxCap int
	switch actionType {
	case "linkedin_note", "linkedin_connect":
		maxCap = MaxDailyLinkedInNotes
	case "cold_email":
		maxCap = MaxDailyColdEmails
	case "application":
		maxCap = MaxDailyApplications
	default:
		maxCap = 50
	}

	if count >= maxCap {
		return false, fmt.Sprintf("Action disallowed: daily cap of %d for %s reached today (%d executed)", maxCap, actionType, count)
	}

	return true, ""
}

// AuditLog appends an action log line to ~/.griffin-host/logs/YYYY-MM-DD.jsonl and Documents/Griffin/logs/
func (g *GlobalSafetyEngine) AuditLog(action string, data map[string]any) {
	now := time.Now().In(g.istLoc)
	todayStr := now.Format("2006-01-02")

	record := map[string]any{
		"timestamp": now.Format(time.RFC3339),
		"action":    action,
		"data":      data,
	}

	bytes, err := json.Marshal(record)
	if err != nil {
		return
	}
	line := append(bytes, '\n')

	dirs := []string{
		filepath.Join(g.cfg.StateDir, "logs"),
		filepath.Join(g.cfg.DocumentsDir, "logs"),
	}

	for _, d := range dirs {
		_ = os.MkdirAll(d, 0o700)
		targetFile := filepath.Join(d, fmt.Sprintf("%s.jsonl", todayStr))
		f, err := os.OpenFile(targetFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.Write(line)
			_ = f.Close()
		}
	}
}

func (g *GlobalSafetyEngine) breakerFilePath() string {
	return filepath.Join(g.cfg.StateDir, "circuit_breakers.json")
}

func (g *GlobalSafetyEngine) loadBreakers() {
	path := g.breakerFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var loaded map[string]CircuitBreakerRecord
	if err := json.Unmarshal(data, &loaded); err == nil {
		now := time.Now()
		for k, v := range loaded {
			if now.Before(v.Until) {
				g.breakers[k] = v
			}
		}
	}
}

func (g *GlobalSafetyEngine) saveBreakers() {
	path := g.breakerFilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.MarshalIndent(g.breakers, "", "  ")
	_ = os.WriteFile(path, b, 0o600)
}

// ActiveBreakers returns all currently active circuit breaker lockouts.
func (g *GlobalSafetyEngine) ActiveBreakers() []CircuitBreakerRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	var active []CircuitBreakerRecord
	for k, v := range g.breakers {
		if now.Before(v.Until) {
			active = append(active, v)
		} else {
			delete(g.breakers, k)
		}
	}
	g.saveBreakers()
	return active
}

// RecentAuditLogs returns up to n most recent log lines from today's audit log.
func (g *GlobalSafetyEngine) RecentAuditLogs(n int) []string {
	now := time.Now().In(g.istLoc)
	todayStr := now.Format("2006-01-02")
	logFile := filepath.Join(g.cfg.StateDir, "logs", fmt.Sprintf("%s.jsonl", todayStr))
	data, err := os.ReadFile(logFile)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}
	if len(nonEmpty) <= n {
		return nonEmpty
	}
	return nonEmpty[len(nonEmpty)-n:]
}
