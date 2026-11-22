package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// NotificationRepository is the notification outbox.
type NotificationRepository interface {
	// Record writes n once. It reports false, and changes nothing, when a row with the same owner and
	// DedupeKey already exists: the event was already announced.
	Record(ctx context.Context, n *domain.Notification) (bool, error)
	// After lists the owner's rows with seq greater than after, oldest first.
	After(ctx context.Context, userID uuid.UUID, after int64, limit int) ([]*domain.Notification, error)
	// LatestSeq is the owner's highest seq, or 0 when there are none.
	LatestSeq(ctx context.Context, userID uuid.UUID) (int64, error)
	// Ack marks rows as delivered to the owner.
	Ack(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error
}

// PostgresNotificationRepository implements NotificationRepository.
type PostgresNotificationRepository struct{ pool database.Querier }

// NewPostgresNotificationRepository constructs a PostgresNotificationRepository.
func NewPostgresNotificationRepository(pool database.Querier) *PostgresNotificationRepository {
	return &PostgresNotificationRepository{pool: pool}
}

func (r *PostgresNotificationRepository) Record(ctx context.Context, n *domain.Notification) (bool, error) {
	if n.DedupeKey == "" {
		return false, errors.New("record notification: a dedupe key is required")
	}
	priority := n.Priority
	if priority == "" {
		priority = domain.NotificationPriorityNormal
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, title, body, channel, priority, action_url, kind, entity_id, dedupe_key)
		VALUES ($1, $2, $3, 'IN_APP', $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		RETURNING id, seq, created_at`,
		n.UserID, clipText(n.Title, 255), n.Body, priority, n.ActionURL, n.Kind, n.EntityID, n.DedupeKey,
	).Scan(&n.ID, &n.Seq, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record notification: %w", err)
	}
	n.Priority = priority
	return true, nil
}

func (r *PostgresNotificationRepository) After(ctx context.Context, userID uuid.UUID, after int64, limit int) ([]*domain.Notification, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, seq, kind, entity_id, title, body, priority, COALESCE(action_url, ''), created_at, acked_at
		FROM notifications
		WHERE user_id = $1 AND seq > $2 AND dedupe_key IS NOT NULL
		ORDER BY seq ASC
		LIMIT $3`, userID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	out := []*domain.Notification{}
	for rows.Next() {
		n := &domain.Notification{UserID: userID}
		if err := rows.Scan(&n.ID, &n.Seq, &n.Kind, &n.EntityID, &n.Title, &n.Body, &n.Priority, &n.ActionURL, &n.CreatedAt, &n.AckedAt); err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *PostgresNotificationRepository) LatestSeq(ctx context.Context, userID uuid.UUID) (int64, error) {
	var seq int64
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM notifications WHERE user_id = $1`, userID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("latest notification: %w", err)
	}
	return seq, nil
}

func (r *PostgresNotificationRepository) Ack(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	// Sent as text and cast: the production pooler uses the simple protocol, which cannot encode a uuid array.
	texts := make([]string, len(ids))
	for i, id := range ids {
		texts[i] = id.String()
	}
	if _, err := r.pool.Exec(ctx, `UPDATE notifications SET acked_at = NOW(), read_at = COALESCE(read_at, NOW()) WHERE user_id = $1 AND id = ANY($2::uuid[]) AND acked_at IS NULL`, userID, texts); err != nil {
		return fmt.Errorf("ack notifications: %w", err)
	}
	return nil
}

func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
