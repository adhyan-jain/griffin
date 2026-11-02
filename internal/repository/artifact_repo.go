package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/domain"
)

// ArtifactRepository defines the persistence contract for artifacts.
type ArtifactRepository interface {
	CreateArtifact(ctx context.Context, artifact *domain.Artifact) (*domain.Artifact, error)
	GetArtifactByID(ctx context.Context, id uuid.UUID) (*domain.Artifact, error)
	ListArtifacts(ctx context.Context, userID uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error)
}

// PostgresArtifactRepository implements ArtifactRepository.
type PostgresArtifactRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresArtifactRepository(pool *pgxpool.Pool) *PostgresArtifactRepository {
	return &PostgresArtifactRepository{pool: pool}
}

func (r *PostgresArtifactRepository) CreateArtifact(ctx context.Context, a *domain.Artifact) (*domain.Artifact, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	if a.Metadata == nil {
		a.Metadata = json.RawMessage("{}")
	}

	query := `
		INSERT INTO artifacts (
			id, user_id, task_id, title, artifact_type, storage_uri, file_size_bytes, mime_type, metadata, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		) RETURNING id, user_id, task_id, title, artifact_type, storage_uri, file_size_bytes, mime_type, metadata, created_at
	`
	row := r.pool.QueryRow(ctx, query,
		a.ID, a.UserID, a.TaskID, a.Title, a.ArtifactType, a.StorageURI, a.FileSizeBytes, a.MimeType, a.Metadata, a.CreatedAt,
	)

	var created domain.Artifact
	err := row.Scan(
		&created.ID, &created.UserID, &created.TaskID, &created.Title, &created.ArtifactType,
		&created.StorageURI, &created.FileSizeBytes, &created.MimeType, &created.Metadata, &created.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	return &created, nil
}

func (r *PostgresArtifactRepository) GetArtifactByID(ctx context.Context, id uuid.UUID) (*domain.Artifact, error) {
	query := `
		SELECT id, user_id, task_id, title, artifact_type, storage_uri, file_size_bytes, mime_type, metadata, created_at
		FROM artifacts
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var a domain.Artifact
	err := row.Scan(
		&a.ID, &a.UserID, &a.TaskID, &a.Title, &a.ArtifactType,
		&a.StorageURI, &a.FileSizeBytes, &a.MimeType, &a.Metadata, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get artifact by id: %w", err)
	}
	return &a, nil
}

func (r *PostgresArtifactRepository) ListArtifacts(ctx context.Context, userID uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error) {
	query := `
		SELECT id, user_id, task_id, title, artifact_type, storage_uri, file_size_bytes, mime_type, metadata, created_at
		FROM artifacts
		WHERE user_id = $1 AND ($2::uuid IS NULL OR task_id = $2)
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID, taskID)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()

	var list []*domain.Artifact
	for rows.Next() {
		var a domain.Artifact
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.TaskID, &a.Title, &a.ArtifactType,
			&a.StorageURI, &a.FileSizeBytes, &a.MimeType, &a.Metadata, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, &a)
	}
	return list, nil
}
