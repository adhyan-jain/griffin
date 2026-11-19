package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

// BriefSources reads what the brief needs from the database.
type BriefSources interface {
	BriefSource(ctx context.Context, userID uuid.UUID, now time.Time) (domain.BriefSource, error)
}

// BriefPhaseSources allows fetching phase-specific brief sources (e.g. evening debrief).
type BriefPhaseSources interface {
	BriefSourcePhase(ctx context.Context, userID uuid.UUID, now time.Time, phase domain.BriefPhase) (domain.BriefSource, error)
}

// Briefer builds the morning brief and evening debrief and pushes them in each enrolled owner's time zone.
type Briefer struct {
	Sources     BriefSources
	Coach       *DsaCoach // today's DSA problems and the owner's time zone
	Sender      notify.Sender
	Watchdog    *OutreachWatchdog
	AppURL      string
	BriefHour   int // local hour, default 8
	DebriefHour int // local hour, default 21 (9 PM)
	Logger      *slog.Logger
	Now         func() time.Time

	mu              sync.Mutex
	lastSent        map[uuid.UUID]time.Time // local day of the last morning push
	lastDebriefSent map[uuid.UUID]time.Time // local day of the last evening push
}

func (b *Briefer) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Build returns the morning brief for the owner as of now.
func (b *Briefer) Build(ctx context.Context, userID uuid.UUID) (domain.Brief, error) {
	return b.BuildPhase(ctx, userID, domain.BriefPhaseMorning)
}

// BuildPhase returns the brief for a specific phase (Morning or Evening).
func (b *Briefer) BuildPhase(ctx context.Context, userID uuid.UUID, phase domain.BriefPhase) (domain.Brief, error) {
	var src domain.BriefSource
	var err error
	if ps, ok := b.Sources.(BriefPhaseSources); ok {
		src, err = ps.BriefSourcePhase(ctx, userID, b.now(), phase)
	} else {
		src, err = b.Sources.BriefSource(ctx, userID, b.now())
		src.Phase = phase
	}
	if err != nil {
		return domain.Brief{}, err
	}
	if phase == domain.BriefPhaseMorning && b.Coach != nil {
		if src.DsaToday, err = b.Coach.Today(ctx, userID); err != nil {
			return domain.Brief{}, err
		}
	}
	return domain.BuildBrief(src), nil
}

// Tick pushes the morning brief and evening debrief to each enrolled owner once per local day.
func (b *Briefer) Tick(ctx context.Context) {
	if b.Coach == nil {
		return
	}
	mHour := b.BriefHour
	if mHour == 0 {
		mHour = 8
	}
	eHour := b.DebriefHour
	if eHour == 0 {
		eHour = 21
	}

	users, err := b.Coach.Store.Users(ctx)
	if err != nil {
		b.log().Warn("brief: listing users failed", "error", err)
		return
	}
	for _, u := range users {
		// Sweep due follow-ups for this user if watchdog is configured
		if b.Watchdog != nil {
			if _, err := b.Watchdog.SweepDueFollowups(ctx, u); err != nil {
				b.log().Warn("brief: watchdog sweep failed", "user_id", u, "error", err)
			}
		}

		if b.Sender == nil {
			continue
		}

		s, err := b.Coach.Store.Settings(ctx, u)
		if err != nil {
			continue
		}
		loc := b.Coach.location(s)
		local := b.now().In(loc)
		day := domain.Day(local, loc)

		// 1. Morning Brief dispatch
		if local.Hour() >= mHour && local.Hour() < eHour {
			b.mu.Lock()
			if b.lastSent == nil {
				b.lastSent = map[uuid.UUID]time.Time{}
			}
			done := b.lastSent[u].Equal(day)
			b.mu.Unlock()
			if !done {
				br, err := b.BuildPhase(ctx, u, domain.BriefPhaseMorning)
				if err != nil {
					b.log().Warn("brief: morning build failed", "error", err)
				} else if err := b.Sender.Send(ctx, notify.Message{Title: "Morning brief", Body: br.Text, ClickURL: strings.TrimRight(b.AppURL, "/") + "/brief?phase=morning", Tags: []string{"sunrise"}}); err != nil {
					b.log().Warn("brief: morning push failed", "error", err)
				} else {
					b.mu.Lock()
					b.lastSent[u] = day
					b.mu.Unlock()
				}
			}
		}

		// 2. Evening Debrief dispatch
		if local.Hour() >= eHour {
			b.mu.Lock()
			if b.lastDebriefSent == nil {
				b.lastDebriefSent = map[uuid.UUID]time.Time{}
			}
			done := b.lastDebriefSent[u].Equal(day)
			b.mu.Unlock()
			if !done {
				br, err := b.BuildPhase(ctx, u, domain.BriefPhaseEvening)
				if err != nil {
					b.log().Warn("brief: evening build failed", "error", err)
				} else if err := b.Sender.Send(ctx, notify.Message{Title: "Evening debrief", Body: br.Text, ClickURL: strings.TrimRight(b.AppURL, "/") + "/brief?phase=evening", Tags: []string{"sunset"}}); err != nil {
					b.log().Warn("brief: evening push failed", "error", err)
				} else {
					b.mu.Lock()
					b.lastDebriefSent[u] = day
					b.mu.Unlock()
				}
			}
		}
	}
}

func (b *Briefer) log() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.Default()
}

// Run ticks every interval until ctx ends.
func (b *Briefer) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	b.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.Tick(ctx)
		}
	}
}
