package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/griffin/core/internal/service"
)

// LeaseReaper periodically scans the database to reclaim tasks abandoned by crashed workers.
type LeaseReaper struct {
	engine   *service.TaskEngine
	interval time.Duration
	logger   *slog.Logger
}

// NewLeaseReaper constructs a new LeaseReaper.
func NewLeaseReaper(engine *service.TaskEngine, interval time.Duration, logger *slog.Logger) *LeaseReaper {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LeaseReaper{
		engine:   engine,
		interval: interval,
		logger:   logger,
	}
}

// Run executes the lease reaper until context cancellation.
func (r *LeaseReaper) Run(ctx context.Context) error {
	r.logger.Info("Starting lease reaper", "interval", r.interval)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("Lease reaper shutting down")
			return ctx.Err()
		case <-ticker.C:
			reaped, err := r.engine.ReapExpiredLeases(ctx)
			if err != nil {
				r.logger.Error("Lease reaper error", "error", err)
			} else if reaped > 0 {
				r.logger.Warn("Reaped expired task leases", "reaped_count", reaped)
			}
		}
	}
}
