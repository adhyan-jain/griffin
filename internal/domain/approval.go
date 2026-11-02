package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "PENDING"
	ApprovalStatusApproved ApprovalStatus = "APPROVED"
	ApprovalStatusRejected ApprovalStatus = "REJECTED"
	ApprovalStatusExpired  ApprovalStatus = "EXPIRED"
)

type ApprovalRiskLevel string

const (
	ApprovalRiskLow      ApprovalRiskLevel = "LOW"
	ApprovalRiskMedium   ApprovalRiskLevel = "MEDIUM"
	ApprovalRiskHigh     ApprovalRiskLevel = "HIGH"
	ApprovalRiskCritical ApprovalRiskLevel = "CRITICAL"
)

type Approval struct {
	ID             uuid.UUID         `json:"id"`
	UserID         uuid.UUID         `json:"user_id"`
	TaskID         uuid.UUID         `json:"task_id"`
	ActionType     string            `json:"action_type"`
	RiskLevel      ApprovalRiskLevel `json:"risk_level"`
	Summary        string            `json:"summary"`
	ActionPayload  json.RawMessage   `json:"action_payload"`
	Status         ApprovalStatus    `json:"status"`
	DecidedBy      *uuid.UUID        `json:"decided_by,omitempty"`
	DecisionReason *string           `json:"decision_reason,omitempty"`
	DecidedAt      *time.Time        `json:"decided_at,omitempty"`
	ExpiresAt      *time.Time        `json:"expires_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}
