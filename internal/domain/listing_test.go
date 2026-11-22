package domain

import (
	"strings"
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

func TestTheInternshipsListIsWrittenOnlyFromVerifiedListings(t *testing.T) {
	at := time.Now()
	open := ListingFromPosting(uuid.New(), "unstop", "B", Posting{Company: "Acme", Title: "SDE Intern", Link: "https://unstop.com/1", Location: "Pune", Pay: "₹20,000 /month"}, at)
	top := ListingFromPosting(uuid.New(), "internshala", "A", Posting{Company: "Coy", Title: "Software Engineer Intern", Link: "https://internshala.com/2", Pay: "₹45,000 /month"}, at)
	gone := ListingFromPosting(uuid.New(), "unstop", "A", Posting{Company: "Gone", Title: "ML Intern", Link: "https://unstop.com/3"}, at)
	gone.Stage = StageClosed
	text := ListingsListText([]*Listing{open, gone, top})

	if !strings.Contains(text, VerifiedListMarker) || strings.Index(text, "Coy") > strings.Index(text, "Acme") || strings.Index(text, "Acme") > strings.Index(text, "Gone") {
		t.Fatalf("best tier first, closed last:\n%s", text)
	}
	if !ListingIsClosed(text, "Gone", "ML Intern") || ListingIsClosed(text, "Acme", "SDE Intern") {
		t.Fatalf("a closed listing reads as closed, an open one does not:\n%s", text)
	}
	if !ListingIsUnverified(text, "Qdrant") || ListingIsUnverified(text, "acme") {
		t.Fatal("a company that is not on the verified list is unverified; one that is, is not")
	}
	if ListingIsUnverified("| Company |\n| Qdrant |", "Other") {
		t.Fatal("an old hand-written list says nothing about verification")
	}
	if rows := strings.Count(text, "\n| Tier "); rows != 4 { // the header and three listings
		t.Fatalf("one row per listing: %d", rows)
	}
}
