package domain

import (
	"time"

	"github.com/google/uuid"
)

// EvidenceStatus represents the verification status of an evidence item.
type EvidenceStatus string

const (
	EvidenceDiscovered    EvidenceStatus = "discovered"
	EvidenceInspected     EvidenceStatus = "inspected"
	EvidenceSupported     EvidenceStatus = "supported"
	EvidenceCrossVerified EvidenceStatus = "cross_verified"
	EvidenceConflicting   EvidenceStatus = "conflicting"
	EvidenceOutdated      EvidenceStatus = "outdated"
	EvidenceUnverified    EvidenceStatus = "unverified"
	EvidenceRejected      EvidenceStatus = "rejected"
)

// SourceType classifies the origin type of retrieved content.
type SourceType string

const (
	SourceOfficialATS          SourceType = "official_ats"
	SourceOfficialCareerPage   SourceType = "official_career_page"
	SourceCompanyAnnouncement  SourceType = "company_announcement"
	SourceGovernmentRegulatory SourceType = "government_regulatory"
	SourceAcceleratorDirectory SourceType = "accelerator_directory"
	SourceCrediblePublication  SourceType = "credible_publication"
	SourcePublicProfile        SourceType = "public_profile"
	SourceCommunityDiscussion  SourceType = "community_discussion"
	SourceUnverifiedWeb        SourceType = "unverified_web"
	SourceInternalDB           SourceType = "internal_db"
)

// ReliabilityTier represents the authoritative weight of a source.
type ReliabilityTier string

const (
	ReliabilityPrimaryOfficial   ReliabilityTier = "primary_official"
	ReliabilityHighAuthority     ReliabilityTier = "high_authority"
	ReliabilitySecondaryCredible ReliabilityTier = "secondary_credible"
	ReliabilityCommunityUser     ReliabilityTier = "community_user"
	ReliabilityUnverified        ReliabilityTier = "unverified"
)

// ResearchSource is a tracked URL or document visited during research.
type ResearchSource struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	RunID           *uuid.UUID      `json:"run_id,omitempty"`
	URL             string          `json:"url"`
	CanonicalURL    string          `json:"canonical_url"`
	Title           string          `json:"title"`
	Domain          string          `json:"domain"`
	SourceType      SourceType      `json:"source_type"`
	ReliabilityTier ReliabilityTier `json:"reliability_tier"`
	ContentHash     string          `json:"content_hash,omitempty"`
	RawText         string          `json:"raw_text,omitempty"`
	RetrievedAt     time.Time       `json:"retrieved_at"`
}

// EvidenceItem is an atomic, verified claim traceable to a specific source excerpt.
type EvidenceItem struct {
	ID               uuid.UUID       `json:"id"`
	UserID           uuid.UUID       `json:"user_id"`
	RunID            *uuid.UUID      `json:"run_id,omitempty"`
	SourceID         *uuid.UUID      `json:"source_id,omitempty"`
	EntityID         *uuid.UUID      `json:"entity_id,omitempty"`
	Claim            string          `json:"claim"`
	ExtractedValue   string          `json:"extracted_value,omitempty"`
	ExcerptQuote     string          `json:"excerpt_quote"`
	SourceURL        string          `json:"source_url"`
	SourceDomain     string          `json:"source_domain"`
	SourceType       SourceType      `json:"source_type"`
	PublicationDate  *time.Time      `json:"publication_date,omitempty"`
	RetrievedAt      time.Time       `json:"retrieved_at"`
	VerifiedAt       *time.Time      `json:"verified_at,omitempty"`
	Reliability      ReliabilityTier `json:"reliability"`
	Confidence       int             `json:"confidence"` // 0-100
	Status           EvidenceStatus  `json:"status"`
	ExtractionMethod string          `json:"extraction_method"` // rule, model, deterministic
	ModelUsed        string          `json:"model_used,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}
