package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
)

var (
	reInterview = regexp.MustCompile(`(?i)\b(interview|invitation|invited to interview|schedule a chat|screening round|technical round|hiring team)\b`)
	reCalendar  = regexp.MustCompile(`(?i)\b(calendar reminder|google calendar|invitation:|meeting with|scheduled call)\b`)
	reUrgent    = regexp.MustCompile(`(?i)\b(verification code|security alert|urgent action|password reset|otp\b)\b`)
)

// InboundTriageService evaluates incoming notifications and signals from mobile devices, webhooks, or mail.
type InboundTriageService struct {
	Events   *repository.InboundEventRepository
	Outreach *repository.OutreachRepository
	Sender   notify.Sender
	Now      func() time.Time
}

func (s *InboundTriageService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Ingest evaluates, triages, and persists an inbound notification or event.
func (s *InboundTriageService) Ingest(ctx context.Context, userID uuid.UUID, source, pkgName, title, text string, rawPayload json.RawMessage) (domain.InboundEvent, domain.TriageResult, error) {
	result := s.Evaluate(title, text, pkgName)

	// Check if this matches a candidate we reached out to
	if s.Outreach != nil && (result.Category == domain.CategoryOpportunity || result.Category == domain.CategoryChatReply || result.Category == domain.CategoryOther) {
		targets, err := s.Outreach.AwaitingReply(ctx, userID)
		if err == nil {
			for _, t := range targets {
				if t.Name != "" && strings.Contains(strings.ToLower(title+" "+text), strings.ToLower(t.Name)) {
					result.Category = domain.CategoryOpportunity
					result.Actionable = true
					result.TriageAction = "outreach_reply"
					result.Candidate = t.Name
					// Update target status in outreach
					_, _ = s.Outreach.RecordReplyByName(ctx, userID, t.Name, text)
					break
				}
			}
		}
	}

	event := domain.InboundEvent{
		UserID:       userID,
		Source:       source,
		PackageName:  pkgName,
		Category:     result.Category,
		Title:        title,
		Text:         text,
		Actionable:   result.Actionable,
		TriageAction: result.TriageAction,
		Payload:      rawPayload,
		CreatedAt:    s.now(),
	}

	if s.Events != nil {
		if err := s.Events.Create(ctx, &event); err != nil {
			return event, result, fmt.Errorf("persist inbound event: %w", err)
		}
	}

	// If actionable opportunity or urgent alert, send immediate push if sender configured
	if s.Sender != nil && result.Actionable {
		tag := "briefcase"
		if result.Category == domain.CategoryUrgentAlert {
			tag = "warning"
		}
		_ = s.Sender.Send(ctx, notify.Message{
			Title: fmt.Sprintf("[%s] %s", strings.ToUpper(string(result.Category)), title),
			Body:  result.Summary,
			Tags:  []string{tag},
		})
	}

	return event, result, nil
}

// Evaluate performs pure rule-based and heuristic classification on the text.
func (s *InboundTriageService) Evaluate(title, text, pkgName string) domain.TriageResult {
	combined := title + "\n" + text
	links := domain.ExtractLinks(combined)

	var assessments []string
	seenAssess := map[string]bool{}
	for _, l := range links {
		if domain.IsAssessmentLink(l, combined) && !seenAssess[l] {
			seenAssess[l] = true
			assessments = append(assessments, l)
		}
	}

	// Check for deadlines
	rTemp := domain.AnalyzeReply(domain.BriefReply{}, combined)
	deadlines := rTemp.Deadlines

	// 1. Check for assessment or interview opportunity
	if len(assessments) > 0 || (reInterview.MatchString(combined) && len(links) > 0) {
		summary := fmt.Sprintf("Actionable opportunity detected: %s", title)
		if len(deadlines) > 0 {
			summary += " (Deadline: " + deadlines[0] + ")"
		}
		return domain.TriageResult{
			Category:     domain.CategoryOpportunity,
			Actionable:   true,
			TriageAction: "assessment_deadline",
			Summary:      summary,
			Assessments:  assessments,
			Deadlines:    deadlines,
		}
	}

	// 2. Calendar / Meeting alert
	if strings.Contains(pkgName, "calendar") || reCalendar.MatchString(combined) {
		return domain.TriageResult{
			Category:     domain.CategoryCalendarAlert,
			Actionable:   true,
			TriageAction: "calendar_alert",
			Summary:      fmt.Sprintf("Calendar update: %s", title),
			Deadlines:    deadlines,
		}
	}

	// 3. Urgent security or OTP alert
	if reUrgent.MatchString(combined) {
		return domain.TriageResult{
			Category:     domain.CategoryUrgentAlert,
			Actionable:   false, // Ephemeral; no workflow task needed
			TriageAction: "urgent_alert",
			Summary:      fmt.Sprintf("Urgent notification: %s", title),
		}
	}

	// 4. Default: informational
	return domain.TriageResult{
		Category:     domain.CategoryInformational,
		Actionable:   false,
		TriageAction: "none",
		Summary:      title,
		Assessments:  assessments,
		Deadlines:    deadlines,
	}
}
