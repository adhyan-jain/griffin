package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// ListingsAPI is the part of the listings repository the tools use (*repository.OpportunityRepository satisfies it).
type ListingsAPI interface {
	ListAssessed(ctx context.Context, userID uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*domain.Listing, error)
	Move(ctx context.Context, userID, id uuid.UUID, from, to, note string, taskID *uuid.UUID) error
}

// TrackerAPI reads and writes a tracker's shared markdown list (*service.TrackerLists satisfies it).
type TrackerAPI interface {
	ReadList(ctx context.Context, userID uuid.UUID, trackerType string) (string, bool, error)
	WriteList(ctx context.Context, userID uuid.UUID, trackerType, text string) error
}

// VaultLister lists the vault's documents (*storage.StorageService satisfies it). The tools show names only.
type VaultLister interface {
	ListByKind(ctx context.Context, userID uuid.UUID, kind string) ([]*domain.Artifact, error)
}

const (
	maxInlineLinkChecks = 10
	maxListingsShown    = 100
)

// trackerType turns the owner's or model's wording into a tracker type id.
func trackerType(s string) (string, error) {
	t := strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := domain.TrackerTypes[t]; !ok {
		var names []string
		for k := range domain.TrackerTypes {
			names = append(names, k)
		}
		sort.Strings(names)
		return "", fmt.Errorf("unknown tracker %q; the trackers are: %s", s, strings.Join(names, ", "))
	}
	return t, nil
}

// workUndo is what the undo data of a tracker or listing edit holds.
type workUndo struct {
	Kind  string      `json:"kind"` // "tracker" or "listings"
	Type  string      `json:"type,omitempty"`
	Text  string      `json:"text,omitempty"`
	Items []stageItem `json:"items,omitempty"`
}

type stageItem struct {
	ID    uuid.UUID `json:"id"`
	Stage string    `json:"stage"`
}

func (d Deps) listingsChanged(ctx context.Context, user uuid.UUID) {
	if d.ListingsChanged != nil {
		d.ListingsChanged(ctx, user)
	}
}

func undoWork(d Deps) func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
	return func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
		var u workUndo
		if err := json.Unmarshal(data, &u); err != nil {
			return "", ErrNoUndo
		}
		switch u.Kind {
		case "tracker":
			if d.Trackers == nil {
				return "", ErrNoUndo
			}
			if err := d.Trackers.WriteList(ctx, user, u.Type, u.Text); err != nil {
				return "", err
			}
			return "The " + u.Type + " list is back as it was.", nil
		case "listings":
			n := 0
			for _, it := range u.Items {
				if d.Listings.Move(ctx, user, it.ID, domain.StageClosed, it.Stage, "reopened by the owner", nil) == nil {
					n++
				}
			}
			if n == 0 {
				return "", errors.New("those listings cannot be reopened (they changed since)")
			}
			d.listingsChanged(ctx, user)
			return fmt.Sprintf("Reopened %d of %d listing(s).", n, len(u.Items)), nil
		}
		return "", ErrNoUndo
	}
}

// ---- trackers ----

func readTracker(d Deps) *Tool {
	return &Tool{
		Name: "read_tracker",
		Description: "Read one of the owner's tracker lists (applied, internships, hackathons, opensource, alumni, community) as rows. Every row has a row key; " +
			"use the keys with remove_tracker_rows or update_tracker_row. Long lists continue with read_more.",
		Effect: EffectRead,
		Schema: obj(map[string]any{"type": str("applied, internships, hackathons, opensource, alumni or community")}, "type"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ Type string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			t, err := trackerType(a.Type)
			if err != nil {
				return Output{}, err
			}
			text, ok, err := d.Trackers.ReadList(ctx, c.UserID, t)
			if err != nil {
				return Output{}, err
			}
			header, rows := domain.TrackerTable(text)
			if !ok || len(rows) == 0 {
				return Output{Text: "The " + t + " list has no rows."}, nil
			}
			var b strings.Builder
			var keys []string
			fmt.Fprintf(&b, "%s list, %d rows (row key | %s):\n", t, len(rows), strings.Join(header, " | "))
			for _, r := range rows {
				keys = append(keys, r.Key)
				fmt.Fprintf(&b, "%s | %s\n", r.Key, strings.Join(r.Cells, " | "))
			}
			return Output{Text: b.String(), IDs: keys, Untrusted: true}, nil
		},
	}
}

