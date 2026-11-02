package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is the subset of pgx used by repositories. *pgxpool.Pool and pgx.Tx both satisfy it.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ErrNoDatabaseScope is returned when a repository is used from a context that carries neither a
// user transaction nor an explicit system scope. Access is denied rather than defaulting to a
// privileged connection.
var ErrNoDatabaseScope = errors.New("database: no authorized scope in context (user transaction or explicit system scope required)")

type scopeKey int

const (
	userTxKey scopeKey = iota
	systemKey
)

// WithUserTx attaches a request-scoped transaction (RLS enforced, claims set) to ctx.
func WithUserTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, userTxKey, tx)
}

// WithSystem marks ctx as an explicitly authorized system execution path (workers, reaper,
// scheduler, worker-key-authenticated routes). Only such code may use the system connection.
func WithSystem(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemKey, true)
}

// IsSystem reports whether ctx carries the explicit system scope.
func IsSystem(ctx context.Context) bool {
	v, _ := ctx.Value(systemKey).(bool)
	return v
}

// Scoped routes each repository call by context: a user transaction if present (RLS enforced),
// else the system pool if explicitly authorized, else an error.
type Scoped struct {
	system Querier
}

// NewScoped builds a Scoped over the system (service-role) pool.
func NewScoped(system Querier) *Scoped { return &Scoped{system: system} }

func (s *Scoped) resolve(ctx context.Context) Querier {
	if tx, ok := ctx.Value(userTxKey).(pgx.Tx); ok && tx != nil {
		return tx // a user tx always wins: it is the more restrictive scope
	}
	if IsSystem(ctx) && s.system != nil {
		return s.system
	}
	return denied{}
}

func (s *Scoped) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.resolve(ctx).Exec(ctx, sql, args...)
}
func (s *Scoped) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return s.resolve(ctx).Query(ctx, sql, args...)
}
func (s *Scoped) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.resolve(ctx).QueryRow(ctx, sql, args...)
}
func (s *Scoped) Begin(ctx context.Context) (pgx.Tx, error) { return s.resolve(ctx).Begin(ctx) }

type denied struct{}

func (denied) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, ErrNoDatabaseScope
}
func (denied) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, ErrNoDatabaseScope
}
func (denied) QueryRow(context.Context, string, ...any) pgx.Row { return errRow{} }
func (denied) Begin(context.Context) (pgx.Tx, error)            { return nil, ErrNoDatabaseScope }

type errRow struct{}

func (errRow) Scan(...any) error { return ErrNoDatabaseScope }

// UserTxManager opens per-request transactions on the RLS-enforced pool and establishes the
// caller's identity transaction-locally (set_config(..., is_local=true)), so nothing survives
// COMMIT/ROLLBACK and nothing can leak to the next user of a pooled connection.
type UserTxManager struct {
	Pool *pgxpool.Pool
}

// Begin starts a transaction for userID. auth.uid() inside it returns userID.
func (m *UserTxManager) Begin(ctx context.Context, userID uuid.UUID) (pgx.Tx, error) {
	if userID == uuid.Nil {
		return nil, errors.New("database: refusing to open user transaction for nil user id")
	}
	tx, err := m.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin user tx: %w", err)
	}
	claims := fmt.Sprintf(`{"sub":%q,"role":"authenticated"}`, userID.String())
	if _, err := tx.Exec(ctx,
		`SELECT set_config('request.jwt.claims', $1, true),
		        set_config('request.jwt.claim.sub', $2, true),
		        set_config('request.jwt.claim.role', 'authenticated', true)`,
		claims, userID.String()); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, fmt.Errorf("set user claims: %w", err)
	}
	return tx, nil
}

// AssertRoleIsolation verifies the connected role cannot bypass row-level security. Used at
// startup on the user pool; the process refuses to serve user traffic otherwise.
func AssertRoleIsolation(ctx context.Context, pool *pgxpool.Pool) error {
	var name string
	var super, bypass bool
	err := pool.QueryRow(ctx,
		`SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&name, &super, &bypass)
	if err != nil {
		return fmt.Errorf("inspect database role: %w", err)
	}
	if super || bypass {
		return fmt.Errorf("database role %q must be NOSUPERUSER and NOBYPASSRLS for the user (RLS-enforced) connection (superuser=%t bypassrls=%t)", name, super, bypass)
	}
	return nil
}
