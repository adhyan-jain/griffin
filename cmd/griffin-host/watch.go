package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/griffin/core/internal/storage"
	"github.com/griffin/core/internal/watch"
)

func newWatcherConfig(logger *slog.Logger) watch.WatcherConfig {
	return watch.WatcherConfig{
		S3Bucket:     envOr("AWS_S3_BUCKET", "griffin-artifacts-220115213279-ap-south-1"),
		S3Key:        envOr("OSS_TARGETS_S3_KEY", watch.DefaultS3Key),
		LocalTargets: envOr("OSS_TARGETS_FILE", watch.DefaultLocalTargetsPath()),
		AWSProfile:   envOr("AWS_PROFILE", "griffin-bootstrap"),
		AWSRegion:    envOr("AWS_REGION", "ap-south-1"),
		GitHubToken:  watch.GitHubToken(),
		NtfyTopic:    envOr("NTFY_TOPIC", ""),
		NtfyServer:   envOr("NTFY_SERVER", "https://ntfy.sh"),
		NtfyToken:    envOr("NTFY_TOKEN", ""),
		Logger:       logger,
	}
}

func watchHackathonsCmd(ctx context.Context, logger *slog.Logger) error {
	svc := watch.NewWatchService(newWatcherConfig(logger))
	fmt.Println("Checking Devpost, Devfolio, and Unstop for online hackathons open to Indian students with cash prizes...")
	matches, err := svc.RunHackathonCheck(ctx)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		fmt.Println("No new eligible hackathons found.")
		return nil
	}
	fmt.Printf("Found %d matching hackathons:\n\n", len(matches))
	for i, m := range matches {
		prize := m.Prize
		if prize == "" {
			prize = "Cash prize"
		}
		fmt.Printf("%d. %s\n   Source: %s | Prize: %s | Closes: %s\n   Link:   %s\n\n",
			i+1, m.Name, m.Source, prize, m.RegCloses.Format("Jan 02, 2006"), m.Link)
	}
	return nil
}

func watchOSSCmd(ctx context.Context, logger *slog.Logger) error {
	svc := watch.NewWatchService(newWatcherConfig(logger))
	fmt.Println("Checking target repos for open unassigned good-first/help-wanted issues in last 30 days (Go/Python)...")
	issues, err := svc.RunOSSCheck(ctx)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		fmt.Println("No matching good-first issues found right now.")
		return nil
	}
	fmt.Printf("Top %d matching issues:\n\n", len(issues))
	for i, it := range issues {
		fmt.Printf("%d. %s [%s]\n   URL:     %s\n   Created: %s\n\n",
			i+1, it.Summary, it.Language, it.URL, it.CreatedAt.Format("Jan 02, 2006"))
	}
	return nil
}

func watchSchedulerCmd(ctx context.Context, logger *slog.Logger) error {
	svc := watch.NewWatchService(newWatcherConfig(logger))
	fmt.Println("Starting Griffin Watch scheduler: daily 08:30 hackathons, weekly Monday 08:30 OSS issues.")
	return svc.RunScheduler(ctx)
}

func targetsUploadCmd(ctx context.Context, logger *slog.Logger, filePath string) error {
	cfg := newWatcherConfig(logger)
	if filePath == "" {
		filePath = cfg.LocalTargets
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("File %s not found. Creating default curated targets list...\n", filePath)
			var merr error
			data, merr = json.MarshalIndent(watch.DefaultTargets, "", "  ")
			if merr != nil {
				return merr
			}
			if err := watch.SaveTargetsToFile(filePath, data); err != nil {
				return fmt.Errorf("write default targets to %s: %w", filePath, err)
			}
			fmt.Printf("Saved default targets to %s\n", filePath)
		} else {
			return err
		}
	}

	// Validate JSON
	targets, err := watch.ParseTargets(data)
	if err != nil {
		return fmt.Errorf("invalid targets JSON: %w", err)
	}

	fmt.Printf("Uploading %d target repos from %s to s3://%s/%s...\n", len(targets), filePath, cfg.S3Bucket, cfg.S3Key)
	s3Opts := storage.S3ClientOptions{
		Bucket:     cfg.S3Bucket,
		AWSProfile: cfg.AWSProfile,
		Region:     cfg.AWSRegion,
	}

	if err := watch.UploadTargetsToS3(ctx, cfg.S3Bucket, cfg.S3Key, data, s3Opts); err != nil {
		return fmt.Errorf("upload to s3: %w", err)
	}

	fmt.Printf("✓ Successfully uploaded %d targets to s3://%s/%s\n", len(targets), cfg.S3Bucket, cfg.S3Key)
	return nil
}

func pipelineSyncCmd(ctx context.Context, logger *slog.Logger) error {
	svc := watch.NewWatchService(newWatcherConfig(logger))
	fmt.Println("Reconciling dual-copy storage between local (/home/adhyan/Documents/Griffin) and S3...")
	if err := svc.DualSync.ReconcileAll(ctx); err != nil {
		return fmt.Errorf("sync dual storage: %w", err)
	}
	fmt.Println("✓ Dual-copy reconciliation completed successfully.")
	return nil
}