// internshipsToListings maps the rows of the Internships list back to the listings they were drawn from.
func internshipsToListings(ctx context.Context, d Deps, user uuid.UUID, header []string, rows []domain.TrackerRow) ([]*domain.Listing, error) {
	all, err := d.Listings.ListAssessed(ctx, user, domain.AssessedFilter{Limit: 500})
	if err != nil {
		return nil, err
	}
	col := func(name string) int {
		for i, h := range header {
			if strings.Contains(strings.ToLower(h), name) {
				return i
			}
		}
		return -1
	}
	co, ro, li := col("company"), col("role"), col("link")
	cell := func(r domain.TrackerRow, i int) string {
		if i < 0 || i >= len(r.Cells) {
			return ""
		}
		return strings.TrimSpace(r.Cells[i])
	}
	var out []*domain.Listing
	for _, r := range rows {
		for _, l := range all {
			if (cell(r, li) != "" && strings.Contains(cell(r, li), l.URL) && l.URL != "") ||
				(strings.EqualFold(cell(r, co), l.Company) && strings.EqualFold(cell(r, ro), l.Role)) {
				out = append(out, l)
				break
			}
		}
	}
	return out, nil
}

func removeTrackerRows(d Deps) *Tool {
	return &Tool{
		Name: "remove_tracker_rows",
		Description: "Remove rows from a tracker list by row key (keys come from read_tracker). Any number of rows at once. " +
			"For the internships list the matching listings are closed instead. The owner can undo.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"type": str("tracker type, as in read_tracker"), "row_keys": strs("row keys from read_tracker")}, "type", "row_keys"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				Type    string
				RowKeys []string `json:"row_keys"`
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			t, err := trackerType(a.Type)
			if err != nil {
				return Output{}, err
			}
			if len(a.RowKeys) == 0 {
				return Output{}, errors.New("row_keys is empty")
			}
			for _, k := range a.RowKeys {
				if err := c.RequireSeen(strings.TrimSpace(k)); err != nil {
					return Output{}, err
				}
			}
			text, ok, err := d.Trackers.ReadList(ctx, c.UserID, t)
			if err != nil || !ok {
				return Output{}, errors.New("that list could not be read")
			}
			header, rows := domain.TrackerTable(text)
			if t == "internships" {
				if d.Listings == nil {
					return Output{}, errors.New("internship rows can only be closed through the listings, which are not available")
				}
				picked := map[string]bool{}
				for _, k := range a.RowKeys {
					picked[strings.TrimSpace(k)] = true
				}
				var chosen []domain.TrackerRow
				for _, r := range rows {
					if picked[r.Key] {
						chosen = append(chosen, r)
					}
				}
				ls, err := internshipsToListings(ctx, d, c.UserID, header, chosen)
				if err != nil {
					return Output{}, err
				}
				return closeListingRecords(ctx, d, c.UserID, ls, len(chosen)), nil
			}
			out, n := domain.RemoveTrackerRows(text, a.RowKeys)
			if n == 0 {
				return Output{Text: "None of those row keys are in the list; nothing was removed. Read the list again."}, nil
			}
			if err := d.Trackers.WriteList(ctx, c.UserID, t, out); err != nil {
				return Output{}, err
			}
			undo, _ := json.Marshal(workUndo{Kind: "tracker", Type: t, Text: text})
			return Output{Text: fmt.Sprintf("Removed %d row(s) from the %s list.", n, t), Summary: fmt.Sprintf("Removed %d row(s) from the %s tracker", n, t), UndoData: undo}, nil
		},
		Undo: undoWork(d),
	}
}

