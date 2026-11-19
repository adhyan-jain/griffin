package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

// AttentionEngine routes inbound alerts through an attention-priority model, deciding when to speak or interrupt.
type AttentionEngine struct {
	TTS    *TTSService
	Sender notify.Sender
	Logger *slog.Logger
}

func (e *AttentionEngine) log() *slog.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

// RouteAlert evaluates host presence and urgency, generating neural speech when an interruption is warranted.
func (e *AttentionEngine) RouteAlert(ctx context.Context, userID uuid.UUID, urgency string, presence domain.HostPresenceState, summary string) (domain.AttentionAction, []byte, error) {
	action := domain.EvaluateAttention(urgency, presence, summary)

	var audioBytes []byte
	if action.ShouldSpeakVoice && e.TTS != nil {
		audio, err := e.TTS.Synthesize(ctx, summary, "ryan")
		if err != nil {
			e.log().Warn("neural speech synthesis failed for attention alert", "error", err)
		} else {
			audioBytes = audio
		}
	}

	if e.Sender != nil {
		switch action.Level {
		case domain.AttentionCriticalInterrupt:
			_ = e.Sender.Send(ctx, notify.Message{
				Title: "CRITICAL: " + summary,
				Body:  "Griffin attention interrupt",
				Tags:  []string{"rotating_light", "urgent"},
			})
		case domain.AttentionHighPriorityBrief:
			_ = e.Sender.Send(ctx, notify.Message{
				Title: "Update: " + summary,
				Body:  "Staged for next briefing",
				Tags:  []string{"bell"},
			})
		}
	}

	return action, audioBytes, nil
}
