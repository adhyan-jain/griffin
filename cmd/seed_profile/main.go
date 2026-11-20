// seed_profile uploads the owner's verified profile (without the phone number) as a private file that application
// tasks are shown, so drafts use real facts and the real name and links.
// Usage: DATABASE_URL=... S3_BUCKET=... AWS_PROFILE=... go run ./cmd/seed_profile [-profile path]
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
	path := flag.String("profile", filepath.Join(home, "Documents", "Griffin", "profile", "owner_profile.json"), "the owner profile")
	flag.Parse()
	ctx := database.WithSystem(context.Background())

	raw, err := os.ReadFile(*path)
	if err != nil {
		fail(err)
	}
	var p domain.OwnerProfile
	if err := json.Unmarshal(raw, &p); err != nil {
		fail(err)
	}
	if err := p.Validate(); err != nil {
		fail(fmt.Errorf("the profile is not safe to ground text: %w", err))
	}
	body := profileText(&p)

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	var userID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM profiles LIMIT 1").Scan(&userID); err != nil {
		fail(fmt.Errorf("no profile: %w", err))
	}
	bucket := os.Getenv("S3_BUCKET")
	prov, err := storage.NewS3StorageProvider(ctx, storage.S3ClientOptions{Region: "ap-south-1", Bucket: bucket, AWSProfile: os.Getenv("AWS_PROFILE")})
	if err != nil {
		fail(err)
	}
	svc := storage.NewStorageService(prov, repository.NewPostgresArtifactRepository(database.NewScoped(pool)), bucket, slog.Default())

	existing, err := svc.ListByKind(ctx, userID, domain.ProfileKind)
	if err != nil {
		fail(err)
	}
	if len(existing) > 0 {
		for _, a := range existing {
			if _, err := svc.UpdateText(ctx, a.ID, userID, body, false, 1<<20); err != nil {
				fail(err)
			}
		}
		fmt.Printf("updated the stored owner profile (%d facts)\n", len(p.Facts))
		return
	}
	meta, _ := json.Marshal(map[string]string{"kind": domain.ProfileKind})
	if _, err := svc.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: userID, Title: "Owner profile", Filename: "owner-profile.md", ArtifactType: "MARKDOWN",
		ContentType: "text/markdown; charset=utf-8", Body: bytes.NewReader([]byte(body)), Metadata: meta,
	}); err != nil {
		fail(err)
	}
	fmt.Printf("stored the owner profile (%d facts)\n", len(p.Facts))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
