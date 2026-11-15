package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/storage"
)

// WatcherConfig holds settings for hackathon and OSS watchers.
type WatcherConfig struct {
	S3Bucket     string
	S3Key        string
	LocalTargets string
	AWSProfile   string
	AWSRegion    string
	GitHubToken  string
	NtfyTopic    string
	NtfyServer   string
	NtfyToken    string
	StateDir     string
	Logger       *slog.Logger
	DevpostBase  string // defaults to https://devpost.com
	DevfolioBase string // defaults to https://devfolio.co
	UnstopBase   string // defaults to https://unstop.com
	Sender       notify.Sender
}

func defaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "griffin-host", "watch")
	}
	return filepath.Join(home, ".griffin-host", "watch")
}

// CheckHackathons executes one check of Devpost, Devfolio, and Unstop, returning eligible AI/systems hackathons.
func CheckHackathons(ctx context.Context, hc *http.Client, now time.Time, devpostBase, devfolioBase, unstopBase string) ([]Hackathon, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	if devpostBase == "" {
		devpostBase = "https://devpost.com"
	}
	if devfolioBase == "" {
		devfolioBase = "https://devfolio.co"
	}
	if unstopBase == "" {
		unstopBase = "https://unstop.com"
	}

	var all []Hackathon

	dp, err := FetchDevpost(ctx, hc, devpostBase)
	if err == nil {
		all = append(all, dp...)
	}

	df, err := FetchDevfolio(ctx, hc, devfolioBase)
	if err == nil {
		all = append(all, df...)
	}

	un, err := FetchUnstop(ctx, hc, unstopBase)
	if err == nil {
		all = append(all, un...)
	}

	var eligible []Hackathon
	for _, h := range all {
		if h.Eligible(now) && h.WantedTheme() {
			eligible = append(eligible, h)
		}
	}
	return eligible, nil
}

// CheckOSSIssues fetches and ranks top Go & Python issues from target repositories.
func CheckOSSIssues(ctx context.Context, hc *http.Client, targets []TargetRepo, now time.Time, token string) ([]Issue, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	since := now.Add(-30 * 24 * time.Hour)

	var allIssues []Issue
	for _, target := range targets {
		issues, err := FetchRepoIssues(ctx, hc, "", target, since, token)
		if err == nil {
			allIssues = append(allIssues, issues...)
		}
	}

	return RankAndFilterIssues(allIssues, 5), nil
}

// WatchService manages the scheduled and on-demand execution of both watchers.
type WatchService struct {
	cfg      WatcherConfig
	sender   notify.Sender
	logger   *slog.Logger
	state    string
	Safety   *GlobalSafetyEngine
	DualSync *DualStorageSync
	Tailor   *ResumeTailorEngine
	Manager  *PipelineManager
}

// NewWatchService initializes the watch service.
func NewWatchService(cfg WatcherConfig) *WatchService {
	state := cfg.StateDir
	if state == "" {
		state = defaultStateDir()
	}
	_ = os.MkdirAll(state, 0o700)

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	sender := cfg.Sender
	if sender == nil && cfg.NtfyTopic != "" {
		server := cfg.NtfyServer
		if server == "" {
			server = "https://ntfy.sh"
		}
		sender = &notify.Ntfy{Server: server, Topic: cfg.NtfyTopic, Token: cfg.NtfyToken}
	}

	safety := NewGlobalSafetyEngine(SafetyConfig{
		StateDir: filepath.Dir(state),
		Sender:   sender,
		Logger:   logger,
	})

	dualSync := NewDualStorageSync(DualSyncConfig{
		S3Bucket:   cfg.S3Bucket,
		AWSProfile: cfg.AWSProfile,
		AWSRegion:  cfg.AWSRegion,
		Logger:     logger,
	})

	tailor := NewResumeTailorEngine(ResumeTailorConfig{
		Safety:   safety,
		DualSync: dualSync,
		Sender:   sender,
		Logger:   logger,
	})

	svc := &WatchService{
		cfg:      cfg,
		sender:   sender,
		logger:   logger,
		state:    state,
		Safety:   safety,
		DualSync: dualSync,
		Tailor:   tailor,
	}

	svc.Manager = NewPipelineManager(safety, dualSync, tailor, svc, sender, logger)
	return svc
}

