package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/sources"
)

const ownerFactsFixture = "Adhyan is a CSE student at VIT. He built ComputeHive, a distributed task runner in Go with Redis and Docker. He also does AI agent work."

const redditReply = "Worker pools on top of a Redis list are a solid start. The part that usually bites is a worker dying mid job, so I would add a visibility timeout and retries before anything else. " +
	"Are your jobs idempotent, and do they need strict ordering? That decides whether a plain queue is enough. I hit the same questions building ComputeHive, a distributed task runner in Go with Redis and Docker, so happy to compare notes."

// plainReply helps without mentioning any project, for threads that are not flagged for one.
const plainReply = "Worker pools on top of a Redis list are a solid start. The part that usually bites is a worker dying mid job, so I would add a visibility timeout and retries before anything else. " +
	"Are your jobs idempotent, and do they need strict ordering? That decides whether a plain queue is enough, and it is worth measuring where jobs get lost before adding anything heavier."

const hnReply = "Not a framework as such, but the agent loop itself is small: call the model, parse a tool request, run the tool, feed the result back. Most of the real work is the tool layer and keeping state tidy, " +
	"so a thin library plus your own loop may serve you better. Are you after bindings to an existing runtime, or do you want the whole loop native in Rust? I work on AI agents too, happy to compare notes."

// replyModel is a fake Gemini: it answers from a function of the prompt and counts its calls.
type replyModel struct {
	calls   int
	prompts []string
	answer  func(prompt string) string
}

func (m *replyModel) Chat(_ context.Context, _ string, turns []gemini.Turn) (string, error) {
	m.calls++
	p := turns[len(turns)-1].Text
	m.prompts = append(m.prompts, p)
	return m.answer(p), nil
}

func jsonReply(s string) string {
	b, _ := json.Marshal(map[string]string{"reply": s})
	return string(b)
}

// goodModel answers each fixture thread with its own checked reply.
func goodModel() *replyModel {
	return &replyModel{answer: func(p string) string {
		if strings.Contains(p, "agent framework") {
			return "```json\n" + jsonReply(hnReply) + "\n```"
		}
		return jsonReply(redditReply)
	}}
}

// replyGet serves the scan fixtures plus the thread pages the reply step reads.
func replyGet(blockThreadPages bool, asked *[]string) sources.Get {
	base := scanGet(200)
	return func(ctx context.Context, u string) ([]byte, error) {
		switch {
		case strings.Contains(u, "/api/v1/items/"):
			*asked = append(*asked, u)
			return []byte(`{"title":"Ask HN: Does C/C++/Rust/Zig have agent framework?","text":"<p>I want something small for a side tool.","children":[{"text":"Look at the loop, not the framework"}]}`), nil
		case strings.HasSuffix(u, "q.json"):
			*asked = append(*asked, u)
			if blockThreadPages {
				return nil, &sources.StatusError{URL: u, Code: 403}
			}
			return []byte(`[{"data":{"children":[{"data":{"selftext":"jobs across many worker nodes and some get lost"}}]}}]`), nil
		}
		return base(ctx, u)
	}
}

func scanWith(t *testing.T, store *commStore, model *replyModel, facts string, get sources.Get) string {
	t.Helper()
	w := &TrackerLists{Store: store, ReplyModel: model, OwnerFacts: func(context.Context, uuid.UUID) string { return facts }}
	answer, ok := w.ScanCommunities(context.Background(), scanTask(), get, func() time.Time { return scanNow })
	if !ok {
		t.Fatal("the scan is handled")
	}
	return answer
}

// cells splits one table row into its trimmed cells.
func cells(row string) []string {
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(row), "|"), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func TestEachNewThreadGetsACheckedReplyBeforeItsLink(t *testing.T) {
	store, model := &commStore{}, goodModel()
	var asked []string
	answer := scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	if !strings.HasPrefix(store.text, "# Communities tracker\n\n"+CommunityHeader) || !strings.Contains(CommunityHeader, "Posted | Reply you can send | Link | Status") {
		t.Fatalf("the column sits before Link:\n%s", store.text)
	}
	rows := tableRows(store.text)
	if len(rows) != 2 || model.calls != 2 {
		t.Fatalf("two threads, one call each: %d rows, %d calls", len(rows), model.calls)
	}
	for _, r := range rows {
		c := cells(r)
		if len(c) != 9 || c[6] == noReply || !strings.HasPrefix(c[7], "https://") || c[8] != "new" {
			t.Fatalf("row is nine cells with a reply then the link: %v", c)
		}
	}
	if cells(rows[0])[6] != redditReply || cells(rows[1])[6] != hnReply {
		t.Fatalf("each row carries its own reply:\n%s", store.text)
	}
	if len(asked) != 2 || !strings.Contains(model.prompts[0]+model.prompts[1], "jobs across many worker nodes and some get lost") || !strings.Contains(model.prompts[0]+model.prompts[1], "Look at the loop, not the framework") {
		t.Fatalf("the thread pages were read and shown to the model: %v", asked)
	}
	if !strings.Contains(answer, "Wrote 2 ready-to-send replies") {
		t.Fatalf("the summary says so:\n%s", answer)
	}
	t.Logf("sample reply for %q:\n%s", cells(rows[0])[2], cells(rows[0])[6])

	// A second scan finds nothing new: no model call, and the replies stay.
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	if model.calls != 2 || cells(tableRows(store.text)[0])[6] != redditReply {
		t.Fatalf("threads already listed keep their reply and cost no call: %d calls\n%s", model.calls, store.text)
	}
}

