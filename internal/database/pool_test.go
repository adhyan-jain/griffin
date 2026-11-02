package database_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/database"
)

func TestIsTransactionPooler(t *testing.T) {
	cases := map[string]bool{
		"postgres://u:p@aws-0-ap-south-1.pooler.supabase.com:6543/postgres?sslmode=require": true,
		"postgres://u:p@aws-0-ap-south-1.pooler.supabase.com:5432/postgres?sslmode=require": false,
		"postgres://u:p@db.example.com/postgres":                                            false,
		"postgres://u:p@localhost:6543/postgres?sslmode=disable":                            true,
	}
	for url, want := range cases {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		if got := database.IsTransactionPooler(cfg); got != want {
			t.Errorf("%s: got %v want %v", url, got, want)
		}
	}
}