func updateTrackerRow(d Deps) *Tool {
	return &Tool{
		Name:        "update_tracker_row",
		Description: "Change one cell of one tracker row: give the row key from read_tracker, the column name and the new value. The owner can undo. Not for the internships list (its rows come from listings: use close_listings).",
		Effect:      EffectInternal,
		Schema: obj(map[string]any{"type": str("tracker type, as in read_tracker"), "row_key": str("row key from read_tracker"),
			"column": str("column name, as in the list's header"), "value": str("the new cell text")}, "type", "row_key", "column", "value"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				Type   string
				RowKey string `json:"row_key"`
				Column string
				Value  string
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			t, err := trackerType(a.Type)
			if err != nil {
				return Output{}, err
			}
			if t == "internships" {
				return Output{Text: "The internships list is drawn from the listings, so a cell cannot be edited here. To drop a role use close_listings; tell the owner other edits are not possible yet."}, nil
			}
			if err := c.RequireSeen(strings.TrimSpace(a.RowKey)); err != nil {
				return Output{}, err
			}
			if len([]rune(a.Value)) > 500 {
				return Output{}, errors.New("value is too long")
			}
			text, ok, err := d.Trackers.ReadList(ctx, c.UserID, t)
			if err != nil || !ok {
				return Output{}, errors.New("that list could not be read")
			}
			out, newKey, err := domain.SetTrackerCell(text, strings.TrimSpace(a.RowKey), a.Column, a.Value)
			if err != nil {
				return Output{}, err
			}
			if err := d.Trackers.WriteList(ctx, c.UserID, t, out); err != nil {
				return Output{}, err
			}
			undo, _ := json.Marshal(workUndo{Kind: "tracker", Type: t, Text: text})
			return Output{Text: fmt.Sprintf("Set %s to %q on that row of the %s list (its new row key is %s).", a.Column, a.Value, t, newKey),
				Summary: fmt.Sprintf("Set %s to %q in the %s tracker", a.Column, truncate(a.Value, 80), t), UndoData: undo, IDs: []string{newKey}}, nil
		},
		Undo: undoWork(d),
	}
}

// ---- listings ----

func listListings(d Deps) *Tool {
	return &Tool{
		Name: "list_listings",
		Description: "List the owner's verified internship listings with ids: tier (A pays most), company, role, location, pay, stage and link. " +
			"Use it to answer what internships exist or why there is none, and before closing, checking or preparing listings. Closed listings are left out unless stage is 'closed'.",
		Effect: EffectRead,
		Schema: obj(map[string]any{"tier": str("optional pay tier: A, B or C"), "stage": str("optional stage: verified, drafted, approved, submitting, confirmed, needs_you or closed"),
			"query": str("optional words to find in the company, role or location"), "limit": num("at most this many (default 40)")}),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct {
				Tier, Stage, Query string
				Limit              int
			}
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			f := domain.AssessedFilter{Limit: 500}
			if t := strings.ToUpper(strings.TrimSpace(a.Tier)); t != "" {
				if t != "A" && t != "B" && t != "C" {
					return Output{}, errors.New("tier must be A, B or C")
				}
				f.Tiers = []string{t}
			}
			if s := strings.ToLower(strings.TrimSpace(a.Stage)); s != "" {
				f.Stages = []string{s}
			}
			if a.Limit <= 0 || a.Limit > maxListingsShown {
				a.Limit = 40
			}
			all, err := d.Listings.ListAssessed(ctx, c.UserID, f)
			if err != nil {
				return Output{}, err
			}
			words := strings.Fields(strings.ToLower(a.Query))
			var b strings.Builder
			var ids []string
			matched := 0
			for _, l := range all {
				if len(f.Stages) == 0 && l.Stage == domain.StageClosed {
					continue
				}
				hay := strings.ToLower(l.Company + " " + l.Role + " " + l.Location)
				fits := true
				for _, w := range words {
					fits = fits && strings.Contains(hay, w)
				}
				if !fits {
					continue
				}
				matched++
				if len(ids) < a.Limit {
					ids = append(ids, l.ID.String())
					fmt.Fprintf(&b, "%s | Tier %s | %s | %s | %s | %s | %s | %s\n", l.ID, l.Tier, truncate(l.Company, 60), truncate(l.Role, 80), truncate(l.Location, 40), truncate(l.Pay, 40), l.Stage, l.URL)
				}
			}
			if matched == 0 {
				return Output{Text: "No listings match. (The listings are read from InternDock and the companies' boards by the LISTINGS SYNC routine.)"}, nil
			}
			return Output{Text: fmt.Sprintf("%d listings match, showing %d (id | tier | company | role | location | pay | stage | link):\n%s", matched, len(ids), b.String()), IDs: ids, Untrusted: true}, nil
		},
	}
}

