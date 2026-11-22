package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
)

func TestAPageThatCannotBeReadIsAFailedScanAndClosesNothing(t *testing.T) {
	store := &memListings{}
	unstop := `{"data":{"data":[{"title":"Software Engineer Intern","seo_url":"https://unstop.com/internships/coy-1","regn_open":1,"status":"LIVE","end_date":"2099-01-01T00:00:00+05:30","organisation":{"name":"Coy"},"locations":["Gurgaon"],"jobDetail":{"min_salary":45000,"max_salary":45000,"pay_in":"monthly","paid_unpaid":"paid"}}]}}`
	up := true
	sync := &ListingSync{Listings: store, Check: func(context.Context, string) PostingState { return PostingOpen }, Get: func(_ context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "unstop.com") {
			if up && strings.HasSuffix(u, "searchTerm=software") {
				return []byte(unstop), nil
			}
			return []byte(`{"data":{"data":[]}}`), nil
		}
		return nil, errors.New("offline")
	}}
	if res, err := sync.Run(context.Background(), uuid.New()); err != nil || res.Added != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	up = false // the search that held it now answers an empty page, and every other site is down
	res, _ := sync.Run(context.Background(), uuid.New())
	if res.Closed != 0 || store.rows[0].Stage == domain.StageClosed {
		t.Fatalf("a listing is not closed because a page was down or came back without it: %+v", res)
	}
	var failed, ok int
	for _, s := range store.scans {
		switch s.Status {
		case "failed":
			failed++
			if s.Error == "" {
				t.Errorf("a failed scan names its error: %+v", s)
			}
		case "ok":
			ok++
		}
	}
	pages := len(sources.InternshalaPages) + len(domain.InternDockPages)
	if ok != 2*len(sources.UnstopSearches) || failed != 2*pages || len(res.Problems) != pages {
		t.Fatalf("every page of every run is in the scan history: ok %d failed %d problems %v", ok, failed, res.Problems)
	}
}
