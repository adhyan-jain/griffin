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

// ReportRepository reads across tasks for the scorecard and a single task's audit trail. Read-only.
type ReportRepository struct{ pool database.Querier }

// NewReportRepository constructs a ReportRepository.
func NewReportRepository(pool database.Querier) *ReportRepository {
	return &ReportRepository{pool: pool}
}

// ScoreRows returns the owner's finished tasks since the cutoff, with how many approvals each needed.
// Chat turns are excluded: they are conversation, not work.
func (r *ReportRepository) ScoreRows(ctx context.Context, userID uuid.UUID, since time.Time) ([]domain.ScoreRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.status, t.output_payload, COALESCE(t.failure_reason, ''),
		       COUNT(a.id)::int, COUNT(a.id) FILTER (WHERE a.status = 'REJECTED')::int
		FROM tasks t
		LEFT JOIN approvals a ON a.task_id = t.id
		WHERE t.user_id = $1 AND t.status IN ('COMPLETED','FAILED','CANCELLED')
		  AND COALESCE(t.completed_at, t.updated_at) >= $2
		  AND COALESCE(t.input_payload->>'kind', '') <> 'chat'
		GROUP BY t.id`, userID, since)
	if err != nil {
		return nil, fmt.Errorf("score rows: %w", err)
	}
	defer rows.Close()
	var out []domain.ScoreRow
	for rows.Next() {
		var s domain.ScoreRow
		var output []byte
		if err := rows.Scan(&s.Status, &output, &s.FailureReason, &s.Approvals, &s.Rejected); err != nil {
			return nil, fmt.Errorf("scan score row: %w", err)
		}
		s.Output = json.RawMessage(output)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Audit returns the replayable story of one task of the owner's, or ErrNotFound.
func (r *ReportRepository) Audit(ctx context.Context, userID, taskID uuid.UUID) ([]domain.AuditEntry, error) {
	var t domain.Task
	err := r.pool.QueryRow(ctx, `SELECT id, goal, status, failure_reason, completed_at, created_at FROM tasks WHERE id = $1 AND user_id = $2`,
		taskID, userID).Scan(&t.ID, &t.Goal, &t.Status, &t.FailureReason, &t.CompletedAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("audit task: %w", err)
	}

	var events []*domain.TaskEvent
	erows, err := r.pool.Query(ctx, `SELECT id, event_type, message, created_at FROM task_events WHERE task_id = $1 ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("audit events: %w", err)
	}
	for erows.Next() {
		e := &domain.TaskEvent{TaskID: taskID}
		if err := erows.Scan(&e.ID, &e.EventType, &e.Message, &e.CreatedAt); err != nil {
			erows.Close()
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, e)
	}
	erows.Close()

	var attempts []*domain.TaskAttempt
	arows, err := r.pool.Query(ctx, `SELECT attempt_number, worker_id, status, error_message, started_at FROM task_attempts WHERE task_id = $1 ORDER BY attempt_number`, taskID)
	if err != nil {
		return nil, fmt.Errorf("audit attempts: %w", err)
	}
	for arows.Next() {
		a := &domain.TaskAttempt{TaskID: taskID}
		if err := arows.Scan(&a.AttemptNumber, &a.WorkerID, &a.Status, &a.ErrorMessage, &a.StartedAt); err != nil {
			arows.Close()
			return nil, fmt.Errorf("scan audit attempt: %w", err)
		}
		attempts = append(attempts, a)
	}
	arows.Close()

	var approvals []*domain.Approval
	prows, err := r.pool.Query(ctx, `SELECT id, action_type, summary, status, decision_reason, decided_at, created_at FROM approvals WHERE task_id = $1 AND user_id = $2 ORDER BY created_at`, taskID, userID)
	if err != nil {
		return nil, fmt.Errorf("audit approvals: %w", err)
	}
	for prows.Next() {
		a := &domain.Approval{TaskID: taskID}
		if err := prows.Scan(&a.ID, &a.ActionType, &a.Summary, &a.Status, &a.DecisionReason, &a.DecidedAt, &a.CreatedAt); err != nil {
			prows.Close()
			return nil, fmt.Errorf("scan audit approval: %w", err)
		}
		approvals = append(approvals, a)
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, err
	}
	return domain.BuildAudit(&t, events, attempts, approvals), nil
}

// BriefSource gathers what the morning brief is built from, except the DSA problems (the coach owns those).
func (r *ReportRepository) BriefSource(ctx context.Context, userID uuid.UUID, now time.Time) (domain.BriefSource, error) {
	src := domain.BriefSource{Now: now}
	lines := func(query string, args []any, scan func(pgx.Rows) (string, error)) ([]string, error) {
		rows, err := r.pool.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			s, err := scan(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, rows.Err()
	}
	var err error
	if src.PendingApprovals, err = lines(`SELECT action_type, summary FROM approvals WHERE user_id = $1 AND status = 'PENDING' ORDER BY created_at LIMIT 20`, []any{userID},
		func(rs pgx.Rows) (string, error) {
			var kind, sum string
			err := rs.Scan(&kind, &sum)
			return kind + " - " + sum, err
		}); err != nil {
		return src, fmt.Errorf("brief approvals: %w", err)
	}
	if src.FollowUpsDue, err = lines(`SELECT COALESCE(NULLIF(name,''), handle) FROM outreach_targets WHERE user_id = $1 AND status = 'sent' AND next_followup_at <= $2 ORDER BY next_followup_at LIMIT 20`, []any{userID, now},
		func(rs pgx.Rows) (string, error) { var s string; return s, rs.Scan(&s) }); err != nil {
		return src, fmt.Errorf("brief follow-ups: %w", err)
	}
	if src.FailedTasks, err = lines(`SELECT title FROM tasks WHERE user_id = $1 AND status = 'FAILED' AND COALESCE(completed_at, updated_at) >= $2 AND COALESCE(input_payload->>'kind','') <> 'chat' ORDER BY completed_at DESC LIMIT 10`, []any{userID, now.Add(-24 * time.Hour)},
		func(rs pgx.Rows) (string, error) { var s string; return s, rs.Scan(&s) }); err != nil {
		return src, fmt.Errorf("brief failed tasks: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT name, handle, replied_at, reply_text FROM outreach_targets WHERE user_id = $1 AND status = 'replied' ORDER BY replied_at DESC LIMIT 20`, userID)
	if err != nil {
		return src, fmt.Errorf("brief replies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var br domain.BriefReply
		var at *time.Time
		var text string
		if err := rows.Scan(&br.Name, &br.Handle, &at, &text); err != nil {
			return src, fmt.Errorf("scan brief reply: %w", err)
		}
		if at != nil {
			br.RepliedAt = *at
		}
		src.Replies = append(src.Replies, domain.AnalyzeReply(br, text))
	}
	return src, rows.Err()
}