// closeListingRecords closes the listings that can be closed and builds the output and undo for it.
func closeListingRecords(ctx context.Context, d Deps, user uuid.UUID, ls []*domain.Listing, asked int) Output {
	var closed []string
	var reopen []stageItem
	var skipped []string
	for _, l := range ls {
		name := l.Company + " - " + l.Role
		switch {
		case l.Stage == domain.StageClosed:
			skipped = append(skipped, name+" (already closed)")
		case !domain.CanMove(l.Stage, domain.StageClosed):
			skipped = append(skipped, name+" ("+l.Stage+" listings cannot be closed)")
		default:
			prev := l.Stage
			if err := d.Listings.Move(ctx, user, l.ID, prev, domain.StageClosed, domain.ClosedByOwnerNote, nil); err != nil {
				skipped = append(skipped, name+" ("+err.Error()+")")
				continue
			}
			closed = append(closed, name)
			if domain.CanMove(domain.StageClosed, prev) {
				reopen = append(reopen, stageItem{ID: l.ID, Stage: prev})
			}
		}
	}
	if missing := asked - len(ls); missing > 0 && asked > 0 && len(ls) < asked {
		skipped = append(skipped, fmt.Sprintf("%d row(s) that match no listing", missing))
	}
	text := fmt.Sprintf("Closed %d. Skipped: %s.", len(closed), orNone(skipped))
	out := Output{Text: text}
	if len(closed) == 0 {
		return out
	}
	d.listingsChanged(ctx, user)
	out.Summary = fmt.Sprintf("Closed %d listing(s): %s", len(closed), truncate(strings.Join(closed, "; "), 300))
	if len(reopen) > 0 {
		out.UndoData, _ = json.Marshal(workUndo{Kind: "listings", Items: reopen})
	}
	if len(reopen) < len(closed) {
		out.Text += fmt.Sprintf(" %d of them cannot be reopened by undo.", len(closed)-len(reopen))
	}
	return out
}

func closeListings(d Deps) *Tool {
	return &Tool{
		Name:        "close_listings",
		Description: "Close internship listings the owner does not want, by listing id from list_listings. Any number at once. A listing that is only verified can be reopened by undo; one already past that cannot.",
		Effect:      EffectInternal,
		Schema:      obj(map[string]any{"ids": strs("listing ids from list_listings")}, "ids"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ IDs []string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if len(a.IDs) == 0 {
				return Output{}, errors.New("ids is empty")
			}
			var ls []*domain.Listing
			for _, s := range a.IDs {
				id, err := c.ParseSeenID(s)
				if err != nil {
					return Output{}, err
				}
				l, err := d.Listings.Get(ctx, c.UserID, id)
				if err != nil {
					return Output{}, fmt.Errorf("no such listing %s", s)
				}
				ls = append(ls, l)
			}
			return closeListingRecords(ctx, d, c.UserID, ls, len(ls)), nil
		},
		Undo: undoWork(d),
	}
}

