package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/griffin/core/internal/notify"
)

var (
	ErrClaudeCLIFailed    = errors.New("claude CLI tailoring execution failed (no fallback model allowed)")
	ErrBaseResumeNotFound = errors.New("base resume PDF not found")
)

// TailorRequest specifies input for tailoring a resume.
type TailorRequest struct {
	Company       string `json:"company"`
	Role          string `json:"role"`
	Tier          string `json:"tier"`
	JobDesc       string `json:"job_description"`
	JobLink       string `json:"job_link"`
	CompanySource string `json:"company_source"` // URL about company/product
	ScreeningQs   string `json:"screening_questions,omitempty"`
}

// TailoredResult contains the output of the Claude tailoring process.
type TailoredResult struct {
	BaseResumePath   string            `json:"base_resume_path"`
	BulletDiff       string            `json:"bullet_diff"`
	CoverNote        string            `json:"cover_note"`
	ScreeningAnswers map[string]string `json:"screening_answers"`
	SourceURL        string            `json:"source_url"`
	TailoredAt       time.Time         `json:"tailored_at"`
}

// ResumeTailorConfig configures the Claude CLI tailoring engine.
type ResumeTailorConfig struct {
	ClaudeBinary string // /home/adhyan/.local/bin/claude
	ContextPath  string // /home/adhyan/Downloads/AJ_Resume_Context.md
	ResumesDir   string // /home/adhyan/Downloads/resumes
	DownloadsDir string // /home/adhyan/Downloads
	Safety       *GlobalSafetyEngine
	DualSync     *DualStorageSync
	Sender       notify.Sender
	Logger       *slog.Logger
}

// ResumeTailorEngine tailors resumes using Claude Code CLI exclusively.
type ResumeTailorEngine struct {
	cfg    ResumeTailorConfig
	logger *slog.Logger
}

