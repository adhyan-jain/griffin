package service

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/budget"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
)

// ListingsNeedingDetail and SaveDetail make memListings a DetailStore, with the repository's rules: open Internshala
// and Unstop listings with no description, newest first, leaving out those whose page failed in the last day.
func (m *memListings) ListingsNeedingDetail(_ context.Context, _ uuid.UUID, limit int) ([]*domain.Listing, error) {
	var out []*domain.Listing
	byAge := append([]*domain.Listing(nil), m.rows...)
	sort.SliceStable(byAge, func(i, j int) bool { return byAge[i].CreatedAt.After(byAge[j].CreatedAt) })
	for _, r := range byAge {
		if r.Description != "" || (r.Source != sources.Internshala && r.Source != sources.Unstop) || r.Stage == domain.StageClosed || r.ClosedAt != nil {
			continue
		}
		failed := false
		for _, s := range m.scans {
			if s.Source == sources.DetailSource(r.Source) && s.Target == r.URL && s.Status == "failed" && time.Since(s.FinishedAt) < 24*time.Hour {
				failed = true
			}
		}
		if !failed && len(out) < limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memListings) SaveDetail(_ context.Context, _ uuid.UUID, id uuid.UUID, description, hash string, deadline *time.Time) error {
	for _, r := range m.rows {
		if r.ID == id {
			r.Description, r.DescriptionHash = description, &hash
			if deadline != nil {
				r.Deadline = deadline
			}
			return nil
		}
	}
	return domain.ErrNotFound
}

func detailFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../sources/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func aggregatorRow(source, company, url string, age time.Duration) *domain.Listing {
	return &domain.Listing{ID: uuid.New(), Source: source, Company: company, Role: "Software Intern", URL: url, Location: "Remote (India)", Stage: domain.StageVerified,
		SourceKind: domain.SourceKindAggregator, Verification: domain.VerificationLead, CreatedAt: time.Now().Add(-age)}
}

const (
	isLink1 = "https://internshala.com/internship/detail/software-development-internship-in-pune-at-crazy-for-success-foundation1795155236"
	isLink2 = "https://internshala.com/internship/detail/work-from-home-part-time-software-development-internship-at-nayepankh-foundation1795087159"
	unLink  = "https://unstop.com/internships/software-internship-tringflow-private-limited-1803844"
)

func detailGet(t *testing.T, calls *[]string) sources.Get {
	return func(_ context.Context, u string) ([]byte, error) {
		*calls = append(*calls, u)
		switch {
		case u == isLink1:
			return detailFixture(t, "internshala_detail_onsite.html"), nil
		case u == isLink2:
			return detailFixture(t, "internshala_detail_wfh_ppo.html"), nil
		case strings.HasSuffix(u, "/1803844"):
			return detailFixture(t, "unstop_detail_unpaid.json"), nil
		}
		return nil, errors.New("HTTP 404")
	}
}

func TestListingDetailsFillsDescriptionHashDeadlineAndWritesASnapshot(t *testing.T) {
	store := &memListings{}
	a := aggregatorRow(sources.Internshala, "Crazy For Success Foundation", isLink1, 3*time.Hour)
	b := aggregatorRow(sources.Unstop, "Tringflow Private Limited", unLink, time.Hour)
	store.rows = []*domain.Listing{a, b}
	var calls []string
	d := &ListingDetails{Store: store, Get: detailGet(t, &calls), Throttle: time.Millisecond}
	res, err := d.Run(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != 2 || res.Failed != 0 || res.Left != 0 {
		t.Fatalf("%+v", res)
	}
	if len(calls) != 2 || calls[0] != "https://unstop.com/api/public/competition/1803844" || calls[1] != isLink1 {
		t.Fatalf("newest first, each at its own address: %v", calls)
	}
	if !strings.Contains(a.Description, "About the internship") || a.DescriptionHash == nil || *a.DescriptionHash != domain.DescriptionHash(a.Description) {
		t.Fatalf("the description and its hash are stored: %.80q", a.Description)
	}
	if a.Deadline == nil || a.Deadline.Format("2006-01-02") != "2026-12-20" || b.Deadline == nil || b.Deadline.Format("2006-01-02") != "2026-11-24" {
		t.Fatalf("deadlines: %v %v", a.Deadline, b.Deadline)
	}
	if snaps := store.snapshotsOf(a.ID); len(snaps) != 1 || snaps[0].DescriptionHash == nil || *snaps[0].DescriptionHash != *a.DescriptionHash || snaps[0].Deadline == nil {
		t.Fatalf("a snapshot records the new description: %+v", snaps)
	}
	// The assessment keys on the description hash, so the listing is now stale and will be assessed again.
	if domain.AssessmentKey(a) != *a.DescriptionHash {
		t.Fatal("the assessment key must follow the description hash")
	}
	if len(store.scans) != 2 || store.scans[0].Source != "unstop_detail" && store.scans[0].Source != "internshala_detail" || store.scans[0].Status != "ok" {
		t.Fatalf("each site's run is recorded as a scan: %+v", store.scans)
	}
	// Nothing is left to read, so a second run does not ask again.
	calls = nil
	if res, _ := d.Run(context.Background(), uuid.New(), 0); res.Saved != 0 || len(calls) != 0 || !strings.Contains(res.Summary(), "already has its description") {
		t.Fatalf("%+v %v", res, calls)
	}
}

func TestListingDetailsFailuresAreRecordedNeverFatalAndNotRetriedTheSameDay(t *testing.T) {
	store := &memListings{}
	gone := aggregatorRow(sources.Internshala, "Gone Co", "https://internshala.com/internship/detail/gone-co1", 2*time.Hour)
	ok := aggregatorRow(sources.Internshala, "Crazy", isLink1, time.Hour)
	junk := aggregatorRow(sources.Unstop, "Junk Co", "https://unstop.com/internships/junk-77", 3*time.Hour)
	other := aggregatorRow("interndock", "Dock Co", "https://interndock.com/x", 4*time.Hour)
	closed := aggregatorRow(sources.Internshala, "Closed Co", "https://internshala.com/internship/detail/closed1", 5*time.Hour)
	closed.Stage = domain.StageClosed
	store.rows = []*domain.Listing{closed, other, junk, gone, ok}
	var calls []string
	get := func(ctx context.Context, u string) ([]byte, error) {
		if strings.HasSuffix(u, "/77") {
			return []byte("<!DOCTYPE html><title>Not Found</title>"), nil // a page that is not the JSON
		}
		return detailGet(t, &calls)(ctx, u)
	}
	d := &ListingDetails{Store: store, Get: get, Throttle: time.Millisecond}
	res, err := d.Run(context.Background(), uuid.New(), 0)
	if err != nil || res.Saved != 1 || res.Failed != 2 {
		t.Fatalf("one saved, two failed, no error: %+v %v", res, err)
	}
	var failedScans int
	for _, s := range store.scans {
		if s.Status == "failed" && s.Target != detailTarget {
			failedScans++
			if s.Source != sources.DetailSource(strings.Split(strings.TrimPrefix(s.Target, "https://"), ".")[0]) || s.Error == "" {
				t.Errorf("a failure is a source scan named <site>_detail, with the reason: %+v", s)
			}
		}
	}
	if failedScans != 2 || closed.Description != "" || other.Description != "" {
		t.Fatalf("failed scans %d; closed and unsupported listings are left alone", failedScans)
	}
	if !strings.Contains(res.Summary(), "2 pages could not be read") {
		t.Fatal(res.Summary())
	}
	calls = nil
	if res, _ := d.Run(context.Background(), uuid.New(), 0); res.Saved != 0 || res.Failed != 0 || len(calls) != 0 {
		t.Fatalf("a page that failed is left for a day: %+v %v", res, calls)
	}
}

func TestListingDetailsThrottlesPerHostAndStopsAtTheCap(t *testing.T) {
	store := &memListings{}
	for i := 0; i < 4; i++ {
		store.rows = append(store.rows, aggregatorRow(sources.Internshala, "Co", isLink1+string(rune('a'+i)), time.Duration(4-i)*time.Hour))
	}
	var stamps []time.Time
	get := func(context.Context, string) ([]byte, error) {
		stamps = append(stamps, time.Now())
		return detailFixture(t, "internshala_detail_onsite.html"), nil
	}
	d := &ListingDetails{Store: store, Get: get, Throttle: 60 * time.Millisecond}
	res, _ := d.Run(context.Background(), uuid.New(), 3)
	if res.Saved != 3 || res.Left != 1 || len(stamps) != 3 {
		t.Fatalf("the cap is 3 and one is left: %+v", res)
	}
	for i := 1; i < len(stamps); i++ {
		if gap := stamps[i].Sub(stamps[i-1]); gap < 55*time.Millisecond {
			t.Fatalf("two requests to one host %v apart", gap)
		}
	}
	if !strings.Contains(res.Summary(), "1 listings are left for the next run") {
		t.Fatal(res.Summary())
	}
}

func TestListingDetailsWithLittleTimeIsAPartialSuccess(t *testing.T) {
	store := &memListings{rows: []*domain.Listing{aggregatorRow(sources.Internshala, "Co", isLink1, time.Hour), aggregatorRow(sources.Unstop, "Co2", unLink, 2*time.Hour)}}
	calls := 0
	get := func(context.Context, string) ([]byte, error) { calls++; return nil, errors.New("must not be asked") }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second) // under the 5 second reserve
	defer cancel()
	res, err := (&ListingDetails{Store: store, Get: get}).Run(ctx, uuid.New(), 0)
	if err != nil || calls != 0 || res.Left != 2 || res.Saved != 0 || res.Failed != 0 {
		t.Fatalf("no time: nothing started, both left, no failure recorded: %+v %v calls=%d", res, err, calls)
	}
	if !strings.Contains(res.Summary(), "2 listings are left for the next run") || len(store.scans) != 0 {
		t.Fatal(res.Summary())
	}

	// With a little time, the run does what fits and leaves the rest.
	shrinkBudget(t)
	slow := func(ctx context.Context, u string) ([]byte, error) {
		time.Sleep(120 * time.Millisecond)
		return detailGet(t, new([]string))(ctx, u)
	}
	store = &memListings{rows: []*domain.Listing{
		aggregatorRow(sources.Internshala, "A", isLink1, 3*time.Hour), aggregatorRow(sources.Internshala, "B", isLink2, 2*time.Hour),
		aggregatorRow(sources.Unstop, "C", unLink, time.Hour), aggregatorRow(sources.Unstop, "D", "https://unstop.com/internships/d-1803844", 30*time.Minute)}}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 420*time.Millisecond)
	defer cancel2()
	res, err = (&ListingDetails{Store: store, Get: slow, Throttle: time.Millisecond}).Run(ctx2, uuid.New(), 0)
	if err != nil || res.Saved == 0 || res.Left == 0 || res.Saved+res.Failed+res.Left != 4 {
		t.Fatalf("some done, some left, nothing fatal: %+v %v", res, err)
	}
}