func checkListingLinks(d Deps) *Tool {
	return &Tool{
		Name: "check_listing_links",
		Description: "Check whether listings' links still show the internship. Up to 10 listings are checked right now and the result (open, missing or unknown) is returned; " +
			"more than 10 queue the listings sync, which rechecks every link. Ids come from list_listings. It closes nothing: use close_listings for the missing ones.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"ids": strs("listing ids from list_listings")}, "ids"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ IDs []string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if len(a.IDs) == 0 {
				return Output{}, errors.New("ids is empty")
			}
			var ids []uuid.UUID
			for _, s := range a.IDs {
				id, err := c.ParseSeenID(s)
				if err != nil {
					return Output{}, err
				}
				ids = append(ids, id)
			}
			if len(ids) > maxInlineLinkChecks {
				if d.Tasks == nil {
					return Output{}, fmt.Errorf("at most %d links can be checked at once", maxInlineLinkChecks)
				}
				key := fmt.Sprintf("agentrecheck:%s:%d", c.UserID, c.Now.Unix()/60)
				task, err := d.Tasks.CreateTask(ctx, &domain.Task{UserID: c.UserID, ManagerDomain: "career", Title: domain.ListingsSyncTitle + " (recheck asked in chat)",
					Goal: "Refresh the verified listings and recheck every link, asked by the owner in chat.", Priority: domain.TaskPriorityNormalP2, IdempotencyKey: &key}, nil)
				if errors.Is(err, domain.ErrIdempotentConflict) {
					return Output{Text: "The recheck was already queued a moment ago."}, nil
				}
				if err != nil {
					return Output{}, fmt.Errorf("could not queue it: %w", err)
				}
				data, _ := json.Marshal(map[string]string{"id": task.ID.String()})
				return Output{Text: fmt.Sprintf("That is more than %d links, so it was queued as the listings sync (it rechecks every link). The result will show in the listings.", maxInlineLinkChecks),
					Summary: fmt.Sprintf("Queued a recheck of listing links (%d asked)", len(ids)), UndoData: data, Wake: true}, nil
			}
			if d.CheckLink == nil {
				return Output{}, errors.New("link checking is not available")
			}
			listings := make([]*domain.Listing, len(ids))
			for i, id := range ids {
				l, err := d.Listings.Get(ctx, c.UserID, id)
				if err != nil {
					return Output{}, fmt.Errorf("no such listing %s", id)
				}
				listings[i] = l
			}
			states := make([]service.PostingState, len(listings))
			var wg sync.WaitGroup
			for i, l := range listings {
				wg.Add(1)
				go func() {
					defer wg.Done()
					states[i] = d.CheckLink(ctx, l.URL)
				}()
			}
			wg.Wait()
			var b strings.Builder
			for i, l := range listings {
				fmt.Fprintf(&b, "%s | %s - %s | %s\n", l.ID, truncate(l.Company, 60), truncate(l.Role, 80), states[i])
			}
			return Output{Text: "Link check (id | listing | result; open = the internship is on the page, missing = it is not, unknown = needs a browser):\n" + b.String()}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ ID uuid.UUID }
			if err := json.Unmarshal(data, &u); err != nil {
				return "", ErrNoUndo
			}
			if err := d.Tasks.CancelTask(ctx, u.ID, "recheck undone by the owner"); err != nil {
				return "", err
			}
			return "The recheck was cancelled.", nil
		},
	}
}

