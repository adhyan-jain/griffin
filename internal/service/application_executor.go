package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// ProfileSource loads the canonical owner profile. No personal facts live in source code.
type ProfileSource interface {
	Latest(ctx context.Context, userID uuid.UUID) (*domain.OwnerProfile, error)
}

// ErrNoOwnerProfile is returned when an operation needs the owner profile and none is stored.
// Callers must stop and ask the owner rather than guess identity details.
var ErrNoOwnerProfile = errors.New("owner profile is not configured")

// ApplicationExecutor prepares profile-grounded application packages under standing policy rules.
// It never submits anything: it records what would be sent, for review or a form adapter.
type ApplicationExecutor struct {
	DiscoveryRepo *repository.DiscoveryRepository
	Profiles      ProfileSource
	ReceiptDir    string
	Logger        *slog.Logger
	Now           func() time.Time
}

func (e *ApplicationExecutor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *ApplicationExecutor) log() *slog.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

// TailorFacts selects the verified facts most relevant to the role. Facts are returned verbatim;
// nothing is rephrased, added or inflated, and a role with no supported overlap yields no claims.
func (e *ApplicationExecutor) TailorFacts(p *domain.OwnerProfile, role string) []domain.ProfileFact {
	return p.RelevantFacts(role, 4)
}

// FormatFacts renders facts as bullet lines using their verbatim text.
func FormatFacts(facts []domain.ProfileFact) string {
	lines := make([]string, 0, len(facts))
	for _, f := range facts {
		lines = append(lines, "+ "+f.Project+": "+f.Text)
	}
	return strings.Join(lines, "\n")
}

// AutoFillForm maps verified profile values to the fields it positively recognises. It returns the
// payload and the labels of required fields it could not fill from verified data; those must go to
// the owner (unknown fields fail closed). Empty profile values are never filled.
func (e *ApplicationExecutor) AutoFillForm(p *domain.OwnerProfile, fields []domain.FormField) (map[string]any, []string) {
	payload := make(map[string]any)
	var unfilled []string
	for _, f := range fields {
		lbl := strings.ToLower(f.Label + " " + f.Name)
		var val string
		switch {
		case strings.Contains(lbl, "first") && strings.Contains(lbl, "name"):
			val = p.FirstName()
		case strings.Contains(lbl, "last") && strings.Contains(lbl, "name"):
			val = p.LastName()
		case isPersonNameField(lbl):
			val = p.FullName
		case strings.Contains(lbl, "email"):
			val = p.Email
		case strings.Contains(lbl, "phone") || strings.Contains(lbl, "mobile"):
			val = p.Phone
		case strings.Contains(lbl, "school") || strings.Contains(lbl, "university") || strings.Contains(lbl, "college"):
			val = p.School
		case strings.Contains(lbl, "degree") || strings.Contains(lbl, "major"):
			val = p.Degree
		case strings.Contains(lbl, "graduat"):
			val = p.GradYear
		case strings.Contains(lbl, "github"):
			val = p.GitHub
		case strings.Contains(lbl, "linkedin"):
			val = p.LinkedIn
		case strings.Contains(lbl, "website") || strings.Contains(lbl, "portfolio"):
			val = p.Portfolio
		case f.Type == "file" && (strings.Contains(lbl, "resume") || strings.Contains(lbl, "cv")):
			val = p.ResumeFile
		}
		switch {
		case val != "":
			payload[f.Name] = val
		case f.Value != "":
			payload[f.Name] = f.Value // pre-filled by the form itself
		case f.Required:
			unfilled = append(unfilled, f.Label)
		}
	}
	return payload, unfilled
}

// isPersonNameField is true for the applicant's own name, not "company name", "school name" etc.
func isPersonNameField(lbl string) bool {
	if !strings.Contains(lbl, "name") {
		return false
	}
	for _, other := range []string{"company", "school", "university", "college", "employer", "referr", "manager", "reference", "organi", "project", "file", "user"} {
		if strings.Contains(lbl, other) {
			return false
		}
	}
	return true
}

// ProcessApplication evaluates the standing authorization policy, generates tailored collateral, and records verifiable submission receipts.
func (e *ApplicationExecutor) ProcessApplication(ctx context.Context, userID uuid.UUID, company, role, url string, fields []domain.FormField) (*domain.SubmissionReceipt, error) {
	if e.Profiles == nil {
		return nil, ErrNoOwnerProfile
	}
	profile, err := e.Profiles.Latest(ctx, userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrNoOwnerProfile
	}
	decision, reasons := domain.EvaluateStandingPolicy(fields)
	facts := e.TailorFacts(profile, role)
	tailoredBullets := FormatFacts(facts)
	formPayload, unfilled := e.AutoFillForm(profile, fields)
	for _, label := range unfilled {
		reasons = append(reasons, "Required field not answerable from verified profile: "+label)
	}
	if len(reasons) > 0 {
		decision = domain.PolicyEscalate
		formPayload["_escalation_reasons"] = reasons
	}
	factIDs := make([]string, len(facts))
	for i, f := range facts {
		factIDs[i] = f.ID
	}
	formPayload["_supporting_fact_ids"] = factIDs
	formPayload["_profile_version"] = profile.Version

	subTime := e.now()
	payloadJSON, _ := json.Marshal(formPayload)
	receiptHash := domain.GenerateReceiptHash(company, role, url, string(payloadJSON), subTime)

	receiptDir := e.ReceiptDir
	if receiptDir == "" {
		home, _ := os.UserHomeDir()
		receiptDir = filepath.Join(home, "Documents", "Griffin", "reports", "receipts")
	}
	_ = os.MkdirAll(receiptDir, 0o755)

	cleanComp := strings.ToLower(strings.ReplaceAll(company, " ", "_"))
	cleanRole := strings.ToLower(strings.ReplaceAll(role, " ", "_"))
	receiptFileName := fmt.Sprintf("app_%s_%s_%d.json", cleanComp, cleanRole, subTime.Unix())
	receiptPath := filepath.Join(receiptDir, receiptFileName)

	receipt := &domain.SubmissionReceipt{
		ID:              uuid.New(),
		UserID:          userID,
		Company:         company,
		Role:            role,
		URL:             url,
		PolicyDecision:  decision,
		FormPayload:     formPayload,
		TailoredBullets: tailoredBullets,
		ReceiptPath:     receiptPath,
		ReceiptHash:     receiptHash,
		SubmittedAt:     subTime,
	}

	// Write receipt file to disk
	auditData, _ := json.MarshalIndent(receipt, "", "  ")
	_ = os.WriteFile(receiptPath, auditData, 0o644)

	// Persist receipt in database
	if e.DiscoveryRepo != nil {
		if err := e.DiscoveryRepo.RecordSubmissionReceipt(ctx, receipt); err != nil {
			e.log().Warn("failed to persist submission receipt in db", "error", err)
		}
	}

	return receipt, nil
}
