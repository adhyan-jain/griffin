package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

var (
	reDeadline = regexp.MustCompile(`(?i)\b(?:by|before|due|deadline(?:\s+is)?)\s*:?\s*([A-Za-z]+,?\s+[A-Za-z]+\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}(?:\s+at\s+\d{1,2}(?::\d{2})?\s*(?:am|pm|ist|utc))?|\d{1,2}\s+(?:hours|hrs|days))\b`)
	reCompany  = regexp.MustCompile(`(?i)from\s+([A-Za-z0-9\s]{2,30})\s+(?:team|hiring|recruiting|talent)`)
)

// SyncResult details the outcome of an inbox synchronization sweep.
type SyncResult struct {
	MessagesSynced   int                        `json:"messages_synced"`
	AssessmentsFound []*domain.AssessmentRecord `json:"assessments_found"`
	RecruiterReplies int                        `json:"recruiter_replies"`
}

// GmailSyncer handles incremental synchronization, thread assembly, and recruiter assessment parsing.
type GmailSyncer struct {
	InboxRepo *repository.InboxRepository
	Events    *repository.InboundEventRepository
	Triage    *InboundTriageService
	Tasks     repository.TaskRepository
	Logger    *slog.Logger
	Now       func() time.Time
}

func (s *GmailSyncer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *GmailSyncer) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// ProcessMessages inspects synchronized emails, stores them, and extracts actionable assessments & deadlines.
func (s *GmailSyncer) ProcessMessages(ctx context.Context, userID uuid.UUID, messages []domain.EmailMessage) (*SyncResult, error) {
	res := &SyncResult{}

	for _, msg := range messages {
		// 1. Store email in email_threads
		if s.InboxRepo != nil {
			if err := s.InboxRepo.StoreEmailMessage(ctx, userID, &msg); err != nil {
				s.log().Warn("failed to store email message", "id", msg.ID, "error", err)
			}
		}
		res.MessagesSynced++

		// 2. Perform inbound triage for assessments, deadlines, and recruiter reach-outs
		content := fmt.Sprintf("%s\n%s", msg.Subject, msg.BodyText)
		triageResult := domain.TriageResult{}
		if s.Triage != nil {
			triageResult = s.Triage.Evaluate(msg.Subject, msg.BodyText, "com.google.android.gm")
		} else {
			triageResult = s.fallbackTriage(msg)
		}

		// 3. If an assessment is detected, record the assessment and agenda event
		hasAssessment := len(triageResult.Assessments) > 0 || triageResult.TriageAction == "assessment_deadline"
		if hasAssessment {
			testURL := ""
			if len(triageResult.Assessments) > 0 {
				testURL = triageResult.Assessments[0]
			} else {
				testURL = s.findAssessmentURL(msg.BodyText)
			}

			platform := s.inferPlatform(testURL, content)
			company := s.inferCompany(msg.Sender, msg.Subject, content)
			deadline := s.extractDeadline(content, msg.ReceivedAt)

			record := &domain.AssessmentRecord{
				UserID:    userID,
				Company:   company,
				Platform:  platform,
				TestURL:   testURL,
				Deadline:  deadline,
				Status:    domain.AssessmentPending,
				CreatedAt: s.now(),
				UpdatedAt: s.now(),
			}

			// Store in assessments table
			if s.InboxRepo != nil {
				if err := s.InboxRepo.CreateAssessment(ctx, record); err != nil {
					s.log().Warn("failed to create assessment record", "error", err)
				}
			}

			// Create actionable inbound event for Morning Agenda Brief and alerts
			if s.Events != nil {
				payloadMap := map[string]any{
					"company":     company,
					"platform":    platform,
					"test_url":    record.TestURL,
					"deadline":    deadline,
					"subject":     msg.Subject,
					"sender":      msg.Sender,
					"received_at": msg.ReceivedAt,
				}
				pBytes, _ := json.Marshal(payloadMap)
				ev := &domain.InboundEvent{
					UserID:       userID,
					Source:       "gmail_sync",
					PackageName:  "com.google.android.gm",
					Category:     domain.CategoryOpportunity,
					Title:        fmt.Sprintf("Assessment: %s (%s)", company, platform),
					Text:         fmt.Sprintf("Technical assessment received for %s on %s. Due: %v", company, platform, deadline),
					Actionable:   true,
					TriageAction: "agenda_add",
					Payload:      pBytes,
					CreatedAt:    s.now(),
				}
				_ = s.Events.Create(ctx, ev)
				record.InboundEventID = &ev.ID
			}

			res.AssessmentsFound = append(res.AssessmentsFound, record)
		}

		if triageResult.TriageAction == "outreach_reply" {
			res.RecruiterReplies++
		}
	}

	return res, nil
}

