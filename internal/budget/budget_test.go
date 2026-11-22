package budget

import (
	"context"
	"testing"
	"time"
)

func TestNoDeadlineIsUnlimited(t *testing.T) {
	ctx := context.Background()
	if Short(ctx) || Left(ctx) < time.Hour {
		t.Fatal("a context without a deadline must have unlimited time")
	}
	w, cancel := Work(ctx)
	defer cancel()
	if w != ctx {
		t.Fatal("Work must return ctx itself without a deadline")
	}
}

func TestShortNearDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !Short(ctx) {
		t.Fatal("2s left must be short")
	}
	long, cancel2 := context.WithTimeout(context.Background(), time.Minute)
	defer cancel2()
	if Short(long) {
		t.Fatal("a minute left must not be short")
	}
	w, cancelW := Work(long)
	defer cancelW()
	d, _ := w.Deadline()
	pd, _ := long.Deadline()
	if got := pd.Sub(d); got != WriteReserve {
		t.Fatalf("Work must end %v early, ended %v early", WriteReserve, got)
	}
	s, cancelS := Share(long, 0.5)
	defer cancelS()
	if Left(s) > 31*time.Second || Left(s) < 29*time.Second {
		t.Fatalf("Share(0.5) of a minute = %v", Left(s))
	}
}

func TestExpiredIsShort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !Short(ctx) || Left(ctx) != 0 {
		t.Fatal("a cancelled context has no time left")
	}
}

// Shrink makes the reserves small for a test and puts them back afterwards.
func TestReservesCanBeShrunkForTests(t *testing.T) {
	r, w := Reserve, WriteReserve
	Reserve, WriteReserve = 10*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { Reserve, WriteReserve = r, w })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if Short(ctx) {
		t.Fatal("a second left is not short with a 10ms reserve")
	}
}
