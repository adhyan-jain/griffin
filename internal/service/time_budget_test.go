package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
)

// Every routine that runs inside a host claim must finish with a normal summary when little time is left, never
// hang and never fail: the claim request is killed by AWS Lambda at 29 seconds, which leaves the task RUNNING.

func tightCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second) // under the 5 second reserve at once
	t.Cleanup(cancel)
	return ctx
}

func TestEveryCodeRoutineFinishesWithASummaryWhenTimeIsShort(t *testing.T) {
	var fetches int32
	count := func(context.Context, string) ([]byte, error) {
		atomic.AddInt32(&fetches, 1)
		return nil, errors.New("must not be asked: no time is left")
	}
	asked := func(context.Context, string) (string, error) {
		atomic.AddInt32(&fetches, 1)
		return "", errors.New("must not be asked: no time is left")
	}
	assessStore := newMemAssess()
	for i := 0; i < 3; i++ {
		assessStore.add("Co"+strings.Repeat("x", i+1), "Backend Intern", "Pune, India", "", strongDesc+strings.Repeat(" more", i), domain.SourceKindEmployerATS)
	}
	listings := &memListings{rows: []*domain.Listing{aggregatorRow(sources.Internshala, "Co", isLink1, time.Hour)}}
	model := goodModel()
	alumni := domain.AlumniHeader + "\n| Sara Iyer | 2024 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | intern | not contacted | 2026-11-20 |"

	for _, c := range []struct {
		name string
		w    *TrackerLists
		task string
		want string
	}{
		{"LISTINGS SYNC", &TrackerLists{Store: &memList{}, Sync: &ListingSync{Listings: listings, Get: count, Details: &ListingDetails{Store: listings, Get: count}}},
			ListingsSyncTitle, "Left for the next run"},
		{"COMMUNITY SCAN", &TrackerLists{Store: &commStore{}, CommunityGet: count, ReplyModel: model,
			OwnerFacts: func(context.Context, uuid.UUID) string { return ownerFactsFixture }}, CommunityScanTitle, "pages left for the next run"},
		{"OPPORTUNITY ASSESS", &TrackerLists{Assess: &OpportunityAssess{Store: assessStore}}, OpportunityAssessTitle, "3 more wait for the next run"},
		{"LISTING DETAILS", &TrackerLists{Details: &ListingDetails{Store: listings, Get: count}}, ListingDetailsTitle, "1 listings are left for the next run"},
		{"HACKATHON TRACKER", &TrackerLists{Store: newTrackerLists(map[string]string{"hackathons": "keep me"}), Hackathons: &HackathonSync{Get: count,
			Post: func(context.Context, string, []byte) ([]byte, error) {
				atomic.AddInt32(&fetches, 1)
				return nil, errors.New("no time")
			}}},
			"HACKATHON TRACKER (daily)", "left for the next run"},
		{"OPEN-SOURCE WATCH", &TrackerLists{Store: newTrackerLists(map[string]string{"opensource": "keep me"}), OpenSource: &OpenSourceSync{Get: count}},
			"OPEN-SOURCE WATCH (weekly)", "left for the next run"},
		{"REPO HYGIENE", &TrackerLists{Store: newTrackerLists(map[string]string{"hygiene": "keep me"}), Hygiene: &HygieneSync{Login: "someone", Get: count}},
			"REPO HYGIENE (monthly)", "waits for the next run"},
		{"INTERNDOCK SYNC", &TrackerLists{Store: &memList{}}, domain.InternDockSyncTitle, "pages left for the next run"},
		{"ALUMNI DM BATCH", &TrackerLists{Store: alumniStore{alumni}}, domain.AlumniDMBatchTitle, "Sara Iyer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := atomic.LoadInt32(&fetches)
			var answer string
			var how CodeOutcome
			if c.name == "INTERNDOCK SYNC" {
				answer, _ = c.w.SyncInternDock(tightCtx(t), routineTask(c.task), asked)
				how = Answered
			} else {
				answer, how = c.w.RunInCode(tightCtx(t), routineTask(c.task), nil, nil)
			}
			if how != Answered || !strings.Contains(answer, c.want) {
				t.Fatalf("a normal summary is expected (%q): %v %s", c.want, how, answer)
			}
			if atomic.LoadInt32(&fetches) != before {
				t.Fatal("no step is started when no time is left")
			}
		})
	}
	if model.calls != 0 {
		t.Fatalf("no reply is written without time: %d model calls", model.calls)
	}
	if listings.rows[0].Description != "" {
		t.Fatal("nothing was fetched, nothing was invented")
	}
}

