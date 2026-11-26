package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type vitCtl struct {
	ctx  context.Context
	env  *TestEnv
	camp *repository.CampaignRepository
	out  *repository.OutreachRepository
}

func newVitCtl(t *testing.T) *vitCtl {
	env := SetupTestEnv(t)
	t.Cleanup(env.Teardown)
	return &vitCtl{ctx: context.Background(), env: env,
		camp: repository.NewCampaignRepository(env.DB.Pool), out: repository.NewOutreachRepository(env.DB.Pool)}
}

func (v *vitCtl) person(t *testing.T, user uuid.UUID, slug string) (uuid.UUID, string) {
	t.Helper()
	url := "https://www.linkedin.com/in/example-" + slug
	var id uuid.UUID
	if err := v.env.DB.Pool.QueryRow(v.ctx, `INSERT INTO canonical_entities (user_id, kind, canonical_key, name, profile_url)
		VALUES ($1, 'person', $2, $3, $2) RETURNING id`, user, url, "Example "+slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, url
}

func (v *vitCtl) emailCampaign(t *testing.T, user uuid.UUID, name string) uuid.UUID {
	t.Helper()
	lim := domain.DefaultOutreachLimits("email")
	lim.QuietStartHour, lim.QuietEndHour, lim.MinGapSeconds, lim.DailyCap = 0, 0, 0, 5
	c := &domain.OutreachCampaign{UserID: user, Name: name, Goal: "g", Channel: "email", Limits: lim}
	if err := v.out.CreateCampaign(v.ctx, c); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (v *vitCtl) target(t *testing.T, user, camp, person uuid.UUID, url, draft string) uuid.UUID {
	t.Helper()
	err := v.camp.UpsertVITCampaignTarget(v.ctx, user, &domain.VITCampaignTarget{CampaignID: camp, PersonID: person,
		Name: "Example", ProfileURL: url, Company: "Acme", Title: "Eng", ShortNote: draft, ExtendedStatus: domain.TargetStatusDrafted})
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := v.env.DB.Pool.QueryRow(v.ctx, `SELECT id FROM outreach_targets WHERE campaign_id=$1 AND person_id=$2`, camp, person).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (v *vitCtl) state(t *testing.T, id uuid.UUID) (ext, draft string) {
	t.Helper()
	if err := v.env.DB.Pool.QueryRow(v.ctx, `SELECT extended_status, draft FROM outreach_targets WHERE id=$1`, id).Scan(&ext, &draft); err != nil {
		t.Fatal(err)
	}
	return
}

func TestVITCampaignOtherUserCannotUpdateTarget(t *testing.T) {
	v := newVitCtl(t)
	a := CreateTestUser(t, v.env.DB, "vit-a@example.com")
	b := CreateTestUser(t, v.env.DB, "vit-b@example.com")
	c, err := v.camp.EnsureVITAlumniCampaign(v.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	p, url := v.person(t, a, "a1")
	id := v.target(t, a, c.ID, p, url, "hello")

	if err := v.camp.UpdateTargetStatus(v.ctx, b, id, domain.TargetStatusApproved); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if ext, _ := v.state(t, id); ext != domain.TargetStatusDrafted {
		t.Fatalf("owner's row changed: %s", ext)
	}
}

func TestVITCampaignIsNeverAutoSent(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-claim@example.com")
	c, _ := v.camp.EnsureVITAlumniCampaign(v.ctx, u)
	p, url := v.person(t, u, "c1")
	id := v.target(t, u, c.ID, p, url, "hello")
	if err := v.camp.UpdateTargetStatus(v.ctx, u, id, domain.TargetStatusApproved); err != nil {
		t.Fatal(err)
	}
	claim, err := v.out.ClaimNextToSend(v.ctx, u, c.ID, time.Now().UTC())
	if err != nil || claim.Target != nil || claim.Reason == "" {
		t.Fatalf("a LinkedIn campaign must never hand out a target: %+v %v", claim, err)
	}
}

func TestOutreachClaimSkipsDoNotContactPerson(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-dnc@example.com")
	c := v.emailCampaign(t, u, "mail")
	p, url := v.person(t, u, "d1")
	id := v.target(t, u, c, p, url, "hello")
	if err := v.camp.UpdateTargetStatus(v.ctx, u, id, domain.TargetStatusApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := v.env.DB.Pool.Exec(v.ctx, `UPDATE canonical_entities SET do_not_contact = true WHERE id = $1`, p); err != nil {
		t.Fatal(err)
	}
	claim, err := v.out.ClaimNextToSend(v.ctx, u, c, time.Now().UTC())
	if err != nil || claim.Target != nil {
		t.Fatalf("a do-not-contact person must not be claimed: %+v %v", claim, err)
	}
}

func TestVITCampaignReupsertKeepsApprovedTarget(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-reup@example.com")
	c, _ := v.camp.EnsureVITAlumniCampaign(v.ctx, u)
	p, url := v.person(t, u, "r1")
	id := v.target(t, u, c.ID, p, url, "original")
	if err := v.camp.UpdateTargetStatus(v.ctx, u, id, domain.TargetStatusApproved); err != nil {
		t.Fatal(err)
	}
	v.target(t, u, c.ID, p, url, "regenerated")
	if ext, draft := v.state(t, id); ext != domain.TargetStatusApproved || draft != "original" {
		t.Fatalf("approved target was overwritten: %s %q", ext, draft)
	}
}

func TestVITCampaignRefusesInvalidTransition(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-sm@example.com")
	c, _ := v.camp.EnsureVITAlumniCampaign(v.ctx, u)
	p, url := v.person(t, u, "s1")
	id := v.target(t, u, c.ID, p, url, "hello")
	for _, s := range []string{domain.TargetStatusApproved, domain.TargetStatusSentByUser} {
		if err := v.camp.UpdateTargetStatus(v.ctx, u, id, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.camp.UpdateTargetStatus(v.ctx, u, id, domain.TargetStatusApproved); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("approve after sent must be refused, got %v", err)
	}
	if ext, _ := v.state(t, id); ext != domain.TargetStatusSentByUser {
		t.Fatalf("state changed: %s", ext)
	}
}

func TestVITCampaignRefusesDuplicateAcrossCampaigns(t *testing.T) {
	v := newVitCtl(t)
	u := CreateTestUser(t, v.env.DB, "vit-dup@example.com")
	vit, _ := v.camp.EnsureVITAlumniCampaign(v.ctx, u)
	other := v.emailCampaign(t, u, "mail")
	p, url := v.person(t, u, "x1")
	first := v.target(t, u, vit.ID, p, url, "hello")
	second := v.target(t, u, other, p, url, "hello")
	for _, s := range []string{domain.TargetStatusApproved, domain.TargetStatusSentByUser} {
		if err := v.camp.UpdateTargetStatus(v.ctx, u, first, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.camp.UpdateTargetStatus(v.ctx, u, second, domain.TargetStatusApproved); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate approval must be refused, got %v", err)
	}
	// a new target for the same person elsewhere is skipped, not inserted
	third := v.emailCampaign(t, u, "mail2")
	if err := v.camp.UpsertVITCampaignTarget(v.ctx, u, &domain.VITCampaignTarget{CampaignID: third, PersonID: p, Name: "Example", ProfileURL: url, ExtendedStatus: domain.TargetStatusDrafted}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = v.env.DB.Pool.QueryRow(v.ctx, `SELECT count(*) FROM outreach_targets WHERE campaign_id=$1`, third).Scan(&n)
	if n != 0 {
		t.Fatalf("duplicate upsert inserted %d rows", n)
	}
}
