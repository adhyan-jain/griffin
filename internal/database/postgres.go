package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps pgxpool.Pool to provide transaction helpers and health checks.
type DB struct {
	Pool *pgxpool.Pool
}

// PoolOptions tunes the connection pool (use small values on Lambda).
type PoolOptions struct {
	MaxConns int32
	MinConns int32
}

// Connect establishes a connection pool with default pool sizing.
func Connect(ctx context.Context, connString string) (*DB, error) {
	return ConnectWithOptions(ctx, connString, PoolOptions{MaxConns: 25, MinConns: 2})
}

// IsTransactionPooler reports whether the URL targets a PgBouncer/Supavisor transaction-mode
// pooler (port 6543), which does not support prepared statements.
func IsTransactionPooler(cfg *pgxpool.Config) bool {
	return cfg.ConnConfig.Port == 6543
}

// ConnectWithOptions establishes a pool. Behind a transaction-mode pooler (Supabase port 6543)
// it switches pgx to the simple protocol with no statement cache, which is required there.
func ConnectWithOptions(ctx context.Context, connString string, opts PoolOptions) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database conn string: %w", err)
	}

	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns
	cfg.MaxConnLifetime = 1 * time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	if IsTransactionPooler(cfg) {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		cfg.ConnConfig.StatementCacheCapacity = 0
		cfg.ConnConfig.DescriptionCacheCapacity = 0
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgx pool: %w", err)
	}

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// Ping verifies database connectivity.
func (db *DB) Ping(ctx context.Context) error {
	return db.Pool.Ping(ctx)
}

// WithTx executes a callback within a database transaction.
func (db *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}
