package handover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Everything below uses git's plumbing with a temporary index, so a snapshot never changes the owner's
// checked-out branch, their staging area, or a single file in their working tree. The only thing it
// writes is one new branch, and the only push it makes is that branch, never forced.

// secretLike names files that must never leave the machine in a snapshot. It is deliberately broad: a
// missed secret is worse than a missed config file.
func secretLike(name string) bool {
	base := strings.ToLower(path.Base(name))
	switch {
	case base == ".env" || (strings.HasPrefix(base, ".env.") && !strings.HasSuffix(base, ".example") && !strings.HasSuffix(base, ".sample") && !strings.HasSuffix(base, ".template")):
		return true
	case strings.HasSuffix(base, ".pem"), strings.HasSuffix(base, ".key"), strings.HasSuffix(base, ".p12"), strings.HasSuffix(base, ".pfx"),
		strings.HasSuffix(base, ".keystore"), strings.HasSuffix(base, ".jks"), strings.HasSuffix(base, ".tfstate"), strings.HasSuffix(base, ".tfvars"),
		strings.HasPrefix(base, "id_rsa"), strings.HasPrefix(base, "id_ed25519"), strings.HasPrefix(base, "id_ecdsa"),
		base == ".npmrc" || base == ".pypirc" || base == ".netrc" || base == ".git-credentials" || base == ".htpasswd",
		strings.HasPrefix(base, "credentials"), strings.Contains(base, "service-account"), strings.Contains(base, "secret"),
		strings.HasSuffix(base, ".kdbx"), strings.HasSuffix(base, ".gpg"), strings.HasSuffix(base, ".asc"):
		return true
	}
	// Anything under a directory that holds keys.
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if part == ".ssh" || part == ".aws" || part == ".gnupg" {
			return true
		}
	}
	return false
}

// SnapshotOptions control one snapshot.
type SnapshotOptions struct {
	// Branch is the handover branch. The first snapshot creates it from HEAD; later ones continue it, so a
	// push is always a fast-forward.
	Branch string
	// Doc is the handover note, saved as HANDOVER.md on the branch only.
	Doc func(SnapshotFacts) string
	// Push sends the branch to Remote (default: the first remote). A failed push is reported, not fatal.
	Push        bool
	Remote      string
	PushTimeout time.Duration
	// MaxFileBytes skips larger files. Default 5 MiB.
	MaxFileBytes int64
	// Who is used for the commit when git has no identity configured.
	Who string
}

// SnapshotFacts is what the note is built from.
type SnapshotFacts struct {
	Repo       string
	BaseBranch string
	BaseCommit string
	Branch     string
	Changed    []string
	Skipped    []string
	RecentLog  string
}

// SnapshotResult says what happened to one repository.
type SnapshotResult struct {
	Repo    string
	Branch  string
	Commit  string
	Files   int
	Skipped []string
	Clean   bool // nothing to save
	Pushed  bool
	PushErr string
	Notes   []string
}

type gitRun struct {
	dir string
	env []string
}

