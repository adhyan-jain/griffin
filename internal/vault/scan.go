package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/griffin/core/internal/domain"
)

// MaxFileBytes matches the API's upload limit.
const MaxFileBytes = 5<<20 - 1024

// Candidate is a file that looks like something the owner may want on their phone. Finding one reads only
// its name and size: nothing is opened, hashed or sent until the owner says yes.
type Candidate struct {
	Path     string
	Name     string
	Label    string
	Category domain.VaultCategory
	Size     int64
}

var allowedExt = map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".md": true, ".txt": true, ".doc": true, ".docx": true}

// rules map words in a file name to a category and a label. The first rule that matches wins, and the
// sensitive ones come first so a file named "pan_card_resume.pdf" is treated as an ID, the careful choice.
var rules = []struct {
	words    []string
	category domain.VaultCategory
	label    string
}{
	{[]string{"pan", "aadhaar", "aadhar", "passport", "licence", "license", "voter", "dl"}, domain.VaultID, ""},
	{[]string{"bank", "statement", "itr", "form16", "tax", "passbook", "cheque"}, domain.VaultFinance, ""},
	{[]string{"resume", "cv", "curriculum"}, domain.VaultResume, ""},
	{[]string{"context"}, domain.VaultResume, ""}, // AJ_resume_context and the like
	{[]string{"marksheet", "certificate", "transcript", "degree", "offer", "internship", "letter"}, domain.VaultCertificate, ""},
}

// tokens splits a file name into lower-case words: separators, camelCase and digits all break words.
func tokens(name string) []string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case unicode.IsLetter(r):
			if unicode.IsUpper(r) && i > 0 && unicode.IsLower(runes[i-1]) {
				flush()
			}
			cur = append(cur, r)
		case unicode.IsDigit(r) && len(cur) > 0:
			cur = append(cur, r) // keep "form16" together
		default:
			flush()
		}
	}
	flush()
	return out
}

// Classify says whether a file name looks like a document worth offering, and how to treat it.
func Classify(name string) (label string, cat domain.VaultCategory, ok bool) {
	if !allowedExt[strings.ToLower(filepath.Ext(name))] {
		return "", "", false
	}
	toks := tokens(name)
	for _, rule := range rules {
		for _, w := range rule.words {
			for _, t := range toks {
				if t == w {
					return prettyLabel(name), rule.category, true
				}
			}
		}
	}
	return "", "", false
}

func prettyLabel(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(base)
	return strings.Join(strings.Fields(base), " ")
}

var skipDirs = map[string]bool{"node_modules": true, ".git": true, ".cache": true, "vendor": true, "__pycache__": true, ".venv": true, "venv": true, "target": true, "build": true, "dist": true}

// Scan walks the given folders and lists files whose names look like documents. It never reads a file, never
// follows a symbolic link (so it cannot be led outside the folders it was given), skips hidden and
// build folders, and stops at maxDepth levels.
func Scan(ctx context.Context, roots []string, maxDepth int) ([]Candidate, error) {
	if maxDepth <= 0 {
		maxDepth = 4
	}
	var out []Candidate
	seen := map[string]bool{}
	for _, root := range roots {
		root = filepath.Clean(root)
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return nil // unreadable entries are skipped, not fatal
			}
			rel, _ := filepath.Rel(root, path)
			depth := 0
			if rel != "." {
				depth = strings.Count(rel, string(filepath.Separator)) + 1
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if path != root && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()] || depth > maxDepth) {
					return filepath.SkipDir
				}
				return nil
			}
			if depth > maxDepth+1 || seen[path] {
				return nil
			}
			label, cat, ok := Classify(d.Name())
			if !ok {
				return nil
			}
			fi, err := d.Info()
			if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
				return nil
			}
			seen[path] = true
			out = append(out, Candidate{Path: path, Name: d.Name(), Label: label, Category: cat, Size: fi.Size()})
			return nil
		})
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

var missStop = map[string]bool{"a": true, "an": true, "the": true, "of": true, "my": true, "me": true, "copy": true, "image": true, "photo": true, "picture": true, "scan": true, "scanned": true, "file": true, "document": true, "doc": true, "pdf": true, "front": true, "back": true, "card": true, "please": true, "need": true, "get": true, "send": true}

// MatchMiss picks the candidate that best fits a description such as "PAN card image": the one sharing the
// most meaningful words with its name. It returns nil when nothing shares a word, because a wrong
// suggestion about an ID document is worse than none. A tie for the best fit also returns nil.
func MatchMiss(query string, candidates []Candidate) *Candidate {
	want := map[string]bool{}
	for _, t := range tokens(query + ".x") {
		if !missStop[t] && len(t) > 1 {
			want[t] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	best, bestScore, tied := -1, 0, false
	for i, c := range candidates {
		score := 0
		seen := map[string]bool{}
		for _, t := range tokens(c.Name) {
			if want[t] && !seen[t] {
				seen[t] = true
				score++
			}
		}
		switch {
		case score > bestScore:
			best, bestScore, tied = i, score, false
		case score == bestScore && score > 0:
			tied = true
		}
	}
	// Two files fitting equally well is a question for the owner, not a coin toss: a wrong ID is worse than none.
	if best < 0 || tied {
		return nil
	}
	return &candidates[best]
}

// Upload is one document going to the Vault.
type Upload struct {
	Label       string
	Category    domain.VaultCategory
	Fingerprint string
	Filename    string
	Data        []byte
}

// Uploader is the Vault API as the host sees it.
type Uploader interface {
	Add(ctx context.Context, u Upload) (duplicate bool, err error)
}

// Outcome says what Mirror did with one file.
type Outcome struct {
	Candidate Candidate
	Uploaded  bool
	Duplicate bool
	Encrypted bool
}

// Mirror uploads one approved candidate. Sensitive categories are encrypted here, on the laptop, with the
// owner's passphrase, and are refused when there is none: the host never sends an ID in the clear, even
// though the server would also refuse it.
func Mirror(ctx context.Context, up Uploader, passphrase string, c Candidate) (*Outcome, error) {
	if !c.Category.Valid() {
		return nil, errors.New("vault: unknown category")
	}
	sensitive := c.Category.Sensitive()
	if sensitive && passphrase == "" {
		return nil, fmt.Errorf("%s is a %s document and needs your Vault passphrase (set VAULT_PASSPHRASE_FILE) so it can be encrypted before upload", c.Name, c.Category)
	}
	fi, err := os.Lstat(c.Path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() { // a symbolic link could point anywhere
		return nil, fmt.Errorf("%s is not a regular file", c.Name)
	}
	if fi.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%s is %d MB; the Vault takes documents up to 5 MB", c.Name, fi.Size()>>20)
	}
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	payload := data
	if sensitive {
		if payload, err = Encrypt(passphrase, data); err != nil {
			return nil, err
		}
	}
	dup, err := up.Add(ctx, Upload{Label: c.Label, Category: c.Category, Fingerprint: hex.EncodeToString(sum[:]), Filename: c.Name, Data: payload})
	if err != nil {
		return nil, err
	}
	return &Outcome{Candidate: c, Uploaded: !dup, Duplicate: dup, Encrypted: sensitive}, nil
}
