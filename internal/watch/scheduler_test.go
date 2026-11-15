package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/notify"
)

type testNotifier struct {
	messages []notify.Message
}

func (n *testNotifier) Send(_ context.Context, m notify.Message) error {
	n.messages = append(n.messages, m)
	return nil
}

func TestWatchServiceHackathonDedupeAndNotify(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	tsDevpost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dateStr := now.Add(5 * 24 * time.Hour).Format("Jan 02, 2006")
		_, _ = w.Write([]byte(`{
			"hackathons": [
				{
					"title": "Agentic AI Hackathon",
					"url": "https://devpost.com/hackathons/agentic-ai",
					"submission_period_dates": "` + dateStr + ` - ` + dateStr + `",
					"prize_amount": "$5,000",
					"themes": [{"name": "AI"}],
					"prizes_counts": {"cash": 1}
				}
			]
		}`))
	}))
	defer tsDevpost.Close()

	tsEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer tsEmpty.Close()

	notifier := &testNotifier{}
	svc := NewWatchService(WatcherConfig{
		StateDir:     tmpDir,
		DevpostBase:  tsDevpost.URL,
		DevfolioBase: tsEmpty.URL,
		UnstopBase:   tsEmpty.URL,
		Sender:       notifier,
	})

	// First run: should find 1 match and notify
	matches, err := svc.RunHackathonCheck(context.Background())
	require.NoError(t, err)
	assert.Len(t, matches, 1)
	require.Len(t, notifier.messages, 1)
	assert.Contains(t, notifier.messages[0].Title, "1 new AI/systems matches")

	// Second run: deduplication should suppress second notification
	matches2, err := svc.RunHackathonCheck(context.Background())
	require.NoError(t, err)
	assert.Len(t, matches2, 1)
	assert.Len(t, notifier.messages, 1, "no new message because it's already seen")
}