func prepareApplications(d Deps) *Tool {
	return &Tool{
		Name: "prepare_applications",
		Description: "Prepare application packages (resume choice and cover letter) for listings the owner wants to apply to, by listing id from list_listings. " +
			"It queues the preparation; the drafts then wait on the owner's Approvals page. Nothing is submitted: sending stays behind the owner's approval.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{"ids": strs("listing ids from list_listings")}, "ids"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a struct{ IDs []string }
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			if len(a.IDs) == 0 || len(a.IDs) > 10 {
				return Output{}, errors.New("give between 1 and 10 listing ids")
			}
			var ok []uuid.UUID
			var names, skipped []string
			for _, s := range a.IDs {
				id, err := c.ParseSeenID(s)
				if err != nil {
					return Output{}, err
				}
				l, err := d.Listings.Get(ctx, c.UserID, id)
				if err != nil {
					return Output{}, fmt.Errorf("no such listing %s", s)
				}
				if l.Stage != domain.StageVerified && l.Stage != domain.StageDrafted && l.Stage != domain.StageNeedsYou {
					skipped = append(skipped, l.Company+" ("+l.Stage+")")
					continue
				}
				ok = append(ok, id)
				names = append(names, l.Company+" - "+l.Role)
			}
			if len(ok) == 0 {
				return Output{Text: "Nothing queued. Skipped: " + orNone(skipped) + "."}, nil
			}
			payload, _ := json.Marshal(map[string]any{service.PrepListingsKey: ok})
			key := fmt.Sprintf("agentprep:%s:%d", c.UserID, c.Now.Unix()/60)
			task, err := d.Tasks.CreateTask(ctx, &domain.Task{UserID: c.UserID, ManagerDomain: "career", Title: domain.ApplicationPrepTitle + " (asked in chat)",
				Goal: "Prepare application packages for the listings the owner chose.", Priority: domain.TaskPriorityNormalP2, InputPayload: payload, IdempotencyKey: &key}, nil)
			if errors.Is(err, domain.ErrIdempotentConflict) {
				return Output{Text: "That was already queued a moment ago."}, nil
			}
			if err != nil {
				return Output{}, fmt.Errorf("could not queue it: %w", err)
			}
			data, _ := json.Marshal(map[string]string{"id": task.ID.String()})
			return Output{Text: fmt.Sprintf("Queued preparation for %d listing(s): %s. Skipped: %s. The drafts will appear on the Approvals page; nothing is sent.", len(ok), strings.Join(names, "; "), orNone(skipped)),
				Summary: fmt.Sprintf("Queued application preparation for %d listing(s)", len(ok)), UndoData: data, Wake: true}, nil
		},
		Undo: func(ctx context.Context, user uuid.UUID, data json.RawMessage) (string, error) {
			var u struct{ ID uuid.UUID }
			if err := json.Unmarshal(data, &u); err != nil {
				return "", ErrNoUndo
			}
			if err := d.Tasks.CancelTask(ctx, u.ID, "preparation undone by the owner"); err != nil {
				return "", err
			}
			return "The preparation was cancelled.", nil
		},
	}
}

// ---- calls, laptop, vault ----

type callPayload struct {
	Contact string `json:"contact"`
	Message string `json:"message,omitempty"`
	Kind    string `json:"kind"`
}

