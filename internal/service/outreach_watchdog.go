package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
)

// OutreachWatchdog monitors outreach targets that have exceeded their follow-up duration
// without a response and automatically transitions them to drafted follow-ups.
type OutreachWatchdog struct {
	Outreach *repository.OutreachRepository
	Drafter  *OutreachDrafter
	Sender   notify.Sender
	Logger   *slog.Logger
	Now      func() time.Time
}

func (w *OutreachWatchdog) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *OutreachWatchdog) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

// SweepDueFollowups checks targets where status is 'sent' and next_followup_at <= now,
// moves them through followup_due -> drafted with a generated follow-up message,
// and optionally alerts the owner via notification sender.
func (w *OutreachWatchdog) SweepDueFollowups(ctx context.Context, userID uuid.UUID) ([]*domain.OutreachTarget, error) {
	if w.Outreach == nil {
		return nil, nil
	}
	now := w.now()
	due, err := w.Outreach.ListDueFollowups(ctx, userID, now, 10)
	if err != nil {
		return nil, fmt.Errorf("list due followups: %w", err)
	}

	var processed []*domain.OutreachTarget
	for _, target := range due {
		// 1. Transition sent -> followup_due
		toDue := domain.OutreachFollowupDue
		tUpdated, err := w.Outreach.UpdateTarget(ctx, userID, target.ID, repository.TargetUpdate{
			To:    &toDue,
			Event: "Follow-up period elapsed; watchdog staged for follow-up drafting",
		})
		if err != nil {
			w.log().Warn("watchdog: transition to followup_due failed", "target_id", target.ID, "error", err)
			continue
		}

		// 2. Synthesize follow-up draft
		followUpDraft := w.synthesizeDraft(ctx, userID, tUpdated)

		// 3. Transition followup_due -> drafted with the new draft text
		toDrafted := domain.OutreachDrafted
		tFinal, err := w.Outreach.UpdateTarget(ctx, userID, target.ID, repository.TargetUpdate{
			To:    &toDrafted,
			Draft: &followUpDraft,
			Event: fmt.Sprintf("Watchdog auto-generated follow-up draft (attempt #%d)", tUpdated.FollowupsSent+1),
		})
		if err != nil {
			w.log().Warn("watchdog: transition to drafted failed", "target_id", target.ID, "error", err)
			continue
		}

		processed = append(processed, tFinal)
	}

	if len(processed) > 0 && w.Sender != nil {
		_ = w.Sender.Send(ctx, notify.Message{
			Title: "Outreach follow-ups staged",
			Body:  fmt.Sprintf("%d follow-up draft(s) ready for 1-tap review and send.", len(processed)),
			Tags:  []string{"bell"},
		})
	}

	return processed, nil
}

func (w *OutreachWatchdog) synthesizeDraft(ctx context.Context, userID uuid.UUID, t *domain.OutreachTarget) string {
	name := t.Name
	if name == "" {
		name = t.Handle
	}
	if w.Drafter != nil {
		camp, err := w.Outreach.GetCampaign(ctx, userID, t.CampaignID)
		if err == nil {
			draft, err := w.Drafter.Draft(ctx, camp, t)
			if err == nil && strings.TrimSpace(draft) != "" {
				return draft
			}
		}
	}

	return fmt.Sprintf("Hi %s,\n\nFollowing up on my previous note to see if you had a chance to review it. Looking forward to connecting when convenient.\n\nBest,\nAdhyan", name)
}
