// seed_tracker loads the local internship ledger into the shared Internships list, replacing what is there.
// The ledger is the owner's own data (~/Documents/Griffin/trackers/internships.json).
// Usage: DATABASE_URL=... S3_BUCKET=... AWS_PROFILE=... go run ./cmd/seed_tracker [-ledger path]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

func main() {
	home, _ := os.UserHomeDir()
	ledger := flag.String("ledger", filepath.Join(home, "Documents", "Griffin", "trackers", "internships.json"), "the local ledger")
	flag.Parse()
	// An explicit system scope: this one-off command acts for the owner outside any request.
	ctx := database.WithSystem(context.Background())

	raw, err := os.ReadFile(*ledger)
	if err != nil {
		fail(err)
	}
	table, n, err := ledgerTable(raw)
	if err != nil {
		fail(err)
	}

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM profiles LIMIT 1").Scan(&userID); err != nil {
		fail(fmt.Errorf("no profile: %w", err))
	}

	region, bucket := envOr("S3_REGION", "ap-south-1"), os.Getenv("S3_BUCKET")
	prov, err := storage.NewS3StorageProvider(ctx, storage.S3ClientOptions{Region: region, Bucket: bucket, AWSProfile: os.Getenv("AWS_PROFILE")})
	if err != nil {
		fail(err)
	}
	svc := storage.NewStorageService(prov, repository.NewPostgresArtifactRepository(database.NewScoped(pool)), bucket, slog.Default())

	title := domain.TrackerTypes["internships"] + " tracker"
	body := "# " + title + "\n\n" + table
	lists, err := svc.ListByKind(ctx, userID, storage.ListKind)
	if err != nil {
		fail(err)
	}
	// Every existing Internships list (there should be one) is replaced, so no stale copy is left behind.
	replaced := 0
	for _, a := range lists {
		var m struct {
			Tracker string `json:"tracker"`
		}
		if json.Unmarshal(a.Metadata, &m) == nil && m.Tracker == "internships" {
			if _, err := svc.UpdateText(ctx, a.ID, userID, body, false, 1<<20); err != nil {
				fail(err)
			}
			replaced++
		}
	}
	if replaced > 0 {
		fmt.Printf("replaced %d existing Internships list(s) with %d rows\n", replaced, n)
		return
	}
	if _, err := svc.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: userID, Title: title, Filename: "internships-tracker.md", ArtifactType: "MARKDOWN",
		ContentType: "text/markdown; charset=utf-8", Body: bytes.NewReader([]byte(body + "\n")),
		Metadata: domain.TrackerListMetadata(storage.ListKind, "internships"),
	}); err != nil {
		fail(err)
	}
	fmt.Printf("created the Internships list with %d rows\n", n)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
