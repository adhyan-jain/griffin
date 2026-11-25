package tests

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestOwnerVerificationUnlocksTheQueue(t *testing.T) {
	v := newVitCtl(t)
	a := CreateTestUser(t, v.env.DB, "verify-a@example.com")
	b := CreateTestUser(t, v.env.DB, "verify-b@example.com")
	svc := v.queueSvc()
	people := repository.NewPeopleRepository(v.env.DB.Pool)

	// a self-reported person, as the tracker import stores them
	selfReported := func(slug string) uuid.UUID {
		return v.queuePerson(t, a, slug, domain.AffiliationUnconfirmed, domain.CohortUnknown, false)
	}
	verify := func(user, id uuid.UUID, campusSrc string) error {
		fields, err := domain.PersonVerification{
			Campus:     &domain.CampusClaim{Value: "vellore", SourceURL: campusSrc, Excerpt: "B.Tech, VIT Vellore"},
			Graduation: &domain.GraduationClaim{Year: 2021, SourceURL: campusSrc, Excerpt: "Class of 2021"},
		}.Resolve(time.Now())
		require.NoError(t, err)
		_, err = people.ApplyVerification(v.ctx, user, id, fields)
		return err
	}
	queued := func(id uuid.UUID) bool {
		var n int
		require.NoError(t, v.env.DB.Pool.QueryRow(v.ctx, `SELECT count(*) FROM outreach_targets WHERE person_id = $1`, id).Scan(&n))
		return n > 0
	}

	good, viaLinkedIn := selfReported("verified"), selfReported("linkedin-only")
	require.NoError(t, verify(a, good, "https://vit.ac.in/alumni/example"))
	require.NoError(t, verify(a, viaLinkedIn, "https://www.linkedin.com/in/example-linkedin-only"))

	res, err := svc.BuildQueue(v.ctx, a)
	require.NoError(t, err)
	require.Equal(t, 1, res.Queued)
	require.True(t, queued(good))
	require.False(t, queued(viaLinkedIn), "a LinkedIn self-report must not confirm the campus")

	p, err := people.Get(v.ctx, a, good)
	require.NoError(t, err)
	require.Equal(t, domain.AffiliationConfirmedVellore, p.VITAffiliation)
	require.Equal(t, domain.CohortEligible, p.GraduationEligibility)
	prov := p.Metadata["verification"].(map[string]any)["campus"].(map[string]any)
	require.Equal(t, "owner", prov["verified_by"])
	require.Equal(t, "independent", prov["source_kind"])

	// another user cannot verify this person
	require.ErrorIs(t, verify(b, good, "https://vit.ac.in/alumni/example"), domain.ErrNotFound)
}
