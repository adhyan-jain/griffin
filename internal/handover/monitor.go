package handover

import (
	"context"
	"time"
)

// Monitor watches the battery and calls OnLevel once each time a more urgent level is reached. Plugging
// in resets it, so the next drain starts again from the first level.
type Monitor struct {
	Battery    Battery
	Thresholds Thresholds
	// Interval between readings; it shortens when the battery is low. Default 30 s.
	Interval time.Duration
	// OnLevel runs synchronously, so a slow handler delays the next reading rather than overlapping it.
	OnLevel func(ctx context.Context, level Level, r Reading)
	// OnRecover runs once when the machine is back on charge (or above every threshold) after a level fired.
	OnRecover func(ctx context.Context, r Reading)
}

// Run blocks until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	interval := m.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	reached := LevelOK
	for {
		next := interval
		if r, ok := m.Battery.Read(); ok {
			lvl := m.Thresholds.LevelFor(r.Percent, r.Discharging)
			switch {
			case lvl == LevelOK:
				if reached != LevelOK && m.OnRecover != nil {
					m.OnRecover(ctx, r)
				}
				reached = LevelOK
			case lvl > reached:
				reached = lvl
				m.OnLevel(ctx, lvl, r)
			}
			if lvl != LevelOK && interval > 5*time.Second {
				next = 5 * time.Second // every second counts once the battery is low
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(next):
		}
	}
}
