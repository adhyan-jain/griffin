package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
)

func TestClassifyByFileName(t *testing.T) {
	cases := []struct {
		name string
		cat  domain.VaultCategory
		ok   bool
	}{
		{"PAN_card.jpg", domain.VaultID, true},
		{"pan-card-front.PNG", domain.VaultID, true},
		{"Aadhaar.pdf", domain.VaultID, true},
		{"my_passport_scan.pdf", domain.VaultID, true},
		{"DrivingLicense.jpeg", domain.VaultID, true},
		{"HDFC bank statement oct.pdf", domain.VaultFinance, true},
		{"Form16_2025.pdf", domain.VaultFinance, true},
		{"ITR-2025.pdf", domain.VaultFinance, true},
		{"AJ_resume_context.md", domain.VaultResume, true},
		{"resume-backend.md", domain.VaultResume, true},
		{"CV_final.docx", domain.VaultResume, true},
		{"BTech marksheet sem6.pdf", domain.VaultCertificate, true},
		{"internship offer letter.pdf", domain.VaultCertificate, true},
		{"pan_card_resume.pdf", domain.VaultID, true}, // the careful reading wins when two words fit
		{"company panel notes.pdf", "", false},        // "panel" is not "pan"
		{"company_planning.pdf", "", false},
		{"holiday photo.jpg", "", false},
		{"pan.exe", "", false},
		{"pan", "", false},
		{"README.md", "", false},
		{"report.xlsx", "", false},
	}
	for _, c := range cases {
		_, cat, ok := Classify(c.name)
		if ok != c.ok || cat != c.cat {
			t.Errorf("Classify(%q) = %q, %v; want %q, %v", c.name, cat, ok, c.cat, c.ok)
		}
	}
	if label, _, _ := Classify("PAN_card-front.jpg"); label != "PAN card front" {
		t.Errorf("label should be readable, got %q", label)
	}
}

func put(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanFindsDocumentsByNameOnlyAndStaysInsideTheFolders(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	put(t, filepath.Join(root, "Griffin", "AJ_resume_context.md"), 100)
	put(t, filepath.Join(root, "IDs", "PAN_card.jpg"), 100)
	put(t, filepath.Join(root, "IDs", "holiday.jpg"), 100)
	put(t, filepath.Join(root, "a", "b", "c", "d", "marksheet.pdf"), 100) // depth 5
	put(t, filepath.Join(root, "node_modules", "pkg", "resume.md"), 100)
	put(t, filepath.Join(root, ".hidden", "passport.pdf"), 100)
	put(t, filepath.Join(root, "Empty", "resume.md"), 0)
	put(t, filepath.Join(outside, "aadhaar.pdf"), 100)
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "aadhaar.pdf"), filepath.Join(root, "aadhaar-link.pdf")); err != nil {
		t.Fatal(err)
	}

	got, err := Scan(context.Background(), []string{root, filepath.Join(root, "does-not-exist"), root}, 3)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	want := "AJ_resume_context.md,PAN_card.jpg"
	if strings.Join(names, ",") != want {
		t.Fatalf("got %v, want %s (no hidden folders, no node_modules, nothing too deep, no empty files, no symlinks, no duplicates)", names, want)
	}
	if got[1].Category != domain.VaultID || got[0].Category != domain.VaultResume || got[1].Size != 100 {
		t.Errorf("unexpected categories/sizes: %+v", got)
	}
}

func TestMatchMissPicksTheFileThatSharesTheMeaningfulWords(t *testing.T) {
	cands := []Candidate{
		{Name: "AJ_resume_context.md"}, {Name: "PAN_card.jpg"}, {Name: "Aadhaar_front.png"}, {Name: "bank_statement_oct.pdf"},
	}
	cases := map[string]string{
		"PAN card image":           "PAN_card.jpg",
		"my pan":                   "PAN_card.jpg",
		"copy of the aadhaar card": "Aadhaar_front.png",
		"October bank statement":   "bank_statement_oct.pdf",
		"resume context":           "AJ_resume_context.md",
		"passport photo":           "", // nothing shares a word: no guess
		"image":                    "", // only filler words
		"":                         "",
	}
	for query, want := range cases {
		got := MatchMiss(query, cands)
		switch {
		case want == "" && got != nil:
			t.Errorf("%q should match nothing, got %s", query, got.Name)
		case want != "" && (got == nil || got.Name != want):
			t.Errorf("%q should match %s, got %v", query, want, got)
		}
	}
	// two equally good fits are not guessed between
	twins := []Candidate{{Name: "PAN_card.jpg"}, {Name: "PAN_card_old.jpg"}}
	if got := MatchMiss("pan", twins); got != nil {
		t.Errorf("a tie must return nothing, got %s", got.Name)
	}
	if MatchMiss("pan card", nil) != nil {
		t.Error("no candidates, no match")
	}
}