func TestListingSyncUnderADeadlineReadsSomePagesAndLeavesTheRest(t *testing.T) {
	shrinkBudget(t)
	var calls int32
	slow := func(context.Context, string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(60 * time.Millisecond)
		return nil, errors.New("offline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	res, err := (&ListingSync{Listings: &memListings{}, Get: slow}).Run(ctx, uuid.New())
	n := int(atomic.LoadInt32(&calls))
	if err != nil || n == 0 || res.PagesLeft == 0 || len(res.Problems) != n {
		t.Fatalf("some pages read (%d), the rest left (%d), no error: %+v %v", n, res.PagesLeft, res, err)
	}
	if !strings.Contains(res.Summary(), fmt.Sprintf("%d pages not read", res.PagesLeft)) {
		t.Fatal(res.Summary())
	}
}

// threadList is a Communities list with n older threads that have no reply yet, newest first.
func threadList(n int, status string, posted string) string {
	var rows []string
	for i := 0; i < n; i++ {
		rows = append(rows, fmt.Sprintf("| Reddit | r/golang | How do I run a task queue %d? | why | No | %s | %s | https://www.reddit.com/r/golang/comments/old%d/q/ | %s |", i, posted, noReply, i, status))
	}
	return "# Communities tracker\n\n" + CommunityHeader + "\n" + strings.Join(rows, "\n") + "\n"
}

func backfillModel() *replyModel {
	return &replyModel{answer: func(p string) string {
		switch {
		case strings.Contains(p, "agent framework"):
			return jsonReply(hnReply)
		case strings.Contains(p, "after helping"):
			return jsonReply(redditReply)
		}
		return jsonReply(plainReply)
	}}
}

func TestOlderThreadsWithoutAReplyAreBackfilledNewestFirstWithinTheRunsCap(t *testing.T) {
	store := &commStore{exists: true, text: threadList(20, "new", "2026-11-20 08:00 UTC")}
	model := backfillModel()
	var asked []string
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	rows := tableRows(store.text)
	if len(rows) != 22 || model.calls != communityReplyCap {
		t.Fatalf("2 new threads and 13 older ones share the 15-call cap: %d rows, %d calls", len(rows), model.calls)
	}
	for i, r := range rows {
		c := cells(r)
		filled := c[6] != noReply
		switch {
		case i < 2 && !filled:
			t.Errorf("new thread %d has no reply: %v", i, c)
		case i >= 2 && i < 15 && !filled:
			t.Errorf("row %d is among the 13 newest older threads and is filled in: %v", i, c)
		case i >= 15 && filled:
			t.Errorf("row %d is beyond the cap and still waits: %v", i, c)
		}
		if len(c) != 9 || c[8] != "new" {
			t.Fatalf("the row keeps its shape and status: %v", c)
		}
	}
	// The fetch used the row's own link: the thread text reached the model.
	if !strings.Contains(strings.Join(model.prompts, "\n"), "How do I run a task queue 12?") || strings.Contains(strings.Join(model.prompts, "\n"), "task queue 13?") {
		t.Fatal("the thirteen newest older threads were drafted, no more")
	}

	// The next run fills in the other seven; the run after that has nothing to do.
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	if model.calls != communityReplyCap+7 {
		t.Fatalf("seven more drafts: %d calls", model.calls)
	}
	for _, r := range tableRows(store.text) {
		if cells(r)[6] == noReply {
			t.Fatalf("every thread has a reply now:\n%s", store.text)
		}
	}
	before := store.text
	answer := scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	if model.calls != communityReplyCap+7 || store.text != before || strings.Contains(answer, "more threads have no reply") {
		t.Fatalf("a finished list costs no call: %d calls\n%s", model.calls, answer)
	}
}

func TestBackfillLeavesRowsTheOwnerActedOnAndThreadsTooOldAlone(t *testing.T) {
	for name, text := range map[string]string{
		"owner replied": threadList(3, "replied", "2026-11-20 08:00 UTC"),
		"too old":       threadList(3, "new", "2026-11-01 08:00 UTC"),
		"no date":       threadList(3, "new", "yesterday"),
	} {
		store := &commStore{exists: true, text: text}
		model := backfillModel()
		var asked []string
		scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
		if model.calls != 2 { // the two new threads only
			t.Errorf("%s: %d calls", name, model.calls)
		}
		for _, r := range tableRows(store.text)[2:] {
			if cells(r)[6] != noReply {
				t.Errorf("%s: a row that is not backfilled was changed: %v", name, cells(r))
			}
		}
	}
}

func TestBackfillStillFailsTheFactCheckAndKeepsTheCellEmpty(t *testing.T) {
	store := &commStore{exists: true, text: threadList(2, "new", "2026-11-20 08:00 UTC")}
	invented := &replyModel{answer: func(string) string {
		return jsonReply(strings.Replace(plainReply, "a Redis list", "a Redis list with 40 percent fewer lost jobs", 1))
	}}
	var asked []string
	answer := scanWith(t, store, invented, ownerFactsFixture, replyGet(false, &asked))
	for _, r := range tableRows(store.text)[2:] {
		if cells(r)[6] != noReply {
			t.Fatalf("an invented number never reaches the list: %v", cells(r))
		}
	}
	if !strings.Contains(answer, "more threads have no reply") {
		t.Fatalf("the summary says some still wait:\n%s", answer)
	}
}

func TestRepliesStopWhenTimeIsShortAndTheRestAreLeftForTheNextRun(t *testing.T) {
	shrinkBudget(t)
	var picked []sources.Scored
	for i := 0; i < 8; i++ {
		picked = append(picked, scoredReddit(i))
	}
	model := &replyModel{answer: func(string) string { time.Sleep(100 * time.Millisecond); return jsonReply(plainReply) }}
	w := &TrackerLists{ReplyModel: model, OwnerFacts: func(context.Context, uuid.UUID) string { return ownerFactsFixture }}
	run := w.newReplyRun(context.Background(), uuid.New(), func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") })
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	replies, rejected, untried := run.fill(ctx, picked)
	if len(replies) == 0 || untried == 0 || len(replies)+rejected+untried != 8 || model.calls != len(replies)+rejected {
		t.Fatalf("some written, the rest untried, none lost: %d replies, %d rejected, %d untried, %d calls", len(replies), rejected, untried, model.calls)
	}
}

func TestOpportunityAssessUnderADeadlineAssessesSomeAndLeavesTheRest(t *testing.T) {
	shrinkBudget(t)
	base := newMemAssess()
	for i := 0; i < 8; i++ {
		base.add("Co"+strings.Repeat("x", i+1), "Backend Intern", "Pune, India", "", strongDesc+strings.Repeat(" more", i), domain.SourceKindEmployerATS)
	}
	store := &slowAssessStore{memAssess: base, delay: 80 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()
	res, err := (&OpportunityAssess{Store: store, Budget: time.Hour}).Run(ctx, uuid.New(), "")
	if err != nil || res.Assessed == 0 || res.Remaining == 0 || res.Assessed+res.Remaining != 8 {
		t.Fatalf("the deadline, not the four-minute budget, ends the run: %+v %v", res, err)
	}
	if !strings.Contains(res.Summary(), "more wait for the next run") {
		t.Fatal(res.Summary())
	}
	// The next run (no deadline) finishes the rest, and only the rest.
	res2, _ := (&OpportunityAssess{Store: base}).Run(context.Background(), uuid.New(), "")
	if res2.Assessed != res.Remaining {
		t.Fatalf("it resumes where it stopped: %+v then %+v", res, res2)
	}
}

func TestOpportunityAssessDoesNotStartAModelCallWithoutTimeForOne(t *testing.T) {
	shrinkBudget(t) // a model call needs 160ms; the code-only reserve is 100ms
	store := newMemAssess()
	store.add("Lab", "Research Intern", "Remote", "", vagueRemote, domain.SourceKindEmployerATS)
	var calls int32
	model := func(context.Context, string, string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", errors.New("no")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond) // 190ms of work time, then under 160ms is left at once
	defer cancel()
	time.Sleep(60 * time.Millisecond)
	res, _ := (&OpportunityAssess{Store: store, Model: model}).Run(ctx, uuid.New(), "")
	if atomic.LoadInt32(&calls) != 0 || res.Assessed != 0 || res.Remaining != 1 {
		t.Fatalf("no model call is started, and no assessment is saved without the answer it would have had: %+v", res)
	}
}

func TestOpportunityAssessLeavesAListingWhoseModelCallWasCutOffByTheDeadline(t *testing.T) {
	store := newMemAssess()
	store.add("Lab", "Research Intern", "Remote", "", vagueRemote, domain.SourceKindEmployerATS)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	cut := func(c context.Context, _, _ string) (string, error) {
		cancel() // the request ends while the model is answering
		<-c.Done()
		return "", c.Err()
	}
	res, err := (&OpportunityAssess{Store: store, Model: cut}).Run(ctx, uuid.New(), "")
	_ = err // the closing reads use the cancelled context; the point is what was saved
	if len(store.assessments) != 0 || res.Remaining != 1 {
		t.Fatalf("the listing is left for the next run, which asks again: %+v saved=%d", res, len(store.assessments))
	}
}

// slowAssessStore makes every saved assessment take a while, to stand for slow database and model work.
type slowAssessStore struct {
	*memAssess
	delay time.Duration
}

func (s *slowAssessStore) SaveAssessment(ctx context.Context, a *domain.Assessment, url string) error {
	time.Sleep(s.delay)
	return s.memAssess.SaveAssessment(ctx, a, url)
}
