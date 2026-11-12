package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OutreachStatus is where one person is in a campaign. The table in [outreachTransitions] is the only
// definition of which moves are legal; the repository enforces it in SQL with the same list.
//
//	found ─▶ drafted ─▶ approved ─▶ sent ─▶ replied ─▶ closed
//	  │         │           │         │         ▲
//	  └─────────┴───────────┴─────────┴──▶ closed      sent ─▶ followup_due ─▶ approved (a follow-up draft is approved again)
type OutreachStatus string

const (
	OutreachFound       OutreachStatus = "found"
	OutreachDrafted     OutreachStatus = "drafted"
	OutreachApproved    OutreachStatus = "approved"
	OutreachSent        OutreachStatus = "sent"
	OutreachReplied     OutreachStatus = "replied"
	OutreachFollowupDue OutreachStatus = "followup_due"
	OutreachClosed      OutreachStatus = "closed"
)

// AllOutreachStatuses lists every status, for validation and for the completeness test.
var AllOutreachStatuses = []OutreachStatus{OutreachFound, OutreachDrafted, OutreachApproved, OutreachSent, OutreachReplied, OutreachFollowupDue, OutreachClosed}

var outreachTransitions = map[OutreachStatus][]OutreachStatus{
	OutreachFound:       {OutreachDrafted, OutreachClosed},
	OutreachDrafted:     {OutreachDrafted, OutreachApproved, OutreachClosed},
	OutreachApproved:    {OutreachDrafted, OutreachSent, OutreachClosed},
	OutreachSent:        {OutreachReplied, OutreachFollowupDue, OutreachClosed},
	OutreachFollowupDue: {OutreachDrafted, OutreachReplied, OutreachClosed},
	OutreachReplied:     {OutreachClosed},
	OutreachClosed:      {},
}

// OutreachTransitionsFrom lists the statuses reachable from s (a copy).
func OutreachTransitionsFrom(s OutreachStatus) []OutreachStatus {
	return append([]OutreachStatus(nil), outreachTransitions[s]...)
}

// OutreachPrevious lists the statuses from which s can be entered, for the SQL guard of an update.
func OutreachPrevious(s OutreachStatus) []OutreachStatus {
	var out []OutreachStatus
	for _, from := range AllOutreachStatuses {
		for _, to := range outreachTransitions[from] {
			if to == s {
				out = append(out, from)
				break
			}
		}
	}
	return out
}

