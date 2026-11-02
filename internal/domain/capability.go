package domain

import (
	"fmt"
	"strings"
)

// Standard Capability Constants
const (
	CapabilityResearchRead     = "research:read"
	CapabilityRepoRead         = "repo:read"
	CapabilityRepoWrite        = "repo:write"
	CapabilityWebRead          = "web:read"
	CapabilityEmailDraft       = "email:draft"
	CapabilityEmailSend        = "email:send"
	CapabilityCalendarRead     = "calendar:read"
	CapabilityCalendarWrite    = "calendar:write"
	CapabilityFinancialAction  = "financial:action"
	CapabilityJobApply         = "job:apply"
	CapabilityInfraDestructive = "infra:destructive"
)

// Action Types for Approvals
const (
	ActionTypeEmailSend        = "SEND_EMAIL"
	ActionTypeCalendarModify   = "MODIFY_CALENDAR"
	ActionTypeFinancialAction  = "FINANCIAL_ACTION"
	ActionTypeJobApply         = "APPLY_JOB"
	ActionTypeRepoModify       = "MODIFY_REPO"
	ActionTypeInfraDestructive = "DESTRUCTIVE_INFRA"
	ActionTypeToolExecution    = "EXECUTE_TOOL"
)

// AllKnownCapabilities defines the set of recognized system capabilities.
var AllKnownCapabilities = map[string]bool{
	CapabilityResearchRead:     true,
	CapabilityRepoRead:         true,
	CapabilityRepoWrite:        true,
	CapabilityWebRead:          true,
	CapabilityEmailDraft:       true,
	CapabilityEmailSend:        true,
	CapabilityCalendarRead:     true,
	CapabilityCalendarWrite:    true,
	CapabilityFinancialAction:  true,
	CapabilityJobApply:         true,
	CapabilityInfraDestructive: true,
}

// SensitiveCapabilities maps capabilities that ALWAYS require explicit user approval before execution.
var SensitiveCapabilities = map[string]ApprovalRiskLevel{
	CapabilityEmailSend:        ApprovalRiskHigh,
	CapabilityCalendarWrite:    ApprovalRiskMedium,
	CapabilityFinancialAction:  ApprovalRiskCritical,
	CapabilityJobApply:         ApprovalRiskHigh,
	CapabilityInfraDestructive: ApprovalRiskCritical,
	CapabilityRepoWrite:        ApprovalRiskMedium,
}

// IsKnownCapability checks whether a capability string is recognized by the system.
func IsKnownCapability(cap string) bool {
	return AllKnownCapabilities[strings.TrimSpace(cap)]
}

// IsSensitiveCapability checks whether a capability represents a sensitive action requiring human approval.
func IsSensitiveCapability(cap string) (bool, ApprovalRiskLevel) {
	level, ok := SensitiveCapabilities[strings.TrimSpace(cap)]
	return ok, level
}

// RequiresApproval determines whether a set of capabilities or a specific action requires human approval.
func RequiresApproval(capabilities []string, actionType string) (bool, ApprovalRiskLevel) {
	highestRisk := ApprovalRiskLow
	requires := false

	// Check capability sensitivity
	for _, cap := range capabilities {
		if sensitive, risk := IsSensitiveCapability(cap); sensitive {
			requires = true
			if riskLevelWeight(risk) > riskLevelWeight(highestRisk) {
				highestRisk = risk
			}
		}
	}

	// Check action type directly
	switch actionType {
	case ActionTypeEmailSend, ActionTypeJobApply:
		requires = true
		if riskLevelWeight(ApprovalRiskHigh) > riskLevelWeight(highestRisk) {
			highestRisk = ApprovalRiskHigh
		}
	case ActionTypeFinancialAction, ActionTypeInfraDestructive:
		requires = true
		highestRisk = ApprovalRiskCritical
	case ActionTypeCalendarModify, ActionTypeRepoModify:
		requires = true
		if riskLevelWeight(ApprovalRiskMedium) > riskLevelWeight(highestRisk) {
			highestRisk = ApprovalRiskMedium
		}
	}

	return requires, highestRisk
}

// ValidateCapabilities ensures all requested capabilities are valid and contained within allowed bounds.
func ValidateCapabilities(requested []string, allowed []string) error {
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[strings.TrimSpace(a)] = true
	}

	for _, req := range requested {
		cleaned := strings.TrimSpace(req)
		if cleaned == "" {
			continue
		}
		if !IsKnownCapability(cleaned) {
			return fmt.Errorf("%w: unknown capability '%s'", ErrValidation, cleaned)
		}
		if len(allowed) > 0 && !allowedSet[cleaned] {
			return fmt.Errorf("%w: capability '%s' is not permitted for this agent/worker", ErrForbidden, cleaned)
		}
	}
	return nil
}

func riskLevelWeight(level ApprovalRiskLevel) int {
	switch level {
	case ApprovalRiskLow:
		return 1
	case ApprovalRiskMedium:
		return 2
	case ApprovalRiskHigh:
		return 3
	case ApprovalRiskCritical:
		return 4
	default:
		return 0
	}
}
