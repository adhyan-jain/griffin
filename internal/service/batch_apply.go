package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

const maxBatchApplications = 25

func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func col(headers []string, needles ...string) int {
	for i, h := range headers {
		for _, n := range needles {
			if strings.Contains(strings.ToLower(h), n) {
				return i
			}
		}
	}
	return -1
}

type listing struct{ company, role, link string }

// batchListings picks the rows of a list that should get an application: the tier asked for, not already applied
// to, deadline not passed.
func batchListings(table, tier string, today time.Time) []listing {
	lines := strings.Split(table, "\n")
	if len(lines) < 3 {
		return nil
	}
	h := tableCells(lines[0])
	cTier, cCo, cRole, cLink, cStatus, cDead := col(h, "tier"), col(h, "company", "name"), col(h, "role", "title"), col(h, "link", "apply", "url"), col(h, "status"), col(h, "deadline", "due")
	if cCo < 0 || cRole < 0 {
		return nil
	}
	get := func(c []string, i int) string {
		if i >= 0 && i < len(c) {
			return c[i]
		}
		return ""
	}
	var out []listing
	for _, l := range lines[2:] {
		c := tableCells(l)
		if tier != "" && !strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(get(c, cTier)), "tier")), tier) {
			continue
		}
		if s := strings.ToLower(get(c, cStatus)); s != "" && s != "not_applied" && s != "not applied" {
			continue
		}
		if d, err := time.Parse("2006-01-02", get(c, cDead)); err == nil && d.Before(today.Truncate(24*time.Hour)) {
			continue
		}
		if get(c, cCo) == "" || get(c, cRole) == "" {
			continue
		}
		out = append(out, listing{get(c, cCo), get(c, cRole), get(c, cLink)})
	}
	return out
}

// BatchApply handles a task that asks for applications to be prepared for many listings. It creates one apply
// task per matching listing (skipping any that already has one) and returns the parent's answer. handled is false
// for any other task, which then runs as usual.
func (w *TrackerLists) BatchApply(ctx context.Context, task *domain.Task, create func(context.Context, *domain.Task) error) (answer string, handled bool) {
	if w == nil || w.Store == nil || create == nil {
		return "", false
	}
	tier, ok := domain.ApplyBatchOf(task.Title, task.Goal, task.InputPayload)
	if !ok {
		return "", false
	}
	list := w.list(ctx, task.UserID, "list", "internships")
	if list == nil {
		return "", false
	}
	table, err := w.Store.ReadText(ctx, task.UserID, list.ID, trackerReadLimit)
	if err != nil {
		return "", false
	}
	tbl, _ := domain.FirstTable(table)
	rows := batchListings(tbl, tier, time.Now())
	if len(rows) == 0 {
		return "There are no listings to prepare: none match" + tierWords(tier) + " that are unapplied and still open.", true
	}

	existing := map[string]bool{}
	if w.Tasks != nil {
		uid := task.UserID
		if all, lerr := w.Tasks.ListTasks(ctx, repository.TaskFilter{UserID: &uid, Limit: 1000}); lerr == nil {
			for _, t := range all {
				var p struct {
					ApplyFor struct{ Company, Role string } `json:"apply_for"`
				}
				if json.Unmarshal(t.InputPayload, &p) == nil && p.ApplyFor.Company != "" && t.Status != domain.TaskStatus("CANCELLED") && t.Status != domain.TaskStatus("FAILED") {
					existing[strings.ToLower(p.ApplyFor.Company+"|"+p.ApplyFor.Role)] = true
				}
			}
		}
	}

	var made, skipped []string
	for _, r := range rows {
		if existing[strings.ToLower(r.company+"|"+r.role)] {
			skipped = append(skipped, r.company)
			continue
		}
		if len(made) >= maxBatchApplications {
			break
		}
		title, goal, payload := domain.ApplyTaskFor(r.company, r.role, r.link)
		parent := task.ID
		child := &domain.Task{
			ID: uuid.New(), UserID: task.UserID, ParentID: &parent, Title: title, Goal: goal,
			ManagerDomain: "career", Priority: task.Priority, InputPayload: payload,
		}
		if err := create(ctx, child); err != nil {
			if w.Logger != nil {
				w.Logger.Warn("Could not create an application task", "company", r.company, "error", err)
			}
			continue
		}
		made = append(made, r.company+" ("+r.role+")")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Split into %d application tasks, one per listing%s, so each is drafted on its own. Each prepares the full package (resume summary, cover note, answers) and waits for your approval; nothing is submitted.\n\n", len(made), tierWords(tier))
	for _, m := range made {
		b.WriteString("- " + m + "\n")
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "\nAlready had an application task, so skipped: %s.\n", strings.Join(skipped, ", "))
	}
	return strings.TrimSpace(b.String()), true
}

func tierWords(tier string) string {
	if tier == "" {
		return ""
	}
	return " in Tier " + tier
}
