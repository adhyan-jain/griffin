package domain

import (
	"time"

	"github.com/google/uuid"
)

// AssessmentStatus tracks the lifecycle of an online technical assessment.
type AssessmentStatus string

const (
	AssessmentPending   AssessmentStatus = "pending"
	AssessmentCompleted AssessmentStatus = "completed"
	AssessmentExpired   AssessmentStatus = "expired"
)

// EmailAttachment describes a file attachment on an email message.
type EmailAttachment struct {
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	Data     []byte `json:"-"`
}

// EmailMessage is a single email turn or message in a thread.
type EmailMessage struct {
	ID          string            `json:"id"`
	ThreadID    string            `json:"thread_id"`
	Sender      string            `json:"sender"`
	Recipient   string            `json:"recipient"`
	Subject     string            `json:"subject"`
	Snippet     string            `json:"snippet"`
	BodyText    string            `json:"body_text"`
	Attachments []EmailAttachment `json:"attachments,omitempty"`
	ReceivedAt  time.Time         `json:"received_at"`
}

// EmailThread groups email messages with conversation understanding.
type EmailThread struct {
	ID          uuid.UUID      `json:"id"`
	UserID      uuid.UUID      `json:"user_id"`
	ThreadID    string         `json:"thread_id"`
	Subject     string         `json:"subject"`
	Messages    []EmailMessage `json:"messages"`
	LastMessage time.Time      `json:"last_message"`
}

// AssessmentRecord represents a recruiter-dispatched technical assessment.
type AssessmentRecord struct {
	ID             uuid.UUID        `json:"id"`
	UserID         uuid.UUID        `json:"user_id"`
	Company        string           `json:"company"`
	Platform       string           `json:"platform"` // codility, hackerrank, codesignal, karat, take_home
	Role           string           `json:"role,omitempty"`
	TestURL        string           `json:"test_url"`
	Deadline       *time.Time       `json:"deadline,omitempty"`
	Status         AssessmentStatus `json:"status"`
	InboundEventID *uuid.UUID       `json:"inbound_event_id,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// OAuthCredential holds access and refresh credentials for an external provider.
type OAuthCredential struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	Provider     string     `json:"provider"` // google, linkedin
	Email        string     `json:"email"`
	AccessToken  string     `json:"-"`
	RefreshToken string     `json:"-"`
	TokenType    string     `json:"token_type"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Scope        string     `json:"scope"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// SyncState tracks incremental synchronization markers (e.g. Gmail historyId).
type SyncState struct {
	UserID        uuid.UUID `json:"user_id"`
	Provider      string    `json:"provider"`
	LastSyncAt    time.Time `json:"last_sync_at"`
	HistoryID     string    `json:"history_id,omitempty"`
	NextPageToken string    `json:"next_page_token,omitempty"`
}
