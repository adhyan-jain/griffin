package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// memListings is an in-memory listings table with the same one-per-company-and-role rule.
type memListings struct {
	rows []*domain.Listing
}

func (m *memListings) Upsert(_ context.Context, l *domain.Listing) (bool, error) {
	for _, r := range m.rows {
		if strings.EqualFold(r.Company, l.Company) && strings.EqualFold(r.Role, l.Role) {
			*l = *r
			return false, nil
		}
	}
	l.ID = uuid.New()
	m.rows = append(m.rows, l)
	return true, nil
}
func (m *memListings) List(_ context.Context, _ uuid.UUID, stages []string, _ int) ([]*domain.Listing, error) {
	var out []*domain.Listing
	for _, r := range m.rows {
		if len(stages) == 0 { // like the table: no stages means every listing
			out = append(out, r)
			continue
		}
		for _, s := range stages {
			if r.Stage == s {
				out = append(out, r)
			}
		}
	}
	return out, nil
}
func (m *memListings) Get(_ context.Context, _ uuid.UUID, id uuid.UUID) (*domain.Listing, error) {
	for _, r := range m.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (m *memListings) Move(_ context.Context, _ uuid.UUID, id uuid.UUID, from, to, note string, _ *uuid.UUID) error {
	for _, r := range m.rows {
		if r.ID == id && r.Stage == from && domain.CanMove(from, to) {
			r.Stage, r.StageNote = to, note
			return nil
		}
	}
	return errors.New("illegal move")
}
func (m *memListings) Checked(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func TestTheSyncListsOnlyRealFittingPostingsAndClosesDeadOnes(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	store := &memListings{rows: []*domain.Listing{{ID: uuid.New(), Company: "Gone Co", Role: "SDE Intern", URL: "https://gone", Stage: domain.StageVerified, LastCheckedAt: &old}}}
	unstop := `{"data":{"data":[
	 {"title":"Software Engineer Intern","seo_url":"https://unstop.com/internships/coy-1","regn_open":1,"status":"LIVE","end_date":"2099-01-01T00:00:00+05:30","organisation":{"name":"Coy"},"locations":["Gurgaon"],"jobDetail":{"type":"office","min_salary":45000,"max_salary":45000,"pay_in":"monthly","paid_unpaid":"paid"}},
	 {"title":"Software Testing Internship","seo_url":"https://unstop.com/internships/qa-1","regn_open":1,"status":"LIVE","end_date":"2099-01-01T00:00:00+05:30","organisation":{"name":"QA Co"},"locations":["Pune"],"jobDetail":{"min_salary":30000,"max_salary":30000,"pay_in":"monthly","paid_unpaid":"paid"}},
	 {"title":"Backend Developer Internship","seo_url":"https://unstop.com/internships/cheap-1","regn_open":1,"status":"LIVE","end_date":"2099-01-01T00:00:00+05:30","organisation":{"name":"Cheap Co"},"locations":["Delhi"],"jobDetail":{"min_salary":3000,"max_salary":5000,"pay_in":"monthly","paid_unpaid":"paid"}}]}}`
	sync := &ListingSync{
		Listings: store,
		Get: func(_ context.Context, u string) ([]byte, error) {
			if strings.Contains(u, "unstop.com") {
				return []byte(unstop), nil
			}
			return nil, errors.New("offline")
		},
		Check: func(_ context.Context, link string) PostingState {
			if link == "https://gone" {
				return PostingMissing
			}
			return PostingOpen
		},
	}
	res, err := sync.Run(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Closed != 1 || store.rows[0].Stage != domain.StageClosed {
		t.Fatalf("only the paid software internship is added (not testing, not ₹5k), and the dead one closes: %+v", res)
	}
	added := store.rows[1]
	if added.Company != "Coy" || added.Tier != "A" || added.Source != "unstop" || added.URL != "https://unstop.com/internships/coy-1" {
		t.Fatalf("added: %+v", added)
	}
	again, _ := sync.Run(context.Background(), uuid.New())
	if again.Added != 0 || again.Seen != 1 {
		t.Fatalf("a second run adds nothing twice: %+v", again)
	}
	if !strings.Contains(res.Summary(), "Software Engineer Intern at Coy") || !strings.Contains(res.Summary(), "Internshala") {
		t.Fatalf("the summary says what was added and what could not be read: %s", res.Summary())
	}
}

func TestTheSyncRoutineRunsInCode(t *testing.T) {
	w := &TrackerLists{Sync: &ListingSync{Listings: &memListings{}, Get: func(context.Context, string) ([]byte, error) { return []byte(`{"data":{"data":[]}}`), nil }}}
	answer, how := w.RunInCode(context.Background(), &domain.Task{UserID: uuid.New(), Title: ListingsSyncTitle}, nil, nil)
	if how != Answered || !strings.Contains(answer, "No new openings today") {
		t.Fatalf("%v %s", how, answer)
	}
}

func TestTheInternshipsListIsReplacedByTheVerifiedListingsAndFakeApplicationsAreDropped(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	fake := "# Internships tracker\n\n| Tier | Company | Role | Status |\n|---|---|---|---|\n| Tier A | Qdrant | Open Source Vector Engine Intern | not applied |\n"
	list := &memList{text: fake}
	listings := &memListings{rows: []*domain.Listing{
		{ID: uuid.New(), Company: "Coy", Role: "Software Engineer Intern", URL: "https://unstop.com/1", Tier: "A", Stage: domain.StageVerified},
	}}
	w := &TrackerLists{Store: list, Sync: &ListingSync{Listings: listings}}
	if err := w.WriteListingsList(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(list.text, "Qdrant") || !strings.Contains(list.text, "| Tier A | Coy | Software Engineer Intern |") {
		t.Fatalf("the list holds only verified listings:\n%s", list.text)
	}

	var cancelled []string
	cancel := func(_ context.Context, _ uuid.UUID, why string) error { cancelled = append(cancelled, why); return nil }
	apply := func(company, role string) *domain.Task {
		p, _ := json.Marshal(map[string]any{"apply_for": map[string]string{"company": company, "role": role}})
		return &domain.Task{ID: uuid.New(), UserID: owner, Title: "APPLY - " + company, InputPayload: p}
	}
	if !w.SkipClosedListing(ctx, apply("Qdrant", "Open Source Vector Engine Intern"), cancel) || !strings.Contains(cancelled[0], "not a verified listing") {
		t.Fatalf("an application for a company that was never verified is dropped: %v", cancelled)
	}
	if w.SkipClosedListing(ctx, apply("Coy", "Software Engineer Intern"), cancel) {
		t.Fatal("an application for a verified listing goes ahead")
	}
}
