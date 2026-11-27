package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// Evidence recorded on a person survives the next import of the Alumni list, and the evidence check remembers
// which page and quote it already checked.
func TestEvidenceSurvivesReimportAndIsCheckedOnce(t *testing.T) {
	v := newVitCtl(t)
	user := CreateTestUser(t, v.env.DB, "evidence@example.com")
	people := repository.NewPeopleRepository(v.env.DB.Pool)
	row := domain.AlumniImportRow{Name: "Evidence Person", Company: "Beta", Role: "Software Engineer", Club: "ACM VIT",
		ProfileURL: "https://www.linkedin.com/in/evidence-person/", GraduationYear: 2021, ContactState: domain.ContactNotContacted}
	_, err := people.ImportAlumni(v.ctx, user, []domain.AlumniImportRow{row})
	require.NoError(t, err)

	st, err := people.EvidenceStateByProfile(v.ctx, user, row.ProfileURL)
	require.NoError(t, err)
	require.Empty(t, st.Checked)

	row.EvidenceURL, row.Quote = "https://example.org/team", "B.Tech CSE, VIT Vellore (2017-2021), ACM VIT core team"
	ver, ok := domain.EvidenceVerification(row, "Team: B.Tech CSE, VIT Vellore (2017-2021), ACM VIT core team")
	require.True(t, ok)
	fields, err := ver.Resolve(time.Now())
	require.NoError(t, err)
	_, err = people.ApplyVerification(v.ctx, user, st.ID, fields)
	require.NoError(t, err)
	require.NoError(t, people.MarkEvidenceChecked(v.ctx, user, st.ID, "key-1", "verified"))

	// the daily import runs again with the tracker's (unverified) view of the person
	_, err = people.ImportAlumni(v.ctx, user, []domain.AlumniImportRow{row})
	require.NoError(t, err)
	p, err := people.Get(v.ctx, user, st.ID)
	require.NoError(t, err)
	require.Equal(t, domain.AffiliationConfirmedVellore, p.VITAffiliation, "a re-import must not undo verified evidence")
	require.Equal(t, "confirmed", p.ACMAffiliation)
	require.Equal(t, domain.CohortEligible, p.GraduationEligibility, "2021 is before the 26 Nov 2022 cutoff")

	st, err = people.EvidenceStateByProfile(v.ctx, user, "https://linkedin.com/in/evidence-person")
	require.NoError(t, err)
	require.Equal(t, "key-1", st.Checked)
	_, err = people.EvidenceStateByProfile(v.ctx, CreateTestUser(t, v.env.DB, "evidence-b@example.com"), row.ProfileURL)
	require.ErrorIs(t, err, domain.ErrNotFound, "another user never sees this person")
}
