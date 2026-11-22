package service

import (
	"testing"
	"time"
)

func TestEveryIsDueAtMostOncePerInterval(t *testing.T) {
	at := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	e := &Every{Interval: time.Minute, Now: func() time.Time { return at }}
	if !e.Due() || e.Due() {
		t.Fatal("due the first time, not again right after")
	}
	at = at.Add(59 * time.Second)
	if e.Due() {
		t.Fatal("not before the interval")
	}
	at = at.Add(time.Second)
	if !e.Due() {
		t.Fatal("due once the interval has passed")
	}
}
