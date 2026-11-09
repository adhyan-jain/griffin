package handover

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DocContext is what the note says about the machine; SnapshotFacts adds what it says about one repo.
type DocContext struct {
	Host    string
	Battery int
	Level   Level
	At      time.Time
	// Tasks that were running and were handed back.
	Tasks []string
}

// BuildDoc writes HANDOVER.md: what was happening, where the work is, and how to continue. It names files
// and never includes their contents, so it cannot leak anything that was skipped.
func BuildDoc(c DocContext, f SnapshotFacts) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Handover: %s\n\n", f.Branch)
	fmt.Fprintf(&b, "Saved automatically at %s from **%s** because its battery reached %d%% (stage: %s).\n\n",
		c.At.Format("2006-01-02 15:04 MST"), c.Host, c.Battery, c.Level)

	b.WriteString("## Where the work is\n")
	fmt.Fprintf(&b, "- Branch: `%s` (the owner was on `%s`, at commit `%s`)\n", f.Branch, f.BaseBranch, short(f.BaseCommit))
	fmt.Fprintf(&b, "- Files saved: %d\n", len(f.Changed))
	changed := append([]string(nil), f.Changed...)
	sort.Strings(changed)
	for i, name := range changed {
		if i == 40 {
			fmt.Fprintf(&b, "  - … and %d more\n", len(changed)-40)
			break
		}
		fmt.Fprintf(&b, "  - `%s`\n", name)
	}
	if len(f.Skipped) > 0 {
		b.WriteString("- Not saved, on purpose (they stay on the laptop):\n")
		for _, s := range f.Skipped {
			fmt.Fprintf(&b, "  - `%s`\n", s)
		}
	}
	if f.RecentLog != "" {
		b.WriteString("\nRecent commits before this one:\n\n```\n" + f.RecentLog + "\n```\n")
	}

	b.WriteString("\n## Tasks that were running\n")
	if len(c.Tasks) == 0 {
		b.WriteString("- None.\n")
	}
	for _, t := range c.Tasks {
		fmt.Fprintf(&b, "- %s (handed back to Griffin; it will run again from the start)\n", t)
	}

	b.WriteString("\n## How to continue\n")
	b.WriteString("1. Check out this branch and read the diff against the base commit above.\n")
	b.WriteString("2. Nothing here was tested or reviewed: it is a snapshot, not a finished change. Run the project's tests first.\n")
	b.WriteString("3. Keep working on this branch. Do not push to the owner's main branch, and do not force-push anything.\n")
	b.WriteString("4. The owner reviews before anything is merged.\n")
	return b.String()
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
