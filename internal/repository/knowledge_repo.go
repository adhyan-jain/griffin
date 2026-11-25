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

// KnowledgeRepository manages canonical entities, aliases, relationships and observations.
type KnowledgeRepository struct {
	pool database.Querier
}

// NewKnowledgeRepository creates a new KnowledgeRepository.
func NewKnowledgeRepository(pool database.Querier) *KnowledgeRepository {
	return &KnowledgeRepository{pool: pool}
}

// UpsertEntity stores or updates a canonical entity keyed by (user_id, kind, canonical_key).
func (r *KnowledgeRepository) UpsertEntity(ctx context.Context, e *domain.CanonicalEntity) error {
	metaJSON, _ := json.Marshal(e.Metadata)

	err := r.pool.QueryRow(ctx, `
		INSERT INTO canonical_entities (user_id, kind, canonical_key, name, summary, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, kind, canonical_key) DO UPDATE SET
			name = EXCLUDED.name,
			summary = CASE WHEN EXCLUDED.summary <> '' THEN EXCLUDED.summary ELSE canonical_entities.summary END,
			metadata = canonical_entities.metadata || EXCLUDED.metadata,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`,
		e.UserID, e.Kind, e.CanonicalKey, e.Name, e.Summary, metaJSON,
	).Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert canonical entity: %w", err)
	}
	return nil
}

// GetEntity retrieves an entity by its ID.
func (r *KnowledgeRepository) GetEntity(ctx context.Context, userID, id uuid.UUID) (*domain.CanonicalEntity, error) {
	var e domain.CanonicalEntity
	var metaJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, kind, canonical_key, name, summary, metadata, created_at, updated_at
		FROM canonical_entities WHERE user_id = $1 AND id = $2`,
		userID, id,
	).Scan(&e.ID, &e.UserID, &e.Kind, &e.CanonicalKey, &e.Name, &e.Summary, &metaJSON, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get canonical entity: %w", err)
	}
	_ = json.Unmarshal(metaJSON, &e.Metadata)
	return &e, nil
}

// FindEntityByKey retrieves an entity by its canonical key.
func (r *KnowledgeRepository) FindEntityByKey(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, key string) (*domain.CanonicalEntity, error) {
	var e domain.CanonicalEntity
	var metaJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, kind, canonical_key, name, summary, metadata, created_at, updated_at
		FROM canonical_entities WHERE user_id = $1 AND kind = $2 AND canonical_key = $3`,
		userID, kind, key,
	).Scan(&e.ID, &e.UserID, &e.Kind, &e.CanonicalKey, &e.Name, &e.Summary, &metaJSON, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find entity by key: %w", err)
	}
	_ = json.Unmarshal(metaJSON, &e.Metadata)
	return &e, nil
}

// ListEntities lists entities of a given kind (or all if kind is empty).
func (r *KnowledgeRepository) ListEntities(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, limit int) ([]*domain.CanonicalEntity, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, user_id, kind, canonical_key, name, summary, metadata, created_at, updated_at FROM canonical_entities WHERE user_id = $1`
	args := []any{userID}
	if kind != "" {
		q += ` AND kind = $2 ORDER BY updated_at DESC LIMIT $3`
		args = append(args, kind, limit)
	} else {
		q += ` ORDER BY updated_at DESC LIMIT $2`
		args = append(args, limit)
	}

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()

	var entities []*domain.CanonicalEntity
	for rows.Next() {
		var e domain.CanonicalEntity
		var metaJSON []byte
		if err := rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.CanonicalKey, &e.Name, &e.Summary, &metaJSON, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		_ = json.Unmarshal(metaJSON, &e.Metadata)
		entities = append(entities, &e)
	}
	return entities, rows.Err()
}

// AddAlias records an alternative name or identifier for an entity.
func (r *KnowledgeRepository) AddAlias(ctx context.Context, a *domain.EntityAlias) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO entity_aliases (entity_id, user_id, alias, kind)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (entity_id, alias) DO NOTHING
		RETURNING id, created_at`,
		a.EntityID, a.UserID, a.Alias, a.Kind,
	).Scan(&a.ID, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already exists
	}
	if err != nil {
		return fmt.Errorf("add alias: %w", err)
	}
	return nil
}

