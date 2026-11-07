package antigravity

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Probe reports how much room the machine has. It is an interface so tests can supply numbers.
type Probe interface {
	MemAvailableMB() (int, error)
	Load1() (float64, error)
	CPUs() int
}

// procProbe reads Linux's /proc. Where it cannot (another OS), the limiter falls back to its ceiling alone.
type procProbe struct{}

func (procProbe) CPUs() int { return runtime.NumCPU() }

func (procProbe) MemAvailableMB() (int, error) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, err := strconv.Atoi(f[1])
				return kb / 1024, err
			}
		}
	}
	return 0, os.ErrNotExist
}

func (procProbe) Load1() (float64, error) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, os.ErrNotExist
	}
	return strconv.ParseFloat(f[0], 64)
}

// LimiterConfig tunes how many agents may run at once.
type LimiterConfig struct {
	// Ceiling is the most agents ever allowed at once, whatever the machine looks like.
	Ceiling int
	// ReserveMB is memory kept free for the operating system and the worker itself.
	ReserveMB int
	// AgentMB is the starting guess for one agent's memory; it is replaced by what finished runs actually used.
	AgentMB int
	// LoadFactor: a new agent starts only while the 1-minute load average is below CPUs x LoadFactor.
	LoadFactor float64
	Probe      Probe
}

// Limiter admits agents by what the machine can hold, not by a fixed number. More queued tasks means more
// run at once, until memory or CPU says stop. One agent is always admitted when none is running, so work
// never stalls even on a crowded machine.
type Limiter struct {
	cfg LimiterConfig

	mu      sync.Mutex
	running int
	estMB   float64 // learned memory per agent
}

func NewLimiter(cfg LimiterConfig) *Limiter {
	if cfg.Ceiling <= 0 {
		cfg.Ceiling = 1
	}
	if cfg.ReserveMB <= 0 {
		cfg.ReserveMB = 900
	}
	if cfg.AgentMB <= 0 {
		cfg.AgentMB = 500
	}
	if cfg.LoadFactor <= 0 {
		cfg.LoadFactor = 1.5
	}
	if cfg.Probe == nil {
		cfg.Probe = procProbe{}
	}
	return &Limiter{cfg: cfg, estMB: float64(cfg.AgentMB)}
}

// canAdmitLocked decides whether one more agent fits. The caller holds l.mu.
func (l *Limiter) canAdmitLocked() bool {
	if l.running >= l.cfg.Ceiling {
		return false
	}
	if l.running == 0 {
		return true // always make progress
	}
	if free, err := l.cfg.Probe.MemAvailableMB(); err == nil {
		if free-l.cfg.ReserveMB < int(l.estMB*1.2) { // 20% headroom on the learned size
			return false
		}
	}
	if load, err := l.cfg.Probe.Load1(); err == nil {
		if load >= float64(l.cfg.Probe.CPUs())*l.cfg.LoadFactor {
			return false
		}
	}
	return true
}

// HasCapacity reports whether another agent would be admitted right now (used before claiming a task).
func (l *Limiter) HasCapacity() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.canAdmitLocked()
}

// Acquire waits until an agent can start, or the context ends.
func (l *Limiter) Acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.canAdmitLocked() {
			l.running++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Release frees a slot. peakMB is the most memory that run used (0 if unknown); it refines the estimate so
// admission follows what agents really cost on this machine.
func (l *Limiter) Release(peakMB int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running > 0 {
		l.running--
	}
	if peakMB > 50 { // ignore implausibly small samples
		l.estMB = 0.7*l.estMB + 0.3*float64(peakMB)
	}
}

// Running and EstimateMB are for logs and tests.
func (l *Limiter) Running() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

func (l *Limiter) EstimateMB() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(l.estMB)
}

// groupRSSMB sums the resident memory of every process in the process group led by pgid, so an agent that
// spawns helpers is measured as a whole. It returns 0 where /proc is unavailable.
func groupRSSMB(pgid int) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	totalKB := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		// pgrp is the 5th field after the ")" that closes the process name.
		s := string(stat)
		i := strings.LastIndex(s, ")")
		if i < 0 {
			continue
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 3 {
			continue
		}
		if g, err := strconv.Atoi(f[2]); err != nil || g != pgid {
			continue
		}
		status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				if kf := strings.Fields(line); len(kf) >= 2 {
					if kb, err := strconv.Atoi(kf[1]); err == nil {
						totalKB += kb
					}
				}
			}
		}
	}
	return totalKB / 1024
}
