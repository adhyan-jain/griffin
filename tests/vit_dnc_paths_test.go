package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// queuePerson inserts a person with the typed classification columns BuildQueue reads.
func (v *vitCtl) queuePerson(t *testing.T, user uuid.UUID, slug, vit, elig string, dnc bool) uuid.UUID {
	t.Helper()
	url := "https://www.linkedin.com/in/example-" + slug
	var id uuid.UUID
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `
		INSERT INTO canonical_entities (user_id, kind, canonical_key, name, profile_url, metadata, vit_affiliation, graduation_eligibility, do_not_contact)
		VALUES ($1, 'person', $2, $3, $2, '{"company":"Example Labs","title":"Software Engineer"}'::jsonb, $4, $5, $6) RETURNING id`,
		user, url, "Example "+slug, vit, elig, dnc).Scan(&id))
	return id
}

func (v *vitCtl) queueSvc() *service.VITCampaignService {
	return service.NewVITCampaignService(v.env.DB.Pool, repository.NewPeopleRepository(v.env.DB.Pool), v.camp)
}

func TestBuildQueueQueuesOnlyConfirmedEligibleNonDNC(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-queue@example.com")
	svc := v.queueSvc()

	empty, err := svc.BuildQueue(v.ctx, u)
	require.NoError(t, err)
	require.Zero(t, empty.Queued)
	require.Zero(t, empty.Stats.TotalPeople)

	good := v.queuePerson(t, u, "good", domain.AffiliationConfirmedVellore, domain.CohortEligible, false)
	dnc := v.queuePerson(t, u, "dnc", domain.AffiliationConfirmedVellore, domain.CohortEligible, true)
	unconfirmed := v.queuePerson(t, u, "unconf", domain.AffiliationUnconfirmed, domain.CohortEligible, false)
	borderline := v.queuePerson(t, u, "border", domain.AffiliationConfirmedVellore, domain.CohortBorderline2022, false)
	other := v.queuePerson(t, u, "other", domain.AffiliationOtherCampus, domain.CohortEligible, false)

	res, err := svc.BuildQueue(v.ctx, u)
	require.NoError(t, err)
	require.Equal(t, 5, res.Considered)
	require.Equal(t, 1, res.Queued)
	require.Equal(t, 1, res.SkippedDNC)
	require.Equal(t, 3, res.SkippedUnverified)
	require.Equal(t, 1, res.NeedsUserReview, "no owner profile means placeholders, so the target needs review")
	require.Equal(t, 5, res.Stats.TotalPeople)
	require.Equal(t, 3, res.Stats.Unverified)

	queued := func(id uuid.UUID) (string, bool) {
		var ext string
		err := v.env.DB.Pool.QueryRow(v.ctx, `SELECT extended_status FROM outreach_targets WHERE person_id = $1`, id).Scan(&ext)
		return ext, err == nil
	}
	ext, ok := queued(good)
	require.True(t, ok)
	require.Equal(t, domain.TargetStatusNeedsUserReview, ext)
	for _, id := range []uuid.UUID{dnc, unconfirmed, borderline, other} {
		_, ok := queued(id)
		require.False(t, ok, "person must not be queued")
	}

	// the person's tier is stored and no pay evidence means LOW/UNKNOWN pay, never an exclusion
	var tier string
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `SELECT priority_tier FROM canonical_entities WHERE id = $1`, good).Scan(&tier))
	require.Equal(t, domain.PriorityP4, tier)
	q, err := svc.GetCampaignQueue(v.ctx, u, 10)
	require.NoError(t, err)
	require.Len(t, q, 1)
	require.Equal(t, domain.PayPotentialLowUnknown, q[0].PayPotential)

	// a second build leaves an approved target untouched
	c, _ := v.camp.EnsureVITAlumniCampaign(v.ctx, u)
	var tid uuid.UUID
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `SELECT id FROM outreach_targets WHERE campaign_id = $1 AND person_id = $2`, c.ID, good).Scan(&tid))
	require.NoError(t, v.camp.UpdateTargetStatus(v.ctx, u, tid, domain.TargetStatusApproved))
	_, draftBefore := v.state(t, tid)
	_, err = svc.BuildQueue(v.ctx, u)
	require.NoError(t, err)
	ext2, draftAfter := v.state(t, tid)
	require.Equal(t, domain.TargetStatusApproved, ext2)
	require.Equal(t, draftBefore, draftAfter)
}

