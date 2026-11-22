package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestListingsAreStoredOnceAndMoveOnlyAlongThePipeline(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "listings@example.com")
	repo := repository.NewListingRepository(env.DB.Pool)

	p := domain.Posting{Company: "Acme", Title: "SDE Intern", Link: "https://unstop.com/internships/acme-1", Location: "Bengaluru, India"}
	l := domain.ListingFromPosting(owner, "unstop", "A", p, time.Now())
	created, err := repo.Upsert(ctx, l)
	if err != nil || !created || l.Stage != domain.StageVerified {
		t.Fatalf("first upsert: %v %v %+v", created, err, l)
	}
	again := domain.ListingFromPosting(owner, "unstop", "A", p, time.Now())
	if created, err := repo.Upsert(ctx, again); err != nil || created || again.ID != l.ID {
		t.Fatalf("the same posting is one listing: %v %v", created, err)
	}

	if err := repo.Move(ctx, owner, l.ID, domain.StageVerified, domain.StageSubmitting, "skip", nil); !errors.Is(err, repository.ErrIllegalMove) {
		t.Fatalf("nothing is submitted without a draft and an approval: %v", err)
	}
	if err := repo.Move(ctx, owner, l.ID, domain.StageVerified, domain.StageDrafted, "drafted", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.Move(ctx, owner, l.ID, domain.StageVerified, domain.StageDrafted, "twice", nil); !errors.Is(err, repository.ErrIllegalMove) {
		t.Fatalf("a second worker moving the same listing from the same stage loses: %v", err)
	}
	if err := repo.Move(ctx, owner, l.ID, domain.StageDrafted, domain.StageClosed, "posting gone", nil); err != nil {
		t.Fatal(err)
	}
	reopened := domain.ListingFromPosting(owner, "unstop", "A", p, time.Now())
	if _, err := repo.Upsert(ctx, reopened); err != nil || reopened.Stage != domain.StageVerified {
		t.Fatalf("a closed listing whose posting is back reopens: %v %+v", err, reopened)
	}
	all, err := repo.List(ctx, owner, []string{domain.StageVerified}, 10)
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %v %d", err, len(all))
	}
}
