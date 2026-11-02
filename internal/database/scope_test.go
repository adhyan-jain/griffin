package database

import (
	"context"
	"errors"
	"testing"
)

func TestScopedFailsClosedWithoutScope(t *testing.T) {
	s := NewScoped(nil)
	ctx := context.Background()
	if _, err := s.Exec(ctx, "SELECT 1"); !errors.Is(err, ErrNoDatabaseScope) {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := s.Query(ctx, "SELECT 1"); !errors.Is(err, ErrNoDatabaseScope) {
		t.Fatalf("Query: %v", err)
	}
	if err := s.QueryRow(ctx, "SELECT 1").Scan(); !errors.Is(err, ErrNoDatabaseScope) {
		t.Fatalf("QueryRow: %v", err)
	}
	if _, err := s.Begin(ctx); !errors.Is(err, ErrNoDatabaseScope) {
		t.Fatalf("Begin: %v", err)
	}
	// System scope with no system pool configured must still deny (nothing to route to).
	if _, err := s.Exec(WithSystem(ctx), "SELECT 1"); !errors.Is(err, ErrNoDatabaseScope) {
		t.Fatalf("system without pool: %v", err)
	}
	if IsSystem(ctx) || !IsSystem(WithSystem(ctx)) {
		t.Fatal("IsSystem mismatch")
	}
}
