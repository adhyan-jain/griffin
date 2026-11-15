package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// DsaRepository stores the DSA coach's settings and problem list.
type DsaRepository struct{ pool database.Querier }

// NewDsaRepository constructs a DsaRepository.
func NewDsaRepository(pool database.Querier) *DsaRepository { return &DsaRepository{pool: pool} }

// Settings returns the owner's settings, creating the default row (which enrols them) when there is none.
func (r *DsaRepository) Settings(ctx context.Context, userID uuid.UUID) (domain.DsaSettings, error) {
	d := domain.DefaultDsaSettings()
	if _, err := r.pool.Exec(ctx, `INSERT INTO dsa_settings (user_id, daily_count, timezone, leetcode_username, nag_from_hour) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		userID, d.DailyCount, d.Timezone, d.LeetcodeUsername, d.NagFromHour); err != nil {
		return d, fmt.Errorf("enrol dsa: %w", err)
	}
	var s domain.DsaSettings
	err := r.pool.QueryRow(ctx, `SELECT daily_count, timezone, leetcode_username, nag_from_hour FROM dsa_settings WHERE user_id = $1`, userID).
		Scan(&s.DailyCount, &s.Timezone, &s.LeetcodeUsername, &s.NagFromHour)
	if err != nil {
		return d, fmt.Errorf("dsa settings: %w", err)
	}
	s.LeetcodeUsername = domain.CleanLeetCodeUsername(s.LeetcodeUsername)
	if s.LeetcodeUsername == "" {
		s.LeetcodeUsername = "AdhyanJain"
	}
	return s, nil
}

// SaveSettings replaces the owner's settings.
func (r *DsaRepository) SaveSettings(ctx context.Context, userID uuid.UUID, s domain.DsaSettings) error {
	s.LeetcodeUsername = domain.CleanLeetCodeUsername(s.LeetcodeUsername)
	if s.LeetcodeUsername == "" {
		s.LeetcodeUsername = "AdhyanJain"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO dsa_settings (user_id, daily_count, timezone, leetcode_username, nag_from_hour) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id) DO UPDATE SET daily_count=$2, timezone=$3, leetcode_username=$4, nag_from_hour=$5, updated_at=NOW()`,
		userID, s.DailyCount, s.Timezone, s.LeetcodeUsername, s.NagFromHour)
	if err != nil {
		return fmt.Errorf("save dsa settings: %w", err)
	}
	return nil
}

// Users lists everyone enrolled in the coach. System scope only (the nag loop).
func (r *DsaRepository) Users(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id FROM dsa_settings`)
	if err != nil {
		return nil, fmt.Errorf("dsa users: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Items returns the owner's whole list, adding any seed problem they do not have yet.
func (r *DsaRepository) Items(ctx context.Context, userID uuid.UUID) ([]*domain.DsaItem, error) {
	for i, s := range domain.DsaSeed {
		if _, err := r.pool.Exec(ctx, `INSERT INTO dsa_items (user_id, slug, title, topic, difficulty, ord) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			userID, s.Slug, s.Title, s.Topic, s.Difficulty, i); err != nil {
			return nil, fmt.Errorf("seed dsa: %w", err)
		}
	}
	rows, err := r.pool.Query(ctx, `SELECT slug, title, topic, difficulty, ord, state, assigned_on, due_on, attempts, solved_at FROM dsa_items WHERE user_id = $1 ORDER BY ord`, userID)
	if err != nil {
		return nil, fmt.Errorf("list dsa: %w", err)
	}
	defer rows.Close()
	var out []*domain.DsaItem
	for rows.Next() {
		it := &domain.DsaItem{}
		var assigned, due *time.Time
		if err := rows.Scan(&it.Slug, &it.Title, &it.Topic, &it.Difficulty, &it.Ord, &it.State, &assigned, &due, &it.Attempts, &it.SolvedAt); err != nil {
			return nil, fmt.Errorf("scan dsa: %w", err)
		}
		it.AssignedOn, it.DueOn = utcDay(assigned), utcDay(due)
		out = append(out, it)
	}
	return out, rows.Err()
}

func utcDay(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	y, m, d := t.Date()
	v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &v
}

// SaveItems writes back the state of the given problems.
func (r *DsaRepository) SaveItems(ctx context.Context, userID uuid.UUID, items []*domain.DsaItem) error {
	for _, it := range items {
		if _, err := r.pool.Exec(ctx, `UPDATE dsa_items SET state=$3, assigned_on=$4, due_on=$5, attempts=$6, solved_at=$7 WHERE user_id=$1 AND slug=$2`,
			userID, it.Slug, it.State, it.AssignedOn, it.DueOn, it.Attempts, it.SolvedAt); err != nil {
			return fmt.Errorf("save dsa item: %w", err)
		}
	}
	return nil
}