type fakeUploader struct {
	got []Upload
	dup bool
	err error
}

func (f *fakeUploader) Add(_ context.Context, u Upload) (bool, error) {
	f.got = append(f.got, u)
	return f.dup, f.err
}

func TestMirrorEncryptsSensitiveDocumentsBeforeTheyLeave(t *testing.T) {
	dir := t.TempDir()
	secret := []byte("PAN ABCDE1234F issued to A. Jain")
	path := filepath.Join(dir, "PAN_card.jpg")
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	c := Candidate{Path: path, Name: "PAN_card.jpg", Label: "PAN card", Category: domain.VaultID, Size: int64(len(secret))}
	up := &fakeUploader{}

	if _, err := Mirror(context.Background(), up, "", c); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("an ID must not go up without a passphrase, got %v", err)
	}
	if len(up.got) != 0 {
		t.Fatal("nothing may be sent when the passphrase is missing")
	}

	out, err := Mirror(context.Background(), up, "vault passphrase", c)
	if err != nil || !out.Uploaded || !out.Encrypted {
		t.Fatalf("got %+v, %v", out, err)
	}
	sent := up.got[0]
	if bytes.Contains(sent.Data, []byte("ABCDE1234F")) || !IsEncrypted(sent.Data) {
		t.Fatal("the bytes sent must be ciphertext")
	}
	if plain, err := Decrypt("vault passphrase", sent.Data); err != nil || !bytes.Equal(plain, secret) {
		t.Fatalf("the owner must be able to read it back: %v", err)
	}
	if len(sent.Fingerprint) != 64 || sent.Label != "PAN card" || sent.Category != domain.VaultID {
		t.Errorf("upload metadata wrong: %+v", sent)
	}
}

func TestMirrorSendsOrdinaryDocumentsAsTheyAreAndReportsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.md")
	os.WriteFile(path, []byte("# Resume"), 0o644)
	c := Candidate{Path: path, Name: "resume.md", Label: "resume", Category: domain.VaultResume}
	up := &fakeUploader{}
	out, err := Mirror(context.Background(), up, "", c)
	if err != nil || out.Encrypted || string(up.got[0].Data) != "# Resume" {
		t.Fatalf("ordinary documents need no passphrase and go as they are: %+v %v", out, err)
	}
	up.dup = true
	out, _ = Mirror(context.Background(), up, "", c)
	if out.Uploaded || !out.Duplicate {
		t.Errorf("a duplicate is reported as such: %+v", out)
	}
}

func TestMirrorRefusesWhatItShouldNotSend(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.pdf")
	os.WriteFile(real, []byte("x"), 0o644)
	link := filepath.Join(dir, "resume.pdf")
	os.Symlink(real, link)
	big := filepath.Join(dir, "resume-big.pdf")
	os.WriteFile(big, make([]byte, MaxFileBytes+1), 0o644)
	up := &fakeUploader{}
	for name, c := range map[string]Candidate{
		"symlink":      {Path: link, Name: "resume.pdf", Label: "r", Category: domain.VaultResume},
		"too large":    {Path: big, Name: "resume-big.pdf", Label: "r", Category: domain.VaultResume},
		"missing":      {Path: filepath.Join(dir, "gone.pdf"), Name: "gone.pdf", Label: "r", Category: domain.VaultResume},
		"bad category": {Path: real, Name: "real.pdf", Label: "r", Category: "weird"},
	} {
		if _, err := Mirror(context.Background(), up, "p", c); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(up.got) != 0 {
		t.Fatal("nothing refused may be sent")
	}
	up.err = errors.New("api down")
	if _, err := Mirror(context.Background(), up, "p", Candidate{Path: real, Name: "real.pdf", Label: "r", Category: domain.VaultOther}); err == nil {
		t.Error("an upload error must be returned")
	}
}
