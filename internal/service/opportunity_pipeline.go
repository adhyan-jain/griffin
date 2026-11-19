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
	reTierA = regexp.MustCompile(`(?i)\b(ai|agent|systems|distributed|compiler|cuda|gpu|low-latency|infra|backend|kernel|llm|performance)\b`)
)

// OpportunityInput specifies an extracted job or client opportunity.
type OpportunityInput struct {
	Company     string `json:"company"`
	Role        string `json:"role"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// OpportunityPipeline scores incoming listings and automatically stages high-match applications with tailored collateral.
type OpportunityPipeline struct {
	Tasks    repository.TaskRepository
	Profiles ProfileSource // verified owner profile; nil means no pitch claims are generated
	Logger   *slog.Logger
	Now      func() time.Time
}

func (p *OpportunityPipeline) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *OpportunityPipeline) log() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}

// ScoreOpportunity checks if a listing is Tier A based on role and description keywords.
func (p *OpportunityPipeline) ScoreOpportunity(op OpportunityInput) (bool, string) {
	combined := op.Role + " " + op.Description
	matches := reTierA.FindAllString(combined, -1)
	if len(matches) >= 2 || (len(matches) >= 1 && strings.Contains(strings.ToLower(op.Role), "ai")) {
		return true, "Tier A"
	}
	return false, "Standard"
}

// StageOpportunity crafts tailored collateral and stages a task + approval for the owner.
func (p *OpportunityPipeline) StageOpportunity(ctx context.Context, userID uuid.UUID, op OpportunityInput) (*domain.Task, *domain.Approval, error) {
	if p.Tasks == nil {
		return nil, nil, fmt.Errorf("task repository not provided")
	}

	tierA, tierLabel := p.ScoreOpportunity(op)
	pitch, factIDs, pitchStatus := p.buildPitch(ctx, userID, op)

	payloadMap := map[string]any{
		"kind":           "job_application",
		"company":        op.Company,
		"role":           op.Role,
		"url":            op.URL,
		"tier":           tierLabel,
		"tailored_pitch": pitch,
		"pitch_status":   pitchStatus,
		"fact_ids":       factIDs,
		"staged_at":      p.now().Format(time.RFC3339),
	}
	payloadBytes, _ := json.Marshal(payloadMap)

	task := &domain.Task{
		UserID:        userID,
		Title:         fmt.Sprintf("Application: %s at %s", op.Role, op.Company),
		Goal:          fmt.Sprintf("Submit tailored application for %s at %s: %s", op.Role, op.Company, op.URL),
		ManagerDomain: "career",
		Priority:      domain.TaskPriorityHighP1,
		InputPayload:  payloadBytes,
		Status:        domain.TaskStatusQueued,
		CreatedAt:     p.now(),
		UpdatedAt:     p.now(),
	}
	if tierA {
		task.Priority = domain.TaskPriorityCriticalP0
	}

	createdTask, err := p.Tasks.CreateTask(ctx, task)
	if err != nil {
		return nil, nil, fmt.Errorf("create opportunity task: %w", err)
	}

	expiresAt := p.now().Add(7 * 24 * time.Hour)
	approval := &domain.Approval{
		UserID:        userID,
		TaskID:        createdTask.ID,
		ActionType:    "APPLY_JOB",
		RiskLevel:     domain.ApprovalRiskHigh,
		Summary:       fmt.Sprintf("Apply for %s at %s with tailored highlights", op.Role, op.Company),
		ActionPayload: payloadBytes,
		ExpiresAt:     &expiresAt,
	}

	if err := p.Tasks.TransitionToApproval(ctx, createdTask.ID, approval); err != nil {
		return createdTask, nil, fmt.Errorf("stage opportunity approval: %w", err)
	}

	createdTask.Status = domain.TaskStatusAwaitingApproval
	return createdTask, approval, nil
}

// buildPitch drafts a cover note from verified profile facts only. With no stored profile it
// returns an empty pitch rather than inventing one; the owner is told the profile is missing.
func (p *OpportunityPipeline) buildPitch(ctx context.Context, userID uuid.UUID, op OpportunityInput) (pitch string, factIDs []string, status string) {
	if p.Profiles == nil {
		return "", nil, "profile_missing"
	}
	prof, err := p.Profiles.Latest(ctx, userID)
	if err != nil || prof == nil {
		return "", nil, "profile_missing"
	}
	facts := prof.RelevantFacts(op.Role+" "+op.Description, 3)
	var b strings.Builder
	fmt.Fprintf(&b, "Hi %s Hiring Team,\n\nI'm applying for the %s position.", op.Company, op.Role)
	if len(facts) > 0 {
		b.WriteString(" Relevant experience:\n")
		for _, f := range facts {
			b.WriteString("- " + f.Project + ": " + f.Text + "\n")
			factIDs = append(factIDs, f.ID)
		}
	} else {
		b.WriteString("\n")
	}
	b.WriteString("\nBest regards,\n" + prof.FullName)
	if prof.GitHub != "" {
		b.WriteString("\n" + prof.GitHub)
	}
	return b.String(), factIDs, "grounded"
}
