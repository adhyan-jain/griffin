package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAListingMovesOnlyForwardThroughThePipeline(t *testing.T) {
	ok := [][2]string{{StageVerified, StageDrafted}, {StageDrafted, StageApproved}, {StageApproved, StageSubmitting}, {StageSubmitting, StageConfirmed}, {StageSubmitting, StageNeedsYou}, {StageNeedsYou, StageApproved}, {StageClosed, StageVerified}}
	for _, m := range ok {
		if !CanMove(m[0], m[1]) {
			t.Errorf("%s -> %s must be allowed", m[0], m[1])
		}
	}
	bad := [][2]string{{StageVerified, StageSubmitting}, {StageVerified, StageConfirmed}, {StageDrafted, StageConfirmed}, {StageConfirmed, StageVerified}, {StageClosed, StageSubmitting}, {"", StageVerified}}
	for _, m := range bad {
		if CanMove(m[0], m[1]) {
			t.Errorf("%s -> %s must be refused: nothing is submitted without a draft and an approval", m[0], m[1])
		}
	}
}

func TestAFetchedPostingBecomesAVerifiedListing(t *testing.T) {
	at := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	l := ListingFromPosting(uuid.New(), "unstop", "A", Posting{Company: " Acme ", Title: "SDE Intern", Link: "https://x/1", Location: "Bengaluru"}, at)
	if l.Stage != StageVerified || l.Company != "Acme" || l.VerifiedAt == nil || l.PostedAt != nil || l.Tier != "A" {
		t.Fatalf("%+v", l)
	}
}
