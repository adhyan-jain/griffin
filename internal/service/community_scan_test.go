package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/storage"
)

// commStore is a Communities list that keeps what is written to it; it has no list until the first save.
type commStore struct {
	text    string
	exists  bool
	saveErr error
}

func (s *commStore) SaveArtifact(_ context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error) {
	if s.saveErr != nil {
		return nil, s.saveErr
	}
	b, _ := io.ReadAll(in.Body)
	s.text, s.exists = string(b), true
	return &domain.Artifact{ID: uuid.New()}, nil
}
func (s *commStore) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	if !s.exists {
		return nil, nil
	}
	return []*domain.Artifact{{ID: uuid.New(), Metadata: domain.TrackerListMetadata("list", "community")}}, nil
}
func (s *commStore) ReadText(context.Context, uuid.UUID, uuid.UUID, int64) (string, error) {
	return s.text, nil
}
func (s *commStore) UpdateText(_ context.Context, _ uuid.UUID, _ uuid.UUID, text string, _ bool, _ int64) (*domain.Artifact, error) {
	s.text = text
	return &domain.Artifact{}, nil
}

var scanNow = time.Unix(1795343000, 0).UTC() // 2026-11-22 10:03 UTC

const redditBody = `{"data":{"children":[
 {"data":{"subreddit":"golang","title":"How do you build a task queue with Redis and worker pools in Go?","selftext":"jobs across many worker nodes","permalink":"/r/golang/comments/1abc001/q/","created_utc":1795340000,"num_comments":4,"score":12}},
 {"data":{"subreddit":"golang","title":"Pinned: who is hiring Go developers","permalink":"/r/golang/comments/1abc003/p/","created_utc":1795340100,"num_comments":2,"stickied":true}}]}}`

const hnBody = `{"hits":[
 {"objectID":"50589077","title":"Ask HN: Does C/C++/Rust/Zig have agent framework?","created_at_i":1795268125,"num_comments":2,"points":4,"_tags":["story","ask_hn"]},
 {"objectID":"50000001","title":"Ask HN: Recommended pasta recipes?","created_at_i":1795300000,"num_comments":2,"points":4,"_tags":["story","ask_hn"]}]}`

func scanGet(redditStatus int) sources.Get {
	return func(_ context.Context, u string) ([]byte, error) {
		switch {
		case strings.Contains(u, "reddit.com"):
			if redditStatus != 200 {
				return nil, &sources.StatusError{URL: u, Code: redditStatus}
			}
			return []byte(redditBody), nil
		case strings.Contains(u, "hn.algolia.com"):
			return []byte(hnBody), nil
		}
		return nil, errors.New("offline")
	}
}

func scanTask() *domain.Task {
	return &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: CommunityScanTitle}
}

func tableRows(text string) []string {
	var rows []string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "| ") && !strings.HasPrefix(l, "| Platform") {
			rows = append(rows, l)
		}
	}
	return rows
}

func TestTheCommunityScanListsFittingThreadsOnceWithTheirReasons(t *testing.T) {
	store := &commStore{}
	w := &TrackerLists{Store: store}
	task := scanTask()
	answer, ok := w.ScanCommunities(context.Background(), task, scanGet(200), func() time.Time { return scanNow })
	if !ok {
		t.Fatal("the scan routine is handled")
	}
	rows := tableRows(store.text)
	if len(rows) != 2 || !strings.HasPrefix(store.text, "# Communities tracker\n\n"+CommunityHeader) {
		t.Fatalf("the task-queue question and the agent-framework ask are listed, not the pinned post or pasta:\n%s", store.text)
	}
	first := rows[0]
	for _, want := range []string{"| Reddit | r/golang | How do you build a task queue", "ComputeHive", "2026-11-22 09:33 UTC", "https://www.reddit.com/r/golang/comments/1abc001/q/ | new |"} {
		if !strings.Contains(first, want) {
			t.Errorf("row lacks %q: %s", want, first)
		}
	}
	if !strings.Contains(rows[1], "| Hacker News | Ask HN |") || !strings.Contains(rows[1], "Yes: his AI agent work") {
		t.Fatalf("the HN row: %s", rows[1])
	}
	if !strings.Contains(answer, "2 new conversations") || !strings.Contains(answer, "2 where one of your projects") || !strings.Contains(answer, "draft a reply") {
		t.Fatalf("the summary counts the finds and says replies are drafted for approval:\n%s", answer)
	}

	// A second scan lists nothing twice and says so.
	again, _ := w.ScanCommunities(context.Background(), task, scanGet(200), func() time.Time { return scanNow })
	if len(tableRows(store.text)) != 2 || !strings.Contains(again, "0 new conversations") || !strings.Contains(again, "2 were already listed") || !strings.Contains(again, "Nothing new today") {
		t.Fatalf("second scan:\n%s\n%s", again, store.text)
	}
}

