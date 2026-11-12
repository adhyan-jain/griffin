package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestOutreachLifecycleAndLimits(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "outreach@example.com")
	repo := repository.NewOutreachRepository(env.DB.Pool)

	lim := domain.DefaultOutreachLimits("email")
	lim.QuietStartHour, lim.QuietEndHour, lim.MinGapSeconds, lim.DailyCap = 0, 0, 0, 2
	c := &domain.OutreachCampaign{UserID: u, Name: "Founders", Goal: "cold mail", Channel: "email", Limits: lim}
	if err := repo.CreateCampaign(ctx, c); err != nil {
		t.Fatal(err)
	}

	added, err := repo.UpsertTargets(ctx, u, c.ID, []domain.OutreachTarget{
		{Name: "Ada", Handle: "ada@x.com"}, {Name: "Bo", Handle: "bo@x.com"}, {Name: "Ada again", Handle: "ADA@x.com"}, {Name: "", Handle: "ada@x.com", Info: "CEO"},
	})
	if err != nil || added != 3 { // ADA@x.com differs by case in the unique key; the last upsert fills info only
		t.Fatalf("added=%d err=%v", added, err)
	}

	list, _ := repo.ListTargets(ctx, u, c.ID, nil)
	var ada *domain.OutreachTarget
	for _, x := range list {
		if x.Handle == "ada@x.com" {
			ada = x
		}
	}
	if ada == nil || ada.Info != "CEO" || ada.Name != "Ada" {
		t.Fatalf("upsert must fill blanks and keep existing: %+v", ada)
	}

	approved, drafted := domain.OutreachApproved, domain.OutreachDrafted
	if _, err := repo.UpdateTarget(ctx, u, ada.ID, repository.TargetUpdate{To: &approved}); err == nil {
		t.Fatal("found -> approved must be refused: nothing has been drafted")
	}
	draft := "Hi Ada, ..."
	if _, err := repo.UpdateTarget(ctx, u, ada.ID, repository.TargetUpdate{To: &drafted, Draft: &draft, Event: "drafted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateTarget(ctx, u, ada.ID, repository.TargetUpdate{To: &approved, Event: "approved by owner"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claim, err := repo.ClaimNextToSend(ctx, u, c.ID, now)
	if err != nil || claim.Target == nil || claim.Target.ID != ada.ID {
		t.Fatalf("expected Ada to be claimed: %+v %v", claim, err)
	}
	again, _ := repo.ClaimNextToSend(ctx, u, c.ID, now)
	if again.Target != nil {
		t.Fatal("a leased person must not be handed out twice")
	}

	sent := domain.OutreachSent
	follow := now.Add(5 * 24 * time.Hour)
	got, err := repo.UpdateTarget(ctx, u, ada.ID, repository.TargetUpdate{To: &sent, NextFollowupAt: &follow, Event: "sent"})
	if err != nil || got.SentAt == nil || got.FollowupsSent != 0 {
		t.Fatalf("sent: %+v %v", got, err)
	}

	r, err := repo.RecordReplyByHandle(ctx, u, "Ada@X.com", "sounds good")
	if err != nil || r == nil || r.Status != domain.OutreachReplied || r.ReplyText != "sounds good" {
		t.Fatalf("reply not recorded: %+v %v", r, err)
	}
	if none, _ := repo.RecordReplyByHandle(ctx, u, "nobody@x.com", "x"); none != nil {
		t.Fatal("a reply from a stranger must not match anyone")
	}
}

func TestOutreachDailyCapHoldsUnderConcurrentSenders(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "outreach_cap@example.com")
	repo := repository.NewOutreachRepository(env.DB.Pool)

	lim := domain.DefaultOutreachLimits("email")
	lim.QuietStartHour, lim.QuietEndHour, lim.MinGapSeconds, lim.DailyCap = 0, 0, 0, 3
	c := &domain.OutreachCampaign{UserID: u, Name: "Cap", Channel: "email", Limits: lim}
	if err := repo.CreateCampaign(ctx, c); err != nil {
		t.Fatal(err)
	}
	var people []domain.OutreachTarget
	for _, h := range []string{"a", "b", "c", "d", "e", "f"} {
		people = append(people, domain.OutreachTarget{Name: h, Handle: h + "@x.com"})
	}
	if _, err := repo.UpsertTargets(ctx, u, c.ID, people); err != nil {
		t.Fatal(err)
	}
	list, _ := repo.ListTargets(ctx, u, c.ID, nil)
	drafted, approved := domain.OutreachDrafted, domain.OutreachApproved
	for _, x := range list {
		d := "hi"
		if _, err := repo.UpdateTarget(ctx, u, x.ID, repository.TargetUpdate{To: &drafted, Draft: &d}); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.UpdateTarget(ctx, u, x.ID, repository.TargetUpdate{To: &approved}); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c2, err := repo.ClaimNextToSend(ctx, u, c.ID, time.Now().UTC())
			if err != nil {
				t.Errorf("claim error: %v", err)
			}
			if err == nil && c2.Target == nil {
				t.Logf("no target: %s", c2.Reason)
			}
			if err == nil && c2.Target != nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if won != 3 {
		t.Fatalf("six senders, a cap of three: %d were handed a person", won)
	}
}
