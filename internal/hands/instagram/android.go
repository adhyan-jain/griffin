package instagram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/whatsapp"
)

// AndroidConfig configures the Instagram Android driver.
type AndroidConfig struct {
	ADB     whatsapp.ADB
	Package string // default: com.instagram.android
	Timeout time.Duration
	Logger  *slog.Logger
}

// Android drives the Instagram app on a phone attached via adb (USB or wireless debugging).
type Android struct {
	cfg AndroidConfig
}

// NewAndroid creates an Instagram Android driver.
func NewAndroid(cfg AndroidConfig) *Android {
	if cfg.ADB == nil {
		cfg.ADB = whatsapp.ExecADB{}
	}
	if cfg.Package == "" {
		cfg.Package = "com.instagram.android"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Android{cfg: cfg}
}

// Name implements Driver.
func (a *Android) Name() string { return "instagram-android" }

// Device implements Driver.
func (a *Android) Device() string { return "phone" }

// Status checks whether the driver can reach a phone with Instagram installed.
func (a *Android) Status(ctx context.Context) error {
	st, err := a.cfg.ADB.Run(ctx, "get-state")
	if err != nil || strings.TrimSpace(st) != "device" {
		return errors.New("no phone is connected with adb (connect by USB or wireless debugging and allow this computer)")
	}
	pk, err := a.cfg.ADB.Run(ctx, "shell", "pm", "list", "packages", a.cfg.Package)
	if err != nil || !strings.Contains(pk, a.cfg.Package) {
		return fmt.Errorf("Instagram (%s) is not installed on the connected phone", a.cfg.Package)
	}
	return nil
}

func (a *Android) ready(ctx context.Context) error {
	if err := a.Status(ctx); err != nil {
		return fmt.Errorf("%w: %v", whatsapp.ErrNotReady, err)
	}
	return nil
}

var safeUsername = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Do implements Driver.
func (a *Android) Do(ctx context.Context, act *domain.MessagingAction) (*whatsapp.Result, error) {
	if act.Channel != "" && act.Channel != domain.MessagingChannelInstagram {
		return nil, fmt.Errorf("%w: instagram-android driver does not handle %q", whatsapp.ErrNotReady, act.Channel)
	}
	if err := a.ready(ctx); err != nil {
		return nil, err
	}

	switch act.Op {
	case domain.MessagingSend:
		target := strings.TrimPrefix(act.Target(), "@")
		if !safeUsername.MatchString(target) {
			return nil, fmt.Errorf("%w: invalid instagram username %q", domain.ErrValidation, target)
		}

		// ig.me/m/<username> opens the direct message thread with that user in the Instagram app
		dmURL := fmt.Sprintf("https://ig.me/m/%s", url.PathEscape(target))
		if _, err := a.cfg.ADB.Run(ctx, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", dmURL, "-p", a.cfg.Package); err != nil {
			return nil, fmt.Errorf("%w: could not open Instagram chat: %v", whatsapp.ErrNotReady, err)
		}

		time.Sleep(1200 * time.Millisecond)

		// Type the text into the direct message composer
		draft := strings.ReplaceAll(act.Text, " ", "%s")
		if _, err := a.cfg.ADB.Run(ctx, "shell", "input", "text", draft); err != nil {
			return nil, fmt.Errorf("%w: could not type message into Instagram: %v", whatsapp.ErrNotReady, err)
		}

		time.Sleep(300 * time.Millisecond)

		// Keycode 66 is KEYCODE_ENTER
		if _, err := a.cfg.ADB.Run(ctx, "shell", "input", "keyevent", "66"); err != nil {
			return nil, fmt.Errorf("%w: could not send message in Instagram: %v", whatsapp.ErrUnconfirmed, err)
		}

		return &whatsapp.Result{
			Via:  "instagram-android",
			Sent: true,
			Notes: []string{
				fmt.Sprintf("Sent via Instagram on connected Android phone to @%s", target),
			},
		}, nil

	case domain.MessagingUnread:
		_, _ = a.cfg.ADB.Run(ctx, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", "https://instagram.com/direct_v2", "-p", a.cfg.Package)
		return &whatsapp.Result{
			Via: "instagram-android",
			Notes: []string{
				"Instagram Direct inbox opened on connected Android phone",
			},
		}, nil

	case domain.MessagingReadRecent:
		return &whatsapp.Result{
			Via: "instagram-android",
			Notes: []string{
				"Reading Instagram chat history requires on-device notification access or screen inspection",
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported op %q for instagram", act.Op)
}
