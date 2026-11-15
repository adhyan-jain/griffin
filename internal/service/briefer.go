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

// Briefer builds the morning brief and pushes it once a day at BriefHour in each enrolled owner's time zone.
type Briefer struct {
	Sources   BriefSources
	Coach     *DsaCoach // today's DSA problems and the owner's time zone
	Sender    notify.Sender
	AppURL    string
	BriefHour int // local hour, default 8
	Logger    *slog.Logger
	Now       func() time.Time

	mu       sync.Mutex
	lastSent map[uuid.UUID]time.Time // local day of the last push
}

func (b *Briefer) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Build returns the brief for the owner as of now.
func (b *Briefer) Build(ctx context.Context, userID uuid.UUID) (domain.Brief, error) {
	src, err := b.Sources.BriefSource(ctx, userID, b.now())
	if err != nil {
		return domain.Brief{}, err
	}
	if b.Coach != nil {
		if src.DsaToday, err = b.Coach.Today(ctx, userID); err != nil {
			return domain.Brief{}, err
		}
	}
	return domain.BuildBrief(src), nil
}

// Tick pushes the brief to each enrolled owner once per local day, from BriefHour on.
func (b *Briefer) Tick(ctx context.Context) {
	if b.Sender == nil || b.Coach == nil {
		return
	}
	hour := b.BriefHour
	if hour == 0 {
		hour = 8
	}
	users, err := b.Coach.Store.Users(ctx)
	if err != nil {
		b.log().Warn("brief: listing users failed", "error", err)
		return
	}
	for _, u := range users {
		s, err := b.Coach.Store.Settings(ctx, u)
		if err != nil {
			continue
		}
		loc := b.Coach.location(s)
		local := b.now().In(loc)
		if local.Hour() < hour {
			continue
		}
		day := domain.Day(local, loc)
		b.mu.Lock()
		if b.lastSent == nil {
			b.lastSent = map[uuid.UUID]time.Time{}
		}
		done := b.lastSent[u].Equal(day)
		b.mu.Unlock()
		if done {
			continue
		}
		br, err := b.Build(ctx, u)
		if err != nil {
			b.log().Warn("brief: build failed", "error", err)
			continue
		}
		if err := b.Sender.Send(ctx, notify.Message{Title: "Morning brief", Body: br.Text, ClickURL: strings.TrimRight(b.AppURL, "/") + "/brief", Tags: []string{"sunrise"}}); err != nil {
			b.log().Warn("brief: push failed", "error", err) // not marked sent: the next tick retries
			continue
		}
		b.mu.Lock()
		b.lastSent[u] = day
		b.mu.Unlock()
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
