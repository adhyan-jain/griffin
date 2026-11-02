package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Migration advisory lock key so concurrent runners serialize.
const migrationLockKey int64 = 0x6772696666696e

const localShimPath = "local/auth_shim.sql"

// ErrSchemaNotCurrent is returned by CheckSchema when migrations are pending or tampered with.
var ErrSchemaNotCurrent = errors.New("database schema is not up to date; run griffin-migrate")

// MigrateOptions controls an explicit migration run.
type MigrateOptions struct {
	// LocalAuthShim applies migrations/local/auth_shim.sql first. Only for plain PostgreSQL
	// (local dev / CI). Never set against Supabase.
	LocalAuthShim bool
}

type migrationFile struct {
	version  string // e.g. 000001_init_schema
	sql      string
	checksum string
}

func loadMigrations(fsys fs.FS) ([]migrationFile, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("reading migrations: %w", err)
	}
	var out []migrationFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", name, err)
		}
		sum := sha256.Sum256(b)
		out = append(out, migrationFile{
			version:  strings.TrimSuffix(name, ".up.sql"),
			sql:      string(b),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

const ensureTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	checksum TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// requireAuthSchema fails fast if the Supabase auth objects the schema depends on are missing.
func requireAuthSchema(ctx context.Context, db *DB) error {
	var hasUsers, hasUID bool
	err := db.Pool.QueryRow(ctx, `SELECT
		to_regclass('auth.users') IS NOT NULL,
		EXISTS (SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		        WHERE n.nspname = 'auth' AND p.proname = 'uid')`).Scan(&hasUsers, &hasUID)
	if err != nil {
		return fmt.Errorf("checking auth schema: %w", err)
	}
	if !hasUsers || !hasUID {
		return errors.New("auth.users / auth.uid() not found: target is not a Supabase database " +
			"(use -local-auth-shim only for plain local PostgreSQL)")
	}
	return nil
}

// Migrate applies pending migrations in order, each in its own transaction, recording version and
// checksum in schema_migrations. It is idempotent, serialized by an advisory lock, and refuses to
// run if an already-applied migration file has changed. Returns the versions applied.
func Migrate(ctx context.Context, db *DB, fsys fs.FS, opts MigrateOptions) ([]string, error) {
	files, err := loadMigrations(fsys)
	if err != nil {
		return nil, err
	}

	conn, err := db.Pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockKey) }()

	if opts.LocalAuthShim {
		shim, err := fs.ReadFile(fsys, localShimPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", localShimPath, err)
		}
		if _, err := conn.Exec(ctx, string(shim)); err != nil {
			return nil, fmt.Errorf("applying local auth shim: %w", err)
		}
	}
	if err := requireAuthSchema(ctx, db); err != nil {
		return nil, err
	}

	if _, err := conn.Exec(ctx, ensureTable); err != nil {
		return nil, fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied := map[string]string{}
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	for rows.Next() {
		var v, c string
		if err := rows.Scan(&v, &c); err != nil {
			rows.Close()
			return nil, err
		}
		applied[v] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var done []string
	for _, f := range files {
		if sum, ok := applied[f.version]; ok {
			if sum != f.checksum {
				return done, fmt.Errorf("migration %s was modified after being applied (checksum mismatch)", f.version)
			}
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return done, fmt.Errorf("begin %s: %w", f.version, err)
		}
		if _, err := tx.Exec(ctx, f.sql); err != nil {
			_ = tx.Rollback(ctx)
			return done, fmt.Errorf("executing migration %s: %w", f.version, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", f.version, f.checksum); err != nil {
			_ = tx.Rollback(ctx)
			return done, fmt.Errorf("recording migration %s: %w", f.version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return done, fmt.Errorf("commit %s: %w", f.version, err)
		}
		done = append(done, f.version)
	}
	return done, nil
}

// Status returns applied and pending versions without changing the database.
func Status(ctx context.Context, db *DB, fsys fs.FS) (applied, pending []string, err error) {
	files, err := loadMigrations(fsys)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]string{}
	var exists bool
	if err := db.Pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return nil, nil, err
	}
	if exists {
		rows, err := db.Pool.Query(ctx, "SELECT version, checksum FROM schema_migrations")
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var v, c string
			if err := rows.Scan(&v, &c); err != nil {
				return nil, nil, err
			}
			have[v] = c
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}
	for _, f := range files {
		if c, ok := have[f.version]; ok && c == f.checksum {
			applied = append(applied, f.version)
		} else {
			pending = append(pending, f.version)
		}
	}
	return applied, pending, nil
}

// CheckSchema is a read-only startup guard: it never modifies the database. It fails if any
// embedded migration is pending or differs from what was applied.
func CheckSchema(ctx context.Context, db *DB, fsys fs.FS) error {
	_, pending, err := Status(ctx, db, fsys)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%w (pending or modified: %s)", ErrSchemaNotCurrent, strings.Join(pending, ", "))
	}
	return nil
}
