package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// These tests run the people store against the real schema: the classification lives in typed columns, a person is
// one row per canonical LinkedIn URL, and nothing is merged by name.

func vitPerson(name, url, company, title string) *domain.Person {
	return &domain.Person{Name: name, ProfileURL: &url, Company: company, Title: title}
}

func TestLinkedInURLVariantsCollapseToOnePerson(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)

	for _, u := range []string{
		"https://www.linkedin.com/in/Asha-Rao/", "http://linkedin.com/in/asha-rao", "https://www.linkedin.com/in/asha-rao?utm_source=x", "linkedin.com/in/asha-rao/#top",
	} {
		_, err := p.people.UpsertPerson(ctx, user, vitPerson("Asha Rao", u, "Razorpay", "Backend Engineer"))
		require.NoError(t, err, u)
	}
	// the tracker import of the same profile joins the same row
	res, err := p.people.ImportAlumni(ctx, user, []domain.AlumniImportRow{{Name: "Asha Rao", ProfileURL: "https://www.linkedin.com/in/asha-rao/", Company: "Razorpay", Role: "Backend Engineer", ContactState: domain.ContactNotContacted}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Updated)
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person'`, user))
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person' AND profile_url='https://linkedin.com/in/asha-rao' AND canonical_key='li:https://linkedin.com/in/asha-rao'`, user))

	// the do-not-contact test and the graduation-year write accept any form of the link
	ok, err := p.people.SetGraduationYear(ctx, user, "https://www.linkedin.com/in/asha-rao/", 2020, nil)
	require.NoError(t, err)
	require.True(t, ok)
	dnc, err := p.people.DoNotContactProfiles(ctx, user)
	require.NoError(t, err)
	require.Empty(t, dnc)
}

func TestSameNameDifferentProfilesStayTwoPeople(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)
	_, err := p.people.UpsertPerson(ctx, user, vitPerson("Sara Iyer", "https://www.linkedin.com/in/sara-iyer", "Google", "ML Engineer"))
	require.NoError(t, err)
	_, err = p.people.UpsertPerson(ctx, user, vitPerson("Sara Iyer", "https://www.linkedin.com/in/sara-iyer-2", "Google", "ML Engineer"))
	require.NoError(t, err)
	require.Equal(t, 2, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person' AND name='Sara Iyer'`, user))
}

func TestJobChangeKeepsEmploymentHistory(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)
	const link = "https://www.linkedin.com/in/dev-rao"

	first, err := p.people.UpsertPerson(ctx, user, vitPerson("Dev Rao", link, "Razorpay", "Backend Engineer"))
	require.NoError(t, err)
	require.Empty(t, mustEmployments(t, p, user, first.ID), "a first sighting is not a job change")

	_, err = p.people.UpsertPerson(ctx, user, vitPerson("Dev Rao", link+"/", "Razorpay", "Backend Engineer"))
	require.NoError(t, err)
	require.Empty(t, mustEmployments(t, p, user, first.ID), "the same job again is not a change")

	_, err = p.people.UpsertPerson(ctx, user, vitPerson("Dev Rao", link, "Acme Labs", "Staff Engineer"))
	require.NoError(t, err)
	_, err = p.people.UpsertPerson(ctx, user, vitPerson("Dev Rao", link, "Globex", "Staff Engineer"))
	require.NoError(t, err)

	got, err := p.people.Get(ctx, user, first.ID)
	require.NoError(t, err)
	require.Equal(t, "Globex", got.Company, "the person record shows the latest job")

	var current, past []string
	for _, e := range mustEmployments(t, p, user, first.ID) {
		if e.IsCurrent {
			current = append(current, e.EmployerName)
		} else {
			past = append(past, e.EmployerName+"/"+e.Title)
		}
	}
	require.Equal(t, []string{"Globex"}, current, "exactly one current job")
	require.ElementsMatch(t, []string{"Razorpay/Backend Engineer", "Acme Labs/Staff Engineer"}, past)
}

func mustEmployments(t *testing.T, p *peopleEnv, user, person uuid.UUID) []domain.PersonEmployment {
	t.Helper()
	es, err := p.people.ListEmployments(database.WithSystem(context.Background()), user, person)
	require.NoError(t, err)
	return es
}