func placeCall(d Deps) *Tool {
	return &Tool{
		Name: "place_call",
		Description: "This is the owner's call feature: use it whenever the owner wants a call made, in any wording ('call him', 'do a sim call', 'use the call feature I built', 'ring mitul', 'WhatsApp call'). Call a contact from the owner's phone. kind: 'sim' is a normal phone call over the SIM that the owner then talks on; 'whatsapp' is a WhatsApp call the owner talks on; " +
			"'spoken' is an automated call where JJ speaks the message. 'contact' is the person's name as the owner said it (never a pronoun like him or her: if the owner did not name anyone, ask who). " +
			"The phone still asks the owner before it dials.",
		Effect: EffectExternal,
		Schema: obj(map[string]any{"contact": str("the contact's name or number"), "kind": str("sim, whatsapp or spoken"),
			"message": str("what to say; only for kind spoken")}),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a callPayload
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			a.Contact, a.Message, a.Kind = strings.TrimSpace(a.Contact), strings.TrimSpace(a.Message), strings.ToLower(strings.TrimSpace(a.Kind))
			if !service.ValidCallContact(a.Contact) {
				return nil, "", errors.New("no contact was named, so nothing was done. Ask the owner who to call")
			}
			if a.Kind == "" {
				a.Kind = "sim"
			}
			switch a.Kind {
			case "sim", "whatsapp", "spoken":
			default:
				return nil, "", errors.New("kind must be sim, whatsapp or spoken")
			}
			if a.Kind != "spoken" {
				a.Message = ""
			}
			b, _ := json.Marshal(a)
			summary := fmt.Sprintf("Call %s (%s)", a.Contact, a.Kind)
			if a.Message != "" {
				summary += ", saying: " + truncate(a.Message, 150)
			}
			return b, summary, nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			var a callPayload
			if err := json.Unmarshal(payload, &a); err != nil {
				return "", err
			}
			channel, mode := service.CallChannelCellular, service.CallModeDirectTalk
			switch a.Kind {
			case "whatsapp":
				channel = service.CallChannelWhatsApp
			case "spoken":
				mode = service.CallModeAutomatedBg
				if a.Message == "" {
					a.Message = fmt.Sprintf("Hello %s, this is an automated message from Griffin, an assistant calling on behalf of its owner.", a.Contact)
				}
			}
			plan := service.BuildCallTask(a.Contact, a.Message, channel, mode, false, domain.TaskPriorityHighP1)
			key := fmt.Sprintf("agentcall:%s:%x", user, hashOf(a.Kind+"|"+strings.ToLower(a.Contact)+"|"+a.Message))
			_, err := d.Tasks.CreateTask(ctx, &domain.Task{UserID: user, ManagerDomain: plan.ManagerDomain, Title: plan.Title, Goal: plan.Goal,
				Priority: plan.Priority, InputPayload: plan.InputPayload, IdempotencyKey: &key}, nil)
			if errors.Is(err, domain.ErrIdempotentConflict) {
				return fmt.Sprintf("The call to %s was already queued; the phone will ask before it dials.", a.Contact), nil
			}
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Queued a %s call to %s. The phone will ask you before it dials.", a.Kind, a.Contact), nil
		},
	}
}

func hashOf(s string) uint32 {
	var h uint32 = 2166136261
	for _, b := range []byte(s) {
		h = (h ^ uint32(b)) * 16777619
	}
	return h
}

type laptopPayload struct {
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
}

