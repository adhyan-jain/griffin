// Command griffin-seed-profile stores the owner's canonical profile (identity fields and verified
// facts) as the next version. The profile JSON lives outside the repository so personal data is
// never committed.
//
//	DATABASE_URL=... griffin-seed-profile -user <uuid> -file ~/Documents/Griffin/profile/owner_profile.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func main() {
	user := flag.String("user", "", "owner user id (uuid)")
	file := flag.String("file", "", "path to owner profile JSON")
	flag.Parse()
	uid, err := uuid.Parse(*user)
	if err != nil || *file == "" {
		fmt.Fprintln(os.Stderr, "usage: griffin-seed-profile -user <uuid> -file <profile.json>")
		os.Exit(2)
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read profile: %v\n", err)
		os.Exit(1)
	}
	var p domain.OwnerProfile
	if err := json.Unmarshal(raw, &p); err != nil {
		fmt.Fprintf(os.Stderr, "parse profile: %v\n", err)
		os.Exit(1)
	}
	if err := p.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(database.WithSystem(context.Background()), time.Minute)
	defer cancel()
	db, err := database.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	v, err := repository.NewOwnerProfileRepository(db.Pool).Save(ctx, uid, &p, "seed:"+*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("saved owner profile version %d with %d verified facts\n", v, len(p.Facts))
}