func TestACMConfirmedWithoutASourceIsStoredAsLikely(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)

	bare := vitPerson("No Source", "https://www.linkedin.com/in/no-source", "Razorpay", "Backend Engineer")
	bare.ACMAffiliation, bare.Campus, bare.VITAffiliation = "confirmed", domain.CampusVellore, domain.AffiliationConfirmedVellore
	got, err := p.people.UpsertPerson(ctx, user, bare)
	require.NoError(t, err)
	require.Equal(t, "likely", got.ACMAffiliation)
	require.NotEqual(t, domain.PriorityP1, got.PriorityTier)
	require.NotEqual(t, domain.PriorityP2, got.PriorityTier, "an unproven ACM claim never reaches P1 or P2")
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE id=$1 AND acm_vit_affiliation='likely'`, got.ID), "the column holds it")

	proven := vitPerson("With Source", "https://www.linkedin.com/in/with-source", "Razorpay", "Backend Engineer")
	proven.ACMAffiliation, proven.ACMSource, proven.ACMEvidenceExcerpt = "confirmed", "https://acmvit.in/team", "Core committee 2019-20"
	proven.Campus, proven.VITAffiliation = domain.CampusVellore, domain.AffiliationConfirmedVellore
	y := 2020
	proven.GraduationYear = &y
	got, err = p.people.UpsertPerson(ctx, user, proven)
	require.NoError(t, err)
	require.Equal(t, "confirmed", got.ACMAffiliation)
	require.Equal(t, domain.PriorityP2, got.PriorityTier)
}

func TestTrackerImportIsSelfReportedAndLandsInReview(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)
	_, err := p.people.ImportAlumni(ctx, user, []domain.AlumniImportRow{
		{Name: "Sara Iyer", GraduationYear: 2021, Company: "Razorpay", Role: "ML Engineer", Club: "ACM VIT", ProfileURL: "https://www.linkedin.com/in/sara/", ContactState: domain.ContactNotContacted},
	})
	require.NoError(t, err)
	people, err := p.people.List(ctx, user, repository.PersonFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, people, 1)
	s := people[0]
	require.Equal(t, "linkedin_self_reported", s.Metadata["source"])
	require.Equal(t, "likely", s.ACMAffiliation, "a club mentioning ACM is never confirmed")
	require.Equal(t, domain.CampusUnknown, s.Campus, "no campus is invented from a club")
	require.Equal(t, domain.AffiliationUnconfirmed, s.VITAffiliation)
	require.Equal(t, domain.PriorityReview, s.PriorityTier)
	require.Equal(t, domain.CohortEligible, s.GraduationEligibility)

	// a better source's classification survives a later import of the same person
	better := vitPerson("Sara Iyer", "https://www.linkedin.com/in/sara", "Razorpay", "ML Engineer")
	better.Campus, better.VITAffiliation = domain.CampusVellore, domain.AffiliationConfirmedVellore
	better.ACMAffiliation, better.ACMSource, better.ACMEvidenceExcerpt = "confirmed", "https://acmvit.in/team", "Member 2020"
	better.Metadata = map[string]any{"source": "acm_vit_site"}
	_, err = p.people.UpsertPerson(ctx, user, better)
	require.NoError(t, err)
	// (the person was self-reported first, so the first import's source is replaced by the better one)
	_, err = p.people.ImportAlumni(ctx, user, []domain.AlumniImportRow{
		{Name: "Sara Iyer", GraduationYear: 2021, Company: "Razorpay", Role: "ML Engineer", Club: "ACM VIT", ProfileURL: "https://www.linkedin.com/in/sara/", ContactState: domain.ContactNotContacted},
	})
	require.NoError(t, err)
	people, err = p.people.List(ctx, user, repository.PersonFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, people, 1)
	require.Equal(t, "confirmed", people[0].ACMAffiliation)
	require.Equal(t, domain.CampusVellore, people[0].Campus)
}

func TestPeopleFiltersCombineOnStoredColumns(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)
	y20, y22, y23 := 2020, 2022, 2023
	mk := func(name, slug, title, campus, acm string, year *int) {
		per := vitPerson(name, "https://www.linkedin.com/in/"+slug, "Razorpay", title)
		per.Campus = campus
		if campus == domain.CampusVellore {
			per.VITAffiliation = domain.AffiliationConfirmedVellore
		}
		per.ACMAffiliation, per.GraduationYear = acm, year
		if acm == "confirmed" {
			per.ACMSource, per.ACMEvidenceExcerpt = "https://acmvit.in/team", "member"
		}
		_, err := p.people.UpsertPerson(ctx, user, per)
		require.NoError(t, err, name)
	}
	mk("Vellore ACM Tech", "a", "Backend Engineer", domain.CampusVellore, "confirmed", &y20)
	mk("Vellore ACM Ops", "b", "Operations Manager", domain.CampusVellore, "confirmed", &y20)
	mk("Vellore Plain Tech", "c", "Software Engineer", domain.CampusVellore, "unknown", &y22)
	mk("Chennai ACM Tech", "d", "Backend Engineer", domain.CampusChennai, "confirmed", &y20)
	mk("Vellore ACM Tech Late", "e", "Backend Engineer", domain.CampusVellore, "confirmed", &y23)

	names := func(f repository.PersonFilter) []string {
		f.Limit = 50
		ps, err := p.people.List(ctx, user, f)
		require.NoError(t, err)
		var out []string
		for _, x := range ps {
			out = append(out, x.Name)
		}
		return out
	}
	require.ElementsMatch(t, []string{"Vellore ACM Tech", "Vellore ACM Tech Late"},
		names(repository.PersonFilter{Campus: "vellore", ACMAffiliation: "confirmed", ProfessionalFunction: "technology"}))
	require.Equal(t, []string{"Vellore ACM Tech"},
		names(repository.PersonFilter{Campus: "vellore", ACMAffiliation: "confirmed", ProfessionalFunction: "technology", GradEligibility: domain.CohortEligible}))
	require.Equal(t, []string{"Vellore Plain Tech"}, names(repository.PersonFilter{GradEligibility: domain.CohortBorderline2022}))
	require.Equal(t, []string{"Vellore ACM Ops"}, names(repository.PersonFilter{Campus: "vellore", ProfessionalFunction: "operations"}))
	require.Equal(t, []string{"Chennai ACM Tech"}, names(repository.PersonFilter{Campus: "chennai"}))
	min := 80
	require.Len(t, names(repository.PersonFilter{MinClassificationConfidence: &min}), 5, "every title here is classified at 85")
	min = 90
	require.Empty(t, names(repository.PersonFilter{MinClassificationConfidence: &min}))
	require.Len(t, names(repository.PersonFilter{ContactabilityStatus: "unknown"}), 5)
	require.Empty(t, names(repository.PersonFilter{ContactabilityStatus: "profile_only"}))

	// the priority sort reads the stored score: the P2 people come before the REVIEW people
	ps, err := p.people.List(ctx, user, repository.PersonFilter{SortByPriority: true, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, domain.PriorityP2, ps[0].PriorityTier)
	require.Equal(t, domain.PriorityReview, ps[len(ps)-1].PriorityTier)
}
