package antigravity

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProbe struct {
	freeMB int
	load   float64
	cpus   int
}

func (f *fakeProbe) MemAvailableMB() (int, error) { return f.freeMB, nil }
func (f *fakeProbe) Load1() (float64, error)      { return f.load, nil }
func (f *fakeProbe) CPUs() int                    { return f.cpus }

func newTestLimiter(p *fakeProbe, ceiling int) *Limiter {
	return NewLimiter(LimiterConfig{Ceiling: ceiling, ReserveMB: 1000, AgentMB: 500, LoadFactor: 1.5, Probe: p})
}

func TestLimiter_ScalesWithRoomUpToTheCeiling(t *testing.T) {
	p := &fakeProbe{freeMB: 8000, load: 0.2, cpus: 2}
	l := newTestLimiter(p, 4)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		require.True(t, l.HasCapacity(), "agent %d should fit", i+1)
		require.NoError(t, l.Acquire(ctx))
	}
	assert.Equal(t, 4, l.Running())
	assert.False(t, l.HasCapacity(), "the ceiling is a hard stop however much room there is")
	l.Release(0)
	assert.True(t, l.HasCapacity(), "a finished agent frees a slot")
}

func TestLimiter_StopsWhenMemoryIsTight(t *testing.T) {
	p := &fakeProbe{freeMB: 2500, load: 0.1, cpus: 2} // 2500 - 1000 reserve = 1500 for new agents
	l := newTestLimiter(p, 8)
	require.NoError(t, l.Acquire(context.Background())) // first one always runs
	assert.True(t, l.HasCapacity(), "1500 MB free beyond the reserve fits one more 500 MB agent (with headroom)")
	require.NoError(t, l.Acquire(context.Background()))
	p.freeMB = 1400 // now only 400 beyond the reserve: not enough for another
	assert.False(t, l.HasCapacity())
	p.freeMB = 3000
	assert.True(t, l.HasCapacity(), "memory freed up again")
}

func TestLimiter_StopsWhenTheMachineIsBusy(t *testing.T) {
	p := &fakeProbe{freeMB: 8000, load: 0.5, cpus: 2}
	l := newTestLimiter(p, 8)
	require.NoError(t, l.Acquire(context.Background()))
	assert.True(t, l.HasCapacity())
	p.load = 3.5 // above 2 CPUs x 1.5
	assert.False(t, l.HasCapacity())
}

func TestLimiter_AlwaysAdmitsTheFirstAgentSoWorkNeverStalls(t *testing.T) {
	p := &fakeProbe{freeMB: 100, load: 9, cpus: 2} // crowded and low on memory
	l := newTestLimiter(p, 4)
	assert.True(t, l.HasCapacity())
	require.NoError(t, l.Acquire(context.Background()))
	assert.False(t, l.HasCapacity(), "but a second one waits")
}

func TestLimiter_LearnsWhatAnAgentReallyCosts(t *testing.T) {
	p := &fakeProbe{freeMB: 8000, load: 0.1, cpus: 2}
	l := newTestLimiter(p, 4)
	require.Equal(t, 500, l.EstimateMB())
	for i := 0; i < 8; i++ {
		require.NoError(t, l.Acquire(context.Background()))
		l.Release(1200) // agents turn out to use ~1.2 GB
	}
	assert.Greater(t, l.EstimateMB(), 1000, "the estimate moves toward what runs actually used")
	require.NoError(t, l.Acquire(context.Background()))
	p.freeMB = 2300 // 1300 beyond the reserve is no longer enough for one more at ~1.16 GB plus 20% headroom (~1390)
	assert.False(t, l.HasCapacity())
	l.Release(0)
	assert.Greater(t, l.EstimateMB(), 1000, "a run with unknown memory (0) does not reset what was learned")
}

func TestLimiter_AcquireWaitsThenProceedsAndHonoursCancellation(t *testing.T) {
	p := &fakeProbe{freeMB: 8000, load: 0.1, cpus: 2}
	l := newTestLimiter(p, 1)
	require.NoError(t, l.Acquire(context.Background()))

	got := make(chan error, 1)
	go func() { got <- l.Acquire(context.Background()) }()
	select {
	case <-got:
		t.Fatal("a second agent must wait while the ceiling is reached")
	case <-time.After(150 * time.Millisecond):
	}
	l.Release(0)
	select {
	case err := <-got:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting agent should start once a slot frees")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, l.Acquire(ctx), context.Canceled, "a cancelled wait returns instead of hanging")
}

func TestGroupRSSMB_MeasuresTheCurrentProcessGroup(t *testing.T) {
	if _, err := os.Stat("/proc/self/status"); err != nil {
		t.Skip("no /proc on this OS")
	}
	// The test process is in its own group on Linux when run by `go test`; at least its own memory counts.
	assert.Greater(t, groupRSSMB(syscall.Getpgrp()), 0)
	assert.Equal(t, 0, groupRSSMB(999999999), "an unknown group measures zero")
}