// RunHackathonCheck runs the hackathon search, logs and notifies about new matches.
func (s *WatchService) RunHackathonCheck(ctx context.Context) ([]Hackathon, error) {
	now := time.Now()
	matches, err := CheckHackathons(ctx, nil, now, s.cfg.DevpostBase, s.cfg.DevfolioBase, s.cfg.UnstopBase)
	if err != nil {
		return nil, fmt.Errorf("hackathon check: %w", err)
	}

	s.logger.Info("hackathon check finished", "total_matches", len(matches))

	if len(matches) == 0 {
		return nil, nil
	}

	// Filter out previously notified keys
	seenFile := filepath.Join(s.state, "seen_hackathons.json")
	seen := loadSeen(seenFile)
	var newMatches []Hackathon
	for _, m := range matches {
		if !seen[m.Key()] {
			newMatches = append(newMatches, m)
			seen[m.Key()] = true
		}
	}

	if len(newMatches) == 0 {
		s.logger.Info("no new hackathons since last run")
		return matches, nil
	}

	saveSeen(seenFile, seen)

	// Format notification
	var sb strings.Builder
	for i, h := range newMatches {
		if i >= 5 {
			sb.WriteString(fmt.Sprintf("\n...and %d more", len(newMatches)-5))
			break
		}
		prize := h.Prize
		if prize == "" {
			prize = "Cash prize"
		}
		sb.WriteString(fmt.Sprintf("• %s (%s, closes %s)\n  %s\n", h.Name, prize, h.RegCloses.Format("Jan 02"), h.Link))
	}

	title := fmt.Sprintf("Hackathons: %d new AI/systems matches", len(newMatches))
	body := strings.TrimSpace(sb.String())

	if s.sender != nil {
		_ = s.sender.Send(ctx, notify.Message{
			Title:    title,
			Body:     body,
			Priority: 3,
			Tags:     []string{"trophy"},
		})
	}

	return newMatches, nil
}

// RunOSSCheck runs the OSS issues check and notifies with the top 5 Go/Python issues.
func (s *WatchService) RunOSSCheck(ctx context.Context) ([]Issue, error) {
	now := time.Now()
	s3Opts := storage.S3ClientOptions{
		Bucket:     s.cfg.S3Bucket,
		AWSProfile: s.cfg.AWSProfile,
		Region:     s.cfg.AWSRegion,
	}

	targets, src, err := ResolveTargets(ctx, s.cfg.S3Bucket, s.cfg.S3Key, s.cfg.LocalTargets, s3Opts)
	if err != nil {
		return nil, fmt.Errorf("resolve targets: %w", err)
	}
	s.logger.Info("loaded target repos", "count", len(targets), "source", src)

	token := s.cfg.GitHubToken
	if token == "" {
		token = GitHubToken()
	}

	issues, err := CheckOSSIssues(ctx, nil, targets, now, token)
	if err != nil {
		return nil, fmt.Errorf("check oss issues: %w", err)
	}

	s.logger.Info("oss check finished", "top_matches", len(issues))
	if len(issues) == 0 {
		return nil, nil
	}

	var sb strings.Builder
	for _, it := range issues {
		sb.WriteString(fmt.Sprintf("• %s [%s]\n  %s\n", it.Summary, it.Language, it.URL))
	}

	title := fmt.Sprintf("OSS Good-First Issues: %d matches (Go/Python)", len(issues))
	body := strings.TrimSpace(sb.String())

	if s.sender != nil {
		_ = s.sender.Send(ctx, notify.Message{
			Title:    title,
			Body:     body,
			Priority: 3,
			Tags:     []string{"octocat"},
		})
	}

	return issues, nil
}

