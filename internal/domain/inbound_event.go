package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type EventCategory string

const (
	CategoryOpportunity   EventCategory = "opportunity"
	CategoryCalendarAlert EventCategory = "calendar_alert"
	CategoryUrgentAlert   EventCategory = "urgent_alert"
	CategoryChatReply     EventCategory = "chat_reply"
	CategoryInformational EventCategory = "informational"
	CategoryOther         EventCategory = "other"
)

// InboundEvent represents a triaged inbound communication, notification, or webhook signal.
type InboundEvent struct {
	ID           uuid.UUID       `json:"id"`
	UserID       uuid.UUID       `json:"user_id"`
	Source       string          `json:"source"`
	PackageName  string          `json:"package_name"`
	Category     EventCategory   `json:"category"`
	Title        string          `json:"title"`
	Text         string          `json:"text"`
	Actionable   bool            `json:"actionable"`
	TriageAction string          `json:"triage_action"`
	Payload      json.RawMessage `json:"payload"`
	CreatedAt    time.Time       `json:"created_at"`
}

// TriageResult holds the outcome of evaluating an inbound notification or event against operational heuristics.
type TriageResult struct {
	Category     EventCategory `json:"category"`
	Actionable   bool          `json:"actionable"`
	TriageAction string        `json:"triage_action"`
	Summary      string        `json:"summary"`
	Assessments  []string      `json:"assessments,omitempty"`
	Deadlines    []string      `json:"deadlines,omitempty"`
	Candidate    string        `json:"candidate,omitempty"`
	DraftReply   string        `json:"draft_reply,omitempty"`
}
