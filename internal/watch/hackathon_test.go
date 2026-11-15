package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDevpostDates(t *testing.T) {
	cases := []struct {
		input     string
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			input:     "Oct 16 - Nov 15, 2026",
			wantStart: time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.November, 15, 23, 59, 59, 0, time.UTC),
		},
		{
			input:     "Nov 13 - 15, 2026",
			wantStart: time.Date(2026, time.November, 13, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, time.November, 15, 23, 59, 59, 0, time.UTC),
		},
		{
			input:     "Dec 30, 2026 - Jan 5, 2027",
			wantStart: time.Date(2026, time.December, 30, 0, 0, 0, 0, time.UTC),
			wantEnd:   time.Date(2027, time.January, 5, 23, 59, 59, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		s, e := ParseDevpostDates(tc.input)
		assert.Equal(t, tc.wantStart, s, tc.input)
		assert.Equal(t, tc.wantEnd, e, tc.input)
	}
}

func TestHackathonEligibleAndWantedTheme(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	h := Hackathon{
		Source:    "devpost",
		Name:      "Global GenAI Agents Hackathon",
		Online:    true,
		India:     "yes",
		CashPrize: true,
		RegCloses: now.Add(5 * 24 * time.Hour),
		Themes:    []string{"Artificial Intelligence", "Machine Learning"},
	}

	assert.True(t, h.Eligible(now))
	assert.True(t, h.WantedTheme())

	// Not eligible if offline
	hOffline := h
	hOffline.Online = false
	assert.False(t, hOffline.Eligible(now))

	// Not eligible if India explicitly barred
	hNoIndia := h
	hNoIndia.India = "no"
	assert.False(t, hNoIndia.Eligible(now))

	// Not eligible if no cash prize
	hNoCash := h
	hNoCash.CashPrize = false
	assert.False(t, hNoCash.Eligible(now))

	// Not eligible if deadline is under 3 days away
	hTooLate := h
	hTooLate.RegCloses = now.Add(2 * 24 * time.Hour)
	assert.False(t, hTooLate.Eligible(now))

	// Unwanted theme (e.g. fashion/design only)
	hOtherTheme := h
	hOtherTheme.Name = "Global Fashion & Design Fest"
	hOtherTheme.Themes = []string{"Fashion", "Art"}
	hOtherTheme.Blurb = "Designing clothing patterns"
	assert.False(t, hOtherTheme.WantedTheme())
}

func TestFetchDevpost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/hackathons", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"hackathons": [
				{
					"title": "Agentic AI Challenge",
					"url": "https://agentic-ai.devpost.com",
					"submission_period_dates": "Nov 10 - Dec 1, 2026",
					"prize_amount": "$10,000",
					"invite_only": false,
					"themes": [{"name": "AI"}, {"name": "DevOps"}],
					"prizes_counts": {"cash": 1}
				}
			]
		}`))
	}))
	defer ts.Close()

	hackathons, err := FetchDevpost(context.Background(), ts.Client(), ts.URL)
	require.NoError(t, err)
	require.Len(t, hackathons, 1)
	assert.Equal(t, "Agentic AI Challenge", hackathons[0].Name)
	assert.True(t, hackathons[0].CashPrize)
	assert.True(t, hackathons[0].WantedTheme())
}
