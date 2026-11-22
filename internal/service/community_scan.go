package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/griffin/core/internal/budget"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/storage"
)

// CommunityScanTitle is the daily routine that finds conversations worth joining, in code.
const CommunityScanTitle = "COMMUNITY SCAN"

// CommunityRepliesTitle is the routine that only writes replies for listed threads that have none: reading the sites
// takes most of a run's time, so replies get a run of their own.
const CommunityRepliesTitle = "COMMUNITY REPLIES"

// IsCommunityScan reports whether a task is that routine (or its replies-only run).
func IsCommunityScan(title string) bool {
	upper := strings.ToUpper(title)
	return strings.Contains(upper, CommunityScanTitle) || strings.Contains(upper, CommunityRepliesTitle)
}

// CommunityHeader is the columns of the Communities list.
const CommunityHeader = "| Platform | Community | Thread | Why it fits | Mention a project? | Posted | Reply you can send | Link | Status |\n|---|---|---|---|---|---|---|---|---|"

// communityOldCells is how many cells a row had before the reply column existed.
const communityOldCells = 8

const (
	communityPerDay = 15
	communityMaxRow = 200
)

// CommunityScanResult counts what one scan did.
type CommunityScanResult struct {
	Read, Added, Mentions, Seen int
	Replies, NoReply            int // replies written for new threads, and new threads left without one
	Backfilled, ReplyLeft       int // replies written for older threads that had none, and threads still without one
	Names                       []string
	Problems                    []string
	SaveError                   string
	RepliesOnly                 bool // a run that wrote replies for listed threads and read no site
}

// Summary is the owner-facing line for a scan.
func (r CommunityScanResult) Summary() string {
	var b strings.Builder
	if r.RepliesOnly {
		if r.SaveError != "" {
			return "Wrote replies for listed threads but could not save them to the Communities list: " + r.SaveError
		}
		fmt.Fprintf(&b, "Community replies: wrote %d ready-to-send replies for threads on the Communities list.", r.Backfilled)
		if r.ReplyLeft > 0 {
			fmt.Fprintf(&b, " %d threads still have none (a draft failed the fact check, or there was no time); the next run tries them.", r.ReplyLeft)
		}
		return b.String()
	}
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
	if r.Replies > 0 {
		fmt.Fprintf(&b, "\n\nWrote %d ready-to-send replies in the list", r.Replies)
		if r.NoReply > 0 {
			fmt.Fprintf(&b, " (%d threads have none: the draft failed a fact check or could not be written)", r.NoReply)
		}
		b.WriteString(".")
	}
	if r.Backfilled > 0 {
		fmt.Fprintf(&b, "\n\nWrote %d more replies for older threads that had none.", r.Backfilled)
	}
	if r.ReplyLeft > 0 {
		fmt.Fprintf(&b, "\n\n%d more threads have no reply yet; the next run tries them (at most %d replies a run, and only while there is time).", r.ReplyLeft, communityReplyCap)
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
	// Reading the threads may use only part of the time, so some is left for the replies; fetches and model calls
	// end before the deadline so the list can still be saved.
	work, cancelWork := budget.Work(ctx)
	defer cancelWork()
	repliesOnly := strings.Contains(strings.ToUpper(task.Title), CommunityRepliesTitle)
	var threads []sources.Thread
	var problems []string
	if !repliesOnly {
		readCtx, cancelRead := budget.Share(work, 0.6)
		threads, problems = sources.ReadCommunities(readCtx, get, at)
		cancelRead()
	}
	res := CommunityScanResult{Read: len(threads), Problems: problems, RepliesOnly: repliesOnly}

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
	var replies, fills map[string]string
	if run := w.newReplyRun(work, task.UserID, get); run != nil {
		var rejected, untried int
		replies, rejected, untried = run.fill(work, picked)
		res.Replies, res.NoReply, res.ReplyLeft = len(replies), rejected, untried
		// Threads listed on an earlier run that never got a reply are written now, newest first, while the run's
		// cap and the time allow.
		var rejectedOld, untriedOld int
		fills, rejectedOld, untriedOld = run.fill(work, replyBackfill(text, at))
		res.Backfilled, res.ReplyLeft = len(fills), res.ReplyLeft+rejectedOld+untriedOld
	} else {
		res.NoReply = len(picked)
	}
	// A list from before the reply column is rewritten with it even on a day with nothing new.
	if body := communityList(text, picked, replies, fills); len(picked) > 0 || len(fills) > 0 || (list != nil && !hasReplyColumn(text)) {
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
func communityList(current string, picked []sources.Scored, replies, fills map[string]string) string {
	prefix, rows := "# Communities tracker\n\n", []string(nil)
	if tbl, ok := domain.FirstTable(current); ok {
		lines := strings.Split(tbl, "\n")
		rows = make([]string, 0, len(lines)-2)
		for _, r := range lines[2:] {
			rows = append(rows, withReply(withReplyCell(r), fills))
		}
		if i := strings.Index(current, lines[0]); i > 0 {
			prefix = current[:i]
		} else if i == 0 {
			prefix = ""
		}
	}
	fresh := make([]string, 0, len(picked)+len(rows))
	for _, s := range picked {
		fresh = append(fresh, communityRow(s, replies[strings.ToLower(s.Thread.URL)]))
	}
	all := append(fresh, rows...)
	if len(all) > communityMaxRow {
		all = all[:communityMaxRow]
	}
	return prefix + CommunityHeader + "\n" + strings.Join(all, "\n") + "\n"
}

func communityRow(s sources.Scored, reply string) string {
	if reply = replyCell(reply); reply == "" {
		reply = noReply
	}
	mention := "No"
	if s.Mention != "" {
		mention = "Yes: " + s.Mention
	}
	return "| " + strings.Join([]string{
		cell(s.Thread.Platform), cell(s.Thread.Community), cell(shortTitle(s.Thread.Title)), cell(s.Why), cell(mention),
		s.Thread.Posted.UTC().Format("2006-01-02 15:04 UTC"), reply, s.Thread.URL, "new",
	}, " | ") + " |"
}

// hasReplyColumn reports whether the list's table already has the reply column (or there is no table to migrate).
func hasReplyColumn(text string) bool {
	tbl, ok := domain.FirstTable(text)
	return !ok || strings.Contains(strings.SplitN(tbl, "\n", 2)[0], "Reply you can send")
}

// withReplyCell gives a row written before the reply column existed a "—" there, before its Link. Rows that
// already have the column, and any row of another shape, are left as they are.
func withReplyCell(row string) string {
	t := strings.TrimSpace(row)
	if !strings.HasPrefix(t, "|") || !strings.HasSuffix(t, "|") {
		return row
	}
	cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(t, "|"), "|"), "|")
	if len(cells) != communityOldCells {
		return row
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	cells = append(cells[:6], append([]string{noReply}, cells[6:]...)...)
	return "| " + strings.Join(cells, " | ") + " |"
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
