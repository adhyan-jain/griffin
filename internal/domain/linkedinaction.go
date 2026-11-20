package domain

import (
	"encoding/json"
	"fmt"
)

// LinkedInScan is a task that scrolls the owner's LinkedIn feed (on the laptop, else the phone), keeps the real
// internship posts, and adds them to the shared Internships list. It only reads.
type LinkedInScan struct {
	Minutes   int `json:"minutes"`
	Workspace int `json:"workspace"` // Hyprland workspace for the laptop window; 0 leaves the window where it opens
}

// ParseLinkedInScan reads the payload's "linkedin_scan" object; nil when the task has none.
func ParseLinkedInScan(payload json.RawMessage) (*LinkedInScan, error) {
	var p struct {
		Scan *LinkedInScan `json:"linkedin_scan"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &p) != nil || p.Scan == nil {
		return nil, nil
	}
	s := p.Scan
	if s.Minutes == 0 {
		s.Minutes = 10
	}
	if s.Minutes < 1 || s.Minutes > 30 {
		return nil, fmt.Errorf("%w: a LinkedIn scan runs 1 to 30 minutes, got %d", ErrValidation, s.Minutes)
	}
	if s.Workspace < 0 || s.Workspace > 10 {
		return nil, fmt.Errorf("%w: workspace must be 0 to 10, got %d", ErrValidation, s.Workspace)
	}
	return s, nil
}

// ApprovalActionType is the approval a host asks for when the owner has set LinkedIn to "ask".
func (s *LinkedInScan) ApprovalActionType() string { return "host_action:browser:read:linkedin.com" }

// Summary describes the scan to the owner.
func (s *LinkedInScan) Summary() string {
	return fmt.Sprintf("Scroll your LinkedIn feed for %d minutes and add real internship posts to your Internships list (read only)", s.Minutes)
}