func TestBuildQueueUsesStoredPayObservationsOnly(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-pay@example.com")
	svc := v.queueSvc()
	pid := v.queuePerson(t, u, "pay", domain.AffiliationConfirmedVellore, domain.CohortEligible, false)

	var cid uuid.UUID
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `INSERT INTO companies (user_id, name, normalized_name, kind, discovered_via, status)
		VALUES ($1, 'Example Labs', 'example labs', 'unknown', 'research', 'candidate') RETURNING id`, u).Scan(&cid))
	_, err := v.env.DB.Pool.Exec(v.ctx, `INSERT INTO company_observations (user_id, company_id, property, value, source_url, claim_origin)
		VALUES ($1, $2, 'advertised_pay', '30 LPA', 'https://example.org/pay', 'independent')`, u, cid)
	require.NoError(t, err)

	res, err := svc.BuildQueue(v.ctx, u)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stats.WithPayEvidence)
	var tier string
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `SELECT priority_tier FROM canonical_entities WHERE id = $1`, pid).Scan(&tier))
	require.Equal(t, domain.PriorityP3, tier, "confirmed Vellore, eligible, HIGH pay evidence")
}

func TestDoNotContactPersonIsRefusedOnEveryPath(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-dnc-paths@example.com")
	svc := v.queueSvc()
	pid := v.queuePerson(t, u, "dncpath", domain.AffiliationConfirmedVellore, domain.CohortEligible, false)

	// queued and approved first, in the campaign and in an email campaign
	_, err := svc.BuildQueue(v.ctx, u)
	require.NoError(t, err)
	c, _ := v.camp.EnsureVITAlumniCampaign(v.ctx, u)
	var tid uuid.UUID
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `SELECT id FROM outreach_targets WHERE campaign_id = $1 AND person_id = $2`, c.ID, pid).Scan(&tid))
	mail := v.emailCampaign(t, u, "mail")
	var url string
	require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `SELECT profile_url FROM canonical_entities WHERE id = $1`, pid).Scan(&url))
	mailTarget := v.target(t, u, mail, pid, url, "hello")
	require.NoError(t, v.camp.UpdateTargetStatus(v.ctx, u, mailTarget, domain.TargetStatusApproved))

	_, err = v.env.DB.Pool.Exec(v.ctx, `UPDATE canonical_entities SET do_not_contact = true WHERE id = $1`, pid)
	require.NoError(t, err)

	// drafting is refused
	p, err := repository.NewPeopleRepository(v.env.DB.Pool).Get(v.ctx, u, pid)
	require.NoError(t, err)
	_, err = service.GenerateVITAlumniDraft(p, domain.OwnerProfile{})
	require.ErrorIs(t, err, domain.ErrConflict)

	// approving and marking sent are refused
	require.ErrorIs(t, v.camp.UpdateTargetStatus(v.ctx, u, tid, domain.TargetStatusApproved), domain.ErrConflict)
	require.ErrorIs(t, v.camp.UpdateTargetStatus(v.ctx, u, mailTarget, domain.TargetStatusSentByUser), domain.ErrConflict)
	require.ErrorIs(t, v.camp.UpdateTargetStatus(v.ctx, u, tid, domain.TargetStatusSentByUser), domain.ErrConflict)

	// the sender never claims the target
	claim, err := v.out.ClaimNextToSend(v.ctx, u, mail, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, claim.Target)

	// building the queue again does not queue or change anything for this person
	res, err := svc.BuildQueue(v.ctx, u)
	require.NoError(t, err)
	require.Equal(t, 1, res.SkippedDNC)
	require.Zero(t, res.Queued)
}

func TestAlumniHTTPPathsRefuseDoNotContact(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	user, jwt := p.setup(t)
	p.saveAlumniList(t, user, alumniFixture)
	doJSON(t, p.ts, "POST", "/api/v1/alumni/import", jwt, nil, nil)
	meera := p.list(t, jwt, "?company=razorpay&grad_year_max=2020")[0]
	code, body := doJSON(t, p.ts, "PATCH", "/api/v1/alumni/"+meera.ID.String(), jwt, nil, map[string]any{"do_not_contact": true})
	require.Equal(t, http.StatusOK, code, string(body))

	code, _ = doJSON(t, p.ts, "GET", "/api/v1/alumni/"+meera.ID.String()+"/draft", jwt, nil, nil)
	require.Equal(t, http.StatusConflict, code)
	code, _ = doJSON(t, p.ts, "POST", "/api/v1/alumni/"+meera.ID.String()+"/outreach", jwt, nil, map[string]string{"draft_text": "Hi", "channel": "email"})
	require.Equal(t, http.StatusConflict, code)

	// imported tracker rows carry no confirmed campus, so building the queue queues nobody
	code, body = doJSON(t, p.ts, "POST", "/api/v1/campaigns/vit-alumni/build-queue", jwt, nil, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	require.Contains(t, string(body), `"queued":0`)

	// the approved alumni DM is held when the person is do-not-contact
	var cancelled []string
	cancel := func(_ context.Context, _ uuid.UUID, why string) error { cancelled = append(cancelled, why); return nil }
	lists := &service.TrackerLists{Store: p.storage, Alumni: &service.AlumniPeople{Lists: p.storage, People: p.people}}
	_, out := lists.RunInCode(database.WithSystem(context.Background()), approvedDMTask(user, "https://www.linkedin.com/in/meera"), cancel, nil)
	require.Equal(t, service.Dropped, out)
	require.Len(t, cancelled, 1)
}
