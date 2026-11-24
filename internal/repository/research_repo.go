package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// researchRunColumns is the column list every run read uses, in scan order.
const researchRunColumns = `id, user_id, playbook, objective, depth, status, plan, context_data,
	constraints_list, budget_config, progress_metrics, findings,
	unresolved_questions, report_markdown, report_structured, error_message,
	round, pending_queries, executed_queries, pending_urls, seen_urls, stop_reason, next_step_index, revision,
	created_at, updated_at, completed_at`

// runJSON is a run's JSON columns, marshalled once so every write reports a marshalling error.
type runJSON struct {
	plan, ctx, constraints, budget, metrics, findings, questions, structured []byte
	pendingQ, executedQ, pendingURLs, seenURLs                               []byte
}

func marshalRun(run *domain.ResearchRun) (runJSON, error) {
	var j runJSON
	var err error
	for _, f := range []struct {
		dst *[]byte
		v   any
		nm  string
	}{
		{&j.plan, run.Plan, "plan"},
		{&j.ctx, orEmptyMap(run.ContextData), "context_data"},
		{&j.constraints, orEmptyList(run.ConstraintsList), "constraints_list"},
		{&j.budget, orEmptyIntMap(run.BudgetConfig), "budget_config"},
		{&j.metrics, orEmptyIntMap(run.ProgressMetrics), "progress_metrics"},
		{&j.findings, orEmptyFindings(run.Findings), "findings"},
		{&j.questions, orEmptyList(run.UnresolvedQuestions), "unresolved_questions"},
		{&j.structured, orEmptyMap(run.ReportStructured), "report_structured"},
		{&j.pendingQ, orEmptyList(run.PendingQueries), "pending_queries"},
		{&j.executedQ, orEmptyList(run.ExecutedQueries), "executed_queries"},
		{&j.pendingURLs, orEmptyList(run.PendingURLs), "pending_urls"},
		{&j.seenURLs, orEmptyList(run.SeenURLs), "seen_urls"},
	} {
		if *f.dst, err = json.Marshal(f.v); err != nil {
			return j, fmt.Errorf("marshal research run %s: %w", f.nm, err)
		}
	}
	return j, nil
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func orEmptyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

func orEmptyList(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func orEmptyFindings(f []domain.ResearchFinding) []domain.ResearchFinding {
	if f == nil {
		return []domain.ResearchFinding{}
	}
	return f
}

// scanRun reads one run row selected with researchRunColumns.
func scanRun(row pgx.Row) (*domain.ResearchRun, error) {
	var run domain.ResearchRun
	var plan, ctxJ, constraints, budget, metrics, findings, questions, structured, pendingQ, executedQ, pendingURLs, seenURLs []byte
	if err := row.Scan(
		&run.ID, &run.UserID, &run.Playbook, &run.Objective, &run.Depth, &run.Status,
		&plan, &ctxJ, &constraints, &budget, &metrics, &findings,
		&questions, &run.ReportMarkdown, &structured, &run.ErrorMessage,
		&run.Round, &pendingQ, &executedQ, &pendingURLs, &seenURLs, &run.StopReason, &run.NextStepIndex, &run.Revision,
		&run.CreatedAt, &run.UpdatedAt, &run.CompletedAt,
	); err != nil {
		return nil, err
	}
	for _, f := range []struct {
		raw []byte
		dst any
		nm  string
	}{
		{plan, &run.Plan, "plan"}, {ctxJ, &run.ContextData, "context_data"},
		{constraints, &run.ConstraintsList, "constraints_list"}, {budget, &run.BudgetConfig, "budget_config"},
		{metrics, &run.ProgressMetrics, "progress_metrics"}, {findings, &run.Findings, "findings"},
		{questions, &run.UnresolvedQuestions, "unresolved_questions"}, {structured, &run.ReportStructured, "report_structured"},
		{pendingQ, &run.PendingQueries, "pending_queries"}, {executedQ, &run.ExecutedQueries, "executed_queries"},
		{pendingURLs, &run.PendingURLs, "pending_urls"}, {seenURLs, &run.SeenURLs, "seen_urls"},
	} {
		if len(f.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(f.raw, f.dst); err != nil {
			return nil, fmt.Errorf("decode research run %s: %w", f.nm, err)
		}
	}
	return &run, nil
}

// CreateRun initializes a new research run.
func (r *ResearchRepository) CreateRun(ctx context.Context, run *domain.ResearchRun) error {
	j, err := marshalRun(run)
	if err != nil {
		return err
	}
	if run.NextStepIndex < 1 {
		run.NextStepIndex = 1
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO research_runs (
			user_id, playbook, objective, depth, status, plan, context_data,
			constraints_list, budget_config, progress_metrics, findings,
			unresolved_questions, report_markdown, report_structured, error_message,
			round, pending_queries, executed_queries, pending_urls, seen_urls, stop_reason, next_step_index
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22
		) RETURNING id, created_at, updated_at`,
		run.UserID, run.Playbook, run.Objective, run.Depth, run.Status,
		j.plan, j.ctx, j.constraints, j.budget, j.metrics, j.findings,
		j.questions, run.ReportMarkdown, j.structured, run.ErrorMessage,
		run.Round, j.pendingQ, j.executedQ, j.pendingURLs, j.seenURLs, run.StopReason, run.NextStepIndex,
	).Scan(&run.ID, &run.CreatedAt, &run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create research run: %w", err)
	}
	return nil
}

// GetRun retrieves a research run by ID.
func (r *ResearchRepository) GetRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	run, err := scanRun(r.pool.QueryRow(ctx,
		`SELECT `+researchRunColumns+` FROM research_runs WHERE user_id = $1 AND id = $2`, userID, runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get research run: %w", err)
	}
	return run, nil
}

// UpdateRun writes the run's state, but only while the run is still active and nobody else has written it since
// it was read (the revision matches). A run that finished, was paused or cancelled, or was advanced by another
// worker is left as it is and ErrResearchRunInactive is returned, so a late writer can never undo a pause or a
// cancel, or fork the run.
func (r *ResearchRepository) UpdateRun(ctx context.Context, run *domain.ResearchRun) error {
	j, err := marshalRun(run)
	if err != nil {
		return err
	}
	var rev int64
	var updated time.Time
	err = r.pool.QueryRow(ctx, `
		UPDATE research_runs SET
			status = $4,
			plan = $5,
			context_data = $6,
			constraints_list = $7,
			budget_config = $8,
			progress_metrics = $9,
			findings = $10,
			unresolved_questions = $11,
			report_markdown = $12,
			report_structured = $13,
			error_message = $14,
			round = $15,
			pending_queries = $16,
			executed_queries = $17,
			pending_urls = $18,
			seen_urls = $19,
			stop_reason = $20,
			next_step_index = $21,
			revision = revision + 1,
			completed_at = CASE WHEN $4 IN ('COMPLETED', 'CANCELLED', 'FAILED') THEN COALESCE(completed_at, NOW()) ELSE completed_at END,
			updated_at = NOW()
		WHERE user_id = $1 AND id = $2 AND revision = $3
		  AND status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED', 'PAUSED')
		RETURNING revision, updated_at`,
		run.UserID, run.ID, run.Revision, run.Status, j.plan, j.ctx, j.constraints, j.budget, j.metrics,
		j.findings, j.questions, run.ReportMarkdown, j.structured, run.ErrorMessage,
		run.Round, j.pendingQ, j.executedQ, j.pendingURLs, j.seenURLs, run.StopReason, run.NextStepIndex,
	).Scan(&rev, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if qerr := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM research_runs WHERE user_id = $1 AND id = $2)`,
			run.UserID, run.ID).Scan(&exists); qerr == nil && !exists {
			return domain.ErrNotFound
		}
		return domain.ErrResearchRunInactive
	}
	if err != nil {
		return fmt.Errorf("update research run: %w", err)
	}
	run.Revision, run.UpdatedAt = rev, updated
	return nil
}

// TransitionRun moves a run to a new status only if its current status is one of from (a compare-and-set), and
// records the stop reason and error message when given. It reports whether the run was changed. Pause, resume,
// cancel and fail use it, so they act on the run's real state instead of on a copy that may be stale.
func (r *ResearchRepository) TransitionRun(ctx context.Context, userID, runID uuid.UUID, to domain.ResearchPhase,
	from []domain.ResearchPhase, stopReason string, errMsg *string) (bool, error) {
	fromStr := make([]string, len(from))
	for i, f := range from {
		fromStr[i] = string(f)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE research_runs SET
			status = $3,
			stop_reason = CASE WHEN $5 <> '' THEN $5 ELSE stop_reason END,
			error_message = COALESCE($6, error_message),
			revision = revision + 1,
			completed_at = CASE WHEN $3 IN ('COMPLETED', 'CANCELLED', 'FAILED') THEN COALESCE(completed_at, NOW()) ELSE completed_at END,
			updated_at = NOW()
		WHERE user_id = $1 AND id = $2 AND status = ANY($4::text[])`,
		userID, runID, string(to), fromStr, stopReason, errMsg)
	if err != nil {
		return false, fmt.Errorf("transition research run: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListRuns lists recent research runs for a user.
func (r *ResearchRepository) ListRuns(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+researchRunColumns+` FROM research_runs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`,
		userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list research runs: %w", err)
	}
	defer rows.Close()

	var runs []*domain.ResearchRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan research run: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// RecordStep writes a research step, once per (run, step_index): a repeat updates the row while it is not done,
// and returns ErrResearchStepDone once it is, so a retry or a second worker cannot duplicate or undo a step.
func (r *ResearchRepository) RecordStep(ctx context.Context, step *domain.ResearchStep) error {
	inJSON, err := json.Marshal(orEmptyMap(step.InputData))
	if err != nil {
		return fmt.Errorf("marshal step input: %w", err)
	}
	outJSON, err := json.Marshal(orEmptyMap(step.OutputData))
	if err != nil {
		return fmt.Errorf("marshal step output: %w", err)
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO research_steps (
			run_id, user_id, step_index, phase, action, input_data, output_data, duration_ms, status
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		) ON CONFLICT (run_id, step_index) DO UPDATE SET
			phase = EXCLUDED.phase, action = EXCLUDED.action, input_data = EXCLUDED.input_data,
			output_data = EXCLUDED.output_data, duration_ms = EXCLUDED.duration_ms, status = EXCLUDED.status
		WHERE research_steps.status NOT IN ('done', 'completed') AND research_steps.user_id = EXCLUDED.user_id
		RETURNING id, created_at`,
		step.RunID, step.UserID, step.StepIndex, step.Phase, step.Action,
		inJSON, outJSON, step.DurationMs, step.Status,
	).Scan(&step.ID, &step.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrResearchStepDone
	}
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
		if err := json.Unmarshal(inJSON, &s.InputData); err != nil {
			return nil, fmt.Errorf("decode research step input: %w", err)
		}
		if err := json.Unmarshal(outJSON, &s.OutputData); err != nil {
			return nil, fmt.Errorf("decode research step output: %w", err)
		}
		steps = append(steps, &s)
	}
	return steps, rows.Err()
}

// AddSource records a fetched page, once per (run, canonical URL): fetching the same page again refreshes its
// hash, title and retrieved_at and returns the same row. The run must belong to the same user.
func (r *ResearchRepository) AddSource(ctx context.Context, s *domain.ResearchSource) error {
	var hash, raw *string
	if s.ContentHash != "" {
		hash = &s.ContentHash
	}
	if s.RawText != "" {
		raw = &s.RawText
	}
	var retrieved *time.Time
	if !s.RetrievedAt.IsZero() {
		retrieved = &s.RetrievedAt
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO research_sources (
			user_id, run_id, url, canonical_url, title, domain, source_type, reliability_tier, content_hash, raw_text, retrieved_at
		) SELECT $1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11::timestamptz, NOW())
		WHERE $2::uuid IS NULL OR EXISTS (SELECT 1 FROM research_runs WHERE id = $2::uuid AND user_id = $1::uuid)
		ON CONFLICT (run_id, canonical_url) DO UPDATE SET
			title = EXCLUDED.title, content_hash = EXCLUDED.content_hash,
			raw_text = COALESCE(EXCLUDED.raw_text, research_sources.raw_text), retrieved_at = EXCLUDED.retrieved_at
		RETURNING id, retrieved_at`,
		s.UserID, s.RunID, s.URL, s.CanonicalURL, s.Title, s.Domain, s.SourceType, s.ReliabilityTier, hash, raw, retrieved,
	).Scan(&s.ID, &s.RetrievedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: the research run does not belong to the user", domain.ErrValidation)
	}
	if err != nil {
		return fmt.Errorf("add research source: %w", err)
	}
	return nil
}

// AddEvidence records an atomic extracted claim traceable to a source. The run, source and entity it names must
// belong to the same user. With a DedupeKey the same claim from the same page is stored once per run.
func (r *ResearchRepository) AddEvidence(ctx context.Context, e *domain.EvidenceItem) error {
	var key *string
	if e.DedupeKey != "" {
		key = &e.DedupeKey
	}
	var retrieved *time.Time
	if !e.RetrievedAt.IsZero() {
		retrieved = &e.RetrievedAt
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO evidence_items (
			user_id, run_id, source_id, entity_id, claim, extracted_value, excerpt_quote,
			source_url, source_domain, source_type, publication_date, retrieved_at, verified_at, reliability,
			confidence, status, extraction_method, model_used, dedupe_key
		) SELECT $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11::timestamptz,
			COALESCE($12::timestamptz, NOW()), $13::timestamptz, $14, $15, $16, $17, $18, $19
		WHERE ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM research_runs WHERE id = $2::uuid AND user_id = $1::uuid))
		  AND ($3::uuid IS NULL OR EXISTS (SELECT 1 FROM research_sources WHERE id = $3::uuid AND user_id = $1::uuid))
		  AND ($4::uuid IS NULL OR EXISTS (SELECT 1 FROM canonical_entities WHERE id = $4::uuid AND user_id = $1::uuid))
		ON CONFLICT (run_id, dedupe_key) DO UPDATE SET dedupe_key = EXCLUDED.dedupe_key
		RETURNING id, retrieved_at, created_at, status`,
		e.UserID, e.RunID, e.SourceID, e.EntityID, e.Claim, e.ExtractedValue, e.ExcerptQuote,
		e.SourceURL, e.SourceDomain, e.SourceType, e.PublicationDate, retrieved, e.VerifiedAt, e.Reliability,
		e.Confidence, e.Status, e.ExtractionMethod, nilIfEmpty(e.ModelUsed), key,
	).Scan(&e.ID, &e.RetrievedAt, &e.CreatedAt, &e.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: the run, source or entity does not belong to the user", domain.ErrValidation)
	}
	if err != nil {
		return fmt.Errorf("add evidence: %w", err)
	}
	return nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SetEvidenceStatus changes the verification status of one evidence item (used to mark conflicts).
func (r *ResearchRepository) SetEvidenceStatus(ctx context.Context, userID, evidenceID uuid.UUID, status domain.EvidenceStatus) error {
	tag, err := r.pool.Exec(ctx, `UPDATE evidence_items SET status = $3 WHERE user_id = $1 AND id = $2`,
		userID, evidenceID, string(status))
	if err != nil {
		return fmt.Errorf("set evidence status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListEvidenceForRun returns all evidence collected during a specific research run.
func (r *ResearchRepository) ListEvidenceForRun(ctx context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, run_id, source_id, entity_id, claim, extracted_value, excerpt_quote,
			   source_url, source_domain, source_type, publication_date, retrieved_at, verified_at,
			   reliability, confidence, status, extraction_method, COALESCE(model_used, ''), created_at
		FROM evidence_items WHERE user_id = $1 AND run_id = $2 ORDER BY created_at ASC, id ASC`,
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