// NewResumeTailorEngine constructs the tailoring engine.
func NewResumeTailorEngine(cfg ResumeTailorConfig) *ResumeTailorConfigEngine {
	if cfg.ClaudeBinary == "" {
		cfg.ClaudeBinary = "/home/adhyan/.local/bin/claude"
	}
	if cfg.ContextPath == "" {
		cfg.ContextPath = "/home/adhyan/Downloads/AJ_Resume_Context.md"
	}
	if cfg.DownloadsDir == "" {
		cfg.DownloadsDir = "/home/adhyan/Downloads"
	}
	if cfg.ResumesDir == "" {
		cfg.ResumesDir = filepath.Join(cfg.DownloadsDir, "resumes")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &ResumeTailorEngine{cfg: cfg, logger: cfg.Logger}
}

// ResumeTailorConfigEngine type alias for clarity.
type ResumeTailorConfigEngine = ResumeTailorEngine

// SelectBaseResume chooses the best matching baseline resume.
func (r *ResumeTailorEngine) SelectBaseResume(role string) (string, error) {
	lower := strings.ToLower(role)

	aiCandidates := []string{
		filepath.Join(r.cfg.DownloadsDir, "AJ_Resume_AI.pdf"),
		filepath.Join(r.cfg.DownloadsDir, "AJ_Resume_AIML_Fullstack.pdf"),
	}

	sdeCandidates := []string{
		filepath.Join(r.cfg.DownloadsDir, "AJ_Resume_SDE.pdf"),
		filepath.Join(r.cfg.ResumesDir, "AdhyanJain_Resume_Base_Latest.pdf"),
		filepath.Join(r.cfg.ResumesDir, "AdhyanJain_Resume_Base.pdf"),
	}

	isAI := strings.Contains(lower, "ai") ||
		strings.Contains(lower, "ml") ||
		strings.Contains(lower, "learning") ||
		strings.Contains(lower, "llm") ||
		strings.Contains(lower, "agent") ||
		strings.Contains(lower, "eval")

	candidates := sdeCandidates
	if isAI {
		candidates = aiCandidates
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Fallback to any found
	for _, path := range append(aiCandidates, sdeCandidates...) {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("%w: checked %v", ErrBaseResumeNotFound, candidates)
}

// Tailor executes Claude Code CLI with strict single-page and truthfulness prompts.
func (r *ResumeTailorEngine) Tailor(ctx context.Context, req TailorRequest) (*TailoredResult, error) {
	basePath, err := r.SelectBaseResume(req.Role)
	if err != nil {
		return nil, err
	}

	contextData, err := os.ReadFile(r.cfg.ContextPath)
	if err != nil {
		return nil, fmt.Errorf("read resume context: %w", err)
	}

	prompt := fmt.Sprintf(`You are an expert technical resume tailoring agent.
Context Document:
%s

Target Role: %s at %s (Tier: %s)
Job Description:
%s
Company Source URL: %s

STRICT CONSTRAINTS (VIOLATIONS WILL BE REJECTED):
1. Never state any skill, metric, role, or date missing from the context document.
2. RxGPT MUST be named 'Frontend Engineering Intern'. Palni MUST be named 'AI Engineer Intern'.
3. Maintain strictly ONE PAGE by reordering and rephrasing existing bullet points to align with the JD.
4. Draft a cover note: MAXIMUM 90 words, plain text, exactly one company-specific line that cites %s.
5. Provide concise answers for any screening questions.

Output your response strictly as valid JSON with keys:
{
  "bullet_diff": "Description of bullet changes",
  "cover_note": "Max 90 words cover note with source citation",
  "screening_answers": {"question": "answer"}
}`, string(contextData), req.Role, req.Company, req.Tier, req.JobDesc, req.CompanySource, req.CompanySource)

	// Call Claude CLI
	cmd := exec.CommandContext(ctx, r.cfg.ClaudeBinary, "-p", prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		r.logger.Error("Claude CLI failed", "error", err, "stderr", stderr.String())
		if r.cfg.Sender != nil {
			_ = r.cfg.Sender.Send(ctx, notify.Message{
				Title:    "🚨 Resume Tailoring Failed",
				Body:     fmt.Sprintf("Claude CLI tailoring failed for %s at %s. No fallback model allowed per rule.", req.Role, req.Company),
				Priority: 4,
				Tags:     []string{"x", "page_facing_up"},
			})
		}
		return nil, fmt.Errorf("%w: %v (stderr: %s)", ErrClaudeCLIFailed, err, stderr.String())
	}

	rawOutput := stdout.String()
	// Extract JSON
	jsonStart := strings.Index(rawOutput, "{")
	jsonEnd := strings.LastIndex(rawOutput, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd < jsonStart {
		return nil, fmt.Errorf("%w: Claude output contained no valid JSON", ErrClaudeCLIFailed)
	}

	var parsed struct {
		BulletDiff       string            `json:"bullet_diff"`
		CoverNote        string            `json:"cover_note"`
		ScreeningAnswers map[string]string `json:"screening_answers"`
	}

	if err := json.Unmarshal([]byte(rawOutput[jsonStart:jsonEnd+1]), &parsed); err != nil {
		return nil, fmt.Errorf("parse Claude JSON: %w", err)
	}

	// Validate with GlobalSafetyEngine
	if r.cfg.Safety != nil {
		if err := r.cfg.Safety.ValidateResumeTruthfulness(parsed.BulletDiff + " " + parsed.CoverNote); err != nil {
			return nil, err
		}
		// Validate cover note contains source
		cleaned, ok := r.cfg.Safety.ValidatePersonalizedLine(parsed.CoverNote, req.CompanySource)
		if !ok {
			r.logger.Warn("dropping ungrounded cover note")
			parsed.CoverNote = ""
		} else {
			parsed.CoverNote = cleaned
		}
	}

	result := &TailoredResult{
		BaseResumePath:   basePath,
		BulletDiff:       parsed.BulletDiff,
		CoverNote:        parsed.CoverNote,
		ScreeningAnswers: parsed.ScreeningAnswers,
		SourceURL:        req.CompanySource,
		TailoredAt:       time.Now().UTC(),
	}

	return result, nil
}

// BuildApprovalCard formats the approval request for the phone and web interface.
func (r *ResumeTailorEngine) BuildApprovalCard(req TailorRequest, res *TailoredResult) map[string]any {
	return map[string]any{
		"action_type": "application_submission",
		"summary":     fmt.Sprintf("Submit application to %s for %s (%s)", req.Company, req.Role, req.Tier),
		"risk_level":  "HIGH",
		"payload": map[string]any{
			"company":           req.Company,
			"role":              req.Role,
			"tier":              req.Tier,
			"job_link":          req.JobLink,
			"source_url":        res.SourceURL,
			"base_resume":       filepath.Base(res.BaseResumePath),
			"bullet_diff":       res.BulletDiff,
			"cover_note":        res.CoverNote,
			"screening_answers": res.ScreeningAnswers,
		},
	}
}

// RecordApplication saves the approved application and dual-syncs to S3.
func (r *ResumeTailorEngine) RecordApplication(ctx context.Context, req TailorRequest, res *TailoredResult) error {
	todayStr := time.Now().Format("2006-01-02")
	cleanCompany := strings.ReplaceAll(req.Company, " ", "_")
	cleanRole := strings.ReplaceAll(req.Role, " ", "_")

	// Save sent PDF record
	sentRelPDF := fmt.Sprintf("resumes/sent/%s_%s_%s.pdf", cleanCompany, cleanRole, todayStr)
	baseData, err := os.ReadFile(res.BaseResumePath)
	if err == nil && r.cfg.DualSync != nil {
		_ = r.cfg.DualSync.SaveDual(ctx, sentRelPDF, baseData)
	}

	// Update applied.json
	appliedRel := "inbox/applied.json"
	type AppRecord struct {
		Company    string    `json:"company"`
		Role       string    `json:"role"`
		Tier       string    `json:"tier"`
		JobLink    string    `json:"job_link"`
		SourceURL  string    `json:"source_url"`
		ResumeFile string    `json:"resume_file"`
		Status     string    `json:"status"` // applied, OA, interview, offer, rejected
		AppliedAt  time.Time `json:"applied_at"`
	}

	var appliedList []AppRecord
	if r.cfg.DualSync != nil {
		data, _ := r.cfg.DualSync.ReadDual(ctx, appliedRel)
		if len(data) > 0 {
			_ = json.Unmarshal(data, &appliedList)
		}
	}

	newRec := AppRecord{
		Company:    req.Company,
		Role:       req.Role,
		Tier:       req.Tier,
		JobLink:    req.JobLink,
		SourceURL:  res.SourceURL,
		ResumeFile: sentRelPDF,
		Status:     "applied",
		AppliedAt:  time.Now().UTC(),
	}
	appliedList = append(appliedList, newRec)

	b, _ := json.MarshalIndent(appliedList, "", "  ")
	if r.cfg.DualSync != nil {
		_ = r.cfg.DualSync.SaveDual(ctx, appliedRel, b)
	}

	if r.cfg.Safety != nil {
		r.cfg.Safety.AuditLog("application_submitted", map[string]any{
			"company": req.Company,
			"role":    req.Role,
			"tier":    req.Tier,
		})
	}

	return nil
}