func (s *GmailSyncer) fallbackTriage(msg domain.EmailMessage) domain.TriageResult {
	body := strings.ToLower(msg.Subject + " " + msg.BodyText)
	var tr domain.TriageResult

	for _, p := range []string{"codility", "hackerrank", "codesignal", "karat"} {
		if strings.Contains(body, p) {
			tr.Category = domain.CategoryOpportunity
			tr.Actionable = true
			tr.TriageAction = "assessment_deadline"
			if u := s.findAssessmentURL(msg.BodyText); u != "" {
				tr.Assessments = []string{u}
			}
			break
		}
	}
	return tr
}

func (s *GmailSyncer) findAssessmentURL(text string) string {
	for _, p := range []string{"codility", "hackerrank", "codesignal", "karat"} {
		reURL := regexp.MustCompile(`https?://[^\s<>"]*` + p + `[^\s<>"]*`)
		if m := reURL.FindString(text); m != "" {
			return m
		}
	}
	return ""
}

func (s *GmailSyncer) inferPlatform(testURL, content string) string {
	lower := strings.ToLower(testURL + " " + content)
	switch {
	case strings.Contains(lower, "codility"):
		return "codility"
	case strings.Contains(lower, "hackerrank"):
		return "hackerrank"
	case strings.Contains(lower, "codesignal"):
		return "codesignal"
	case strings.Contains(lower, "karat"):
		return "karat"
	default:
		return "technical_assessment"
	}
}

func (s *GmailSyncer) inferCompany(sender, subject, content string) string {
	// 1. Check sender display name before '<'
	if idx := strings.Index(sender, "<"); idx > 0 {
		name := strings.TrimSpace(sender[:idx])
		name = strings.Trim(name, "\"")
		for _, suffix := range []string{"Recruiting Team", "Recruiting", "Talent Team", "Talent", "Hiring Team", "Hiring", "Team"} {
			if strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
				name = strings.TrimSpace(name[:len(name)-len(suffix)])
				break
			}
		}
		if name != "" {
			return name
		}
	}

	// 2. Check subject prefix before colon (e.g. "Sarvam AI: Assessment...")
	if idx := strings.Index(subject, ":"); idx > 0 {
		prefix := strings.TrimSpace(subject[:idx])
		if len(prefix) > 1 && len(prefix) < 40 && !strings.Contains(strings.ToLower(prefix), "invitation") {
			return prefix
		}
	}

	// 3. Check regex match in content
	if m := reCompany.FindStringSubmatch(content); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}

	// 4. Extract from sender domain if possible (e.g. "recruiting@sarvam.ai" -> "Sarvam")
	if strings.Contains(sender, "@") {
		parts := strings.Split(sender, "@")
		if len(parts) > 1 {
			domainPart := strings.TrimRight(parts[1], ">")
			domainName := strings.Split(domainPart, ".")[0]
			if domainName != "gmail" && domainName != "yahoo" && domainName != "outlook" {
				return strings.Title(domainName)
			}
		}
	}
	if strings.Contains(strings.ToLower(subject), "at ") {
		idx := strings.Index(strings.ToLower(subject), "at ")
		return strings.TrimSpace(subject[idx+3:])
	}
	return "Tech Company"
}

func (s *GmailSyncer) extractDeadline(content string, refTime time.Time) *time.Time {
	if m := reDeadline.FindStringSubmatch(content); len(m) > 1 {
		raw := strings.ToLower(m[1])
		if strings.Contains(raw, "48 hours") || strings.Contains(raw, "48 hrs") {
			d := refTime.Add(48 * time.Hour)
			return &d
		}
		if strings.Contains(raw, "24 hours") || strings.Contains(raw, "24 hrs") {
			d := refTime.Add(24 * time.Hour)
			return &d
		}
		if strings.Contains(raw, "72 hours") || strings.Contains(raw, "3 days") {
			d := refTime.Add(72 * time.Hour)
			return &d
		}
	}
	// Default assessment window: 48 hours from receipt
	d := refTime.Add(48 * time.Hour)
	return &d
}
