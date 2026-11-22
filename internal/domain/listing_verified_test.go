package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTheListSaysWhichListingsAreLeadsAndWhichAreFromTheEmployer(t *testing.T) {
	at := time.Now()
	lead := ListingFromPosting(uuid.New(), "unstop", "B", Posting{Company: "Acme", Title: "SDE Intern", Link: "https://unstop.com/1"}, at)
	emp := ListingFromPosting(uuid.New(), "greenhouse", "A", Posting{Company: "Coy", Title: "SWE Intern", Link: "https://boards.greenhouse.io/coy/1"}, at)
	emp.SourceKind, emp.Verification = SourceKindEmployerATS, VerificationEmployer
	text := ListingsListText([]*Listing{lead, emp})
	if !strings.Contains(text, "| Source | Verified | Link | Status |") {
		t.Fatalf("the list has a Verified column:\n%s", text)
	}
	if !strings.Contains(text, "| unstop | lead | https://unstop.com/1 | not applied |") || !strings.Contains(text, "| greenhouse | employer | https://boards.greenhouse.io/coy/1 | not applied |") {
		t.Fatalf("lead for an aggregator listing, employer for an employer one:\n%s", text)
	}
	if lead.SourceKind != SourceKindAggregator || lead.Verification != VerificationLead {
		t.Fatalf("a posting read from a site is a lead: %+v", lead)
	}
	closed := *lead
	closed.Stage = StageClosed
	if !ListingIsClosed(ListingsListText([]*Listing{&closed}), "Acme", "SDE Intern") {
		t.Fatal("the extra column does not hide a closed listing from the readers of the list")
	}
}
