package tests

import (
	"context"
	"os"
	"testing"
)

// Supabase grants ALL on new public objects to anon/authenticated (the public REST API roles). The
// hardening script must remove that, protect the migration ledger, and survive future tables.
func TestHardenPublicSchemaRemovesPublicAPIAccess(t *testing.T) {
	e := setupRLS(t, 1, "")
	ctx := context.Background()
	// Simulate Supabase's platform roles and default grants on plain Postgres.
	if _, err := e.DB.Pool.Exec(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='anon') THEN CREATE ROLE anon NOLOGIN; END IF;
		IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='authenticated') THEN CREATE ROLE authenticated NOLOGIN; END IF;
	END $$;
	GRANT USAGE ON SCHEMA public TO anon, authenticated;
	GRANT ALL ON ALL TABLES IN SCHEMA public TO anon, authenticated;
	GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO anon, authenticated;
	ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO anon, authenticated;`); err != nil {
		t.Fatal(err)
	}
	if !hasPriv(t, e, "anon", "public.schema_migrations", "UPDATE") {
		t.Fatal("test setup: anon should start with write access to the ledger")
	}
	sqlBytes, err := os.ReadFile("../infra/ops/harden-public-schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.DB.Pool.Exec(ctx, string(sqlBytes)); err != nil {
		t.Fatalf("apply hardening: %v", err)
	}
	rows, err := e.DB.Pool.Query(ctx, `SELECT relname FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind='r'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	for _, tbl := range tables {
		for _, role := range []string{"anon", "authenticated"} {
			for _, p := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
				if hasPriv(t, e, role, "public."+tbl, p) {
					t.Errorf("%s still has %s on %s", role, p, tbl)
				}
			}
		}
	}
	var rls, force bool
	if err := e.DB.Pool.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid='public.schema_migrations'::regclass`).Scan(&rls, &force); err != nil || !rls || !force {
		t.Fatalf("schema_migrations must be RLS-forced: %v %v %v", rls, force, err)
	}
	// Future tables must not re-acquire public API grants.
	if _, err := e.DB.Pool.Exec(ctx, `CREATE TABLE public.future_probe (id int)`); err != nil {
		t.Fatal(err)
	}
	if hasPriv(t, e, "anon", "public.future_probe", "SELECT") || hasPriv(t, e, "authenticated", "public.future_probe", "INSERT") {
		t.Error("a table created after hardening re-acquired public API grants")
	}
	_, _ = e.DB.Pool.Exec(ctx, `DROP TABLE public.future_probe`)
	// Runtime roles are unaffected: the system role (BYPASSRLS) can still read the ledger.
	var n int
	if err := e.SystemPool.QueryRow(ctx, `SELECT count(*) FROM public.schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("system role lost access to the ledger: %v", err)
	}
}

func hasPriv(t *testing.T, e *rlsEnv, role, table, priv string) bool {
	t.Helper()
	var ok bool
	if err := e.DB.Pool.QueryRow(context.Background(), `SELECT has_table_privilege($1, $2, $3)`, role, table, priv).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}
