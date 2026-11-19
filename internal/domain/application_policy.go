package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// StandingPolicyDecision represents the outcome of standing authorization evaluation.
type StandingPolicyDecision string

const (
	PolicyAutoStaged    StandingPolicyDecision = "auto_staged"
	PolicyAutoSubmitted StandingPolicyDecision = "auto_submitted"
	PolicyEscalate      StandingPolicyDecision = "escalate"
)

// FormField represents a question or input field on a job application form.
type FormField struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"` // text, textarea, select, checkbox, radio, file
	Required bool   `json:"required"`
	Value    string `json:"value,omitempty"`
}

// SubmissionReceipt records verifiable proof of an application submission.
type SubmissionReceipt struct {
	ID              uuid.UUID              `json:"id"`
	UserID          uuid.UUID              `json:"user_id"`
	Company         string                 `json:"company"`
	Role            string                 `json:"role"`
	URL             string                 `json:"url"`
	PolicyDecision  StandingPolicyDecision `json:"policy_decision"`
	FormPayload     map[string]any         `json:"form_payload"`
	TailoredBullets string                 `json:"tailored_bullets"`
	ReceiptPath     string                 `json:"receipt_path"`
	ReceiptHash     string                 `json:"receipt_hash"`
	SubmittedAt     time.Time              `json:"submitted_at"`
}

var (
	// Legal declarations requiring human legal consent
	reLegalDeclarations = regexp.MustCompile(`(?i)\b(arbitration|non-compete|non-disclosure|nda|intellectual property|ip assignment|export control|background check|credit check|felony|conviction|criminal record|drug test)\b`)

	// Sensitive questions requiring personal human disclosure
	reSensitiveQuestions = regexp.MustCompile(`(?i)\b(disability|veteran|gender identity|race|ethnicity|sexual orientation|sponsorship|visa requirement|work authorization status|expected salary|salary expectation|compensation requirement)\b`)
)

// EvaluateStandingPolicy inspects form fields to enforce Rule 39 safety boundaries.
// Standard biographical & portfolio fields pass for auto-staging.
// Any legal declaration or sensitive question triggers immediate escalation to human confirmation.
func EvaluateStandingPolicy(fields []FormField) (StandingPolicyDecision, []string) {
	var escalationReasons []string

	for _, f := range fields {
		probe := strings.ToLower(f.Label + " " + f.Name)

		if matches := reLegalDeclarations.FindAllString(probe, -1); len(matches) > 0 {
			escalationReasons = append(escalationReasons, fmt.Sprintf("Legal declaration encountered in '%s': %s", f.Label, strings.Join(matches, ", ")))
		}

		if matches := reSensitiveQuestions.FindAllString(probe, -1); len(matches) > 0 {
			escalationReasons = append(escalationReasons, fmt.Sprintf("Sensitive disclosure encountered in '%s': %s", f.Label, strings.Join(matches, ", ")))
		}
	}

	if len(escalationReasons) > 0 {
		return PolicyEscalate, escalationReasons
	}

	return PolicyAutoStaged, nil
}

// GenerateReceiptHash computes a SHA-256 digest of submission collateral.
func GenerateReceiptHash(company, role, url, payloadJSON string, submittedAt time.Time) string {
	raw := fmt.Sprintf("%s:%s:%s:%s:%s", company, role, url, payloadJSON, submittedAt.UTC().Format(time.RFC3339))
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
