package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/griffin/core/internal/domain"
)

// Allowed document extensions for context document caching.
// Note: source code files (.go, .ts, .py, etc.) and binaries are strictly excluded.
var docExtensions = map[string]bool{
	".pdf":  true,
	".docx": true,
	".doc":  true,
	".md":   true,
	".txt":  true,
}

var codeRepoDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"build":        true,
	"dist":         true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".idea":        true,
	".vscode":      true,
}

var codeRepoMarkers = []string{
	".git",
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"pom.xml",
	"build.gradle",
}

// IsInCodeRepo reports whether path is inside a code repository, build folder or dependency tree.
func IsInCodeRepo(path string) bool {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(filepath.Separator))
	for _, p := range parts {
		if codeRepoDirs[p] {
			return true
		}
	}
	dir := filepath.Dir(clean)
	for {
		for _, marker := range codeRepoMarkers {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "." || parent == "" {
			break
		}
		dir = parent
	}
	return false
}

// IsDocumentFile checks if a path is an independent document eligible for caching (not in a code repo).
func IsDocumentFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if !docExtensions[ext] {
		return false
	}
	if IsInCodeRepo(path) {
		return false
	}
	return true
}

// JudgeDocument evaluates whether a local file qualifies for S3 Vault caching.
// It fails closed: symlinks, empty files, files exceeding MaxFileBytes (5MB), and files inside
// code repositories are rejected.
func JudgeDocument(path string) (*Candidate, error) {
	if !IsDocumentFile(path) {
		return nil, errors.New("not an independent document file (must be .pdf, .docx, .md, or .txt outside code repos)")
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("symlinks are rejected for security")
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if fi.Size() == 0 {
		return nil, errors.New("file is empty")
	}
	if fi.Size() > MaxFileBytes {
		return nil, fmt.Errorf("file size (%d bytes) exceeds 5 MB limit", fi.Size())
	}

	label, cat, ok := Classify(filepath.Base(path))
	if !ok {
		label = prettyLabel(filepath.Base(path))
		cat = domain.VaultOther
		lower := strings.ToLower(filepath.Base(path))
		if strings.Contains(lower, "resume") || strings.Contains(lower, "cv") || strings.Contains(lower, "profile") || strings.Contains(lower, "bio") || strings.Contains(lower, "experience") {
			cat = domain.VaultResume
		}
	}
	return &Candidate{
		Path:     path,
		Name:     filepath.Base(path),
		Label:    label,
		Category: cat,
		Size:     fi.Size(),
	}, nil
}

// docPathRe extracts candidate document paths from text.
var docPathRe = regexp.MustCompile(`(?:[A-Za-z]:[/\\]|[~/.])[^\s"'\x60\(\)\[\]<>{}\*\|\?]+\.(?i:pdf|docx?|md|txt)\b`)

// ExtractDocPaths scans arbitrary text for existing, independent document files on disk.
func ExtractDocPaths(text string) []string {
	if text == "" {
		return nil
	}
	matches := docPathRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		m = strings.Trim(m, `"',:;.()[]{}<>`)
		if strings.HasPrefix(m, "~/") && home != "" {
			m = filepath.Join(home, m[2:])
		}
		clean := filepath.Clean(m)
		if !filepath.IsAbs(clean) {
			if abs, err := filepath.Abs(clean); err == nil {
				clean = abs
			}
		}
		if seen[clean] {
			continue
		}
		seen[clean] = true
		if IsDocumentFile(clean) {
			if fi, err := os.Lstat(clean); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 && fi.Size() <= MaxFileBytes {
				out = append(out, clean)
			}
		}
	}
	sort.Strings(out)
	return out
}

