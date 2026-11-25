package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// ResearchRepository persists research runs, steps, sources and evidence items.
type ResearchRepository struct {
	pool database.Querier
}

// NewResearchRepository creates a new ResearchRepository.
func NewResearchRepository(pool database.Querier) *ResearchRepository {
	return &ResearchRepository{pool: pool}
}

// CreateRun initializes a new research run.
func (r *ResearchRepository) CreateRun(ctx context.Context, run *domain.ResearchRun) error {
	planJSON, _ := json.Marshal(run.Plan)
	ctxJSON, _ := json.Marshal(run.ContextData)
	constraintsJSON, _ := json.Marshal(run.ConstraintsList)
	budgetJSON, _ := json.Marshal(run.BudgetConfig)
	metricsJSON, _ := json.Marshal(run.ProgressMetrics)
	findingsJSON, _ := json.Marshal(run.Findings)
	questionsJSON, _ := json.Marshal(run.UnresolvedQuestions)
	structuredJSON, _ := json.Marshal(run.ReportStructured)

	err := r.pool.QueryRow(ctx, `
		INSERT INTO research_runs (
			user_id, playbook, objective, depth, status, plan, context_data,
			constraints_list, budget_config, progress_metrics, findings,
			unresolved_questions, report_markdown, report_structured, error_message
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		) RETURNING id, created_at, updated_at`,
		run.UserID, run.Playbook, run.Objective, run.Depth, run.Status,
		planJSON, ctxJSON, constraintsJSON, budgetJSON, metricsJSON, findingsJSON,
		questionsJSON, run.ReportMarkdown, structuredJSON, run.ErrorMessage,
	).Scan(&run.ID, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create research run: %w", err)
	}
	return nil
}

// GetRun retrieves a research run by ID.
func (r *ResearchRepository) GetRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	var run domain.ResearchRun
	var planJSON, ctxJSON, constraintsJSON, budgetJSON, metricsJSON, findingsJSON, questionsJSON, structuredJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, playbook, objective, depth, status, plan, context_data,
			   constraints_list, budget_config, progress_metrics, findings,
			   unresolved_questions, report_markdown, report_structured, error_message,
			   created_at, updated_at, completed_at
		FROM research_runs WHERE user_id = $1 AND id = $2`,
		userID, runID,
	).Scan(
		&run.ID, &run.UserID, &run.Playbook, &run.Objective, &run.Depth, &run.Status,
		&planJSON, &ctxJSON, &constraintsJSON, &budgetJSON, &metricsJSON, &findingsJSON,
		&questionsJSON, &run.ReportMarkdown, &structuredJSON, &run.ErrorMessage,
		&run.CreatedAt, &run.UpdatedAt, &run.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get research run: %w", err)
	}

	_ = json.Unmarshal(planJSON, &run.Plan)
	_ = json.Unmarshal(ctxJSON, &run.ContextData)
	_ = json.Unmarshal(constraintsJSON, &run.ConstraintsList)
	_ = json.Unmarshal(budgetJSON, &run.BudgetConfig)
	_ = json.Unmarshal(metricsJSON, &run.ProgressMetrics)
	_ = json.Unmarshal(findingsJSON, &run.Findings)
	_ = json.Unmarshal(questionsJSON, &run.UnresolvedQuestions)
	_ = json.Unmarshal(structuredJSON, &run.ReportStructured)

	return &run, nil
}

// UpdateRun updates state, progress metrics, findings, and final reports of a run.
func (r *ResearchRepository) UpdateRun(ctx context.Context, run *domain.ResearchRun) error {
	planJSON, _ := json.Marshal(run.Plan)
	metricsJSON, _ := json.Marshal(run.ProgressMetrics)
	findingsJSON, _ := json.Marshal(run.Findings)
	questionsJSON, _ := json.Marshal(run.UnresolvedQuestions)
	structuredJSON, _ := json.Marshal(run.ReportStructured)

	tag, err := r.pool.Exec(ctx, `
		UPDATE research_runs SET
			status = $3,
			plan = $4,
			progress_metrics = $5,
			findings = $6,
			unresolved_questions = $7,
			report_markdown = $8,
			report_structured = $9,
			error_message = $10,
			completed_at = CASE WHEN $3 IN ('COMPLETED', 'CANCELLED', 'FAILED') THEN NOW() ELSE completed_at END,
			updated_at = NOW()
		WHERE user_id = $1 AND id = $2`,
		run.UserID, run.ID, run.Status, planJSON, metricsJSON, findingsJSON,
		questionsJSON, run.ReportMarkdown, structuredJSON, run.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("update research run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListRuns lists recent research runs for a user.
func (r *ResearchRepository) ListRuns(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, playbook, objective, depth, status, plan, context_data,
			   constraints_list, budget_config, progress_metrics, findings,
			   unresolved_questions, report_markdown, report_structured, error_message,
			   created_at, updated_at, completed_at
		FROM research_runs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list research runs: %w", err)
	}
	defer rows.Close()

	var runs []*domain.ResearchRun
	for rows.Next() {
		var run domain.ResearchRun
		var planJSON, ctxJSON, constraintsJSON, budgetJSON, metricsJSON, findingsJSON, questionsJSON, structuredJSON []byte

		if err := rows.Scan(
			&run.ID, &run.UserID, &run.Playbook, &run.Objective, &run.Depth, &run.Status,
			&planJSON, &ctxJSON, &constraintsJSON, &budgetJSON, &metricsJSON, &findingsJSON,
			&questionsJSON, &run.ReportMarkdown, &structuredJSON, &run.ErrorMessage,
			&run.CreatedAt, &run.UpdatedAt, &run.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan research run: %w", err)
		}

		_ = json.Unmarshal(planJSON, &run.Plan)
		_ = json.Unmarshal(ctxJSON, &run.ContextData)
		_ = json.Unmarshal(constraintsJSON, &run.ConstraintsList)
		_ = json.Unmarshal(budgetJSON, &run.BudgetConfig)
		_ = json.Unmarshal(metricsJSON, &run.ProgressMetrics)
		_ = json.Unmarshal(findingsJSON, &run.Findings)
		_ = json.Unmarshal(questionsJSON, &run.UnresolvedQuestions)
		_ = json.Unmarshal(structuredJSON, &run.ReportStructured)

		runs = append(runs, &run)
	}
	return runs, rows.Err()
}

// RecordStep writes one executed research step to the audit log.
func (r *ResearchRepository) RecordStep(ctx context.Context, step *domain.ResearchStep) error {
	inJSON, _ := json.Marshal(step.InputData)
	outJSON, _ := json.Marshal(step.OutputData)

	err := r.pool.QueryRow(ctx, `
		INSERT INTO research_steps (
			run_id, user_id, step_index, phase, action, input_data, output_data, duration_ms, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		) RETURNING id, created_at`,
		step.RunID, step.UserID, step.StepIndex, step.Phase, step.Action,
		inJSON, outJSON, step.DurationMs, step.Status,
	).Scan(&step.ID, &step.CreatedAt)
	if err != nil {
		return fmt.Errorf("record research step: %w", err)
	}
	return nil
}

// ListSteps returns the execution audit steps for a run.
func (r *ResearchRepository) ListSteps(ctx context.Context, userID, runID uuid.UUID) ([]*domain.ResearchStep, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, run_id, user_id, step_index, phase, action, input_data, output_data, duration_ms, status, created_at
		FROM research_steps WHERE user_id = $1 AND run_id = $2 ORDER BY step_index ASC`,
		userID, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list research steps: %w", err)
	}
	defer rows.Close()

	var steps []*domain.ResearchStep
	for rows.Next() {
		var s domain.ResearchStep
		var inJSON, outJSON []byte
		if err := rows.Scan(
			&s.ID, &s.RunID, &s.UserID, &s.StepIndex, &s.Phase, &s.Action,
			&inJSON, &outJSON, &s.DurationMs, &s.Status, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan research step: %w", err)
		}
		_ = json.Unmarshal(inJSON, &s.InputData)
		_ = json.Unmarshal(outJSON, &s.OutputData)
		steps = append(steps, &s)
	}
	return steps, rows.Err()
}

