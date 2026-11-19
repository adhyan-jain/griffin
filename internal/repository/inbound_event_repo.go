package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// InboundEventRepository persists and reads inbound notifications and events.
type InboundEventRepository struct {
	pool database.Querier
}

// NewInboundEventRepository constructs an InboundEventRepository.
func NewInboundEventRepository(pool database.Querier) *InboundEventRepository {
	return &InboundEventRepository{pool: pool}
}

// Create inserts a new inbound event record into inbound_events.
func (r *InboundEventRepository) Create(ctx context.Context, ev *domain.InboundEvent) error {
	payloadBytes := ev.Payload
	if len(payloadBytes) == 0 {
		payloadBytes = []byte("{}")
	}
	query := `
		INSERT INTO inbound_events (user_id, source, package_name, category, title, text, actionable, triage_action, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE(NULLIF($10, '0001-01-01 00:00:00+00'::timestamptz), NOW()))
		RETURNING id, created_at`
	err := r.pool.QueryRow(ctx, query,
		ev.UserID,
		ev.Source,
		ev.PackageName,
		string(ev.Category),
		ev.Title,
		ev.Text,
		ev.Actionable,
		ev.TriageAction,
		payloadBytes,
		ev.CreatedAt,
	).Scan(&ev.ID, &ev.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert inbound event: %w", err)
	}
	return nil
}

// List returns the latest inbound events for a user up to limit.
func (r *InboundEventRepository) List(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.InboundEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `
		SELECT id, user_id, source, package_name, category, title, text, actionable, triage_action, payload, created_at
		FROM inbound_events
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list inbound events: %w", err)
	}
	defer rows.Close()

	var out []*domain.InboundEvent
	for rows.Next() {
		ev := &domain.InboundEvent{}
		var cat string
		var rawPayload []byte
		if err := rows.Scan(
			&ev.ID,
			&ev.UserID,
			&ev.Source,
			&ev.PackageName,
			&cat,
			&ev.Title,
			&ev.Text,
			&ev.Actionable,
			&ev.TriageAction,
			&rawPayload,
			&ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan inbound event: %w", err)
		}
		ev.Category = domain.EventCategory(cat)
		ev.Payload = json.RawMessage(rawPayload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ListActionable returns only actionable events for a user.
func (r *InboundEventRepository) ListActionable(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.InboundEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := `
		SELECT id, user_id, source, package_name, category, title, text, actionable, triage_action, payload, created_at
		FROM inbound_events
		WHERE user_id = $1 AND actionable = true
		ORDER BY created_at DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list actionable inbound events: %w", err)
	}
	defer rows.Close()

	var out []*domain.InboundEvent
	for rows.Next() {
		ev := &domain.InboundEvent{}
		var cat string
		var rawPayload []byte
		if err := rows.Scan(
			&ev.ID,
			&ev.UserID,
			&ev.Source,
			&ev.PackageName,
			&cat,
			&ev.Title,
			&ev.Text,
			&ev.Actionable,
			&ev.TriageAction,
			&rawPayload,
			&ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan actionable inbound event: %w", err)
		}
		ev.Category = domain.EventCategory(cat)
		ev.Payload = json.RawMessage(rawPayload)
		out = append(out, ev)
	}
	return out, rows.Err()
}
