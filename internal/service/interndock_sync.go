package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

// FetchPage returns a page's HTML. It is a field so tests need no network.
type FetchPage func(ctx context.Context, url string) (string, error)

func httpFetch(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Griffin/1.0")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), err
}

// SyncInternDock adds the InternDock postings that fit the owner to the Internships list, in code, with no model:
// real, dated postings, each with its own page as the link, never one already on the list. handled is false for
// any task that is not the sync.
func (w *TrackerLists) SyncInternDock(ctx context.Context, task *domain.Task, fetch FetchPage) (answer string, handled bool) {
	if w == nil || w.Store == nil || task == nil || !domain.IsInternDockSync(task.Title) {
		return "", false
	}
	if fetch == nil {
		fetch = httpFetch
	}
	var found []domain.Posting
	var failed []string
	for _, p := range domain.InternDockPages {
		page, err := fetch(ctx, domain.InternDockBase+p)
		if err != nil {
			failed = append(failed, p)
			continue
		}
		for _, post := range domain.ParseInternDock(page) {
			if domain.FitsOwner(post) {
				found = append(found, post)
			}
		}
	}
	list := w.list(ctx, task.UserID, storage.ListKind, "internships")
	text := ""
	if list != nil {
		text, _ = w.Store.ReadText(ctx, task.UserID, list.ID, trackerReadLimit)
	}
	header := strings.Split(domain.InternshipsHeader, "\n")[0]
	if tbl, ok := domain.FirstTable(text); ok {
		header = strings.Split(tbl, "\n")[0]
	}
	lower := strings.ToLower(text)
	var rows, added []string
	for _, p := range found {
		key := strings.ToLower("| " + p.Company + " | " + p.Title + " |")
		if strings.Contains(lower, strings.ToLower(p.Link)) || strings.Contains(lower, key) {
			continue
		}
		lower += "\n" + strings.ToLower(p.Link)
		rows = append(rows, domain.ListingRow(header, p))
		added = append(added, p.Title+" at "+p.Company)
	}
	if len(rows) > 0 {
		var err error
		if list == nil {
			_, err = w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
				UserID: task.UserID, Title: "Internships tracker", Filename: "internships-tracker.md",
				ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
				Body:     bytes.NewReader([]byte("# Internships tracker\n\n" + domain.InternshipsHeader + "\n" + strings.Join(rows, "\n") + "\n")),
				Metadata: domain.TrackerListMetadata(storage.ListKind, "internships"),
			})
		} else {
			_, err = w.Store.UpdateText(ctx, list.ID, task.UserID, strings.TrimRight(text, "\n")+"\n"+strings.Join(rows, "\n")+"\n", false, trackerReadLimit)
		}
		if err != nil {
			return "Found " + fmt.Sprint(len(rows)) + " new internships on InternDock but could not save them to the list: " + err.Error(), true
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "InternDock sync: %d postings fit you (software, backend or AI, in India or remote outside the US/UK); %d were new and are now on the Internships list.", len(found), len(added))
	for _, a := range added {
		b.WriteString("\n- " + a)
	}
	if len(failed) > 0 {
		b.WriteString("\nCould not read: " + strings.Join(failed, ", "))
	}
	return b.String(), true
}