func (g gitRun) run(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), g.env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// Snapshot saves the repository's current state (tracked changes and new files that are not ignored) to
// opt.Branch. See the comment above for what it never touches.
func Snapshot(ctx context.Context, repo string, opt SnapshotOptions) (*SnapshotResult, error) {
	if opt.Branch == "" || strings.ContainsAny(opt.Branch, " ~^:?*[\\") || strings.HasPrefix(opt.Branch, "-") {
		return nil, errors.New("handover: a valid branch name is required")
	}
	if opt.MaxFileBytes <= 0 {
		opt.MaxFileBytes = 5 << 20
	}
	g := gitRun{dir: repo}
	top, err := g.run(ctx, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", repo, err)
	}
	g.dir = top
	res := &SnapshotResult{Repo: top, Branch: opt.Branch}

	head, err := g.run(ctx, "", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("%s has no commits yet, so there is nothing to continue from", top)
	}
	baseBranch, _ := g.run(ctx, "", "rev-parse", "--abbrev-ref", "HEAD")

	tmp, err := os.CreateTemp("", "griffin-handover-index-*")
	if err != nil {
		return nil, err
	}
	tmpIndex := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpIndex)
	os.Remove(tmpIndex) // git wants to create it itself
	ig := gitRun{dir: top, env: []string{"GIT_INDEX_FILE=" + tmpIndex}}

	if _, err := ig.run(ctx, "", "read-tree", head); err != nil {
		return nil, err
	}
	if _, err := ig.run(ctx, "", "add", "-A"); err != nil {
		return nil, err
	}
	changedRaw, err := ig.run(ctx, "", "diff", "--cached", "--name-only", "-z", head)
	if err != nil {
		return nil, err
	}
	var skip []string
	var keep []string
	for _, f := range strings.Split(changedRaw, "\x00") {
		if f == "" {
			continue
		}
		reason := ""
		switch {
		case secretLike(f):
			reason = "looks like a secret"
		default:
			if st, err := os.Stat(filepath.Join(top, f)); err == nil && st.Size() > opt.MaxFileBytes {
				reason = "larger than the size limit"
			}
		}
		if reason != "" {
			skip = append(skip, f)
			res.Skipped = append(res.Skipped, f+" ("+reason+")")
			continue
		}
		keep = append(keep, f)
	}
	// Put the skipped files back to what HEAD has (or drop them if new), in the temporary index only.
	for len(skip) > 0 {
		n := len(skip)
		if n > 100 {
			n = 100
		}
		args := append([]string{"reset", "-q", head, "--"}, skip[:n]...)
		if _, err := ig.run(ctx, "", args...); err != nil {
			return nil, err
		}
		skip = skip[n:]
	}
	res.Files = len(keep)
	if len(keep) == 0 {
		res.Clean = true
		if len(res.Skipped) > 0 {
			res.Notes = append(res.Notes, "only files that were skipped for safety had changed")
		}
		return res, nil
	}

	if opt.Doc != nil {
		logOut, _ := g.run(ctx, "", "log", "-5", "--oneline")
		note := opt.Doc(SnapshotFacts{Repo: top, BaseBranch: baseBranch, BaseCommit: head, Branch: opt.Branch, Changed: keep, Skipped: res.Skipped, RecentLog: logOut})
		blob, err := ig.run(ctx, note, "hash-object", "-w", "--stdin")
		if err != nil {
			return nil, err
		}
		if _, err := ig.run(ctx, "", "update-index", "--add", "--cacheinfo", "100644,"+blob+",HANDOVER.md"); err != nil {
			return nil, err
		}
	}
	tree, err := ig.run(ctx, "", "write-tree")
	if err != nil {
		return nil, err
	}

	// Continue the branch if it exists, otherwise start it from HEAD, so a later push fast-forwards.
	parent := head
	ref := "refs/heads/" + opt.Branch
	if tip, err := g.run(ctx, "", "rev-parse", "--verify", "-q", ref); err == nil && tip != "" {
		parent = tip
	}
	cg := gitRun{dir: top}
	if name, _ := g.run(ctx, "", "config", "user.name"); name == "" {
		who := opt.Who
		if who == "" {
			who = "Griffin handover"
		}
		cg.env = append(cg.env, "GIT_AUTHOR_NAME="+who, "GIT_COMMITTER_NAME="+who, "GIT_AUTHOR_EMAIL=griffin@localhost", "GIT_COMMITTER_EMAIL=griffin@localhost")
	}
	msg := fmt.Sprintf("handover: work in progress saved at %s\n\nSaved automatically because the laptop was about to run out of battery.\nSee HANDOVER.md. Review before merging; nothing here has been tested.", time.Now().Format("2006-01-02 15:04"))
	commit, err := cg.run(ctx, "", "commit-tree", tree, "-p", parent, "-m", msg)
	if err != nil {
		return nil, err
	}
	if _, err := g.run(ctx, "", "update-ref", ref, commit); err != nil {
		return nil, err
	}
	res.Commit = commit

	if opt.Push {
		remote := opt.Remote
		if remote == "" {
			remotes, _ := g.run(ctx, "", "remote")
			remote = strings.TrimSpace(strings.SplitN(remotes, "\n", 2)[0])
		}
		if remote == "" {
			res.Notes = append(res.Notes, "no git remote, so the branch is saved on this laptop only")
			return res, nil
		}
		timeout := opt.PushTimeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		pctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		pg := gitRun{dir: top, env: []string{"GIT_TERMINAL_PROMPT=0"}}
		if os.Getenv("GIT_SSH_COMMAND") == "" {
			pg.env = append(pg.env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
		}
		// --no-verify: this is a throwaway safety branch pushed with seconds to spare, so a slow pre-push
		// hook must not stop it. It is never forced and never touches another branch.
		if _, err := pg.run(pctx, "", "push", "--no-verify", remote, ref+":"+ref); err != nil {
			res.PushErr = err.Error()
		} else {
			res.Pushed = true
		}
	}
	return res, nil
}
