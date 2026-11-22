package tests

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

const (
	detailInternshala = "https://internshala.com/internship/detail/software-development-internship-in-pune-at-crazy-for-success-foundation1795155236"
	detailUnstop      = "https://unstop.com/internships/software-internship-tringflow-private-limited-1803844"
	detailGone        = "https://internshala.com/internship/detail/gone-co-1"
)

func detailFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../internal/sources/testdata/" + name)
	require.NoError(t, err)
	return b
}

// exerciseListingDetails reads the detail pages of listings that have no description, against a real database
// through the given pool: the description, its hash and the deadline are stored, a snapshot is written, the assessment
// sees the changed description, a failed page is recorded and left for a day, and closed or employer listings are
// left alone.
func exerciseListingDetails(t *testing.T, ctx context.Context, pool database.Querier, owner uuid.UUID) {
	t.Helper()
	repo := repository.NewOpportunityRepository(pool)
	var _ service.DetailStore = repo
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	in := &service.Ingester{Store: repo, Now: func() time.Time { return at }}
	lead := func(source, company, url string) jobs.RawJob {
		return jobs.RawJob{Source: source, SourceKind: jobs.Aggregator, EmployerName: company, Title: "Software Intern", URL: url, Locations: []string{"Remote (India)"}, Compensation: "Unpaid", FetchedAt: at}
	}
	_, err := in.Ingest(ctx, owner, scan("internshala", "/internships/x/", jobs.ScanOK, at, lead("internshala", "Crazy For Success Foundation", detailInternshala), lead("internshala", "Gone Co", detailGone)))
	require.NoError(t, err)
	_, err = in.Ingest(ctx, owner, scan("unstop", "software", jobs.ScanOK, at, lead("unstop", "Tringflow Private Limited", detailUnstop)))
	require.NoError(t, err)
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at, ghJob("Razorpay", "Backend Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", assessStrong, at)))
	require.NoError(t, err)

	// Before: the assessment of the two leads has nothing to read.
	assessor := &service.OpportunityAssess{Store: repo, Now: func() time.Time { return at }}
	first, err := assessor.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 4, first.Assessed)

	need, err := repo.ListingsNeedingDetail(ctx, owner, 10)
	require.NoError(t, err)
	require.Len(t, need, 3, "the Greenhouse listing has its description already")

	var asked []string
	d := &service.ListingDetails{Store: repo, Throttle: time.Millisecond, Now: func() time.Time { return at.Add(time.Hour) },
		Get: func(_ context.Context, u string) ([]byte, error) {
			asked = append(asked, u)
			switch {
			case u == detailInternshala:
				return detailFixtureBytes(t, "internshala_detail_onsite.html"), nil
			case strings.HasSuffix(u, "/competition/1803844"):
				return detailFixtureBytes(t, "unstop_detail_unpaid.json"), nil
			}
			return nil, errors.New("HTTP 404")
		}}
	res, err := d.Run(ctx, owner, 0)
	require.NoError(t, err)
	require.Equal(t, 2, res.Saved, "%+v", res)
	require.Equal(t, 1, res.Failed)
	require.Len(t, asked, 3)

	listed, err := repo.ListAssessed(ctx, owner, domain.AssessedFilter{})
	require.NoError(t, err)
	var crazy, tring *domain.Listing
	for _, l := range listed {
		switch l.Company {
		case "Crazy For Success Foundation":
			crazy = l
		case "Tringflow Private Limited":
			tring = l
		}
	}
	require.NotNil(t, crazy)
	require.NotNil(t, tring)
	desc, err := repo.ListingDescription(ctx, owner, crazy.ID)
	require.NoError(t, err)
	require.Contains(t, desc, "About the internship")
	require.Contains(t, desc, "Who can apply")
	require.NotNil(t, crazy.DescriptionHash)
	require.Equal(t, domain.DescriptionHash(desc), *crazy.DescriptionHash)
	require.NotNil(t, crazy.Deadline)
	require.Equal(t, "2026-12-20", crazy.Deadline.In(time.FixedZone("IST", 5*3600+1800)).Format("2006-01-02"))
	require.NotNil(t, tring.Deadline)
	snaps, err := repo.Snapshots(ctx, owner, crazy.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 2, "first seen, then the description")
	require.Equal(t, *crazy.DescriptionHash, *snaps[1].DescriptionHash)

	// The failed page is a failed scan named <site>_detail, and is left alone for a day; the rest have nothing to read.
	health, err := repo.SourceHealth(ctx, owner)
	require.NoError(t, err)
	var failed, ok int
	for _, h := range health {
		if h.Source == "internshala_detail" && h.Target == detailGone && h.LastStatus == "failed" {
			failed++
		}
		if strings.HasSuffix(h.Source, "_detail") && h.Target == "listing details" && h.LastStatus != "failed" {
			ok++
		}
	}
	require.Equal(t, 1, failed)
	require.GreaterOrEqual(t, ok, 1)
	need, err = repo.ListingsNeedingDetail(ctx, owner, 10)
	require.NoError(t, err)
	require.Empty(t, need)

	// The changed description makes the assessment stale: the two listings are assessed again, the rest are not.
	second, err := assessor.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 2, second.Assessed, "%+v", second)
	after, err := repo.ListAssessed(ctx, owner, domain.AssessedFilter{})
	require.NoError(t, err)
	for _, l := range after {
		if l.ID == crazy.ID {
			require.NotNil(t, l.Assessment)
			require.NotEqual(t, domain.EligibilityUnknown, l.Assessment.Eligibility, "the page says who can apply: %+v", l.Assessment.EligibilityReasons)
		}
	}

	// A second run asks nobody.
	asked = nil
	again, err := d.Run(ctx, owner, 0)
	require.NoError(t, err)
	require.Equal(t, 0, again.Saved+again.Failed)
	require.Empty(t, asked)
}

func TestListingDetailsAreStoredAndAssessedAgain(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	owner := CreateTestUser(t, env.DB, "details@example.com")
	exerciseListingDetails(t, context.Background(), env.DB.Pool, owner)
}

func TestListingDetailsWorkThroughThePooler(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "pooler-details@example.com")

	url := os.Getenv("GRIFFIN_TEST_DB_URL")
	if url == "" {
		url = TestDBURL
	}
	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()
	exerciseListingDetails(t, ctx, pool, owner)
}