// CanOutreachMove reports whether a target may go from one status to another.
func CanOutreachMove(from, to OutreachStatus) bool {
	for _, t := range outreachTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Valid reports whether s is a known status.
func (s OutreachStatus) Valid() bool {
	for _, v := range AllOutreachStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// OutreachCampaign is one job: "cold-mail 50 founders" or "apply to internships".
type OutreachCampaign struct {
	ID        uuid.UUID      `json:"id"`
	UserID    uuid.UUID      `json:"user_id"`
	Name      string         `json:"name"`
	Goal      string         `json:"goal"`
	Channel   string         `json:"channel"` // email, linkedin, whatsapp, instagram, other
	Status    string         `json:"status"`  // active, paused, done
	Limits    OutreachLimits `json:"limits"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// OutreachTarget is one person (or application) in a campaign. All writes are single-row, so a researching
// agent and a sending agent can work on the same campaign at once without overwriting each other.
type OutreachTarget struct {
	ID             uuid.UUID      `json:"id"`
	CampaignID     uuid.UUID      `json:"campaign_id"`
	UserID         uuid.UUID      `json:"user_id"`
	Name           string         `json:"name"`
	Handle         string         `json:"handle"` // email address, profile URL, phone: unique per campaign
	Info           string         `json:"info,omitempty"`
	Status         OutreachStatus `json:"status"`
	Draft          string         `json:"draft,omitempty"`
	SentAt         *time.Time     `json:"sent_at,omitempty"`
	RepliedAt      *time.Time     `json:"replied_at,omitempty"`
	ReplyText      string         `json:"reply_text,omitempty"`
	NextFollowupAt *time.Time     `json:"next_followup_at,omitempty"`
	FollowupsSent  int            `json:"followups_sent"`
	Notes          string         `json:"notes,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// OutreachLimits keep a campaign inside what the apps (and the owner's reputation) can take. The defaults are
// deliberately conservative: LinkedIn and Gmail restrict accounts that send in bursts.
type OutreachLimits struct {
	DailyCap         int `json:"daily_cap"`          // sends per rolling local day
	MinGapSeconds    int `json:"min_gap_seconds"`    // shortest pause between two sends
	JitterSeconds    int `json:"jitter_seconds"`     // random extra pause, added by the sender
	QuietStartHour   int `json:"quiet_start_hour"`   // no sends from this local hour...
	QuietEndHour     int `json:"quiet_end_hour"`     // ...until this one (both 0 = no quiet hours)
	FollowupAfterDay int `json:"followup_after_day"` // days of silence before a follow-up is due
	MaxFollowups     int `json:"max_followups"`
}

// DefaultOutreachLimits returns the starting limits for a channel.
func DefaultOutreachLimits(channel string) OutreachLimits {
	l := OutreachLimits{DailyCap: 30, MinGapSeconds: 120, JitterSeconds: 90, QuietStartHour: 22, QuietEndHour: 8, FollowupAfterDay: 5, MaxFollowups: 2}
	switch channel {
	case "linkedin":
		l.DailyCap, l.MinGapSeconds, l.JitterSeconds = 15, 300, 180
	case "whatsapp", "instagram":
		l.DailyCap, l.MinGapSeconds, l.JitterSeconds = 20, 180, 120
	}
	return l
}

// Validate checks the limits are usable.
func (l OutreachLimits) Validate() error {
	switch {
	case l.DailyCap < 1 || l.DailyCap > 500:
		return fmt.Errorf("%w: daily_cap must be 1 to 500", ErrValidation)
	case l.MinGapSeconds < 0 || l.JitterSeconds < 0:
		return fmt.Errorf("%w: gaps cannot be negative", ErrValidation)
	case l.QuietStartHour < 0 || l.QuietStartHour > 23 || l.QuietEndHour < 0 || l.QuietEndHour > 23:
		return fmt.Errorf("%w: quiet hours must be 0 to 23", ErrValidation)
	case l.FollowupAfterDay < 1 || l.MaxFollowups < 0 || l.MaxFollowups > 5:
		return fmt.Errorf("%w: follow-ups need 1+ days and at most 5 rounds", ErrValidation)
	}
	return nil
}

func (l OutreachLimits) inQuietHours(now time.Time) bool {
	s, e, h := l.QuietStartHour, l.QuietEndHour, now.Hour()
	if s == e {
		return false
	}
	if s < e {
		return h >= s && h < e
	}
	return h >= s || h < e
}

// NextSend says whether a send is allowed at now, given how many went out in the current day and when the last
// one went. When it is not, it returns the earliest time to try again and why. now must be in the owner's zone.
func (l OutreachLimits) NextSend(now time.Time, sentToday int, lastSent *time.Time) (ok bool, retryAt time.Time, reason string) {
	if sentToday >= l.DailyCap {
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		return false, next, fmt.Sprintf("daily limit of %d reached", l.DailyCap)
	}
	if lastSent != nil && l.MinGapSeconds > 0 {
		if at := lastSent.Add(time.Duration(l.MinGapSeconds) * time.Second); now.Before(at) {
			return false, at, "pausing between messages"
		}
	}
	if l.inQuietHours(now) {
		end := time.Date(now.Year(), now.Month(), now.Day(), l.QuietEndHour, 0, 0, 0, now.Location())
		if !end.After(now) {
			end = end.AddDate(0, 0, 1)
		}
		return false, end, "quiet hours"
	}
	return true, time.Time{}, ""
}