func laptop(d Deps) *Tool {
	return &Tool{
		Name: "laptop",
		Description: "Do something on the owner's laptop. action: play, pause, next, previous (media); open_url (target is an http(s) address); open_app (target is the app name); " +
			"screenshot (target screen or phone). For 'play', a target (a song, video or playlist name) opens a YouTube search for it and then plays; without a target it just resumes the current player.",
		Effect: EffectExternal,
		Schema: obj(map[string]any{"action": str("play, pause, next, previous, open_url, open_app or screenshot"), "target": str("what to play, the address, the app, or screen/phone")}, "action"),
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) {
			var a laptopPayload
			if err := decode(raw, &a); err != nil {
				return nil, "", err
			}
			a.Action, a.Target = strings.ToLower(strings.TrimSpace(a.Action)), strings.TrimSpace(a.Target)
			var summary string
			switch a.Action {
			case "play":
				summary = "Resume playing on your laptop"
				if a.Target != "" {
					summary = "Play " + truncate(a.Target, 100) + " on your laptop (YouTube)"
				}
			case "pause", "next", "previous":
				summary = strings.ToUpper(a.Action[:1]) + a.Action[1:] + " the media on your laptop"
			case "open_url":
				if _, err := domain.ParseDesktopAction(desktopPayload(domain.DesktopAction{Op: domain.DesktopOpOpenURL, URL: a.Target})); err != nil {
					return nil, "", errors.New("target must be an http or https address")
				}
				summary = "Open " + truncate(a.Target, 150) + " on your laptop"
			case "open_app":
				if a.Target == "" {
					return nil, "", errors.New("target must name the app")
				}
				summary = "Open " + truncate(a.Target, 80) + " on your laptop"
			case "screenshot":
				if a.Target == "" {
					a.Target = "screen"
				}
				if a.Target != "screen" && a.Target != "phone" {
					return nil, "", errors.New("target must be screen or phone")
				}
				summary = "Take a " + a.Target + " screenshot"
			default:
				return nil, "", errors.New("action must be play, pause, next, previous, open_url, open_app or screenshot")
			}
			b, _ := json.Marshal(a)
			return b, summary, nil
		},
		Exec: func(ctx context.Context, user uuid.UUID, payload json.RawMessage) (string, error) {
			var a laptopPayload
			if err := json.Unmarshal(payload, &a); err != nil {
				return "", err
			}
			make1 := func(title string, act domain.DesktopAction, deps []uuid.UUID) (*domain.Task, error) {
				return d.Tasks.CreateTask(ctx, &domain.Task{UserID: user, ManagerDomain: "personal_ops", Title: title, Goal: title,
					Priority: domain.TaskPriorityNormalP2, InputPayload: desktopPayload(act)}, deps)
			}
			switch a.Action {
			case "play":
				if a.Target == "" {
					_, err := make1("Resume media on the laptop", domain.DesktopAction{Op: domain.DesktopOpMedia, MediaOp: "play"}, nil)
					return "Asked the laptop to resume playing.", err
				}
				open, err := make1("Open YouTube search: "+truncate(a.Target, 60), domain.DesktopAction{Op: domain.DesktopOpOpenURL, URL: "https://www.youtube.com/results?search_query=" + url.QueryEscape(a.Target)}, nil)
				if err != nil {
					return "", err
				}
				if _, err := make1("Play media on the laptop", domain.DesktopAction{Op: domain.DesktopOpMedia, MediaOp: "play"}, []uuid.UUID{open.ID}); err != nil {
					return "", err
				}
				return "Asked the laptop to open a YouTube search for " + truncate(a.Target, 60) + " and then play.", nil
			case "pause", "next", "previous":
				_, err := make1(strings.ToUpper(a.Action[:1])+a.Action[1:]+" media on the laptop", domain.DesktopAction{Op: domain.DesktopOpMedia, MediaOp: a.Action}, nil)
				return "Asked the laptop to " + a.Action + ".", err
			case "open_url":
				_, err := make1("Open "+truncate(a.Target, 80)+" on the laptop", domain.DesktopAction{Op: domain.DesktopOpOpenURL, URL: a.Target}, nil)
				return "Asked the laptop to open " + truncate(a.Target, 80) + ".", err
			case "open_app":
				_, err := make1("Open "+truncate(a.Target, 60)+" on the laptop", domain.DesktopAction{Op: domain.DesktopOpLaunch, App: a.Target}, nil)
				return "Asked the laptop to open " + truncate(a.Target, 60) + ".", err
			case "screenshot":
				_, err := make1("Capture "+a.Target+" screenshot", domain.DesktopAction{Op: domain.DesktopOpScreenshot, Target: a.Target}, nil)
				return "Asked the laptop for a " + a.Target + " screenshot.", err
			}
			return "", errors.New("unknown laptop action")
		},
	}
}

func desktopPayload(a domain.DesktopAction) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"requires": []string{domain.HostCapDesktop}, domain.DesktopKey: a})
	return b
}

func listVault(d Deps) *Tool {
	return &Tool{
		Name:        "list_vault",
		Description: "List the names and kinds of the documents in the owner's vault (id cards, finance papers, resumes, certificates). It shows names only, never the contents.",
		Effect:      EffectRead,
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			arts, err := d.Vault.ListByKind(ctx, c.UserID, domain.VaultKind)
			if err != nil {
				return Output{}, err
			}
			var b strings.Builder
			n := 0
			for _, a := range arts {
				if m, ok := domain.ParseVaultMeta(a.Metadata); ok {
					n++
					fmt.Fprintf(&b, "%s | %s\n", truncate(m.Label, 80), m.Category)
				}
			}
			if n == 0 {
				return Output{Text: "The vault is empty."}, nil
			}
			return Output{Text: fmt.Sprintf("%d documents (name | kind):\n%s", n, b.String())}, nil
		},
	}
}
