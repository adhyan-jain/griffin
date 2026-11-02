// Command griffin-migrate applies Griffin's embedded SQL migrations explicitly.
//
//	griffin-migrate status
//	griffin-migrate up
//	griffin-migrate -local-auth-shim up    # plain local PostgreSQL only; NEVER against Supabase
//
// The connection string is read from MIGRATION_DATABASE_URL (preferred, use the Supabase
// session pooler or direct connection) or DATABASE_URL. Migrations are never run by the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/migrations"
)

func main() {
	shim := flag.Bool("local-auth-shim", false, "create a minimal auth schema on plain PostgreSQL (local/CI only)")
	flag.Parse()
	cmd := flag.Arg(0)
	if cmd != "up" && cmd != "status" {
		fmt.Fprintln(os.Stderr, "usage: griffin-migrate [-local-auth-shim] up|status")
		os.Exit(2)
	}

	url := os.Getenv("MIGRATION_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		fmt.Fprintln(os.Stderr, "MIGRATION_DATABASE_URL or DATABASE_URL is required")
		os.Exit(1)
	}
	if strings.Contains(url, ":6543") {
		fmt.Fprintln(os.Stderr, "refusing to migrate through the transaction pooler (port 6543); use the session pooler (5432) or a direct connection")
		os.Exit(1)
	}
	if *shim && os.Getenv("ENVIRONMENT") == "production" {
		fmt.Fprintln(os.Stderr, "-local-auth-shim is forbidden when ENVIRONMENT=production")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	db, err := database.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	switch cmd {
	case "status":
		applied, pending, err := database.Status(ctx, db, migrations.FS)
		if err != nil {
			fmt.Fprintf(os.Stderr, "status: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("applied: %s\npending: %s\n", strings.Join(applied, ", "), strings.Join(pending, ", "))
	case "up":
		done, err := database.Migrate(ctx, db, migrations.FS, database.MigrateOptions{LocalAuthShim: *shim})
		for _, v := range done {
			fmt.Printf("applied %s\n", v)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
			os.Exit(1)
		}
		if len(done) == 0 {
			fmt.Println("schema already up to date")
		}
	}
}
