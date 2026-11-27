package domain

import (
	"testing"
	"time"
)

var evidenceDay = time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)

func evidenceRow() AlumniImportRow {
	return AlumniImportRow{Name: "Test Person", GraduationYear: 2023, ProfileURL: "https://linkedin.com/in/test-person",
		EvidenceURL: "https://example.org/team", Quote: "B.Tech CSE, VIT Vellore (2019-2023), ACM VIT core team"}
}

const evidencePage = "Our team\nTest Person — Backend engineer.\nB.Tech  CSE, VIT Vellore (2019–2023), ACM VIT core team.\n"

func TestEvidenceIsRecordedOnlyWhenTheQuoteIsOnThePage(t *testing.T) {
	v, ok := EvidenceVerification(evidenceRow(), evidencePage)
	if !ok || v.Campus == nil || v.Campus.Value != CampusVellore || v.Graduation == nil || v.Graduation.Year != 2023 || v.ACM == nil {
		t.Fatalf("quote on the page proves campus, batch and ACM: %+v %v", v, ok)
	}
	f, err := v.Resolve(evidenceDay)
	if err != nil || *f.VITAffiliation != AffiliationConfirmedVellore || *f.ACMAffiliation != "confirmed" || f.Provenance["campus"].(map[string]any)["verified_by"] != EvidenceCheckedBy {
		t.Fatalf("resolved: %+v %v", f, err)
	}
	if _, ok := EvidenceVerification(evidenceRow(), "Our team\nTest Person, backend engineer."); ok {
		t.Fatal("a quote that is not on the page proves nothing")
	}
}

func TestEvidenceRefusesLinkedInShortQuotesAndUnprovenClaims(t *testing.T) {
	r := evidenceRow()
	r.EvidenceURL = "https://www.linkedin.com/in/test-person"
	if _, ok := EvidenceVerification(r, evidencePage); ok {
		t.Error("a LinkedIn page is never evidence")
	}
	r = evidenceRow()
	r.Quote = "VIT Vellore"
	if _, ok := EvidenceVerification(r, evidencePage); ok {
		t.Error("a quote under 20 characters proves too little")
	}
	r = evidenceRow()
	r.GraduationYear = 2021
	v, ok := EvidenceVerification(r, evidencePage)
	if !ok || v.Graduation != nil {
		t.Error("a batch the quote does not contain is not verified")
	}
	r = evidenceRow()
	r.Quote = "Backend engineer at a startup since 2023"
	if _, ok := EvidenceVerification(r, "Backend engineer at a startup since 2023"); ok {
		t.Error("a quote with no campus or ACM proves nothing about the person")
	}
	r = evidenceRow()
	r.Quote = "B.Tech, VIT Chennai (2019-2023)"
	v, ok = EvidenceVerification(r, "B.Tech, VIT Chennai (2019-2023)")
	if !ok || v.Campus == nil || v.Campus.Value != CampusChennai || v.Graduation != nil {
		t.Errorf("another campus is recorded as such, with no batch: %+v", v)
	}
}
