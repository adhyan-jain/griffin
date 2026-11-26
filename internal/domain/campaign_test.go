package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTargetExtendedStatusValid(t *testing.T) {
	validStatuses := []string{
		TargetStatusDiscovered, TargetStatusVerified, TargetStatusQualified, TargetStatusPrioritised,
		TargetStatusDrafted, TargetStatusNeedsUserReview, TargetStatusApproved, TargetStatusSentByUser,
		TargetStatusReplied, TargetStatusFollowUp, TargetStatusClosed, TargetStatusDoNotContact,
		TargetStatusOptedOut, TargetStatusInvalidContact, TargetStatusDuplicate, TargetStatusNeedsResearch,
	}

	for _, s := range validStatuses {
		if !TargetExtendedStatusValid(s) {
			t.Errorf("expected %s to be valid", s)
		}
	}

	if TargetExtendedStatusValid("UNKNOWN_STATUS") {
		t.Errorf("expected UNKNOWN_STATUS to be invalid")
	}
}

func TestEnforceTargetDNC(t *testing.T) {
	if err := EnforceTargetDNC(nil); err == nil {
		t.Errorf("expected error for nil target")
	}

	normal := &VITCampaignTarget{
		ID:           uuid.New(),
		DoNotContact: false,
	}
	if err := EnforceTargetDNC(normal); err != nil {
		t.Errorf("unexpected error for active target: %v", err)
	}

	dnc := &VITCampaignTarget{
		ID:           uuid.New(),
		DoNotContact: true,
	}
	if err := EnforceTargetDNC(dnc); err == nil {
		t.Errorf("expected error for DNC target")
	}

	optedOut := &VITCampaignTarget{
		ID:             uuid.New(),
		ExtendedStatus: TargetStatusOptedOut,
	}
	if err := EnforceTargetDNC(optedOut); err == nil {
		t.Errorf("expected error for opted out target")
	}
}

func TestValidateTargetTransition(t *testing.T) {
	if err := ValidateTargetTransition(TargetStatusDrafted, TargetStatusApproved, false); err != nil {
		t.Errorf("unexpected error for normal transition: %v", err)
	}

	// DNC target trying to transition to approved must fail
	if err := ValidateTargetTransition(TargetStatusDrafted, TargetStatusApproved, true); err == nil {
		t.Errorf("expected error when transitioning DNC target to approved")
	}

	// DNC target transitioning to DO_NOT_CONTACT or OPTED_OUT must succeed
	if err := ValidateTargetTransition(TargetStatusDrafted, TargetStatusDoNotContact, true); err != nil {
		t.Errorf("unexpected error when transitioning DNC target to DO_NOT_CONTACT: %v", err)
	}

	// Invalid status
	if err := ValidateTargetTransition(TargetStatusDrafted, "INVALID", false); err == nil {
		t.Errorf("expected error for invalid target status")
	}
}

func TestValidateTargetTransitionStateMachine(t *testing.T) {
	cases := []struct {
		cur, next string
		dnc       bool
		want      error // nil = allowed
	}{
		{TargetStatusDrafted, TargetStatusApproved, false, nil},
		{TargetStatusNeedsUserReview, TargetStatusApproved, false, nil},
		{TargetStatusDiscovered, TargetStatusApproved, false, ErrConflict},
		{"", TargetStatusApproved, false, ErrConflict},
		{TargetStatusSentByUser, TargetStatusApproved, false, ErrConflict},
		{TargetStatusReplied, TargetStatusApproved, false, ErrConflict},
		{TargetStatusApproved, TargetStatusSentByUser, false, nil},
		{TargetStatusDrafted, TargetStatusSentByUser, false, ErrConflict},
		{TargetStatusReplied, TargetStatusSentByUser, false, ErrConflict},
		{TargetStatusSentByUser, TargetStatusReplied, false, nil},
		{TargetStatusFollowUp, TargetStatusReplied, false, nil},
		{TargetStatusApproved, TargetStatusReplied, false, ErrConflict},
		{TargetStatusDrafted, TargetStatusReplied, false, ErrConflict},
		{TargetStatusDrafted, TargetStatusDoNotContact, false, nil},
		{TargetStatusSentByUser, TargetStatusOptedOut, false, nil},
		{TargetStatusDoNotContact, TargetStatusApproved, false, ErrConflict},
		{TargetStatusOptedOut, TargetStatusDrafted, false, ErrConflict},
		{TargetStatusOptedOut, TargetStatusClosed, false, ErrConflict},
		{TargetStatusDrafted, TargetStatusApproved, true, ErrConflict},
		{TargetStatusDrafted, TargetStatusClosed, true, nil},
		{TargetStatusDrafted, "NOPE", false, ErrValidation},
	}
	for _, c := range cases {
		err := ValidateTargetTransition(c.cur, c.next, c.dnc)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%q -> %s (dnc=%v): got %v, want %v", c.cur, c.next, c.dnc, err, c.want)
		}
	}
}

func TestGenerateLinkedInQueries(t *testing.T) {
	queries := GenerateLinkedInQueries()
	if len(queries) == 0 {
		t.Fatalf("expected non-empty queries list")
	}

	for _, q := range queries {
		if q.Category == "" {
			t.Errorf("query missing category: %+v", q)
		}
		if q.Query == "" {
			t.Errorf("query missing query string: %+v", q)
		}
		if !strings.HasPrefix(q.URL, "https://www.linkedin.com/search/results/people/?") {
			t.Errorf("invalid URL format: %s", q.URL)
		}
		if !strings.Contains(q.URL, "keywords=") {
			t.Errorf("URL missing keywords param: %s", q.URL)
		}
	}
}