// DocUsage tracks frequency and sync status for a document file.
type DocUsage struct {
	Path        string               `json:"path"`
	Filename    string               `json:"filename"`
	Label       string               `json:"label"`
	Category    domain.VaultCategory `json:"category"`
	AccessCount int                  `json:"access_count"`
	FirstSeen   time.Time            `json:"first_seen"`
	LastSeen    time.Time            `json:"last_seen"`
	Synced      bool                 `json:"synced"`
	SyncedAt    time.Time            `json:"synced_at,omitempty"`
	Fingerprint string               `json:"fingerprint,omitempty"`
}

// DocTracker manages local file usage and automatic sync to the user's S3 Vault.
type DocTracker struct {
	mu        sync.Mutex
	statePath string
	usages    map[string]*DocUsage
	logger    *slog.Logger
}

// NewDocTracker loads or creates a document usage tracker.
func NewDocTracker(statePath string, logger *slog.Logger) *DocTracker {
	if logger == nil {
		logger = slog.Default()
	}
	dt := &DocTracker{
		statePath: statePath,
		usages:    make(map[string]*DocUsage),
		logger:    logger,
	}
	dt.loadLocked()
	return dt
}

func (t *DocTracker) loadLocked() {
	if t.statePath == "" {
		return
	}
	data, err := os.ReadFile(t.statePath)
	if err != nil {
		return
	}
	var list []*DocUsage
	if err := json.Unmarshal(data, &list); err == nil {
		for _, u := range list {
			t.usages[u.Path] = u
		}
	}
}

func (t *DocTracker) saveLocked() error {
	if t.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(t.statePath), 0o700); err != nil {
		return err
	}
	list := make([]*DocUsage, 0, len(t.usages))
	for _, u := range t.usages {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := t.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, t.statePath)
}

// RecordAndMaybeSync records file usage. If AccessCount > 1 and an uploader is provided,
// it judges the file and caches it to S3 Vault.
func (t *DocTracker) RecordAndMaybeSync(ctx context.Context, path string, up Uploader, passphrase string) (*DocUsage, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	clean := filepath.Clean(path)
	cand, err := JudgeDocument(clean)
	if err != nil {
		return nil, false, err
	}

	now := time.Now()
	u, ok := t.usages[clean]
	if !ok {
		u = &DocUsage{
			Path:        clean,
			Filename:    cand.Name,
			Label:       cand.Label,
			Category:    cand.Category,
			AccessCount: 1,
			FirstSeen:   now,
			LastSeen:    now,
		}
		t.usages[clean] = u
		_ = t.saveLocked()
		return u, false, nil
	}

	u.AccessCount++
	u.LastSeen = now
	u.Label = cand.Label
	u.Category = cand.Category

	// Only sync if used more than once
	if u.AccessCount <= 1 || up == nil {
		_ = t.saveLocked()
		return u, false, nil
	}

	// Compute current file fingerprint
	data, err := os.ReadFile(clean)
	if err != nil {
		_ = t.saveLocked()
		return u, false, err
	}
	sum := sha256.Sum256(data)
	curFp := hex.EncodeToString(sum[:])

	if u.Synced && u.Fingerprint == curFp {
		_ = t.saveLocked()
		return u, false, nil // Already synced with matching fingerprint
	}

	// Sync to S3 Vault
	t.logger.Info("Document accessed multiple times; caching to S3 Vault", "path", clean, "category", cand.Category, "count", u.AccessCount)
	outcome, err := Mirror(ctx, up, passphrase, *cand)
	if err != nil {
		t.logger.Warn("Failed to cache document to S3 Vault", "path", clean, "error", err)
		_ = t.saveLocked()
		return u, false, err
	}

	u.Synced = true
	u.SyncedAt = now
	u.Fingerprint = curFp
	_ = t.saveLocked()

	return u, outcome.Uploaded || outcome.Duplicate, nil
}

// ListUsages returns a snapshot of all tracked documents.
func (t *DocTracker) ListUsages() []*DocUsage {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*DocUsage, 0, len(t.usages))
	for _, u := range t.usages {
		copy := *u
		out = append(out, &copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AccessCount > out[j].AccessCount })
	return out
}