func TestADraftThatFailsTheFactCheckLeavesTheCellEmpty(t *testing.T) {
	bad := map[string]string{ // what the model says, raw
		"an invented number":  jsonReply(strings.Replace(redditReply, "a Redis list", "a Redis list with 40 percent fewer lost jobs", 1)),
		"an invented product": jsonReply(strings.Replace(redditReply, "a Redis list", "a Pulsarix stream", 1)),
		"not json":            "Sure! Here is a reply.",
		"an empty reply":      `{"reply": ""}`,
		"a link":              jsonReply(redditReply + " See https://example.com"),
	}
	for name, out := range bad {
		store := &commStore{}
		model := &replyModel{answer: func(string) string { return out }}
		var asked []string
		answer := scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
		rows := tableRows(store.text)
		if len(rows) != 2 {
			t.Fatalf("%s: the threads are still listed: %s", name, store.text)
		}
		for _, r := range rows {
			if c := cells(r); len(c) != 9 || c[6] != noReply {
				t.Errorf("%s: a rejected draft is never written, the cell shows %q: %v", name, noReply, c)
			}
		}
		if strings.Contains(answer, "Wrote") {
			t.Errorf("%s: nothing was written, so the summary does not say it was:\n%s", name, answer)
		}
	}
}

func TestAReplyWithPipesAndLineBreaksKeepsTheTableWellFormed(t *testing.T) {
	messy := strings.Replace(redditReply, "Worker pools", "Worker pools | job queues\n\nand", 1)
	store := &commStore{}
	model := &replyModel{answer: func(string) string { return jsonReply(messy) }}
	var asked []string
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	lines := strings.Split(strings.TrimSpace(store.text), "\n")
	for _, r := range tableRows(store.text) {
		if c := cells(r); len(c) != 9 {
			t.Fatalf("still nine cells: %v", c)
		}
	}
	// header, separator and two rows, plus the title and a blank line: nothing broke onto a second line.
	if len(lines) != 6 || !strings.Contains(store.text, "Worker pools / job queues and on top of a Redis list") {
		t.Fatalf("pipes became \" / \" and breaks became spaces:\n%s", store.text)
	}
}

func TestNoProfileOrNoModelMeansNoRepliesAndNoCalls(t *testing.T) {
	var asked []string
	store, model := &commStore{}, goodModel()
	scanWith(t, store, model, "  ", replyGet(false, &asked))
	if model.calls != 0 || len(asked) != 0 {
		t.Fatalf("without a profile the model is never asked and no thread page is read: %d, %v", model.calls, asked)
	}
	for _, r := range tableRows(store.text) {
		if cells(r)[6] != noReply {
			t.Fatalf("empty cell expected: %s", r)
		}
	}
	// No model at all: the scan is as it was, with the column empty.
	store = &commStore{}
	w := &TrackerLists{Store: store}
	if _, ok := w.ScanCommunities(context.Background(), scanTask(), scanGet(200), func() time.Time { return scanNow }); !ok || len(tableRows(store.text)) != 2 {
		t.Fatal("the scan still lists")
	}
}

func scoredReddit(i int) sources.Scored {
	return sources.Scored{Thread: sources.Thread{Platform: sources.Reddit, Community: "r/golang",
		Title: fmt.Sprintf("How do I run a task queue %d?", i), URL: fmt.Sprintf("https://www.reddit.com/r/golang/comments/%d/q/", i)}}
}

func TestRepliesAreCappedAtFifteenCalls(t *testing.T) {
	var picked []sources.Scored
	for i := 0; i < 20; i++ {
		picked = append(picked, scoredReddit(i))
	}
	model := &replyModel{answer: func(string) string { return jsonReply(plainReply) }}
	w := &TrackerLists{ReplyModel: model, OwnerFacts: func(context.Context, uuid.UUID) string { return ownerFactsFixture }}
	replies, _ := w.communityReplies(context.Background(), uuid.New(), picked, func(context.Context, string) ([]byte, error) { return nil, fmt.Errorf("offline") })
	if model.calls != 15 || len(replies) != 15 {
		t.Fatalf("at most 15 calls and replies: %d calls, %d replies", model.calls, len(replies))
	}
	if replies[strings.ToLower(picked[15].Thread.URL)] != "" {
		t.Fatal("the sixteenth thread has none")
	}
}