func pipelineAuditCmd(ctx context.Context, logger *slog.Logger) error {
	svc := watch.NewWatchService(newWatcherConfig(logger))
	now := time.Now()

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}
	istNow := now.In(loc)

	fmt.Printf("=== Griffin Autonomous Pipelines & Safety Audit ===\n")
	fmt.Printf("Current Time (IST): %s\n", istNow.Format("2006-01-02 15:04:05 MST"))

	inWindow := svc.Safety.OperatingWindowActive(now)
	fmt.Printf("Operating Window:   %s (Active: Weekdays 09:30-19:00 IST)\n\n", map[bool]string{true: "ACTIVE", false: "INACTIVE (Outward actions paused)"}[inWindow])

	fmt.Println("--- Daily Quotas (Rule 0 Caps) ---")
	liCount := svc.Safety.TodayActionCount("linkedin_note")
	emailCount := svc.Safety.TodayActionCount("cold_email")
	appCount := svc.Safety.TodayActionCount("application")

	fmt.Printf("LinkedIn Connection Notes: %d / %d\n", liCount, watch.MaxDailyLinkedInNotes)
	fmt.Printf("Startup Cold Emails:       %d / %d\n", emailCount, watch.MaxDailyColdEmails)
	fmt.Printf("Job Applications:          %d / %d\n\n", appCount, watch.MaxDailyApplications)

	fmt.Println("--- Circuit Breakers (24h Lockout on CAPTCHA/2FA/RateLimits) ---")
	breakers := svc.Safety.ActiveBreakers()
	if len(breakers) == 0 {
		fmt.Println("None. All pipelines operational.")
	} else {
		for _, b := range breakers {
			fmt.Printf("🚨 HALTED: Pipeline %q locked until %s (Reason: %s)\n", b.Pipeline, b.Until.In(loc).Format("15:04:05 MST"), b.Reason)
		}
	}
	fmt.Println()

	fmt.Println("--- Recent Audit Logs (Today) ---")
	logs := svc.Safety.RecentAuditLogs(10)
	if len(logs) == 0 {
		fmt.Println("No audit records logged today.")
	} else {
		for _, l := range logs {
			fmt.Println(l)
		}
	}
	return nil
}

func pipelineRunCmd(ctx context.Context, logger *slog.Logger, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("specify pipeline name to run (e.g., internships, linkedin, email, inbox, hackathons, oss, dsa-morning, dsa-night, brief, post, threads, hygiene, report, sync)")
	}
	pName := strings.ToLower(strings.TrimSpace(args[0]))
	svc := watch.NewWatchService(newWatcherConfig(logger))

	switch pName {
	case "internships", "internship", "1":
		fmt.Println("Running Pipeline 1: Internship Tracker...")
		items, err := svc.Manager.RunInternshipTracker(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Found %d active internships. Output saved locally and to S3.\n", len(items))
		return nil

	case "linkedin", "3":
		fmt.Println("Running Pipeline 3: Alumni LinkedIn Outreach...")
		if err := svc.Manager.RunLinkedInOutreach(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Alumni LinkedIn outreach step completed.")
		return nil

	case "email", "cold-email", "4", "4a":
		fmt.Println("Running Pipeline 4A: Startup Cold Email Outreach...")
		if err := svc.Manager.RunStartupColdEmail(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Cold email outreach step completed.")
		return nil

	case "inbox", "5":
		fmt.Println("Running Pipeline 5: Inbox Monitor...")
		if err := svc.Manager.RunInboxMonitor(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Inbox check completed.")
		return nil

	case "hackathons", "hackathon", "6":
		return watchHackathonsCmd(ctx, logger)

	case "oss", "7":
		return watchOSSCmd(ctx, logger)

	case "dsa-morning", "8-morning":
		fmt.Println("Running Pipeline 8: DSA Coach (Morning)...")
		problems, err := svc.Manager.RunDSACoachMorning(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Assigned problems: %s\n", strings.Join(problems, ", "))
		return nil

	case "dsa-night", "8-night":
		fmt.Println("Running Pipeline 8: DSA Coach (Night Review)...")
		username := "adhyan-jain"
		if len(args) > 1 {
			username = args[1]
		}
		streak, total, nextPattern, err := svc.Manager.RunDSACoachNight(ctx, username)
		if err != nil {
			return err
		}
		fmt.Printf("✓ DSA Review completed: Streak = %d days | Solved = %d/150 | Tomorrow Pattern = %s\n", streak, total, nextPattern)
		return nil

	case "brief", "morning-brief", "9":
		fmt.Println("Running Pipeline 9: Morning Brief...")
		if err := svc.Manager.RunMorningBrief(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Morning brief compiled and sent.")
		return nil

	case "post", "weekly-post", "10":
		fmt.Println("Running Pipeline 10: Weekly Post Draft...")
		if err := svc.Manager.RunWeeklyPostDraft(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Weekly post angles drafted.")
		return nil

	case "threads", "thread-alerts", "11":
		fmt.Println("Running Pipeline 11: Community Thread Alerts...")
		if err := svc.Manager.RunThreadAlerts(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Thread alerts scan completed.")
		return nil

	case "hygiene", "repo-hygiene", "12":
		fmt.Println("Running Pipeline 12: Repo Hygiene...")
		if err := svc.Manager.RunRepoHygiene(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Repo hygiene scan completed.")
		return nil

	case "report", "sunday-report", "13":
		fmt.Println("Running Pipeline 13: Sunday Report...")
		if err := svc.Manager.RunSundayReport(ctx); err != nil {
			return err
		}
		fmt.Println("✓ Sunday report generated.")
		return nil

	case "sync":
		return pipelineSyncCmd(ctx, logger)

	default:
		return fmt.Errorf("unknown pipeline %q (valid: internships, linkedin, email, inbox, hackathons, oss, dsa-morning, dsa-night, brief, post, threads, hygiene, report, sync)", pName)
	}
}
