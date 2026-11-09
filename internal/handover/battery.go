// Package handover keeps work from being lost when the laptop is about to die. As the battery drains it
// saves every open project to a new branch (never the owner's branch, index or files), writes a note for
// whoever continues, hands running tasks back to the cloud, and pushes. It only ever adds a new branch.
package handover

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Level says how urgent things are. Levels only go up while the battery drains.
type Level int

const (
	LevelOK    Level = iota
	LevelSave        // save every open project to a handover branch, locally
	LevelPush        // hand running tasks back and push the branches
	LevelFinal       // the last chance: save and push whatever changed since
)

func (l Level) String() string {
	return [...]string{"ok", "save", "push", "final"}[l]
}

// Thresholds are battery percentages at or below which each level starts.
type Thresholds struct{ Save, Push, Final int }

// DefaultThresholds saves early, because the work that matters (pushing over the network) needs time.
// At 2% a laptop can be gone in seconds, so 2% is the last call and not the plan.
var DefaultThresholds = Thresholds{Save: 15, Push: 5, Final: 2}

// ParseThresholds reads "15,5,2". Empty means the defaults.
func ParseThresholds(s string) (Thresholds, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultThresholds, true
	}
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return Thresholds{}, false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 99 {
			return Thresholds{}, false
		}
		v[i] = n
	}
	t := Thresholds{Save: v[0], Push: v[1], Final: v[2]}
	if !(t.Save > t.Push && t.Push > t.Final) {
		return Thresholds{}, false
	}
	return t, true
}

// LevelFor is the level for a battery reading. A machine on charge is always LevelOK.
func (t Thresholds) LevelFor(percent int, discharging bool) Level {
	switch {
	case !discharging:
		return LevelOK
	case percent <= t.Final:
		return LevelFinal
	case percent <= t.Push:
		return LevelPush
	case percent <= t.Save:
		return LevelSave
	}
	return LevelOK
}

// Reading is one look at the battery.
type Reading struct {
	Percent     int
	Discharging bool
}

// Battery reads the laptop's battery. ok is false on a machine with none (a desktop), so nothing fires.
type Battery interface {
	Read() (r Reading, ok bool)
}

// SysfsBattery reads Linux's /sys/class/power_supply.
type SysfsBattery struct{ Root string }

func (b SysfsBattery) Read() (Reading, bool) {
	root := b.Root
	if root == "" {
		root = "/sys/class/power_supply"
	}
	dirs, _ := filepath.Glob(filepath.Join(root, "BAT*"))
	for _, d := range dirs {
		capRaw, err := os.ReadFile(filepath.Join(d, "capacity"))
		if err != nil {
			continue
		}
		pct, err := strconv.Atoi(strings.TrimSpace(string(capRaw)))
		if err != nil || pct < 0 || pct > 100 {
			continue
		}
		st, _ := os.ReadFile(filepath.Join(d, "status"))
		status := strings.ToLower(strings.TrimSpace(string(st)))
		return Reading{Percent: pct, Discharging: status == "discharging"}, true
	}
	return Reading{}, false
}
