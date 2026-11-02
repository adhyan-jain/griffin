package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RunMigrations executes all *.up.sql files in migrationsDir against the database.
func RunMigrations(ctx context.Context, db *DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations directory: %w", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}

	sort.Strings(upFiles)

	for _, file := range upFiles {
		fullPath := filepath.Join(migrationsDir, file)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", file, err)
		}

		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("executing migration %s: %w", file, err)
		}
	}

	return nil
}
