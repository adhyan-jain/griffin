package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
		for _, tool := range []string{"playerctl", "dbus-send", "gtk-launch", "xdg-open", "hyprctl", "swaymsg"} {
			if _, err := exec.LookPath(tool); err == nil {
				env.Tools = append(env.Tools, tool)
			}
		}
	} else if env.OS == "darwin" {
		env.Distro = "macOS"
		for _, tool := range []string{"osascript", "open"} {
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

// LaunchApp launches a desktop application headlessly in the background.
func (s *Service) LaunchApp(ctx context.Context, appName string) (string, error) {
	name := strings.TrimSpace(appName)
	if name == "" {
		return "", fmt.Errorf("app name cannot be empty")
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