func TestListingDetailsRoutineRunsInCodeAndIsARoutine(t *testing.T) {
	store := &memListings{rows: []*domain.Listing{aggregatorRow(sources.Internshala, "Co", isLink1, time.Hour)}}
	var calls []string
	w := &TrackerLists{Details: &ListingDetails{Store: store, Get: detailGet(t, &calls), Throttle: time.Millisecond}}
	answer, how := w.RunInCode(context.Background(), &domain.Task{UserID: uuid.New(), Title: "LISTING DETAILS (every 2 hours)"}, nil, nil)
	if how != Answered || !strings.Contains(answer, "Read the detail pages of 1 listings") || store.rows[0].Description == "" {
		t.Fatalf("%v %s", how, answer)
	}
	if _, how := (&TrackerLists{}).RunInCode(context.Background(), &domain.Task{UserID: uuid.New(), Title: ListingDetailsTitle}, nil, nil); how != NotInCode {
		t.Fatal("with no reader wired, the task is left to the normal path")
	}
	if !domain.IsRoutine(&domain.Task{Title: ListingDetailsTitle}) {
		t.Fatal("the details task is a routine and is never archived by age")
	}
}

func TestListingSyncReadsAFewDetailsAfterItsPages(t *testing.T) {
	store := &memListings{rows: []*domain.Listing{aggregatorRow(sources.Internshala, "Crazy", isLink1, time.Hour), aggregatorRow(sources.Unstop, "Tringflow", unLink, time.Minute)}}
	var calls []string
	pages := func(ctx context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "/internship/detail/") || strings.Contains(u, "/api/public/competition/") {
			return detailGet(t, &calls)(ctx, u)
		}
		return nil, errors.New("offline")
	}
	sync := &ListingSync{Listings: store, Get: pages, Check: func(context.Context, string) PostingState { return PostingOpen },
		Details: &ListingDetails{Store: store, Get: pages, Throttle: time.Millisecond}}
	res, err := sync.Run(context.Background(), uuid.New())
	if err != nil || res.DetailsSaved != 2 || store.rows[0].Description == "" || store.rows[1].Description == "" {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(res.Summary(), "Read the detail pages of 2 listings") {
		t.Fatal(res.Summary())
	}
}

// shrinkBudget makes the time reserves tiny so a test can run out of time in milliseconds.
func shrinkBudget(t *testing.T) {
	t.Helper()
	r, w, m := budget.Reserve, budget.WriteReserve, modelCallTime
	budget.Reserve, budget.WriteReserve, modelCallTime = 100*time.Millisecond, 10*time.Millisecond, 160*time.Millisecond
	t.Cleanup(func() { budget.Reserve, budget.WriteReserve, modelCallTime = r, w, m })
}
