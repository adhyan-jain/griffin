package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/storage"
)

// CommunityScanTitle is the daily routine that finds conversations worth joining, in code.
const CommunityScanTitle = "COMMUNITY SCAN"

// IsCommunityScan reports whether a task is that routine.
func IsCommunityScan(title string) bool {
	return strings.Contains(strings.ToUpper(title), CommunityScanTitle)
}

// CommunityHeader is the columns of the Communities list.
const CommunityHeader = "| Platform | Community | Thread | Why it fits | Mention a project? | Posted | Link | Status |\n|---|---|---|---|---|---|---|---|"

const (
	communityPerDay = 15
	communityMaxRow = 200
)

// CommunityScanResult counts what one scan did.
type CommunityScanResult struct {
	Read, Added, Mentions, Seen int
	Names                       []string
	Problems                    []string
	SaveError                   string
}

// Summary is the owner-facing line for a scan.
func (r CommunityScanResult) Summary() string {
	var b strings.Builder
	if r.SaveError != "" {
		fmt.Fprintf(&b, "Community scan found %d new conversations that fit you but could not save them to the Communities list: %s", r.Added, r.SaveError)
	} else {
		fmt.Fprintf(&b, "Community scan: read %d recent threads on Reddit and Hacker News; %d new conversations fit you and are on the Communities list (%d where one of your projects is worth a mention), %d were already listed.", r.Read, r.Added, r.Mentions, r.Seen)
	}
	if len(r.Names) > 0 {
		b.WriteString("\n\nNew:")
		for _, n := range r.Names {
			b.WriteString("\n- " + n)
		}
	}
	if r.Added == 0 && r.SaveError == "" {
		b.WriteString("\n\nNothing new today. Every thread listed was read from the site itself, so an empty day means no recent thread fit.")
	}
	if len(r.Problems) > 0 {
		b.WriteString("\n\nCould not read: " + strings.Join(r.Problems, "; ") + ".")
	}
	b.WriteString("\n\nTap a thread and ask Griffin to draft a reply: it writes one for your approval and nothing is posted until you approve it.")
	return b.String()
}

// ScanCommunities adds the day's best conversations to the Communities list, in code, with no model: threads read
// from Reddit and Hacker News, scored by their own words, never one already on the list. Read only: nothing is
// posted, voted or sent. get and now are nil for the network and the clock. handled is false for any other task.
func (w *TrackerLists) ScanCommunities(ctx context.Context, task *domain.Task, get sources.Get, now func() time.Time) (answer string, handled bool) {
	if w == nil || w.Store == nil || task == nil || !IsCommunityScan(task.Title) {
		return "", false
	}
	if now == nil {
		now = time.Now
	}
	at := now()
	threads, problems := sources.ReadCommunities(ctx, get, at)
	res := CommunityScanResult{Read: len(threads), Problems: problems}

	list := w.list(ctx, task.UserID, storage.ListKind, "community")
	text := ""
	if list != nil {
		text, _ = w.Store.ReadText(ctx, task.UserID, list.ID, trackerReadLimit)
	}
	known := listedLinks(text)
	picked := sources.PickCommunityThreads(threads, known, at, communityPerDay)
	for _, s := range picked {
		res.Added++
		if s.Mention != "" {
			res.Mentions++
		}
		name := shortTitle(s.Thread.Title)
		if s.Thread.Platform == sources.Reddit {
			name = s.Thread.Community + ": " + name
		}
		res.Names = append(res.Names, name)
	}
	for _, s := range sources.PickCommunityThreads(threads, nil, at, 1<<30) {
		if known[strings.ToLower(s.Thread.URL)] {
			res.Seen++
		}
	}
	if len(picked) > 0 {
		body := communityList(text, picked)
		var err error
		if list == nil {
			_, err = w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
				UserID: task.UserID, Title: "Communities tracker", Filename: "community-tracker.md",
				ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
				Body:     bytes.NewReader([]byte(body)),
				Metadata: domain.TrackerListMetadata(storage.ListKind, "community"),
			})
		} else {
			_, err = w.Store.UpdateText(ctx, list.ID, task.UserID, body, false, trackerReadLimit)
		}
		if err != nil {
			res.SaveError = err.Error()
		}
	}
	return res.Summary(), true
}

// listedLinks is every http link already in the list, lower-cased: a thread listed once is never listed again.
func listedLinks(text string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == '|' || r == '\n' || r == '(' || r == ')' }) {
		if strings.HasPrefix(f, "http") {
			out[strings.ToLower(f)] = true
		}
	}
	return out
}

// communityList is the list with the new rows on top (newest first) and at most communityMaxRow rows. Whatever
// the owner wrote above the table, and the rows he has edited, are kept as they are.
func communityList(current string, picked []sources.Scored) string {
	prefix, rows := "# Communities tracker\n\n", []string(nil)
	if tbl, ok := domain.FirstTable(current); ok {
		lines := strings.Split(tbl, "\n")
		rows = lines[2:]
		if i := strings.Index(current, lines[0]); i > 0 {
			prefix = current[:i]
		} else if i == 0 {
			prefix = ""
		}
	}
	fresh := make([]string, 0, len(picked)+len(rows))
	for _, s := range picked {
		fresh = append(fresh, communityRow(s))
	}
	all := append(fresh, rows...)
	if len(all) > communityMaxRow {
		all = all[:communityMaxRow]
	}
	return prefix + CommunityHeader + "\n" + strings.Join(all, "\n") + "\n"
}

func communityRow(s sources.Scored) string {
	mention := "No"
	if s.Mention != "" {
		mention = "Yes: " + s.Mention
	}
	return "| " + strings.Join([]string{
		cell(s.Thread.Platform), cell(s.Thread.Community), cell(shortTitle(s.Thread.Title)), cell(s.Why), cell(mention),
		s.Thread.Posted.UTC().Format("2006-01-02 15:04 UTC"), s.Thread.URL, "new",
	}, " | ") + " |"
}

// cell makes text safe inside one table cell.
func cell(s string) string {
	return strings.TrimSpace(strings.NewReplacer("|", "/", "\n", " ", "\r", " ").Replace(s))
}

func shortTitle(s string) string {
	s = cell(s)
	if r := []rune(s); len(r) > 140 {
		return string(r[:137]) + "..."
	}
	return s
}