func TestARedditBlockCostsOneReadAndRepliesUseTheTitleOnly(t *testing.T) {
	reads := 0
	get := func(_ context.Context, u string) ([]byte, error) {
		reads++
		return nil, &sources.StatusError{URL: u, Code: 403}
	}
	picked := []sources.Scored{scoredReddit(1), scoredReddit(2), scoredReddit(3)}
	model := &replyModel{answer: func(string) string { return jsonReply(plainReply) }}
	w := &TrackerLists{ReplyModel: model, OwnerFacts: func(context.Context, uuid.UUID) string { return ownerFactsFixture }}
	replies, _ := w.communityReplies(context.Background(), uuid.New(), picked, get)
	if reads != 1 || model.calls != 3 || len(replies) != 3 {
		t.Fatalf("one refused read, then no more asking; every thread still gets a reply from its title: reads=%d calls=%d replies=%d", reads, model.calls, len(replies))
	}
	if !strings.Contains(model.prompts[2], "How do I run a task queue 3?") {
		t.Fatalf("the title reaches the model:\n%s", model.prompts[2])
	}
}

func TestAReplyMayPromoteAProjectOnlyOnAFlaggedThread(t *testing.T) {
	s := scoredReddit(1)
	model := &replyModel{answer: func(string) string { return jsonReply(redditReply) }}
	title := s.Thread.Title
	if got := draftCommunityReply(context.Background(), model, s, title, ownerFactsFixture); got != "" {
		t.Fatalf("a mention on an unflagged thread is dropped: %q", got)
	}
	s.Mention = "ComputeHive (his distributed task runner in Go with Redis and Docker)"
	if got := draftCommunityReply(context.Background(), model, s, title, ownerFactsFixture); got != redditReply {
		t.Fatalf("on a flagged thread it stays: %q", got)
	}
	if !strings.Contains(model.prompts[0], "Do not mention or promote") || !strings.Contains(model.prompts[1], "at most ONE sentence") {
		t.Fatal("the prompt carries the flag")
	}
}

func TestAnOldListGetsTheColumnWithEmptyCellsAndKeepsEverythingElse(t *testing.T) {
	old := "| Platform | Community | Thread | Why it fits | Mention a project? | Posted | Link | Status |\n|---|---|---|---|---|---|---|---|"
	rows := []string{
		"| Reddit | r/golang | Task queue? | why | Yes: ComputeHive | 2026-11-20 08:00 UTC | https://www.reddit.com/r/golang/comments/1abc001/q/ | replied |",
		"| Hacker News | Ask HN | Agents? | why | No | 2026-11-20 09:00 UTC | https://news.ycombinator.com/item?id=50589077 | new |",
	}
	store := &commStore{exists: true, text: "# Communities tracker\n\nmy notes\n\n" + old + "\n" + strings.Join(rows, "\n") + "\n"}
	model := goodModel()
	var asked []string
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	if model.calls != 1 {
		t.Fatalf("both threads were already listed; only the one still marked new gets the reply it lacks: %d calls", model.calls)
	}
	got := tableRows(store.text)
	if len(got) != 2 || !strings.Contains(store.text, CommunityHeader) || !strings.Contains(store.text, "my notes") {
		t.Fatalf("the list is migrated even on a day with nothing new:\n%s", store.text)
	}
	for i, r := range got {
		c := cells(r)
		// The owner's "replied" row keeps its empty cell; the thread still marked new is backfilled.
		if len(c) != 9 || (i == 0) != (c[6] == noReply) || !strings.HasPrefix(c[7], "https://") {
			t.Fatalf("row %d: %v", i, c)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got[0]), "| replied |") {
		t.Fatalf("the owner's status is kept: %s", got[0])
	}

	// Migrating twice changes nothing, and a new thread's reply goes on top of the migrated rows.
	before := store.text
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	if store.text != before {
		t.Fatalf("a migrated list is left alone:\n%s", store.text)
	}
}

func TestAReplyKeptInTheListSurvivesTheNextScanOnTop(t *testing.T) {
	store := &commStore{}
	model := goodModel()
	var asked []string
	scanWith(t, store, model, ownerFactsFixture, replyGet(false, &asked))
	first := tableRows(store.text)
	// A later scan with a different pick must keep the earlier rows and their replies byte for byte.
	later := func(ctx context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "reddit.com") && strings.HasSuffix(u, "json?limit=50") {
			return []byte(`{"data":{"children":[{"data":{"subreddit":"golang","title":"Which task scheduler for background jobs in Go?","permalink":"/r/golang/comments/1abc009/z/","created_utc":1795342000,"num_comments":1,"score":3}}]}}`), nil
		}
		return replyGet(false, &asked)(ctx, u)
	}
	scanWith(t, store, model, ownerFactsFixture, later)
	all := tableRows(store.text)
	if len(all) != 3 || all[1] != first[0] || all[2] != first[1] {
		t.Fatalf("old rows and their replies are untouched under the new one:\n%s", store.text)
	}
}
