package desktop

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Environment holds auto-detected details about the host OS and environment.
type Environment struct {
	OS          string   `json:"os"`
	Distro      string   `json:"distro,omitempty"`
	Desktop     string   `json:"desktop,omitempty"`
	SessionType string   `json:"session_type,omitempty"`
	Shell       string   `json:"shell,omitempty"`
	Tools       []string `json:"tools,omitempty"`
}

// Service provides headless control over desktop applications and media playback.
type Service struct {
	Env    Environment
	Logger *slog.Logger
}

// NewService creates a desktop service and inspects the local host environment.
func NewService(logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{
		Env:    detectEnvironment(),
		Logger: logger,
	}
	logger.Info("desktop service initialized", "os", s.Env.OS, "distro", s.Env.Distro, "desktop", s.Env.Desktop)
	return s
}

func detectEnvironment() Environment {
	env := Environment{
		OS:          runtime.GOOS,
		Desktop:     os.Getenv("XDG_CURRENT_DESKTOP"),
		SessionType: os.Getenv("XDG_SESSION_TYPE"),
		Shell:       os.Getenv("SHELL"),
	}

	if env.OS == "linux" {
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					env.Distro = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
					break
				}
			}
		}
		if env.Distro == "" {
			env.Distro = "Linux"
		}
		for _, tool := range []string{"playerctl", "dbus-send", "gtk-launch", "xdg-open", "hyprctl", "swaymsg", "grim", "import", "scrot", "adb", "notify-send"} {
			if _, err := exec.LookPath(tool); err == nil {
				env.Tools = append(env.Tools, tool)
			}
		}
	} else if env.OS == "darwin" {
		env.Distro = "macOS"
		for _, tool := range []string{"osascript", "open", "screencapture", "adb"} {
			if _, err := exec.LookPath(tool); err == nil {
				env.Tools = append(env.Tools, tool)
			}
		}
	} else if env.OS == "windows" {
		env.Distro = "Windows"
		env.Tools = append(env.Tools, "powershell")
	}

	return env
}

// MediaOp is a media playback operation.
type MediaOp string

const (
	MediaNext      MediaOp = "next"
	MediaPrevious  MediaOp = "previous"
	MediaPlay      MediaOp = "play"
	MediaPause     MediaOp = "pause"
	MediaPlayPause MediaOp = "play-pause"
	MediaStatus    MediaOp = "status"
)

// MediaResult describes the result of a media operation.
type MediaResult struct {
	Op      string `json:"op"`
	Player  string `json:"player,omitempty"`
	Status  string `json:"status,omitempty"`
	Track   string `json:"track,omitempty"`
	Message string `json:"message"`
}

// Media executes an instant media playback command against Spotify or active player.
func (s *Service) Media(ctx context.Context, op MediaOp, targetPlayer string) (*MediaResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	switch s.Env.OS {
	case "linux":
		return s.mediaLinux(ctx, op, targetPlayer)
	case "darwin":
		return s.mediaDarwin(ctx, op, targetPlayer)
	case "windows":
		return s.mediaWindows(ctx, op, targetPlayer)
	default:
		return nil, fmt.Errorf("media control unsupported on OS: %s", s.Env.OS)
	}
}

