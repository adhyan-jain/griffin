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

// OutreachRepository stores campaigns and the people in them. Every write touches one row (or one campaign
// under a row lock), so agents working on the same campaign never overwrite each other.
type OutreachRepository struct{ pool database.Querier }

func NewOutreachRepository(pool database.Querier) *OutreachRepository {
	return &OutreachRepository{pool: pool}
}

const targetCols = `id, campaign_id, user_id, name, handle, info, status, draft, sent_at, replied_at, reply_text,
	next_followup_at, followups_sent, notes, created_at, updated_at`

func scanTarget(row pgx.Row) (*domain.OutreachTarget, error) {
	var t domain.OutreachTarget
	var status string
	if err := row.Scan(&t.ID, &t.CampaignID, &t.UserID, &t.Name, &t.Handle, &t.Info, &status, &t.Draft, &t.SentAt, &t.RepliedAt,
		&t.ReplyText, &t.NextFollowupAt, &t.FollowupsSent, &t.Notes, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Status = domain.OutreachStatus(status)
	return &t, nil
}

func scanCampaign(row pgx.Row) (*domain.OutreachCampaign, error) {
	var c domain.OutreachCampaign
	var limits []byte
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Goal, &c.Channel, &c.Status, &limits, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(limits, &c.Limits); err != nil {
		return nil, fmt.Errorf("campaign limits: %w", err)
	}
	return &c, nil
}

const campaignCols = `id, user_id, name, goal, channel, status, limits, created_at, updated_at`

func (r *OutreachRepository) CreateCampaign(ctx context.Context, c *domain.OutreachCampaign) error {
	if err := c.Limits.Validate(); err != nil {
		return err
	}
	limits, _ := json.Marshal(c.Limits)
	return r.pool.QueryRow(ctx, `INSERT INTO outreach_campaigns (user_id, name, goal, channel, limits) VALUES ($1,$2,$3,$4,$5)
		RETURNING id, status, created_at, updated_at`, c.UserID, c.Name, c.Goal, c.Channel, limits).Scan(&c.ID, &c.Status, &c.CreatedAt, &c.UpdatedAt)
}

func (r *OutreachRepository) ListCampaigns(ctx context.Context, userID uuid.UUID) ([]*domain.OutreachCampaign, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+campaignCols+` FROM outreach_campaigns WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.OutreachCampaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *OutreachRepository) GetCampaign(ctx context.Context, userID, id uuid.UUID) (*domain.OutreachCampaign, error) {
	c, err := scanCampaign(r.pool.QueryRow(ctx, `SELECT `+campaignCols+` FROM outreach_campaigns WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return c, err
}

// UpdateCampaign changes the status ("active", "paused", "done") and/or the limits.
func (r *OutreachRepository) UpdateCampaign(ctx context.Context, userID, id uuid.UUID, status *string, limits *domain.OutreachLimits) (*domain.OutreachCampaign, error) {
	var raw []byte
	if limits != nil {
		if err := limits.Validate(); err != nil {
			return nil, err
		}
		raw, _ = json.Marshal(limits)
	}
	c, err := scanCampaign(r.pool.QueryRow(ctx, `UPDATE outreach_campaigns SET status = COALESCE($3::text, status), limits = COALESCE($4, limits), updated_at = NOW()
		WHERE id = $1 AND user_id = $2 RETURNING `+campaignCols, id, userID, status, raw))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return c, err
}

// UpsertTargets adds people found by a researching agent. A person already in the campaign (same handle) keeps
// their status and draft: only blank name and info fields are filled in.
func (r *OutreachRepository) UpsertTargets(ctx context.Context, userID, campaignID uuid.UUID, in []domain.OutreachTarget) (added int, err error) {
	if _, err := r.GetCampaign(ctx, userID, campaignID); err != nil {
		return 0, err
	}
	for _, t := range in {
		if t.Handle == "" {
			return added, fmt.Errorf("%w: every person needs a handle (email, profile link or phone)", domain.ErrValidation)
		}
		var inserted bool
		if err := r.pool.QueryRow(ctx, `INSERT INTO outreach_targets (campaign_id, user_id, name, handle, info) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (campaign_id, handle) DO UPDATE SET
			  name = CASE WHEN outreach_targets.name = '' THEN EXCLUDED.name ELSE outreach_targets.name END,
			  info = CASE WHEN outreach_targets.info = '' THEN EXCLUDED.info ELSE outreach_targets.info END
			RETURNING (xmax = 0)`, campaignID, userID, t.Name, t.Handle, t.Info).Scan(&inserted); err != nil {
			return added, err
		}
		if inserted {
			added++
		}
	}
	return added, nil
}

// ListTargets returns a campaign's people, newest activity first. Before it does, anyone who has been quiet long
// enough is moved to followup_due, so a list is always current without a background job.
func (r *OutreachRepository) ListTargets(ctx context.Context, userID, campaignID uuid.UUID, status *domain.OutreachStatus) ([]*domain.OutreachTarget, error) {
	if _, err := r.pool.Exec(ctx, `UPDATE outreach_targets t SET status = 'followup_due', updated_at = NOW()
		FROM outreach_campaigns c
		WHERE t.campaign_id = c.id AND t.campaign_id = $1 AND t.user_id = $2 AND t.status = 'sent'
		  AND t.next_followup_at IS NOT NULL AND t.next_followup_at <= NOW()
		  AND t.followups_sent < COALESCE((c.limits->>'max_followups')::int, 0)`, campaignID, userID); err != nil {
		return nil, err
	}
	var st *string
	if status != nil {
		s := string(*status)
		st = &s
	}
	rows, err := r.pool.Query(ctx, `SELECT `+targetCols+` FROM outreach_targets WHERE campaign_id = $1 AND user_id = $2 AND ($3::text IS NULL OR status = $3)
		ORDER BY updated_at DESC LIMIT 2000`, campaignID, userID, st)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.OutreachTarget
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TargetUpdate is one change to one person. To is optional; when set the move must be legal.
type TargetUpdate struct {
	To             *domain.OutreachStatus
	Draft          *string
	Notes          *string
	ReplyText      *string
	NextFollowupAt *time.Time
	Event          string // recorded in the audit trail, e.g. "approved by owner"
}

// UpdateTarget applies an update to one person, refusing an illegal status move. The move is checked in the
// WHERE clause, so two agents racing for the same person cannot both win.
func (r *OutreachRepository) UpdateTarget(ctx context.Context, userID, id uuid.UUID, u TargetUpdate) (*domain.OutreachTarget, error) {
	var to *string
	var prev []string
	if u.To != nil {
		if !u.To.Valid() {
			return nil, fmt.Errorf("%w: unknown status %q", domain.ErrValidation, *u.To)
		}
		s := string(*u.To)
		to = &s
		for _, p := range domain.OutreachPrevious(*u.To) {
			prev = append(prev, string(p))
		}
	}
	t, err := scanTarget(r.pool.QueryRow(ctx, `UPDATE outreach_targets SET
		status = COALESCE($3::text, status),
		draft = COALESCE($4, draft),
		notes = COALESCE($5, notes),
		reply_text = COALESCE($6, reply_text),
		next_followup_at = COALESCE($7, next_followup_at),
		sent_at = CASE WHEN $3::text = 'sent' THEN NOW() ELSE sent_at END,
		replied_at = CASE WHEN $3::text = 'replied' THEN NOW() ELSE replied_at END,
		followups_sent = CASE WHEN $3::text = 'sent' AND sent_at IS NOT NULL THEN followups_sent + 1 ELSE followups_sent END,
		claimed_until = CASE WHEN $3::text IS NOT NULL THEN NULL ELSE claimed_until END,
		updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND ($3::text IS NULL OR status = ANY($8::text[]))
		RETURNING `+targetCols, id, userID, to, u.Draft, u.Notes, u.ReplyText, u.NextFollowupAt, prev))
	if errors.Is(err, pgx.ErrNoRows) {
		// Either it is not theirs or the move was not legal; say which, without leaking another owner's row.
		var exists bool
		_ = r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM outreach_targets WHERE id = $1 AND user_id = $2)`, id, userID).Scan(&exists)
		if !exists {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("%w: that person cannot move to %q from their current status", domain.ErrValidation, derefStatus(u.To))
	}
	if err != nil {
		return nil, err
	}
	if u.Event != "" {
		_, _ = r.pool.Exec(ctx, `INSERT INTO outreach_events (target_id, user_id, kind, detail) VALUES ($1,$2,$3,$4)`, id, userID, string(t.Status), u.Event)
	}
	return t, nil
}

func derefStatus(s *domain.OutreachStatus) string {
	if s == nil {
		return ""
	}
	return string(*s)
}

// RecordReplyByHandle marks the latest sent (or follow-up-due) person with this handle as replied. It is what a
// notification or an inbox check calls when it sees an answer. It returns nil when nobody matches.
func (r *OutreachRepository) RecordReplyByHandle(ctx context.Context, userID uuid.UUID, handle, text string) (*domain.OutreachTarget, error) {
	t, err := scanTarget(r.pool.QueryRow(ctx, `UPDATE outreach_targets SET status = 'replied', replied_at = NOW(), reply_text = $3, updated_at = NOW()
		WHERE id = (SELECT id FROM outreach_targets WHERE user_id = $1 AND lower(handle) = lower($2) AND status IN ('sent','followup_due')
		            ORDER BY sent_at DESC NULLS LAST LIMIT 1)
		RETURNING `+targetCols, userID, handle, text))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// Awaiting is a person who has been messaged and has not answered.
type Awaiting struct {
	Name   string `json:"name"`
	Handle string `json:"handle"`
}

// AwaitingReply lists the people the owner is waiting to hear from, so a phone can recognise their messages
// without sending every notification it sees to the server.
func (r *OutreachRepository) AwaitingReply(ctx context.Context, userID uuid.UUID) ([]Awaiting, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT name, handle FROM outreach_targets WHERE user_id = $1 AND status IN ('sent','followup_due') LIMIT 1000`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Awaiting{}
	for rows.Next() {
		var a Awaiting
		if err := rows.Scan(&a.Name, &a.Handle); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecordReplyByName is RecordReplyByHandle for a chat notification, which shows a contact's name and not their
// address. It only matches when exactly one awaited person has that name, so it never guesses between two.
func (r *OutreachRepository) RecordReplyByName(ctx context.Context, userID uuid.UUID, name, text string) (*domain.OutreachTarget, error) {
	t, err := scanTarget(r.pool.QueryRow(ctx, `UPDATE outreach_targets SET status = 'replied', replied_at = NOW(), reply_text = $3, updated_at = NOW()
		WHERE id = (SELECT id FROM outreach_targets WHERE user_id = $1 AND lower(name) = lower($2) AND status IN ('sent','followup_due'))
		  AND (SELECT COUNT(*) FROM outreach_targets WHERE user_id = $1 AND lower(name) = lower($2) AND status IN ('sent','followup_due')) = 1
		RETURNING `+targetCols, userID, name, text))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// SendClaim is the answer to "what should I send next?": a person to message now, or when to ask again.
type SendClaim struct {
	Target  *domain.OutreachTarget `json:"target,omitempty"`
	RetryAt *time.Time             `json:"retry_at,omitempty"`
	Reason  string                 `json:"reason,omitempty"`
}

// ClaimNextToSend hands a sender the next approved person while keeping the campaign inside its limits. The
// campaign row is locked for the check, so two senders cannot both pass the daily cap or the gap. The person is
// leased for five minutes; marking them sent (or any other move) releases the lease.
func (r *OutreachRepository) ClaimNextToSend(ctx context.Context, userID, campaignID uuid.UUID, now time.Time) (*SendClaim, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := scanCampaign(tx.QueryRow(ctx, `SELECT `+campaignCols+` FROM outreach_campaigns WHERE id = $1 AND user_id = $2 FOR UPDATE`, campaignID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Status != "active" {
		return &SendClaim{Reason: "campaign is " + c.Status}, nil
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var sentToday int
	var last *time.Time
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE sent_at >= $2 OR (claimed_until IS NOT NULL AND claimed_until > NOW())),
		GREATEST(MAX(sent_at), MAX(claimed_until) - interval '5 minutes') FROM outreach_targets WHERE campaign_id = $1`, campaignID, dayStart).Scan(&sentToday, &last); err != nil {
		return nil, err
	}
	if ok, at, why := c.Limits.NextSend(now, sentToday, last); !ok {
		return &SendClaim{RetryAt: &at, Reason: why}, nil
	}
	t, err := scanTarget(tx.QueryRow(ctx, `UPDATE outreach_targets SET claimed_until = NOW() + interval '5 minutes', updated_at = NOW()
		WHERE id = (SELECT id FROM outreach_targets WHERE campaign_id = $1 AND status = 'approved' AND (claimed_until IS NULL OR claimed_until < NOW())
		            ORDER BY updated_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING `+targetCols, campaignID))
	if errors.Is(err, pgx.ErrNoRows) {
		return &SendClaim{Reason: "nobody is approved to message right now"}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SendClaim{Target: t}, nil
}

// CampaignOf returns the campaign a person belongs to.
func (r *OutreachRepository) CampaignOf(ctx context.Context, userID, targetID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT campaign_id FROM outreach_targets WHERE id = $1 AND user_id = $2`, targetID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	return id, err
}

// Counts returns how many people are in each status.
func (r *OutreachRepository) Counts(ctx context.Context, userID, campaignID uuid.UUID) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT status, COUNT(*) FROM outreach_targets WHERE campaign_id = $1 AND user_id = $2 GROUP BY status`, campaignID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}
