package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerificationResolve(t *testing.T) {
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	site, li := "https://vit.ac.in/alumni/x", "https://www.linkedin.com/in/x"

	r, err := PersonVerification{Campus: &CampusClaim{Value: "Vellore", SourceURL: site, Excerpt: "VIT Vellore B.Tech"}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, AffiliationConfirmedVellore, *r.VITAffiliation)
	require.Equal(t, "independent", r.Provenance["campus"].(map[string]any)["source_kind"])
	require.Equal(t, "owner", r.Provenance["campus"].(map[string]any)["verified_by"])

	r, err = PersonVerification{Campus: &CampusClaim{Value: "vellore", SourceURL: li, Excerpt: "VIT"}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, AffiliationUnconfirmed, *r.VITAffiliation, "a LinkedIn source cannot confirm a campus")
	require.Equal(t, SourceKindLinkedInSelfReported, r.Provenance["campus"].(map[string]any)["source_kind"])

	r, err = PersonVerification{Campus: &CampusClaim{Value: "chennai", SourceURL: site, Excerpt: "x"}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, AffiliationOtherCampus, *r.VITAffiliation)

	r, err = PersonVerification{ACM: &ACMClaim{Status: "confirmed", SourceURL: li, Excerpt: "ACM chair"}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, "likely", *r.ACMAffiliation, "a LinkedIn source cannot confirm ACM")
	r, err = PersonVerification{ACM: &ACMClaim{Status: "confirmed", Role: "Chair", SourceURL: site, Excerpt: "ACM chair"}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, "confirmed", *r.ACMAffiliation)

	r, err = PersonVerification{Graduation: &GraduationClaim{Year: 2022, SourceURL: site, Excerpt: "Class of 2022"}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, CohortBorderline2022, EvaluateGraduationEligibility(r.GraduationYear, r.GraduationDate, DefaultCutoffDate))

	r, err = PersonVerification{ContactRoute: &ContactRouteClaim{Value: "me@example.com", SourceURL: site}}.Resolve(now)
	require.NoError(t, err)
	require.Equal(t, "me@example.com", *r.BusinessEmail)
}

func TestVerificationResolveRejectsBadEvidence(t *testing.T) {
	now := time.Now()
	bad := []PersonVerification{
		{},
		{Campus: &CampusClaim{Value: "vellore", SourceURL: "https://a.org", Excerpt: "  "}},
		{Campus: &CampusClaim{Value: "vellore", SourceURL: "not a url", Excerpt: "x"}},
		{Campus: &CampusClaim{Value: "mars", SourceURL: "https://a.org", Excerpt: "x"}},
		{Graduation: &GraduationClaim{Year: 2021, SourceURL: "https://a.org"}},
		{Graduation: &GraduationClaim{Year: 2021, Date: "2020-05-31", SourceURL: "https://a.org", Excerpt: "x"}},
		{ACM: &ACMClaim{Status: "maybe", SourceURL: "https://a.org", Excerpt: "x"}},
		{ContactRoute: &ContactRouteClaim{Value: "me@example.com"}},
		{ContactRoute: &ContactRouteClaim{Value: "nonsense", SourceURL: "https://a.org"}},
	}
	for i, v := range bad {
		_, err := v.Resolve(now)
		require.True(t, errors.Is(err, ErrValidation), "case %d", i)
	}
}
