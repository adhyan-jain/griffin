package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/migrations"
)

// newScratchDB creates an empty throwaway database next to the test DB and returns a connected
// pool. It never touches the shared test database.
func newScratchDB(t *testing.T) *database.DB {
	t.Helper()
	base := os.Getenv("GRIFFIN_TEST_DB_URL")
	if base == "" {
		base = TestDBURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close(ctx)

	name := "mig_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create scratch db: %v", err)
	}
	t.Cleanup(func() {
		c2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel2()
		a, err := pgx.ConnectConfig(c2, cfg)
		if err != nil {
			return
		}
		defer a.Close(c2)
		_, _ = a.Exec(c2, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	url := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, name)
	db, err := database.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect scratch: %v", err)
	}
	t.Cleanup(db.Close)
	return db
}

func count(t *testing.T, db *database.DB, q string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestMigrateAppliesOnceAndIsRepeatable(t *testing.T) {
	db := newScratchDB(t)
	ctx := context.Background()

	done, err := database.Migrate(ctx, db, migrations.FS, database.MigrateOptions{LocalAuthShim: true})
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if len(done) != 6 {
		t.Fatalf("want 6 migrations applied, got %v", done)
	}
	done, err = database.Migrate(ctx, db, migrations.FS, database.MigrateOptions{LocalAuthShim: true})
	if err != nil || len(done) != 0 {
		t.Fatalf("second migrate must be a no-op: done=%v err=%v", done, err)
	}
	if n := count(t, db, "SELECT count(*) FROM schema_migrations"); n != 6 {
		t.Fatalf("want 6 recorded versions, got %d", n)
	}
	if err := database.CheckSchema(ctx, db, migrations.FS); err != nil {
		t.Fatalf("CheckSchema after migrate: %v", err)
	}
}

func TestMigrateRefusesPlainPostgresWithoutShim(t *testing.T) {
	db := newScratchDB(t)
	_, err := database.Migrate(context.Background(), db, migrations.FS, database.MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "auth.uid()") {
		t.Fatalf("expected a clear auth-schema error, got %v", err)
	}
	if n := count(t, db, "SELECT count(*) FROM pg_tables WHERE tablename='tasks'"); n != 0 {
		t.Fatal("no tables may be created when the auth schema check fails")
	}
}

// Emulates a Supabase database: auth.users and a platform-owned auth.uid() already exist.
const supabaseLikeAuth = `
CREATE SCHEMA auth;
CREATE TABLE auth.users (id uuid PRIMARY KEY, email text, aud text);
CREATE FUNCTION auth.uid() RETURNS uuid LANGUAGE sql STABLE AS $$
    SELECT coalesce(nullif(current_setting('request.jwt.claims', true), '')::jsonb ->> 'sub', '')::uuid
$$;
`

func TestMigratePreservesPlatformAuthUID(t *testing.T) {
	db := newScratchDB(t)
	ctx := context.Background()

	if _, err := db.Pool.Exec(ctx, supabaseLikeAuth); err != nil {
		t.Fatalf("setup: %v", err)
	}
	before := funcSource(t, db)

	// Without the shim (the Supabase path) and, defensively, even WITH the shim flag.
	for _, shim := range []bool{false, true} {
		if _, err := database.Migrate(ctx, db, migrations.FS, database.MigrateOptions{LocalAuthShim: shim}); err != nil {
			t.Fatalf("migrate (shim=%v) on supabase-like db: %v", shim, err)
		}
		if after := funcSource(t, db); after != before {
			t.Fatalf("auth.uid() was modified (shim=%v):\nbefore=%q\nafter=%q", shim, before, after)
		}
	}
	if !strings.Contains(before, "request.jwt.claims") {
		t.Fatal("test precondition: unexpected function body")
	}
}

func funcSource(t *testing.T, db *database.DB) string {
	t.Helper()
	var src string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='auth' AND p.proname='uid'`).Scan(&src)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestMigrateDetectsTamperedMigration(t *testing.T) {
	db := newScratchDB(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db, migrations.FS, database.MigrateOptions{LocalAuthShim: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE schema_migrations SET checksum='deadbeef' WHERE version LIKE '000002%'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Migrate(ctx, db, migrations.FS, database.MigrateOptions{LocalAuthShim: true}); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if err := database.CheckSchema(ctx, db, migrations.FS); err == nil {
		t.Fatal("CheckSchema must fail on a tampered migration")
	}
}

func TestMigrateConcurrentRunnersSerialize(t *testing.T) {
	db := newScratchDB(t)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := database.Migrate(context.Background(), db, migrations.FS, database.MigrateOptions{LocalAuthShim: true})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate failed: %v", err)
		}
	}
	if n := count(t, db, "SELECT count(*) FROM schema_migrations"); n != 6 {
		t.Fatalf("want 4 versions, got %d", n)
	}
}

func TestCheckSchemaIsReadOnly(t *testing.T) {
	db := newScratchDB(t)
	ctx := context.Background()
	if err := database.CheckSchema(ctx, db, migrations.FS); err == nil {
		t.Fatal("CheckSchema must fail on an unmigrated database")
	}
	if n := count(t, db, "SELECT count(*) FROM pg_tables WHERE schemaname='public'"); n != 0 {
		t.Fatalf("CheckSchema created %d tables; it must be read-only", n)
	}
}

func TestRLSForcedOnAllTenantTables(t *testing.T) {
	db := newScratchDB(t)
	if _, err := database.Migrate(context.Background(), db, migrations.FS, database.MigrateOptions{LocalAuthShim: true}); err != nil {
		t.Fatal(err)
	}
	n := count(t, db, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname='public' AND c.relkind='r' AND c.relname <> 'schema_migrations'
		AND NOT (c.relrowsecurity AND c.relforcerowsecurity)`)
	if n != 0 {
		t.Fatalf("%d public tables lack ENABLE+FORCE row level security", n)
	}
}

func TestServerNeverMigratesAtStartup(t *testing.T) {
	src, err := os.ReadFile("../cmd/griffin-server/main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if strings.Contains(s, "database.Migrate(") || strings.Contains(s, "RunMigrations") {
		t.Fatal("the server must not apply migrations at startup")
	}
	if !strings.Contains(s, "database.CheckSchema(") {
		t.Fatal("the server must verify the schema (read-only) at startup")
	}
}
