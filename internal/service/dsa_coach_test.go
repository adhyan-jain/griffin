package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

type memDsa struct {
	s     domain.DsaSettings
	items []*domain.DsaItem
}

func (m *memDsa) Settings(context.Context, uuid.UUID) (domain.DsaSettings, error) { return m.s, nil }
func (m *memDsa) SaveSettings(_ context.Context, _ uuid.UUID, s domain.DsaSettings) error {
	m.s = s
	return nil
}
func (m *memDsa) Items(context.Context, uuid.UUID) ([]*domain.DsaItem, error)   { return m.items, nil }
func (m *memDsa) SaveItems(context.Context, uuid.UUID, []*domain.DsaItem) error { return nil }
func (m *memDsa) Users(context.Context) ([]uuid.UUID, error)                    { return []uuid.UUID{{}}, nil }

type fakeChecker struct{ at map[string]time.Time }

func (f fakeChecker) Solved(context.Context, string) (map[string]time.Time, error) { return f.at, nil }

func newCoach(now time.Time, at map[string]time.Time) (*DsaCoach, *memDsa, *captureSender) {
	var items []*domain.DsaItem
	for i := 0; i < 6; i++ {
		items = append(items, &domain.DsaItem{Slug: string(rune('a' + i)), Title: string(rune('A' + i)), Ord: i, State: domain.DsaQueued})
	}
	st := &memDsa{s: domain.DsaSettings{DailyCount: 2, Timezone: "Asia/Kolkata", LeetcodeUsername: "me", NagFromHour: 18}, items: items}
	snd := &captureSender{}
	c := &DsaCoach{Store: st, Checker: fakeChecker{at}, Sender: snd, AppURL: "http://x", Now: func() time.Time { return now }}
	return c, st, snd
}

func TestCoachDayFlow(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 8, 0, 0, 0, loc)
	c, st, snd := newCoach(now, map[string]time.Time{})
	u := uuid.New()

	today, err := c.Today(ctx, u)
	if err != nil || len(today) != 2 {
		t.Fatalf("today: %v %v", today, err)
	}
	// Submitted "a" at noon; "b" never.
	c.Checker = fakeChecker{map[string]time.Time{"a": time.Date(2026, 11, 14, 12, 0, 0, 0, loc), "zzz": now}}
	solved, err := c.Sync(ctx, u)
	if err != nil || len(solved) != 1 || solved[0] != "a" {
		t.Fatalf("sync: %v %v", solved, err)
	}
	// 8am: no nag. 7pm: one nag naming only b. Same evening again: throttled.
	if sent, _ := c.Nag(ctx, u); sent {
		t.Fatal("must not nag at 8am")
	}
	c.Now = func() time.Time { return time.Date(2026, 11, 14, 19, 0, 0, 0, loc) }
	if sent, _ := c.Nag(ctx, u); !sent || len(snd.msgs) != 1 || !strings.Contains(snd.msgs[0].Body, "B") || strings.Contains(snd.msgs[0].Body, "A,") {
		t.Fatalf("expected one nag about B only: %+v", snd.msgs)
	}
	c.Now = func() time.Time { return time.Date(2026, 11, 14, 20, 0, 0, 0, loc) }
	if sent, _ := c.Nag(ctx, u); sent {
		t.Fatal("nag must be throttled within 3 hours")
	}
	c.Now = func() time.Time { return time.Date(2026, 11, 14, 23, 0, 0, 0, loc) }
	if sent, _ := c.Nag(ctx, u); !sent {
		t.Fatal("must nag again after the throttle window")
	}

	// Next day b is failed (rests a week), c and d are handed out; a stays solved.
	c.Now = func() time.Time { return time.Date(2026, 11, 15, 8, 0, 0, 0, loc) }
	today, _ = c.Today(ctx, u)
	if today[0].Slug != "c" || today[1].Slug != "d" {
		t.Fatalf("day 2: %v %v", today[0].Slug, today[1].Slug)
	}
	if st.items[1].State != domain.DsaFailed {
		t.Fatalf("b must be failed, is %s", st.items[1].State)
	}
}

func TestSyncIgnoresAnOldSolve(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Date(2026, 11, 14, 8, 0, 0, 0, loc)
	// "a" was accepted last week, before it was handed out today: that proves nothing.
	c, _, _ := newCoach(now, map[string]time.Time{"a": now.AddDate(0, 0, -7)})
	u := uuid.New()
	if _, err := c.Today(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	solved, _ := c.Sync(context.Background(), u)
	if len(solved) != 0 {
		t.Fatalf("an old acceptance must not count: %v", solved)
	}
}

func TestMarkSolvedRefusesUnassigned(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	c, _, _ := newCoach(time.Date(2026, 11, 14, 8, 0, 0, 0, loc), nil)
	u := uuid.New()
	_, _ = c.Today(context.Background(), u)
	if err := c.MarkSolved(context.Background(), u, "f"); err == nil {
		t.Fatal("a problem not handed out yet cannot be submitted")
	}
	if err := c.MarkSolved(context.Background(), u, "nope"); err == nil {
		t.Fatal("unknown problem must error")
	}
	if err := c.MarkSolved(context.Background(), u, "a"); err != nil {
		t.Fatal(err)
	}
}

func TestLeetCodeCheckerParsesPublicResponse(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"recentAcSubmissionList": []map[string]string{
			{"titleSlug": "two-sum", "timestamp": "1700000000"},
			{"titleSlug": "two-sum", "timestamp": "1700000500"},
			{"titleSlug": "bad", "timestamp": "x"},
		}}})
	}))
	defer srv.Close()
	got, err := (&LeetCodeChecker{Endpoint: srv.URL}).Solved(context.Background(), "someone")
	if err != nil {
		t.Fatal(err)
	}
	if got["two-sum"].Unix() != 1700000500 || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(gotBody, "someone") || strings.Contains(strings.ToLower(gotBody), "cookie") {
		t.Fatalf("request body: %s", gotBody)
	}
	// A server error is an error, never "nothing solved".
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	if _, err := (&LeetCodeChecker{Endpoint: bad.URL}).Solved(context.Background(), "x"); err == nil {
		t.Fatal("HTTP 500 must be an error")
	}
}
