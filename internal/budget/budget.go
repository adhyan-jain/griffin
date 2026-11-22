// Package budget lets a routine that runs inside one HTTP request (AWS Lambda kills it at 29 seconds) work to a
// deadline: it stops starting expensive steps when little time is left, so it can finish with a normal summary
// ("N left for the next run") instead of being killed with its task still RUNNING.
package budget

import (
	"context"
	"time"
)

// These are variables only so that tests can shrink them; nothing else changes them.
var (
	// Reserve is how little time may remain for a new expensive step (a page fetch, a model call) to be started.
	Reserve = 5 * time.Second
	// WriteReserve is how long before the deadline calls in flight are cut off, so the writes that follow still fit.
	WriteReserve = 3 * time.Second
)

// Left is the time until ctx's deadline; a context without one has effectively unlimited time.
func Left(ctx context.Context) time.Duration {
	if ctx.Err() != nil {
		return 0
	}
	if d, ok := ctx.Deadline(); ok {
		return time.Until(d)
	}
	return 1<<62 - 1
}

// Short reports whether too little time remains to start another expensive step.
func Short(ctx context.Context) bool { return Left(ctx) < Reserve }

// Work is a context for the fetches and model calls of a routine: it ends WriteReserve before ctx does, so a call
// started late cannot use the time the routine's final writes need. Without a deadline it is ctx itself.
func Work(ctx context.Context) (context.Context, context.CancelFunc) {
	d, ok := ctx.Deadline()
	if !ok {
		return ctx, func() {}
	}
	return context.WithDeadline(ctx, d.Add(-WriteReserve))
}

// Share is a context that ends after the given fraction of the time left, to keep one phase of a routine from
// using the time a later phase needs. Without a deadline it is ctx itself.
func Share(ctx context.Context, fraction float64) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); !ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(float64(Left(ctx))*fraction))
}
