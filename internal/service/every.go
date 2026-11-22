package service

import (
	"sync/atomic"
	"time"
)

// Every lets periodic upkeep ride on frequent requests: Due is true at most once per interval, across goroutines.
type Every struct {
	Interval time.Duration
	Now      func() time.Time
	last     atomic.Int64
}

// Due reports whether the interval has passed since the last time it returned true, and claims this turn if so.
func (e *Every) Due() bool {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	t := now().UnixNano()
	prev := e.last.Load()
	if prev != 0 && time.Duration(t-prev) < e.Interval {
		return false
	}
	return e.last.CompareAndSwap(prev, t)
}