// FindEntityByAlias finds an entity matching an alias.
func (r *KnowledgeRepository) FindEntityByAlias(ctx context.Context, userID uuid.UUID, alias string) (*domain.CanonicalEntity, error) {
	var e domain.CanonicalEntity
	var metaJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT e.id, e.user_id, e.kind, e.canonical_key, e.name, e.summary, e.metadata, e.created_at, e.updated_at
		FROM canonical_entities e
		JOIN entity_aliases a ON e.id = a.entity_id
		WHERE a.user_id = $1 AND LOWER(a.alias) = LOWER($2)
		LIMIT 1`,
		userID, alias,
	).Scan(&e.ID, &e.UserID, &e.Kind, &e.CanonicalKey, &e.Name, &e.Summary, &metaJSON, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find entity by alias: %w", err)
	}
	_ = json.Unmarshal(metaJSON, &e.Metadata)
	return &e, nil
}

// UpsertRelationship creates or updates a directed graph edge between two entities.
func (r *KnowledgeRepository) UpsertRelationship(ctx context.Context, rel *domain.EntityRelationship) error {
	metaJSON, _ := json.Marshal(rel.Metadata)

	err := r.pool.QueryRow(ctx, `
		INSERT INTO entity_relationships (
			user_id, subject_id, predicate, object_id, evidence_id, confidence, valid_from, valid_to, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		) ON CONFLICT (user_id, subject_id, predicate, object_id) DO UPDATE SET
			evidence_id = COALESCE(EXCLUDED.evidence_id, entity_relationships.evidence_id),
			confidence = EXCLUDED.confidence,
			valid_from = COALESCE(EXCLUDED.valid_from, entity_relationships.valid_from),
			valid_to = COALESCE(EXCLUDED.valid_to, entity_relationships.valid_to),
			metadata = entity_relationships.metadata || EXCLUDED.metadata
		RETURNING id, created_at`,
		rel.UserID, rel.SubjectID, rel.Predicate, rel.ObjectID, rel.EvidenceID,
		rel.Confidence, rel.ValidFrom, rel.ValidTo, metaJSON,
	).Scan(&rel.ID, &rel.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert relationship: %w", err)
	}
	return nil
}

// ListRelationships returns all edges connected to an entity (as subject or object).
func (r *KnowledgeRepository) ListRelationships(ctx context.Context, userID, entityID uuid.UUID) ([]*domain.EntityRelationship, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, subject_id, predicate, object_id, evidence_id, confidence, valid_from, valid_to, metadata, created_at
		FROM entity_relationships
		WHERE user_id = $1 AND (subject_id = $2 OR object_id = $2)
		ORDER BY created_at DESC`,
		userID, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}
	defer rows.Close()

	var rels []*domain.EntityRelationship
	for rows.Next() {
		var rel domain.EntityRelationship
		var metaJSON []byte
		if err := rows.Scan(
			&rel.ID, &rel.UserID, &rel.SubjectID, &rel.Predicate, &rel.ObjectID,
			&rel.EvidenceID, &rel.Confidence, &rel.ValidFrom, &rel.ValidTo, &metaJSON, &rel.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan relationship: %w", err)
		}
		_ = json.Unmarshal(metaJSON, &rel.Metadata)
		rels = append(rels, &rel)
	}
	return rels, rows.Err()
}

// RecordObservation stores an observed property value for an entity.
func (r *KnowledgeRepository) RecordObservation(ctx context.Context, obs *domain.EntityObservation) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO entity_observations (user_id, entity_id, property, value, source_id, evidence_id, observed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		obs.UserID, obs.EntityID, obs.Property, obs.Value, obs.SourceID, obs.EvidenceID, obs.ObservedAt,
	).Scan(&obs.ID, &obs.CreatedAt)
	if err != nil {
		return fmt.Errorf("record observation: %w", err)
	}
	return nil
}

// ListObservations returns the history of observations for an entity.
func (r *KnowledgeRepository) ListObservations(ctx context.Context, userID, entityID uuid.UUID) ([]*domain.EntityObservation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, entity_id, property, value, source_id, evidence_id, observed_at, created_at
		FROM entity_observations
		WHERE user_id = $1 AND entity_id = $2
		ORDER BY observed_at DESC, id DESC`,
		userID, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	defer rows.Close()

	var list []*domain.EntityObservation
	for rows.Next() {
		var obs domain.EntityObservation
		if err := rows.Scan(
			&obs.ID, &obs.UserID, &obs.EntityID, &obs.Property, &obs.Value,
			&obs.SourceID, &obs.EvidenceID, &obs.ObservedAt, &obs.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		list = append(list, &obs)
	}
	return list, rows.Err()
}
