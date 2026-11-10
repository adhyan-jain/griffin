package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffin/core/internal/domain"
)

type mockUploader struct {
	uploads []Upload
}

func (m *mockUploader) Add(ctx context.Context, u Upload) (bool, error) {
	m.uploads = append(m.uploads, u)
	return false, nil
}

func TestIsInCodeRepo(t *testing.T) {
	// A file inside node_modules or .git must be rejected
	if !IsInCodeRepo("/home/user/project/node_modules/foo/readme.md") {
		t.Error("expected node_modules to be in code repo")
	}
	if !IsInCodeRepo("/home/user/project/.git/COMMIT_EDITMSG") {
		t.Error("expected .git to be in code repo")
	}

	// Create a temp git repo and verify child document is detected as code repo
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	_ = os.Mkdir(gitDir, 0o755)
	repoDoc := filepath.Join(tmpDir, "docs", "readme.md")
	_ = os.MkdirAll(filepath.Dir(repoDoc), 0o755)
	_ = os.WriteFile(repoDoc, []byte("# Docs"), 0o644)

	if !IsInCodeRepo(repoDoc) {
		t.Error("expected document inside git repo root to be detected as code repo")
	}

	// Create an independent doc outside any repo
	outsideDir := t.TempDir()
	standaloneDoc := filepath.Join(outsideDir, "my_resume.pdf")
	_ = os.WriteFile(standaloneDoc, []byte("PDF content"), 0o644)
	if IsInCodeRepo(standaloneDoc) {
		t.Error("expected standalone doc outside repo to NOT be in code repo")
	}
}

func TestJudgeDocument(t *testing.T) {
	tmpDir := t.TempDir()

	// Empty file
	emptyFile := filepath.Join(tmpDir, "empty.pdf")
	_ = os.WriteFile(emptyFile, nil, 0o644)
	if _, err := JudgeDocument(emptyFile); err == nil {
		t.Error("expected empty file to be rejected")
	}

	// Code file
	codeFile := filepath.Join(tmpDir, "main.go")
	_ = os.WriteFile(codeFile, []byte("package main"), 0o644)
	if _, err := JudgeDocument(codeFile); err == nil {
		t.Error("expected non-document file (.go) to be rejected")
	}

	// Valid document
	resumeFile := filepath.Join(tmpDir, "Ajay_Resume.pdf")
	_ = os.WriteFile(resumeFile, []byte("%PDF-1.4 mock content"), 0o644)
	cand, err := JudgeDocument(resumeFile)
	if err != nil {
		t.Fatalf("expected valid document to be approved, got: %v", err)
	}
	if cand.Category != domain.VaultResume {
		t.Errorf("expected category %s, got %s", domain.VaultResume, cand.Category)
	}
}

func TestExtractDocPaths(t *testing.T) {
	tmpDir := t.TempDir()
	doc1 := filepath.Join(tmpDir, "doc1.pdf")
	doc2 := filepath.Join(tmpDir, "notes.md")
	_ = os.WriteFile(doc1, []byte("pdf"), 0o644)
	_ = os.WriteFile(doc2, []byte("md"), 0o644)

	text := "Here is " + doc1 + " and also check '" + doc2 + "' for details, but ignore /nonexistent/file.pdf"
	paths := ExtractDocPaths(text)
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
}

func TestDocTracker_BlindSlateToCached(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "doc_usage.json")
	docPath := filepath.Join(tmpDir, "career_resume.docx")
	_ = os.WriteFile(docPath, []byte("resume docx content"), 0o644)

	tracker := NewDocTracker(stateFile, nil)
	uploader := &mockUploader{}

	ctx := context.Background()

	// 1st access: blind slate, recorded count = 1, NOT synced yet
	usage, synced, err := tracker.RecordAndMaybeSync(ctx, docPath, uploader, "")
	if err != nil {
		t.Fatalf("unexpected error on 1st access: %v", err)
	}
	if usage.AccessCount != 1 {
		t.Errorf("expected count 1, got %d", usage.AccessCount)
	}
	if synced || usage.Synced {
		t.Error("document must NOT sync on 1st access (blind slate rule)")
	}
	if len(uploader.uploads) != 0 {
		t.Error("uploader must not be called on 1st access")
	}

	// 2nd access: used more than once -> judged and synced to S3 Vault!
	usage, synced, err = tracker.RecordAndMaybeSync(ctx, docPath, uploader, "")
	if err != nil {
		t.Fatalf("unexpected error on 2nd access: %v", err)
	}
	if usage.AccessCount != 2 {
		t.Errorf("expected count 2, got %d", usage.AccessCount)
	}
	if !synced || !usage.Synced {
		t.Error("document MUST sync on 2nd access")
	}
	if len(uploader.uploads) != 1 {
		t.Fatalf("expected 1 upload, got %d", len(uploader.uploads))
	}
	if uploader.uploads[0].Filename != "career_resume.docx" {
		t.Errorf("expected upload filename career_resume.docx, got %s", uploader.uploads[0].Filename)
	}

	// 3rd access with same content: already synced, uploader not called again
	_, synced, err = tracker.RecordAndMaybeSync(ctx, docPath, uploader, "")
	if err != nil {
		t.Fatalf("unexpected error on 3rd access: %v", err)
	}
	if synced {
		t.Error("document should not re-upload if fingerprint matches")
	}
	if len(uploader.uploads) != 1 {
		t.Errorf("expected upload count still 1, got %d", len(uploader.uploads))
	}
}