func TestRedditBlockingIsAProblemNotACrash(t *testing.T) {
	store := &commStore{}
	w := &TrackerLists{Store: store}
	answer, ok := w.ScanCommunities(context.Background(), scanTask(), scanGet(403), func() time.Time { return scanNow })
	if !ok || len(tableRows(store.text)) != 1 {
		t.Fatalf("Hacker News still lists: %v\n%s", ok, store.text)
	}
	if !strings.Contains(answer, "Could not read: Reddit refused the read (r/LocalLLaMA: HTTP 403)") {
		t.Fatalf("the block is named:\n%s", answer)
	}
}

func TestTheListKeepsNewestFirstAtMostTwoHundredRowsAndTheOwnersEdits(t *testing.T) {
	var old []string
	for i := 0; i < 200; i++ {
		old = append(old, fmt.Sprintf("| Reddit | r/golang | Old %d | why | No | 2026-11-01 08:00 UTC | https://www.reddit.com/old/%d | new |", i, i))
	}
	old[0] = strings.Replace(old[0], "| new |", "| replied |", 1)
	store := &commStore{exists: true, text: "# Communities tracker\n\nmy notes\n\n" + CommunityHeader + "\n" + strings.Join(old, "\n") + "\n"}
	w := &TrackerLists{Store: store}
	if _, ok := w.ScanCommunities(context.Background(), scanTask(), scanGet(200), func() time.Time { return scanNow }); !ok {
		t.Fatal("handled")
	}
	rows := tableRows(store.text)
	if len(rows) != 200 {
		t.Fatalf("capped at 200 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "task queue") || !strings.Contains(rows[1], "agent framework") {
		t.Fatalf("new rows are on top: %s", rows[0])
	}
	if !strings.Contains(store.text, "my notes") || !strings.Contains(store.text, "| replied |") || strings.Contains(store.text, "/old/199 ") {
		t.Fatalf("the owner's text and edits stay; the oldest rows fall off:\n%s", store.text[:300])
	}
}

func TestTheCommunityScanReportsASaveFailureAndIgnoresOtherTasks(t *testing.T) {
	w := &TrackerLists{Store: &commStore{saveErr: errors.New("disk full")}}
	answer, ok := w.ScanCommunities(context.Background(), scanTask(), scanGet(200), func() time.Time { return scanNow })
	if !ok || !strings.Contains(answer, "could not save") || !strings.Contains(answer, "disk full") {
		t.Fatalf("%v %s", ok, answer)
	}
	if _, ok := w.ScanCommunities(context.Background(), &domain.Task{Title: "something else"}, scanGet(200), nil); ok {
		t.Fatal("only the scan task is handled")
	}
}

func TestTheCommunityScanRunsInCodeAndHasItsOwnTracker(t *testing.T) {
	w := &TrackerLists{Store: &commStore{}, CommunityGet: scanGet(200)}
	answer, how := w.RunInCode(context.Background(), scanTask(), nil, nil)
	if how != Answered || !strings.Contains(answer, "Community scan: read") {
		t.Fatalf("the routine is answered in code: %v %s", how, answer)
	}
	if _, how = w.RunInCode(context.Background(), &domain.Task{UserID: uuid.New(), Title: "Something else"}, nil, nil); how != NotInCode {
		t.Fatal("other tasks are not taken")
	}
	if domain.TrackerTypeOf("COMMUNITY SCAN (daily 09:00)", "", nil) != "community" || domain.TrackerTypes["community"] != "Communities" {
		t.Fatal("the Communities tracker type is known")
	}
	if !IsCommunityScan("community scan") {
		t.Fatal("the routine is found by its title")
	}
}
