package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func fixtureProfile() OwnerProfile {
	return OwnerProfile{
		FullName: "Test Candidate", Email: "t@example.com", School: "U", Degree: "B.Tech", GradYear: "2028",
		Facts: []ProfileFact{
			{ID: "a", Project: "Sched", Text: "Built a Go scheduler on Redis", Source: "s", Tags: []string{"go", "redis"}},
			{ID: "b", Project: "Dash", Text: "Built a React dashboard", Source: "s", Tags: []string{"react"}},
			{ID: "c", Project: "Graph", Text: "Designed a knowledge graph", Source: "s", Tags: []string{"graph", "llm"}},
		},
	}
}

func TestRelevantFactsRanksVerbatimAndOmitsUnsupported(t *testing.T) {
	p := fixtureProfile()
	got := p.RelevantFacts("Backend Go Intern working with Redis", 3)
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "Built a Go scheduler on Redis", got[0].Text, "facts are returned verbatim")
	for _, f := range got {
		assert.NotEqual(t, "b", f.ID, "an unrelated fact is not padded in")
	}
	assert.Empty(t, p.RelevantFacts("Marketing Manager", 3), "no overlap means no claims")
}

func TestOwnerProfileValidateAndNames(t *testing.T) {
	p := fixtureProfile()
	assert.NoError(t, p.Validate())
	assert.Equal(t, "Test", p.FirstName())
	assert.Equal(t, "Candidate", p.LastName())

	bad := fixtureProfile()
	bad.Facts[1].Source = ""
	assert.Error(t, bad.Validate(), "a fact without a source is rejected")
	dup := fixtureProfile()
	dup.Facts[1].ID = "a"
	assert.Error(t, dup.Validate())
	noYear := fixtureProfile()
	noYear.GradYear = ""
	assert.Error(t, noYear.Validate())
}