// AddSource records a research source URL.
func (r *ResearchRepository) AddSource(ctx context.Context, s *domain.ResearchSource) error {
	var hash *string
	if s.ContentHash != "" {
		hash = &s.ContentHash
	}
	var raw *string
	if s.RawText != "" {
		raw = &s.RawText
	}

	err := r.pool.QueryRow(ctx, `
		INSERT INTO research_sources (
			user_id, run_id, url, canonical_url, title, domain, source_type, reliability_tier, content_hash, raw_text
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		) RETURNING id, retrieved_at`,
		s.UserID, s.RunID, s.URL, s.CanonicalURL, s.Title, s.Domain, s.SourceType, s.ReliabilityTier, hash, raw,
	).Scan(&s.ID, &s.RetrievedAt)
	if err != nil {
		return fmt.Errorf("add research source: %w", err)
	}
	return nil
}

// AddEvidence records an atomic extracted claim traceable to a source.
func (r *ResearchRepository) AddEvidence(ctx context.Context, e *domain.EvidenceItem) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO evidence_items (
			user_id, run_id, source_id, entity_id, claim, extracted_value, excerpt_quote,
			source_url, source_domain, source_type, publication_date, reliability,
			confidence, status, extraction_method, model_used
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		) RETURNING id, retrieved_at, created_at`,
		e.UserID, e.RunID, e.SourceID, e.EntityID, e.Claim, e.ExtractedValue, e.ExcerptQuote,
		e.SourceURL, e.SourceDomain, e.SourceType, e.PublicationDate, e.Reliability,
		e.Confidence, e.Status, e.ExtractionMethod, e.ModelUsed,
	).Scan(&e.ID, &e.RetrievedAt, &e.CreatedAt)
	if err != nil {
		return fmt.Errorf("add evidence: %w", err)
	}
	return nil
}

// ListEvidenceForRun returns all evidence collected during a specific research run.
func (r *ResearchRepository) ListEvidenceForRun(ctx context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, run_id, source_id, entity_id, claim, extracted_value, excerpt_quote,
			   source_url, source_domain, source_type, publication_date, retrieved_at, verified_at,
			   reliability, confidence, status, extraction_method, COALESCE(model_used, ''), created_at
		FROM evidence_items WHERE user_id = $1 AND run_id = $2 ORDER BY created_at ASC`,
		userID, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list evidence for run: %w", err)
	}
	defer rows.Close()

	var items []*domain.EvidenceItem
	for rows.Next() {
		var e domain.EvidenceItem
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.RunID, &e.SourceID, &e.EntityID, &e.Claim, &e.ExtractedValue, &e.ExcerptQuote,
			&e.SourceURL, &e.SourceDomain, &e.SourceType, &e.PublicationDate, &e.RetrievedAt, &e.VerifiedAt,
			&e.Reliability, &e.Confidence, &e.Status, &e.ExtractionMethod, &e.ModelUsed, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan evidence item: %w", err)
		}
		items = append(items, &e)
	}
	return items, rows.Err()
}
