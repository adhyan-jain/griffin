package domain

import (
	"time"

	"github.com/google/uuid"
)

// DiscoverySource represents an authorized discovery origin.
type DiscoverySource string

const (
	SourceGreenhouse DiscoverySource = "greenhouse"
	SourceLever      DiscoverySource = "lever"
	SourceAshby      DiscoverySource = "ashby"
	SourceLinkedIn   DiscoverySource = "linkedin"
	SourceJobAlert   DiscoverySource = "job_alert"
)

// DiscoveredJob represents a verified job posting discovered from an authorized source.
type DiscoveredJob struct {
	ID          uuid.UUID       `json:"id"`
	UserID      uuid.UUID       `json:"user_id"`
	Source      DiscoverySource `json:"source"`
	ExternalID  string          `json:"external_id"`
	Company     string          `json:"company"`
	Role        string          `json:"role"`
	URL         string          `json:"url"`
	Description string          `json:"description"`
	Location    string          `json:"location,omitempty"`
	Tier        string          `json:"tier"`   // Tier A, Standard
	Status      string          `json:"status"` // discovered, staged, applied, rejected
	TaskID      *uuid.UUID      `json:"task_id,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// DiscoveryFilter dictates criteria for relevant openings.
type DiscoveryFilter struct {
	Keywords   []string `json:"keywords"`
	Locations  []string `json:"locations"`
	InternOnly bool     `json:"intern_only"`
}