func (s *Service) mediaLinux(ctx context.Context, op MediaOp, targetPlayer string) (*MediaResult, error) {
	player := targetPlayer
	if player == "" {
		player = "spotify"
	}

	// 1. Try playerctl (fastest and standard MPRIS D-Bus client on Linux)
	if _, err := exec.LookPath("playerctl"); err == nil {
		args := []string{"-p", player}
		switch op {
		case MediaNext:
			args = append(args, "next")
		case MediaPrevious:
			args = append(args, "previous")
		case MediaPlay:
			args = append(args, "play")
		case MediaPause:
			args = append(args, "pause")
		case MediaPlayPause:
			args = append(args, "play-pause")
		case MediaStatus:
			args = append(args, "status")
		default:
			args = append(args, "play-pause")
		}

		out, err := exec.CommandContext(ctx, "playerctl", args...).CombinedOutput()
		outputStr := strings.TrimSpace(string(out))
		if err != nil {
			// Try without targeting specific player in case active player is chromium or other
			out2, err2 := exec.CommandContext(ctx, "playerctl", string(op)).CombinedOutput()
			if err2 == nil {
				return s.formatMediaResult(op, "active", strings.TrimSpace(string(out2)), ctx)
			}
			return nil, fmt.Errorf("playerctl error: %s (%w)", outputStr, err)
		}
		return s.formatMediaResult(op, player, outputStr, ctx)
	}

	// 2. Fallback to dbus-send
	if _, err := exec.LookPath("dbus-send"); err == nil {
		method := "PlayPause"
		switch op {
		case MediaNext:
			method = "Next"
		case MediaPrevious:
			method = "Previous"
		case MediaPlay:
			method = "Play"
		case MediaPause:
			method = "Pause"
		case MediaPlayPause:
			method = "PlayPause"
		}
		dest := fmt.Sprintf("org.mpris.MediaPlayer2.%s", player)
		cmd := exec.CommandContext(ctx, "dbus-send", "--print-reply", "--dest="+dest,
			"/org/mpris/MediaPlayer2", "org.mpris.MediaPlayer2.Player."+method)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("dbus error: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		return &MediaResult{
			Op:      string(op),
			Player:  player,
			Message: fmt.Sprintf("Executed %s on %s via D-Bus.", op, player),
		}, nil
	}

	return nil, fmt.Errorf("no media control tool (playerctl or dbus-send) found on Linux host")
}

func (s *Service) formatMediaResult(op MediaOp, player, output string, ctx context.Context) (*MediaResult, error) {
	res := &MediaResult{
		Op:     string(op),
		Player: player,
		Status: output,
	}

	// Query track metadata if available
	metaCmd := exec.CommandContext(ctx, "playerctl", "-p", player, "metadata", "--format", "{{artist}} - {{title}}")
	if metaOut, err := metaCmd.Output(); err == nil {
		res.Track = strings.TrimSpace(string(metaOut))
	}

	switch op {
	case MediaNext:
		if res.Track != "" {
			res.Message = fmt.Sprintf("Skipped to next track: %s.", res.Track)
		} else {
			res.Message = "Skipped to next track."
		}
	case MediaPrevious:
		if res.Track != "" {
			res.Message = fmt.Sprintf("Moved to previous track: %s.", res.Track)
		} else {
			res.Message = "Moved to previous track."
		}
	case MediaPause:
		res.Message = "Paused playback."
	case MediaPlay:
		if res.Track != "" {
			res.Message = fmt.Sprintf("Playing %s.", res.Track)
		} else {
			res.Message = "Resumed playback."
		}
	case MediaPlayPause:
		res.Message = "Toggled playback."
	case MediaStatus:
		if res.Track != "" {
			res.Message = fmt.Sprintf("Current track: %s (%s).", res.Track, output)
		} else {
			res.Message = fmt.Sprintf("Status: %s.", output)
		}
	}

	return res, nil
}

func (s *Service) mediaDarwin(ctx context.Context, op MediaOp, player string) (*MediaResult, error) {
	target := player
	if target == "" {
		target = "Spotify"
	}
	var script string
	switch op {
	case MediaNext:
		script = fmt.Sprintf("tell application \"%s\" to next track", target)
	case MediaPrevious:
		script = fmt.Sprintf("tell application \"%s\" to previous track", target)
	case MediaPlay:
		script = fmt.Sprintf("tell application \"%s\" to play", target)
	case MediaPause:
		script = fmt.Sprintf("tell application \"%s\" to pause", target)
	case MediaPlayPause:
		script = fmt.Sprintf("tell application \"%s\" to playpause", target)
	default:
		script = fmt.Sprintf("tell application \"%s\" to playpause", target)
	}

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("osascript error: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return &MediaResult{
		Op:      string(op),
		Player:  target,
		Message: fmt.Sprintf("Executed %s on %s via AppleScript.", op, target),
	}, nil
}

func (s *Service) mediaWindows(ctx context.Context, op MediaOp, player string) (*MediaResult, error) {
	// Windows virtual key codes: Next=0xB0, Prev=0xB1, Stop=0xB2, PlayPause=0xB3
	var vk string
	switch op {
	case MediaNext:
		vk = "0xB0"
	case MediaPrevious:
		vk = "0xB1"
	case MediaPlay, MediaPause, MediaPlayPause:
		vk = "0xB3"
	default:
		vk = "0xB3"
	}
	ps := fmt.Sprintf(`$wscript = New-Object -ComObject WScript.Shell; $wscript.SendKeys([char]%s)`, vk)
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("powershell media error: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	return &MediaResult{
		Op:      string(op),
		Player:  player,
		Message: fmt.Sprintf("Simulated media key %s on Windows.", op),
	}, nil
}

