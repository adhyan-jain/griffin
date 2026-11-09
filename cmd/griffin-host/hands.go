package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/griffin/core/internal/hands/whatsapp"
)

// Configuration for the WhatsApp hands (environment):
//
//	HOST_WHATSAPP         drivers in order of preference: "web,android" (default), "android,web", "web", "android", "off"
//	WHATSAPP_BROWSER      Chrome, Chromium or Brave binary (default: first one found on PATH)
//	WHATSAPP_PROFILE_DIR  browser profile folder holding the WhatsApp Web login (default ~/.griffin-host/whatsapp-profile)
//	WHATSAPP_HEADLESS     "false" to show the browser window while it works (default true)
//	ADB_BINARY            adb binary (default adb)
//	ANDROID_SERIAL        which phone when several are attached
//	WHATSAPP_PACKAGE      WhatsApp's package on the phone (default com.whatsapp; com.whatsapp.w4b for Business)

// whatsappSetup is the drivers this machine can use.
type whatsappSetup struct {
	messenger *whatsapp.Messenger
	web       *whatsapp.Web
	android   *whatsapp.Android
	adbBinary string
}

// newWhatsApp builds the drivers the machine can back. It returns nil when there are none, in which case
// the host must not advertise the messaging capability.
func newWhatsApp(logger *slog.Logger) *whatsappSetup {
	mode := strings.ToLower(strings.ReplaceAll(envOr("HOST_WHATSAPP", "web,android"), " ", ""))
	if mode == "" || mode == "off" {
		return nil
	}
	s := &whatsappSetup{adbBinary: envOr("ADB_BINARY", "adb")}
	var drivers []whatsapp.Driver
	for _, name := range strings.Split(mode, ",") {
		switch name {
		case "web":
			path, err := whatsapp.FindBrowser(os.Getenv("WHATSAPP_BROWSER"))
			if err != nil {
				logger.Warn("WhatsApp Web is unavailable on this machine", "reason", err.Error())
				continue
			}
			s.web = whatsapp.NewWeb(whatsapp.WebConfig{
				BrowserPath: path,
				ProfileDir:  envOr("WHATSAPP_PROFILE_DIR", whatsapp.DefaultProfileDir()),
				Headless:    strings.ToLower(envOr("WHATSAPP_HEADLESS", "true")) != "false",
				Logger:      logger,
			})
			drivers = append(drivers, s.web)
		case "android":
			if _, err := exec.LookPath(s.adbBinary); err != nil {
				logger.Warn("the phone fallback is unavailable: adb was not found", "binary", s.adbBinary)
				continue
			}
			s.android = whatsapp.NewAndroid(whatsapp.AndroidConfig{
				ADB:     whatsapp.ExecADB{Binary: s.adbBinary, Serial: os.Getenv("ANDROID_SERIAL")},
				Package: envOr("WHATSAPP_PACKAGE", "com.whatsapp"),
				Logger:  logger,
			})
			drivers = append(drivers, s.android)
		default:
			logger.Warn("unknown WhatsApp driver in HOST_WHATSAPP, ignoring", "name", name)
		}
	}
	if len(drivers) == 0 {
		return nil
	}
	s.messenger = whatsapp.NewMessenger(logger, drivers...)
	return s
}

// stateDir is the host's private folder (token, WhatsApp profile, audit log). The permission engine keeps
// the agent out of it.
func stateDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".griffin-host")
	}
	return filepath.Join(os.TempDir(), "griffin-host")
}

// runSubcommand handles `griffin-host whatsapp-login` and `griffin-host whatsapp-check`.
func runSubcommand(ctx context.Context, logger *slog.Logger, name string) error {
	switch name {
	case "whatsapp-login":
		return whatsappLogin(ctx, logger)
	case "whatsapp-check":
		return whatsappCheck(ctx, logger)
	case "handover-check":
		return handoverCheck(ctx, logger)
	case "handover-test":
		return handoverTest(ctx, logger)
	}
	return fmt.Errorf("unknown command %q (expected whatsapp-login, whatsapp-check, handover-check or handover-test)", name)
}

func whatsappLogin(ctx context.Context, logger *slog.Logger) error {
	s := newWhatsApp(logger)
	if s == nil || s.web == nil {
		return errors.New("WhatsApp Web is not available here: no Chrome, Chromium or Brave was found (set WHATSAPP_BROWSER)")
	}
	fmt.Println("Opening WhatsApp Web in a browser window.")
	fmt.Println("On your phone: WhatsApp > Settings > Linked devices > Link a device, then scan the code.")
	fmt.Println("Stop griffin-host first if it is running: the browser profile can only be open once.")
	if err := s.web.Login(ctx, 5*time.Minute); err != nil {
		return err
	}
	fmt.Println("Signed in. Griffin can now use WhatsApp Web on this computer.")
	return nil
}

// whatsappCheck reads without changing anything: is WhatsApp Web signed in, do the selectors match, is a
// phone attached. It is the way to find out, before relying on it, whether WhatsApp changed its page.
func whatsappCheck(ctx context.Context, logger *slog.Logger) error {
	s := newWhatsApp(logger)
	if s == nil {
		return errors.New("no WhatsApp driver is available on this machine (need a browser, or adb with a phone)")
	}
	usable := false
	if s.web != nil {
		rep, err := s.web.Check(ctx)
		switch {
		case err != nil:
			fmt.Printf("WhatsApp Web:   NOT READY: %v\n", err)
		default:
			usable = true
			fmt.Printf("WhatsApp Web:   signed in, %d chats seen\n", rep.Chats)
			for name, ok := range rep.Selectors {
				if !ok {
					fmt.Printf("                selector %q matched nothing: WhatsApp may have changed its page\n", name)
					usable = false
				}
			}
		}
	}
	if s.android != nil {
		if err := s.android.Status(ctx); err != nil {
			fmt.Printf("Phone (adb):    NOT READY: %v\n", err)
		} else {
			usable = true
			fmt.Println("Phone (adb):    connected, WhatsApp installed")
		}
	}
	if !usable {
		return errors.New("WhatsApp is not usable on this machine yet")
	}
	return nil
}
