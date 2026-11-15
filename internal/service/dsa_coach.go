package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

// DsaStore is the storage the coach needs.
type DsaStore interface {
	Settings(ctx context.Context, userID uuid.UUID) (domain.DsaSettings, error)
	SaveSettings(ctx context.Context, userID uuid.UUID, s domain.DsaSettings) error
	Items(ctx context.Context, userID uuid.UUID) ([]*domain.DsaItem, error)
	SaveItems(ctx context.Context, userID uuid.UUID, items []*domain.DsaItem) error
	Users(ctx context.Context) ([]uuid.UUID, error)
}

// SolvedChecker reports which problems a LeetCode user has had accepted recently, and when.
type SolvedChecker interface {
	Solved(ctx context.Context, username string) (map[string]time.Time, error)
}

// LeetCodeChecker reads a user's public recent accepted submissions. It needs no password or cookie.
type LeetCodeChecker struct {
	Endpoint string // default https://leetcode.com/graphql
	HTTP     *http.Client
}

// Solved implements SolvedChecker. The newest accepted time per problem wins.
func (c *LeetCodeChecker) Solved(ctx context.Context, username string) (map[string]time.Time, error) {
	username = domain.CleanLeetCodeUsername(username)
	if username == "" {
		username = "AdhyanJain"
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = "https://leetcode.com/graphql"
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{
		"query":     `query recent($u: String!, $n: Int!) { recentAcSubmissionList(username: $u, limit: $n) { titleSlug timestamp } }`,
		"variables": map[string]any{"u": username, "n": 50},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("leetcode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leetcode: status %d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			List []struct {
				Slug      string `json:"titleSlug"`
				Timestamp string `json:"timestamp"`
			} `json:"recentAcSubmissionList"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("leetcode: bad response: %w", err)
	}
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("leetcode: %s", out.Errors[0].Message)
	}
	res := map[string]time.Time{}
	for _, s := range out.Data.List {
		sec, err := strconv.ParseInt(s.Timestamp, 10, 64)
		if err != nil {
			continue
		}
		if t := time.Unix(sec, 0); t.After(res[s.Slug]) {
			res[s.Slug] = t
		}
	}
	return res, nil
}

// DsaCoach hands out problems, checks they were solved, and nags.
type DsaCoach struct {
	Store   DsaStore
	Checker SolvedChecker
	Sender  notify.Sender
	AppURL  string
	Logger  *slog.Logger
	Now     func() time.Time // for tests; defaults to time.Now

	mu      sync.Mutex
	lastNag map[uuid.UUID]time.Time
}

func (c *DsaCoach) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// NagEvery is the least time between two nags to the same owner.
const NagEvery = 3 * time.Hour

func (c *DsaCoach) location(s domain.DsaSettings) *time.Location {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Today brings the list up to date and returns the whole list with today's problems first marked by state.
func (c *DsaCoach) Today(ctx context.Context, userID uuid.UUID) ([]*domain.DsaItem, error) {
	s, err := c.Store.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	// Sync solved problems from LeetCode first so problems already completed by the owner are NEVER assigned as repeats.
	_, _ = c.Sync(ctx, userID)

	items, err := c.Store.Items(ctx, userID)
	if err != nil {
		return nil, err
	}
	today := domain.Day(c.now(), c.location(s))
	slugs := domain.PlanDsaDay(items, today, s.DailyCount)
	if err := c.Store.SaveItems(ctx, userID, items); err != nil {
		return nil, err
	}
	byslug := map[string]*domain.DsaItem{}
	for _, it := range items {
		byslug[it.Slug] = it
	}
	out := make([]*domain.DsaItem, 0, len(slugs))
	for _, sl := range slugs {
		out = append(out, byslug[sl])
	}
	return out, nil
}

// Sync asks LeetCode which problems were accepted and marks today's (and any resting or queued) problems solved. It
// returns the slugs it just marked. If a problem was already solved on LeetCode, it is marked solved so
// it is never handed out again as a repeat.
func (c *DsaCoach) Sync(ctx context.Context, userID uuid.UUID) ([]string, error) {
	s, err := c.Store.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	username := domain.CleanLeetCodeUsername(s.LeetcodeUsername)
	if username == "" {
		username = "AdhyanJain"
	}
	if c.Checker == nil {
		return nil, nil
	}
	accepted, err := c.Checker.Solved(ctx, username)
	if err != nil {
		return nil, err
	}
	items, err := c.Store.Items(ctx, userID)
	if err != nil {
		return nil, err
	}
	var changed []*domain.DsaItem
	var slugs []string
	for _, it := range items {
		at, ok := accepted[it.Slug]
		if !ok || it.State == domain.DsaSolved {
			continue
		}
		t := at
		it.State, it.SolvedAt = domain.DsaSolved, &t
		changed = append(changed, it)
		slugs = append(slugs, it.Slug)
	}
	if len(changed) > 0 {
		if err := c.Store.SaveItems(ctx, userID, changed); err != nil {
			return nil, err
		}
	}
	return slugs, nil
}

// MarkSolved is the manual way, for when the owner solved it somewhere LeetCode cannot see.
func (c *DsaCoach) MarkSolved(ctx context.Context, userID uuid.UUID, slug string) error {
	items, err := c.Store.Items(ctx, userID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Slug == slug {
			if it.State == domain.DsaQueued {
				return domain.ErrConflict // not handed out yet; nothing to submit
			}
			now := c.now()
			it.State, it.SolvedAt = domain.DsaSolved, &now
			return c.Store.SaveItems(ctx, userID, []*domain.DsaItem{it})
		}
	}
	return domain.ErrNotFound
}

// Nag sends a reminder when today's problems are still open, at most once per NagEvery. It reports whether it sent.
func (c *DsaCoach) Nag(ctx context.Context, userID uuid.UUID) (bool, error) {
	if c.Sender == nil {
		return false, nil
	}
	s, err := c.Store.Settings(ctx, userID)
	if err != nil {
		return false, err
	}
	todays, err := c.Today(ctx, userID)
	if err != nil {
		return false, err
	}
	msg, ok := domain.DsaNag(todays, c.now(), c.location(s), s.NagFromHour)
	if !ok {
		return false, nil
	}
	c.mu.Lock()
	if c.lastNag == nil {
		c.lastNag = map[uuid.UUID]time.Time{}
	}
	if last, seen := c.lastNag[userID]; seen && c.now().Sub(last) < NagEvery {
		c.mu.Unlock()
		return false, nil
	}
	c.lastNag[userID] = c.now()
	c.mu.Unlock()
	err = c.Sender.Send(ctx, notify.Message{Title: "DSA: still open", Body: msg, ClickURL: strings.TrimRight(c.AppURL, "/") + "/dsa", Priority: 4, Tags: []string{"warning"}})
	return err == nil, err
}

// Tick runs one round for everyone enrolled: plan the day, check submissions, nag. Errors are logged, not fatal.
func (c *DsaCoach) Tick(ctx context.Context) {
	users, err := c.Store.Users(ctx)
	if err != nil {
		c.log().Warn("dsa coach: listing users failed", "error", err)
		return
	}
	for _, u := range users {
		if _, err := c.Today(ctx, u); err != nil {
			c.log().Warn("dsa coach: plan failed", "error", err)
			continue
		}
		if _, err := c.Sync(ctx, u); err != nil {
			c.log().Warn("dsa coach: submission check failed", "error", err)
		}
		if _, err := c.Nag(ctx, u); err != nil {
			c.log().Warn("dsa coach: nag failed", "error", err)
		}
	}
}

func (c *DsaCoach) log() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// Run ticks every interval until ctx ends.
func (c *DsaCoach) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	c.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Tick(ctx)
		}
	}
}
