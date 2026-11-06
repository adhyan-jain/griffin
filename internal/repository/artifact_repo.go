package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/griffin/core/internal/database"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/domain"
)

// ArtifactRepository defines the persistence contract for artifacts.
type ArtifactRepository interface {
	CreateArtifact(ctx context.Context, artifact *domain.Artifact) (*domain.Artifact, error)
	GetArtifactByID(ctx context.Context, id uuid.UUID) (*domain.Artifact, error)
	GetArtifactByIDAndUser(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Artifact, error)
	GetArtifactByObjectKey(ctx context.Context, bucket, objectKey string) (*domain.Artifact, error)
	ListArtifacts(ctx context.Context, userID uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error)
	DeleteArtifact(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	// UpdateArtifactContentInfo records the new size and checksum after an artifact's object was rewritten.
	UpdateArtifactContentInfo(ctx context.Context, id uuid.UUID, userID uuid.UUID, size int64, checksum string) (*domain.Artifact, error)
}

// PostgresArtifactRepository implements ArtifactRepository.
type PostgresArtifactRepository struct {
	pool database.Querier
}

func NewPostgresArtifactRepository(pool database.Querier) *PostgresArtifactRepository {
	return &PostgresArtifactRepository{pool: pool}
}

func (r *PostgresArtifactRepository) CreateArtifact(ctx context.Context, a *domain.Artifact) (*domain.Artifact, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	if a.StorageProvider == "" {
		a.StorageProvider = "S3"
	}
	if a.Metadata == nil {
		a.Metadata = json.RawMessage("{}")
	}

	query := `
		INSERT INTO artifacts (
			id, user_id, task_id, title, artifact_type, storage_uri,
			storage_provider, bucket, object_key, filename, file_size_bytes,
			mime_type, checksum, metadata, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		) RETURNING id, user_id, task_id, title, artifact_type, storage_uri,
		            storage_provider, bucket, object_key, filename, file_size_bytes,
		            mime_type, checksum, metadata, created_at
	`
	row := r.pool.QueryRow(ctx, query,
		a.ID, a.UserID, a.TaskID, a.Title, a.ArtifactType, a.StorageURI,
		a.StorageProvider, a.Bucket, a.ObjectKey, a.Filename, a.FileSizeBytes,
		a.MimeType, a.Checksum, a.Metadata, a.CreatedAt,
	)

	var created domain.Artifact
	err := row.Scan(
		&created.ID, &created.UserID, &created.TaskID, &created.Title, &created.ArtifactType,
		&created.StorageURI, &created.StorageProvider, &created.Bucket, &created.ObjectKey,
		&created.Filename, &created.FileSizeBytes, &created.MimeType, &created.Checksum,
		&created.Metadata, &created.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	return &created, nil
}

func (r *PostgresArtifactRepository) GetArtifactByID(ctx context.Context, id uuid.UUID) (*domain.Artifact, error) {
	query := `
		SELECT id, user_id, task_id, title, artifact_type, storage_uri,
		       storage_provider, bucket, object_key, filename, file_size_bytes,
		       mime_type, checksum, metadata, created_at
		FROM artifacts
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var a domain.Artifact
	err := row.Scan(
		&a.ID, &a.UserID, &a.TaskID, &a.Title, &a.ArtifactType,
		&a.StorageURI, &a.StorageProvider, &a.Bucket, &a.ObjectKey, &a.Filename,
		&a.FileSizeBytes, &a.MimeType, &a.Checksum, &a.Metadata, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get artifact by id: %w", err)
	}
	return &a, nil
}

func (r *PostgresArtifactRepository) GetArtifactByIDAndUser(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Artifact, error) {
	query := `
		SELECT id, user_id, task_id, title, artifact_type, storage_uri,
		       storage_provider, bucket, object_key, filename, file_size_bytes,
		       mime_type, checksum, metadata, created_at
		FROM artifacts
		WHERE id = $1 AND user_id = $2
	`
	row := r.pool.QueryRow(ctx, query, id, userID)
	var a domain.Artifact
	err := row.Scan(
		&a.ID, &a.UserID, &a.TaskID, &a.Title, &a.ArtifactType,
		&a.StorageURI, &a.StorageProvider, &a.Bucket, &a.ObjectKey, &a.Filename,
		&a.FileSizeBytes, &a.MimeType, &a.Checksum, &a.Metadata, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get artifact by id and user: %w", err)
	}
	return &a, nil
}

func (r *PostgresArtifactRepository) GetArtifactByObjectKey(ctx context.Context, bucket, objectKey string) (*domain.Artifact, error) {
	query := `
		SELECT id, user_id, task_id, title, artifact_type, storage_uri,
		       storage_provider, bucket, object_key, filename, file_size_bytes,
		       mime_type, checksum, metadata, created_at
		FROM artifacts
		WHERE bucket = $1 AND object_key = $2
	`
	row := r.pool.QueryRow(ctx, query, bucket, objectKey)
	var a domain.Artifact
	err := row.Scan(
		&a.ID, &a.UserID, &a.TaskID, &a.Title, &a.ArtifactType,
		&a.StorageURI, &a.StorageProvider, &a.Bucket, &a.ObjectKey, &a.Filename,
		&a.FileSizeBytes, &a.MimeType, &a.Checksum, &a.Metadata, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get artifact by object key: %w", err)
	}
	return &a, nil
}

func (r *PostgresArtifactRepository) ListArtifacts(ctx context.Context, userID uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error) {
	query := `
		SELECT id, user_id, task_id, title, artifact_type, storage_uri,
		       storage_provider, bucket, object_key, filename, file_size_bytes,
		       mime_type, checksum, metadata, created_at
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
			&a.StorageURI, &a.StorageProvider, &a.Bucket, &a.ObjectKey, &a.Filename,
			&a.FileSizeBytes, &a.MimeType, &a.Checksum, &a.Metadata, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, &a)
	}
	return list, nil
}

func (r *PostgresArtifactRepository) DeleteArtifact(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return fmt.Errorf("%w: user id is required for artifact deletion", domain.ErrUnauthorized)
	}
	query := `DELETE FROM artifacts WHERE id = $1 AND user_id = $2`
	tag, err := r.pool.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresArtifactRepository) UpdateArtifactContentInfo(ctx context.Context, id uuid.UUID, userID uuid.UUID, size int64, checksum string) (*domain.Artifact, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: user id is required", domain.ErrUnauthorized)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE artifacts SET file_size_bytes = $3, checksum = $4 WHERE id = $1 AND user_id = $2`, id, userID, size, checksum)
	if err != nil {
		return nil, fmt.Errorf("update artifact content info: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}
	return r.GetArtifactByIDAndUser(ctx, id, userID)
}