var paymentKeywords = []string{
	"paisa", "gpay", "google pay", "phonepe", "paytm", "bhim", "cred",
	"bank", "sbi", "hdfc", "icici", "axis", "kotak", "upi", "razorpay",
	"stripe", "paypal", "wallet", "checkout", "netbanking",
}

// IsPaymentTarget reports whether an app name, package, or URL represents a payment or banking target.
func IsPaymentTarget(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range paymentKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// LaunchApp launches a desktop application headlessly in the background.
func (s *Service) LaunchApp(ctx context.Context, appName string) (string, error) {
	name := strings.TrimSpace(appName)
	if name == "" {
		return "", fmt.Errorf("app name cannot be empty")
	}
	if IsPaymentTarget(name) {
		return "", fmt.Errorf("launching or automating payment/banking application %q is strictly forbidden", name)
	}

	switch s.Env.OS {
	case "linux":
		// Check direct binary
		if bin, err := exec.LookPath(strings.ToLower(name)); err == nil {
			cmd := exec.Command(bin)
			cmd.Stdout = nil
			cmd.Stderr = nil
			if err := cmd.Start(); err != nil {
				return "", fmt.Errorf("could not start %s: %w", name, err)
			}
			return fmt.Sprintf("Launched %s.", name), nil
		}
		// Try gtk-launch or xdg-open
		if _, err := exec.LookPath("gtk-launch"); err == nil {
			cmd := exec.Command("gtk-launch", strings.ToLower(name))
			if err := cmd.Start(); err == nil {
				return fmt.Sprintf("Launched %s via desktop launcher.", name), nil
			}
		}
		return "", fmt.Errorf("application %q not found on Linux", name)
	case "darwin":
		cmd := exec.CommandContext(ctx, "open", "-a", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("open error: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		return fmt.Sprintf("Opened %s on macOS.", name), nil
	case "windows":
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Start-Process", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("start-process error: %s (%w)", strings.TrimSpace(string(out)), err)
		}
		return fmt.Sprintf("Started %s on Windows.", name), nil
	default:
		return "", fmt.Errorf("launching apps unsupported on OS: %s", s.Env.OS)
	}
}

// ScreenshotResult holds metadata and data about a captured screen or phone image.
type ScreenshotResult struct {
	Target   string `json:"target"` // "screen" or "phone"
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
	Data     string `json:"data,omitempty"` // base64-encoded PNG image
}

// Screenshot captures the desktop or connected phone screen, saving it to disk and returning its metadata.
func (s *Service) Screenshot(ctx context.Context, target string, customSavePath string) (*ScreenshotResult, error) {
	savePath := strings.TrimSpace(customSavePath)
	if savePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		dir := filepath.Join(home, ".griffin-host", "screenshots")
		savePath = filepath.Join(dir, fmt.Sprintf("screenshot_%d.png", time.Now().UnixNano()))
	}

	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return nil, fmt.Errorf("could not create screenshot directory: %w", err)
	}

	normalizedTarget := strings.ToLower(strings.TrimSpace(target))
	if normalizedTarget == "phone" {
		adbBin := "adb"
		if _, err := exec.LookPath(adbBin); err != nil {
			if home, err := os.UserHomeDir(); err == nil {
				cand := filepath.Join(home, "Android", "Sdk", "platform-tools", "adb")
				if _, err := os.Stat(cand); err == nil {
					adbBin = cand
				}
			}
		}
		// Financial Safety Guard: verify that no payment or banking app is in focus on the connected phone
		if focusOut, err := exec.CommandContext(ctx, adbBin, "shell", "dumpsys", "window").CombinedOutput(); err == nil {
			focusStr := string(focusOut)
			for _, line := range strings.Split(focusStr, "\n") {
				if strings.Contains(line, "mCurrentFocus") || strings.Contains(line, "mFocusedApp") {
					if IsPaymentTarget(line) {
						return nil, fmt.Errorf("payment or banking application is currently in focus on phone; automation and screen capture are strictly blocked for financial security")
					}
				}
			}
		}

		cmd := exec.CommandContext(ctx, adbBin, "exec-out", "screencap", "-p")
		outFile, err := os.Create(savePath)
		if err != nil {
			return nil, fmt.Errorf("could not create screenshot file: %w", err)
		}
		cmd.Stdout = outFile
		var errBuf strings.Builder
		cmd.Stderr = &errBuf
		runErr := cmd.Run()
		outFile.Close()
		if runErr != nil {
			_ = os.Remove(savePath)
			return nil, fmt.Errorf("phone screencap failed: %s (%w)", strings.TrimSpace(errBuf.String()), runErr)
		}
	} else {
		// Desktop screen capture
		switch s.Env.OS {
		case "linux":
			var captureErr error
			captured := false
			if _, err := exec.LookPath("grim"); err == nil {
				out, err := exec.CommandContext(ctx, "grim", savePath).CombinedOutput()
				if err == nil {
					captured = true
				} else {
					captureErr = fmt.Errorf("grim error: %s (%w)", strings.TrimSpace(string(out)), err)
				}
			}
			if !captured {
				if _, err := exec.LookPath("import"); err == nil {
					out, err := exec.CommandContext(ctx, "import", "-window", "root", savePath).CombinedOutput()
					if err == nil {
						captured = true
					} else {
						captureErr = fmt.Errorf("import error: %s (%w)", strings.TrimSpace(string(out)), err)
					}
				}
			}
			if !captured {
				if _, err := exec.LookPath("scrot"); err == nil {
					out, err := exec.CommandContext(ctx, "scrot", savePath).CombinedOutput()
					if err == nil {
						captured = true
					} else {
						captureErr = fmt.Errorf("scrot error: %s (%w)", strings.TrimSpace(string(out)), err)
					}
				}
			}
			if !captured {
				if captureErr != nil {
					return nil, captureErr
				}
				return nil, fmt.Errorf("no screenshot tool found on Linux (need grim, import, or scrot)")
			}
		case "darwin":
			cmd := exec.CommandContext(ctx, "screencapture", "-x", savePath)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("screencapture error: %s (%w)", strings.TrimSpace(string(out)), err)
			}
		case "windows":
			ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]; $b = New-Object System.Drawing.Bitmap([System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width, [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height); $g = [System.Drawing.Graphics]::FromImage($b); $g.CopyFromScreen((New-Object System.Drawing.Point(0,0)), (New-Object System.Drawing.Point(0,0)), $b.Size); $b.Save('%s'); $g.Dispose(); $b.Dispose()`, savePath)
			cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("powershell screencap error: %s (%w)", strings.TrimSpace(string(out)), err)
			}
		default:
			return nil, fmt.Errorf("screenshots unsupported on OS: %s", s.Env.OS)
		}
	}

	fi, err := os.Stat(savePath)
	if err != nil || fi.Size() == 0 {
		return nil, fmt.Errorf("screenshot capture produced an empty file")
	}

	dataBytes, err := os.ReadFile(savePath)
	if err != nil {
		return nil, fmt.Errorf("reading captured screenshot: %w", err)
	}

	targetName := "screen"
	if normalizedTarget == "phone" {
		targetName = "phone"
	}

	return &ScreenshotResult{
		Target:   targetName,
		Path:     savePath,
		MimeType: "image/png",
		Size:     fi.Size(),
		Data:     base64.StdEncoding.EncodeToString(dataBytes),
	}, nil
}

// Notify sends a native desktop alert banner using the host's notification daemon (e.g. notify-send on Linux).
func Notify(ctx context.Context, title, body, urgency string) error {
	if testing.Testing() {
		return nil
	}
	if urgency == "" {
		urgency = "normal"
	}
	switch runtime.GOOS {
	case "linux":
		if p, err := exec.LookPath("notify-send"); err == nil {
			cmd := exec.CommandContext(ctx, p, "-u", urgency, "-a", "Griffin", title, body)
			return cmd.Run()
		}
	case "darwin":
		if p, err := exec.LookPath("osascript"); err == nil {
			script := fmt.Sprintf(`display notification %q with title %q`, body, title)
			return exec.CommandContext(ctx, p, "-e", script).Run()
		}
	case "windows":
		ps := fmt.Sprintf(`[reflection.assembly]::loadwithpartialname('System.Windows.Forms'); [System.Windows.Forms.MessageBox]::Show('%s', '%s')`, body, title)
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps)
		return cmd.Run()
	}
	return nil
}

// Notify sends a desktop notification via the service instance.
func (s *Service) Notify(ctx context.Context, title, body, urgency string) error {
	return Notify(ctx, title, body, urgency)
}
