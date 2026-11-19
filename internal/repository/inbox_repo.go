package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// InboxRepository persists and queries OAuth tokens, synchronized email threads, and assessments.
type InboxRepository struct {
	pool database.Querier
}

// NewInboxRepository constructs an InboxRepository.
func NewInboxRepository(pool database.Querier) *InboxRepository {
	return &InboxRepository{pool: pool}
}

// UpsertOAuthCredential stores or updates access/refresh credentials for a provider.
func (r *InboxRepository) UpsertOAuthCredential(ctx context.Context, cred *domain.OAuthCredential) error {
	query := `
		INSERT INTO oauth_credentials (user_id, provider, email, access_token, refresh_token, token_type, expires_at, scope, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		ON CONFLICT (user_id, provider, email)
		DO UPDATE SET
			access_token = EXCLUDED.access_token,
			refresh_token = CASE WHEN EXCLUDED.refresh_token <> '' THEN EXCLUDED.refresh_token ELSE oauth_credentials.refresh_token END,
			token_type = EXCLUDED.token_type,
			expires_at = EXCLUDED.expires_at,
			scope = EXCLUDED.scope,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`
	return r.pool.QueryRow(ctx, query,
		cred.UserID,
		cred.Provider,
		cred.Email,
		cred.AccessToken,
		cred.RefreshToken,
		cred.TokenType,
		cred.ExpiresAt,
		cred.Scope,
	).Scan(&cred.ID, &cred.CreatedAt, &cred.UpdatedAt)
}

// GetOAuthCredential retrieves the credentials for a user and provider.
func (r *InboxRepository) GetOAuthCredential(ctx context.Context, userID uuid.UUID, provider string) (*domain.OAuthCredential, error) {
	query := `
		SELECT id, user_id, provider, email, access_token, refresh_token, token_type, expires_at, scope, created_at, updated_at
		FROM oauth_credentials
		WHERE user_id = $1 AND provider = $2
		ORDER BY updated_at DESC
		LIMIT 1`
	cred := &domain.OAuthCredential{}
	err := r.pool.QueryRow(ctx, query, userID, provider).Scan(
		&cred.ID,
		&cred.UserID,
		&cred.Provider,
		&cred.Email,
		&cred.AccessToken,
		&cred.RefreshToken,
		&cred.TokenType,
		&cred.ExpiresAt,
		&cred.Scope,
		&cred.CreatedAt,
		&cred.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get oauth credential: %w", err)
	}
	return cred, nil
}

// StoreEmailMessage persists a synchronized message into email_threads.
func (r *InboxRepository) StoreEmailMessage(ctx context.Context, userID uuid.UUID, msg *domain.EmailMessage) error {
	query := `
		INSERT INTO email_threads (user_id, thread_id, message_id, sender, recipient, subject, snippet, body_text, has_attachments, received_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (user_id, message_id)
		DO UPDATE SET
			snippet = EXCLUDED.snippet,
			body_text = EXCLUDED.body_text,
			has_attachments = EXCLUDED.has_attachments`
	hasAttach := len(msg.Attachments) > 0
	_, err := r.pool.Exec(ctx, query,
		userID,
		msg.ThreadID,
		msg.ID,
		msg.Sender,
		msg.Recipient,
		msg.Subject,
		msg.Snippet,
		msg.BodyText,
		hasAttach,
		msg.ReceivedAt,
	)
	if err != nil {
		return fmt.Errorf("store email message: %w", err)
	}
	return nil
}

// CreateAssessment stores an extracted recruiter technical assessment.
func (r *InboxRepository) CreateAssessment(ctx context.Context, a *domain.AssessmentRecord) error {
	query := `
		INSERT INTO assessments (user_id, company, platform, role, test_url, deadline, status, inbound_event_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, created_at, updated_at`
	return r.pool.QueryRow(ctx, query,
		a.UserID,
		a.Company,
		a.Platform,
		a.Role,
		a.TestURL,
		a.Deadline,
		string(a.Status),
		a.InboundEventID,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

// ListActiveAssessments returns pending assessments due at or before the given deadline cutoff.
func (r *InboxRepository) ListActiveAssessments(ctx context.Context, userID uuid.UUID, dueBefore *time.Time) ([]*domain.AssessmentRecord, error) {
	query := `
		SELECT id, user_id, company, platform, role, test_url, deadline, status, inbound_event_id, created_at, updated_at
		FROM assessments
		WHERE user_id = $1 AND status = 'pending'
		ORDER BY deadline ASC NULLS LAST
		LIMIT 50`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list active assessments: %w", err)
	}
	defer rows.Close()

	var out []*domain.AssessmentRecord
	for rows.Next() {
		a := &domain.AssessmentRecord{}
		var status string
		if err := rows.Scan(
			&a.ID,
			&a.UserID,
			&a.Company,
			&a.Platform,
			&a.Role,
			&a.TestURL,
			&a.Deadline,
			&status,
			&a.InboundEventID,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan assessment: %w", err)
		}
		a.Status = domain.AssessmentStatus(status)
		if dueBefore != nil && a.Deadline != nil && a.Deadline.After(*dueBefore) {
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