// RunScheduler blocks and runs the complete 13-pipeline schedule in IST timezone:
// - Startup: Dual-copy reconciliation (laptop <-> S3)
// - Daily 07:30 IST: Task 8 (DSA Coach Morning problem assignment)
// - Daily 07:45 IST: Task 9 (Morning Brief consolidated notification)
// - Daily 08:00 IST: Task 1 (Internship Tracker) + Task 2 (Resume Tailoring)
// - Daily 08:30 IST: Task 6 (Hackathon Tracker)
// - Monday 09:00 IST: Task 7 (Open-Source Watch & 2027 GSoC/LFX/MLH dates)
// - Weekdays 09:30-19:00 IST: Tasks 3 & 4 (Alumni LinkedIn & Startup Cold Email pacing)
// - Daily 18:00 IST: Task 11 (Thread Alerts)
// - Saturday 10:00 IST: Task 10 (Weekly Post Draft)
// - Sunday 19:00 IST: Task 13 (Sunday Report)
// - Daily 21:30 IST: Task 8 (DSA Coach Night review, streak, LeetCode check)
// - Hourly: Dual-copy reconciliation (laptop <-> S3)
func (s *WatchService) RunScheduler(ctx context.Context) error {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}

	// 1. Startup: Reconcile Dual-Copy Storage
	go func() {
		s.logger.Info("running startup dual-copy reconciliation")
		_ = s.DualSync.ReconcileAll(ctx)
	}()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	s.logger.Info("master 13-pipeline scheduler started (IST timezone)")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			ist := now.In(loc)
			h, m := ist.Hour(), ist.Minute()
			weekday := ist.Weekday()

			// 07:30 IST: DSA Morning
			if h == 7 && m == 30 {
				go func() { _, _ = s.Manager.RunDSACoachMorning(ctx) }()
			}

			// 07:45 IST: Morning Brief
			if h == 7 && m == 45 {
				go func() { _ = s.Manager.RunMorningBrief(ctx) }()
			}

			// 08:00 IST: Internship Tracker
			if h == 8 && m == 0 {
				go func() { _, _ = s.Manager.RunInternshipTracker(ctx) }()
			}

			// 08:30 IST: Hackathon Tracker
			if h == 8 && m == 30 {
				go func() { _, _ = s.RunHackathonCheck(ctx) }()
			}

			// Monday 09:00 IST: Open-Source Watch
			if weekday == time.Monday && h == 9 && m == 0 {
				go func() { _, _ = s.RunOSSCheck(ctx) }()
			}

			// Weekday 10:30 IST: Outreach turn (LinkedIn & Email within daily caps)
			if weekday != time.Saturday && weekday != time.Sunday && h == 10 && m == 30 {
				go func() {
					_ = s.Manager.RunLinkedInOutreach(ctx)
					time.Sleep(s.Safety.RandomPacingDelay())
					_ = s.Manager.RunStartupColdEmail(ctx)
				}()
			}

			// Saturday 10:00 IST: Weekly Post Draft
			if weekday == time.Saturday && h == 10 && m == 0 {
				go func() { _ = s.Manager.RunWeeklyPostDraft(ctx) }()
			}

			// Daily 18:00 IST: Thread Alerts
			if h == 18 && m == 0 {
				go func() { _ = s.Manager.RunThreadAlerts(ctx) }()
			}

			// Sunday 19:00 IST: Sunday Report
			if weekday == time.Sunday && h == 19 && m == 0 {
				go func() { _ = s.Manager.RunSundayReport(ctx) }()
			}

			// Daily 21:30 IST: DSA Night
			if h == 21 && m == 30 {
				go func() { _, _, _, _ = s.Manager.RunDSACoachNight(ctx, "adhyan-jain") }()
			}

			// Hourly dual-copy reconciliation
			if m == 0 {
				go func() { _ = s.DualSync.ReconcileAll(ctx) }()
			}
		}
	}
}

func loadSeen(path string) map[string]bool {
	out := make(map[string]bool)
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func saveSeen(path string, seen map[string]bool) {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.Marshal(seen)
	_ = os.WriteFile(path, b, 0o600)
}
