package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// EntityKind classifies canonical knowledge entities.
type EntityKind string

const (
	EntityCompany     EntityKind = "company"
	EntityPerson      EntityKind = "person"
	EntityJob         EntityKind = "job"
	EntityInstitution EntityKind = "institution"
	EntityTechnology  EntityKind = "technology"
	EntityTopic       EntityKind = "topic"
)

// RelationshipPredicate represents directed graph relationships between entities.
type RelationshipPredicate string

const (
	RelWorksAt        RelationshipPredicate = "works_at"
	RelStudiedAt      RelationshipPredicate = "studied_at"
	RelOperatesIn     RelationshipPredicate = "operates_in"
	RelHasJob         RelationshipPredicate = "has_job"
	RelCompetesWith   RelationshipPredicate = "competes_with"
	RelDiscoveredBy   RelationshipPredicate = "discovered_by"
	RelUsesTechnology RelationshipPredicate = "uses_technology"
	RelFundedBy       RelationshipPredicate = "funded_by"
	RelFoundedBy      RelationshipPredicate = "founded_by"
)

// CanonicalEntity is a deduplicated, canonical identity in the knowledge graph.
type CanonicalEntity struct {
	ID           uuid.UUID      `json:"id"`
	UserID       uuid.UUID      `json:"user_id"`
	Kind         EntityKind     `json:"kind"`
	CanonicalKey string         `json:"canonical_key"` // e.g., company domain, person canonical id, tech slug
	Name         string         `json:"name"`
	Summary      string         `json:"summary"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// EntityAlias records alternative representations or names for a canonical entity.
type EntityAlias struct {
	ID        uuid.UUID `json:"id"`
	EntityID  uuid.UUID `json:"entity_id"`
	UserID    uuid.UUID `json:"user_id"`
	Alias     string    `json:"alias"`
	Kind      string    `json:"kind"` // name, domain, handle, ticker, legal_name
	CreatedAt time.Time `json:"created_at"`
}

// EntityRelationship models a directed edge between two canonical entities with supporting evidence.
type EntityRelationship struct {
	ID         uuid.UUID             `json:"id"`
	UserID     uuid.UUID             `json:"user_id"`
	SubjectID  uuid.UUID             `json:"subject_id"`
	Predicate  RelationshipPredicate `json:"predicate"`
	ObjectID   uuid.UUID             `json:"object_id"`
	EvidenceID *uuid.UUID            `json:"evidence_id,omitempty"`
	Confidence int                   `json:"confidence"` // 0-100
	ValidFrom  *time.Time            `json:"valid_from,omitempty"`
	ValidTo    *time.Time            `json:"valid_to,omitempty"`
	Metadata   map[string]any        `json:"metadata"`
	CreatedAt  time.Time             `json:"created_at"`
}

// EntityObservation stores a temporal, historical fact or observation for an entity.
type EntityObservation struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	EntityID   uuid.UUID  `json:"entity_id"`
	Property   string     `json:"property"`
	Value      string     `json:"value"`
	SourceID   *uuid.UUID `json:"source_id,omitempty"`
	EvidenceID *uuid.UUID `json:"evidence_id,omitempty"`
	ObservedAt time.Time  `json:"observed_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// NormalizeEntityKey creates a normalized canonical lookup key for an entity.
func NormalizeEntityKey(kind EntityKind, raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch kind {
	case EntityCompany:
		// Strip protocols and www if a URL was passed
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		s = strings.TrimPrefix(s, "www.")
		s = strings.TrimRight(s, "/")
		if idx := strings.Index(s, "/"); idx != -1 {
			s = s[:idx]
		}
		return s
	default:
		return strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' {
				return r
			}
			if r == ' ' {
				return '-'
			}
			return -1
		}, s)
	}
}
