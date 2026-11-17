package notify

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"testing"
)

// DesktopSender delivers notifications using the host OS desktop notification service.
type DesktopSender struct{}

func (d *DesktopSender) Send(ctx context.Context, m Message) error {
	if testing.Testing() {
		return nil
	}
	urgency := "normal"
	if m.Priority >= 4 {
		urgency = "critical"
	}
	switch runtime.GOOS {
	case "linux":
		if p, err := exec.LookPath("notify-send"); err == nil {
			cmd := exec.CommandContext(ctx, p, "-u", urgency, "-a", "Griffin", m.Title, m.Body)
			return cmd.Run()
		}
	case "darwin":
		if p, err := exec.LookPath("osascript"); err == nil {
			script := fmt.Sprintf(`display notification %q with title %q`, m.Body, m.Title)
			return exec.CommandContext(ctx, p, "-e", script).Run()
		}
	}
	return nil
}

// MultiSender delivers a message to all configured senders.
type MultiSender []Sender

func (ms MultiSender) Send(ctx context.Context, m Message) error {
	var firstErr error
	for _, s := range ms {
		if s == nil {
			continue
		}
		if err := s.Send(ctx, m); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
