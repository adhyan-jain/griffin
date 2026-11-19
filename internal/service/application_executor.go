package service

import (
	"context"
	"encoding/json"
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

// CandidateProfile encapsulates the verified owner profile data for applications.
type CandidateProfile struct {
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	School        string `json:"school"`
	Degree        string `json:"degree"`
	GradYear      string `json:"grad_year"`
	GitHub        string `json:"github"`
	LinkedIn      string `json:"linkedin"`
	Portfolio     string `json:"portfolio"`
	Country       string `json:"country"`
	WorkAuthIndia string `json:"work_auth_india"`
}

// DefaultCandidateProfile returns the owner's authentic profile.
func DefaultCandidateProfile() CandidateProfile {
	return CandidateProfile{
		FullName:      "Adhyan Jain",
		Email:         "adhyanjain2006@gmail.com",
		Phone:         "+91 99999 99999",
		School:        "Vellore Institute of Technology, Vellore",
		Degree:        "B.Tech in Computer Science and Engineering",
		GradYear:      "2027",
		GitHub:        "https://github.com/adhyan-jain",
		LinkedIn:      "https://linkedin.com/in/adhyan-jain",
		Portfolio:     "https://github.com/adhyan-jain",
		Country:       "India",
		WorkAuthIndia: "Yes",
	}
}

// ApplicationExecutor automates profile-grounded form filling, resume tailoring, and receipt generation under standing policy rules.
type ApplicationExecutor struct {
	DiscoveryRepo *repository.DiscoveryRepository
	ReceiptDir    string
	Profile       CandidateProfile
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

// TailorResumeBullets selects the highest-impact verified project highlights matching role requirements.
func (e *ApplicationExecutor) TailorResumeBullets(company, role string) string {
	lower := strings.ToLower(role)
	switch {
	case strings.Contains(lower, "ai") || strings.Contains(lower, "llm") || strings.Contains(lower, "eval") || strings.Contains(lower, "graph"):
		return fmt.Sprintf(`+ EchoTales: Architected bitemporal narrative knowledge graph querying entity states via state_of(target, timeline, position, observer).
+ EchoTales: Engineered identity coreference resolution engine combining rule pre-filters with learned tiebreaker across 21k spans.
+ EchoTales: Evaluated precision/recall using MUC, B-cubed, and CEAF metrics with a bounded human DEFER escalation queue.
+ SGLang (Open Source): Contributed developer documentation and structured decoding runtime guides with the ACM VIT team.`)

	case strings.Contains(lower, "agent") || strings.Contains(lower, "full") || strings.Contains(lower, "frontend"):
		return fmt.Sprintf(`+ Griffin: Architected autonomous agent orchestrator coordinating 11 domain managers across Engineering and Operations.
+ Griffin: Implemented deterministic intent classification with LLM fallback planning and capability security boundaries.
+ Griffin: Developed full-stack Next.js web application with real-time SSE streaming for live task tracking and multi-threaded chats.
+ Griffin: Designed 4-tier risk classification engine enforcing human approval gates on sensitive tool execution.`)

	default: // Distributed systems / backend
		return fmt.Sprintf(`+ ComputeHive: Engineered distributed task scheduler in Go coordinating job distribution across heterogeneous local/remote nodes.
+ ComputeHive: Enforced atomic state transitions via server-side Redis Lua scripts, strictly serializing claims and eliminating double-leases.
+ ComputeHive: Built outbound pull-based worker protocol over TLS and gRPC with dead-worker lease recovery in <15s.
+ ComputeHive: Hardened container execution sandboxes with dropped Linux capabilities and read-only root filesystems.`)
	}
}

// AutoFillForm maps candidate profile data to standard form fields.
func (e *ApplicationExecutor) AutoFillForm(fields []domain.FormField) map[string]any {
	p := e.Profile
	if p.FullName == "" {
		p = DefaultCandidateProfile()
	}

	payload := make(map[string]any)
	for _, f := range fields {
		lbl := strings.ToLower(f.Label + " " + f.Name)
		switch {
		case strings.Contains(lbl, "first") && strings.Contains(lbl, "name"):
			payload[f.Name] = "Adhyan"
		case strings.Contains(lbl, "last") && strings.Contains(lbl, "name"):
			payload[f.Name] = "Jain"
		case strings.Contains(lbl, "name"):
			payload[f.Name] = p.FullName
		case strings.Contains(lbl, "email"):
			payload[f.Name] = p.Email
		case strings.Contains(lbl, "phone") || strings.Contains(lbl, "mobile"):
			payload[f.Name] = p.Phone
		case strings.Contains(lbl, "school") || strings.Contains(lbl, "university") || strings.Contains(lbl, "college"):
			payload[f.Name] = p.School
		case strings.Contains(lbl, "degree") || strings.Contains(lbl, "major"):
			payload[f.Name] = p.Degree
		case strings.Contains(lbl, "grad") || strings.Contains(lbl, "year"):
			payload[f.Name] = p.GradYear
		case strings.Contains(lbl, "github"):
			payload[f.Name] = p.GitHub
		case strings.Contains(lbl, "linkedin"):
			payload[f.Name] = p.LinkedIn
		case strings.Contains(lbl, "website") || strings.Contains(lbl, "portfolio"):
			payload[f.Name] = p.Portfolio
		case strings.Contains(lbl, "resume") || strings.Contains(lbl, "cv"):
			payload[f.Name] = "Adhyan_Jain_Resume.pdf"
		default:
			if f.Value != "" {
				payload[f.Name] = f.Value
			}
		}
	}
	return payload
}

// ProcessApplication evaluates the standing authorization policy, generates tailored collateral, and records verifiable submission receipts.
func (e *ApplicationExecutor) ProcessApplication(ctx context.Context, userID uuid.UUID, company, role, url string, fields []domain.FormField) (*domain.SubmissionReceipt, error) {
	decision, reasons := domain.EvaluateStandingPolicy(fields)
	tailoredBullets := e.TailorResumeBullets(company, role)
	formPayload := e.AutoFillForm(fields)

	if len(reasons) > 0 {
		formPayload["_escalation_reasons"] = reasons
	}

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
