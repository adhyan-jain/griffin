package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// testOwnerProfile is a synthetic fixture. It is deliberately not the real owner.
func testOwnerProfile() *domain.OwnerProfile {
	return &domain.OwnerProfile{
		FullName: "Test Candidate", Email: "test.candidate@example.com", Phone: "+00 000 000 0000",
		GitHub: "https://github.com/test-candidate", LinkedIn: "https://linkedin.com/in/test-candidate",
		Portfolio: "https://github.com/test-candidate", School: "Example University", Degree: "B.Tech in Computer Science",
		GradYear: "2028", CGPA: "8.0/10", Country: "India", ResumeFile: "Candidate_Resume.pdf",
		Facts: []domain.ProfileFact{
			{ID: "fx-sched", Project: "Sched", Text: "Built a Go scheduler that leases jobs through Redis", Source: "fixture", Tags: []string{"go", "redis", "systems", "distributed"}},
			{ID: "fx-ui", Project: "Dash", Text: "Built a React dashboard", Source: "fixture", Tags: []string{"react", "frontend"}},
		},
	}
}

func TestOwnerProfileVersioningAndTenantIsolation(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	a := CreateTestUser(t, env.DB, "profile-a@example.com")
	b := CreateTestUser(t, env.DB, "profile-b@example.com")
	repo := repository.NewOwnerProfileRepository(env.DB.Pool)

	none, err := repo.Latest(ctx, a)
	require.NoError(t, err)
	assert.Nil(t, none)

	p := testOwnerProfile()
	v1, err := repo.Save(ctx, a, p, "t")
	require.NoError(t, err)
	p.GradYear = "2029"
	v2, err := repo.Save(ctx, a, p, "t")
	require.NoError(t, err)
	assert.Equal(t, 1, v1)
	assert.Equal(t, 2, v2)

	got, err := repo.Latest(ctx, a)
	require.NoError(t, err)
	assert.Equal(t, "2029", got.GradYear)
	assert.Equal(t, 2, got.Version)

	other, err := repo.Latest(ctx, b)
	require.NoError(t, err)
	assert.Nil(t, other, "another user's profile must be invisible")
}
