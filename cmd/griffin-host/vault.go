package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/griffin/core/internal/hostagent"
	"github.com/griffin/core/internal/vault"
)

// Vault (environment). The host only offers documents; it uploads what the owner approves, one by one.
//
//	VAULT_FOLDERS          folders to look in, separated by ":" (default ~/Documents/Griffin)
//	VAULT_PASSPHRASE_FILE  a file holding the Vault passphrase, needed to upload ID and bank documents
//	                       (they are encrypted here before they leave; keep the file chmod 600)

func vaultFolders() []string {
	if v := strings.TrimSpace(os.Getenv("VAULT_FOLDERS")); v != "" {
		var out []string
		for _, p := range strings.Split(v, ":") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, filepath.Clean(p))
			}
		}
		return out
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	return []string{filepath.Join(home, "Documents", "Griffin")}
}

func vaultPassphrase(logger *slog.Logger) (string, error) {
	path := strings.TrimSpace(os.Getenv("VAULT_PASSPHRASE_FILE"))
	if path == "" {
		return "", nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read the passphrase file: %w", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		logger.Warn("the passphrase file is readable by other users; run chmod 600 on it", "path", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

type hostVaultUploader struct{ c *hostagent.Client }

func (u hostVaultUploader) Add(ctx context.Context, up vault.Upload) (bool, error) {
	return u.c.VaultAdd(ctx, up)
}

func vaultClient(logger *slog.Logger) (*hostagent.Client, error) {
	apiURL := strings.TrimSpace(os.Getenv("GRIFFIN_API_URL"))
	if err := validateAPIURL(apiURL); err != nil {
		return nil, err
	}
	token, err := loadToken(logger)
	if err != nil {
		return nil, err
	}
	return hostagent.NewClient(apiURL, token, nil), nil
}

// vaultScan lists what looks like documents. It reads names only and uploads nothing.
func vaultScan(ctx context.Context, logger *slog.Logger) error {
	folders := vaultFolders()
	fmt.Println("Looking at file names (nothing is opened or sent) in:")
	for _, f := range folders {
		fmt.Println("  ", f)
	}
	cands, err := vault.Scan(ctx, folders, 4)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		fmt.Println("No documents found. Put them in one of those folders, or set VAULT_FOLDERS=/path/one:/path/two.")
		return nil
	}
	for _, c := range cands {
		flag := ""
		if c.Category.Sensitive() {
			flag = "  [will be encrypted; needs your passphrase]"
		}
		fmt.Printf("  %-12s %-40s %6s%s\n", c.Category, c.Name, humanSize(c.Size), flag)
	}
	fmt.Printf("\n%d possible documents. Upload the ones you want with: griffin-host vault-sync\n", len(cands))
	return nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// vaultSync answers documents that were needed and missing, then offers the rest, asking before every
// upload. Nothing is sent without a "y".
func vaultSync(ctx context.Context, logger *slog.Logger) error {
	client, err := vaultClient(logger)
	if err != nil {
		return err
	}
	pass, err := vaultPassphrase(logger)
	if err != nil {
		return err
	}
	cands, err := vault.Scan(ctx, vaultFolders(), 4)
	if err != nil {
		return err
	}
	up := hostVaultUploader{client}
	in := bufio.NewReader(os.Stdin)
	ask := func(q string) bool {
		fmt.Print(q + " [y/N] ")
		line, _ := in.ReadString('\n')
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y")
	}
	uploaded := map[string]bool{}
	send := func(c vault.Candidate) bool {
		out, err := vault.Mirror(ctx, up, pass, c)
		switch {
		case err != nil:
			fmt.Println("   not uploaded:", err)
			return false
		case out.Duplicate:
			fmt.Println("   already in your Vault")
		default:
			fmt.Printf("   uploaded%s\n", map[bool]string{true: " (encrypted on this laptop first)", false: ""}[out.Encrypted])
		}
		uploaded[c.Path] = true
		return true
	}

	misses, err := client.VaultMisses(ctx)
	if err != nil {
		if errors.Is(err, hostagent.ErrUnauthorized) {
			return err
		}
		fmt.Println("(could not read the list of documents Griffin needed:", err.Error()+")")
	}
	for _, m := range misses {
		c := vault.MatchMiss(m.Query, cands)
		if c == nil {
			fmt.Printf("Griffin needed %q but nothing here looks like it. Put it in %s and run this again.\n", m.Query, strings.Join(vaultFolders(), " or "))
			continue
		}
		fmt.Printf("Griffin needed %q (asked %d times). Best match: %s\n", m.Query, m.Count, c.Path)
		if ask("   Add it to your Vault?") && send(*c) {
			_ = client.ResolveVaultMiss(ctx, m.ID)
		}
	}

	var rest []vault.Candidate
	for _, c := range cands {
		if !uploaded[c.Path] {
			rest = append(rest, c)
		}
	}
	if len(rest) > 0 {
		fmt.Printf("\n%d other possible documents. For each, say whether to add it.\n", len(rest))
	}
	for _, c := range rest {
		note := ""
		if c.Category.Sensitive() {
			note = " (ID/bank: encrypted here before upload)"
		}
		if ask(fmt.Sprintf("Add %s as %q%s?", c.Path, c.Label, note)) {
			send(c)
		}
	}
	return nil
}
