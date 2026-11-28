package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/service"
)

type fakeListings struct {
	all   []*domain.Listing
	moves []string
}

func (f *fakeListings) ListAssessed(_ context.Context, _ uuid.UUID, fl domain.AssessedFilter) ([]*domain.Listing, error) {
	var out []*domain.Listing
	for _, l := range f.all {
		if len(fl.Tiers) > 0 && l.Tier != fl.Tiers[0] {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}
func (f *fakeListings) Get(_ context.Context, _, id uuid.UUID) (*domain.Listing, error) {
	for _, l := range f.all {
		if l.ID == id {
			return l, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeListings) Move(_ context.Context, _, id uuid.UUID, from, to, _ string, _ *uuid.UUID) error {
	for _, l := range f.all {
		if l.ID == id {
			f.moves = append(f.moves, from+">"+to)
			l.Stage = to
		}
	}
	return nil
}

type fakeTrackers struct{ lists map[string]string }

func (f *fakeTrackers) ReadList(_ context.Context, _ uuid.UUID, t string) (string, bool, error) {
	s, ok := f.lists[t]
	return s, ok, nil
}
func (f *fakeTrackers) WriteList(_ context.Context, _ uuid.UUID, t, text string) error {
	f.lists[t] = text
	return nil
}

func replies(rs ...gemini.ToolReply) []gemini.ToolReply { return rs }

const hackList = "| Name | Mode | Status |\n|---|---|---|\n| AI Hack | Online | open |\n| BuildFest | Mumbai | open |\n| Zero | Online | open |"

func workEnv(t *testing.T) (*env, *fakeListings, *fakeTrackers, *int) {
	t.Helper()
	e := newEnv(t)
	ls := &fakeListings{}
	for _, c := range []struct{ co, pay string }{{"Acme", "₹ 50,000 /month"}, {"Globex", "₹ 20,000 /month"}, {"Umbrella", "Unpaid"}} {
		ls.all = append(ls.all, &domain.Listing{ID: uuid.New(), Company: c.co, Role: "Intern", Pay: c.pay, Tier: domain.TierForPay(c.pay), Stage: domain.StageVerified, URL: "https://x.example/" + c.co})
	}
	tr := &fakeTrackers{lists: map[string]string{"hackathons": hackList}}
	checks := 0
	tools := NewTools(Deps{Tasks: e.tasks, Store: e.store, Listings: ls, Trackers: tr,
		CheckLink: func(context.Context, string) service.PostingState { checks++; return service.PostingMissing }})
	reg, err := NewRegistry(tools...)
	if err != nil {
		t.Fatal(err)
	}
	e.agent.Registry = reg
	return e, ls, tr, &checks
}

func TestTrackerRowsRemoveAndUndo(t *testing.T) {
	e, _, tr, _ := workEnv(t)
	_, rows := domain.TrackerTable(hackList)
	e.model.replies = replies(
		call("read_tracker", map[string]any{"type": "hackathons"}),
		call("remove_tracker_rows", map[string]any{"type": "hackathons", "row_keys": []any{rows[1].Key}}),
		say("Removed BuildFest."))
	res, err := e.run("drop the Mumbai hackathon")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tr.lists["hackathons"], "BuildFest") || !strings.Contains(tr.lists["hackathons"], "AI Hack") {
		t.Fatalf("list = %q", tr.lists["hackathons"])
	}
	if _, err := e.agent.Undo(context.Background(), e.user, res.Actions[0].UndoID); err != nil || tr.lists["hackathons"] != hackList {
		t.Fatalf("undo: %v %q", err, tr.lists["hackathons"])
	}
}

func TestTrackerRowKeyMustBeSeenFirst(t *testing.T) {
	e, _, tr, _ := workEnv(t)
	_, rows := domain.TrackerTable(hackList)
	e.model.replies = replies(call("remove_tracker_rows", map[string]any{"type": "hackathons", "row_keys": []any{rows[0].Key}}), say("I need to read the list first."))
	if _, err := e.run("remove the first hackathon"); err != nil {
		t.Fatal(err)
	}
	if tr.lists["hackathons"] != hackList {
		t.Fatal("an unseen row key must not edit the list")
	}
}

func TestUpdateTrackerRow(t *testing.T) {
	e, _, tr, _ := workEnv(t)
	_, rows := domain.TrackerTable(hackList)
	e.model.replies = replies(
		call("read_tracker", map[string]any{"type": "Hackathons"}),
		call("update_tracker_row", map[string]any{"type": "hackathons", "row_key": rows[0].Key, "column": "Status", "value": "applied"}),
		say("Marked it applied."))
	if _, err := e.run("mark AI Hack as applied"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tr.lists["hackathons"], "| AI Hack | Online | applied |") {
		t.Fatalf("list = %q", tr.lists["hackathons"])
	}
}

func TestInternshipRowsCloseListings(t *testing.T) {
	e, ls, tr, _ := workEnv(t)
	tr.lists["internships"] = "| Tier | Company | Role | Link | Status |\n|---|---|---|---|---|\n| Tier C | Umbrella | Intern | https://x.example/Umbrella | open |"
	_, rows := domain.TrackerTable(tr.lists["internships"])
	e.model.replies = replies(
		call("read_tracker", map[string]any{"type": "internships"}),
		call("remove_tracker_rows", map[string]any{"type": "internships", "row_keys": []any{rows[0].Key}}),
		say("Closed Umbrella."))
	res, err := e.run("remove Umbrella from internships")
	if err != nil {
		t.Fatal(err)
	}
	if ls.all[2].Stage != domain.StageClosed || strings.Contains(strings.Join(ls.moves, ","), "Acme") {
		t.Fatalf("stages: %v", ls.moves)
	}
	if res.Actions[0].UndoID == "" {
		t.Fatal("a verified listing closed from chat should be undoable")
	}
	if _, err := e.agent.Undo(context.Background(), e.user, res.Actions[0].UndoID); err != nil || ls.all[2].Stage != domain.StageVerified {
		t.Fatalf("undo: %v stage %s", err, ls.all[2].Stage)
	}
}

func TestListAndCloseListingsByTier(t *testing.T) {
	e, ls, _, _ := workEnv(t)
	e.model.replies = replies(
		call("list_listings", map[string]any{"tier": "C"}),
		call("close_listings", map[string]any{"ids": []any{ls.all[2].ID.String()}}),
		say("Closed the Tier C listing."))
	res, err := e.run("close the unpaid ones")
	if err != nil || ls.all[2].Stage != domain.StageClosed || ls.all[0].Stage == domain.StageClosed {
		t.Fatalf("err %v moves %v", err, ls.moves)
	}
	if res.Actions[0].UndoID == "" {
		t.Fatal("expected undo")
	}
}

func TestCloseListingsRefusesUnseenAndConfirmed(t *testing.T) {
	e, ls, _, _ := workEnv(t)
	e.model.replies = replies(call("close_listings", map[string]any{"ids": []any{ls.all[0].ID.String()}}), say("List them first."))
	if _, err := e.run("close Acme"); err != nil || len(ls.moves) != 0 {
		t.Fatalf("an unseen id must not close anything: %v %v", err, ls.moves)
	}
	ls.all[1].Stage = domain.StageConfirmed
	e2, ls2, _, _ := workEnv(t)
	ls2.all[1].Stage = domain.StageConfirmed
	e2.model.replies = replies(call("list_listings", map[string]any{"stage": "confirmed"}), call("close_listings", map[string]any{"ids": []any{ls2.all[1].ID.String()}}), say("It could not be closed."))
	res, err := e2.run("close Globex")
	if err != nil || ls2.all[1].Stage != domain.StageConfirmed || len(res.Actions) != 0 {
		t.Fatalf("a confirmed (applied) listing must stay: %v %v", err, res.Actions)
	}
}

func TestCheckListingLinksInlineAndQueuedPastTen(t *testing.T) {
	e, ls, _, checks := workEnv(t)
	e.model.replies = replies(call("list_listings", nil), call("check_listing_links", map[string]any{"ids": []any{ls.all[0].ID.String(), ls.all[1].ID.String()}}), say("Both links are missing."))
	if _, err := e.run("are these still up"); err != nil || *checks != 2 || len(e.tasks.created) != 0 {
		t.Fatalf("err %v checks %d created %d", err, *checks, len(e.tasks.created))
	}

	e2, ls2, _, checks2 := workEnv(t)
	var ids []any
	for i := 0; i < 11; i++ {
		l := &domain.Listing{ID: uuid.New(), Company: "Co", Role: "R", Stage: domain.StageVerified}
		ls2.all = append(ls2.all, l)
		ids = append(ids, l.ID.String())
	}
	e2.model.replies = replies(call("list_listings", map[string]any{"limit": 100}), call("check_listing_links", map[string]any{"ids": ids}), say("Queued the recheck."))
	if _, err := e2.run("check every link"); err != nil {
		t.Fatal(err)
	}
	if *checks2 != 0 || len(e2.tasks.created) != 1 || !domain.IsApplicationPrep("x") && !strings.Contains(e2.tasks.created[0].Title, domain.ListingsSyncTitle) {
		t.Fatalf("checks %d created %+v", *checks2, e2.tasks.created)
	}
}

func TestPrepareApplicationsQueuesOnlyEligibleListings(t *testing.T) {
	e, ls, _, _ := workEnv(t)
	ls.all[1].Stage = domain.StageConfirmed
	e.model.replies = replies(
		call("list_listings", nil),
		call("prepare_applications", map[string]any{"ids": []any{ls.all[0].ID.String(), ls.all[1].ID.String()}}),
		say("Queued the application for Acme."))
	if _, err := e.run("prepare applications for Acme and Globex"); err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.created) != 1 {
		t.Fatalf("created %d", len(e.tasks.created))
	}
	task := e.tasks.created[0]
	got := service.PrepListingIDs(task.InputPayload)
	if !domain.IsApplicationPrep(task.Title) || len(got) != 1 || got[0] != ls.all[0].ID {
		t.Fatalf("task %q ids %v", task.Title, got)
	}
}

func TestCancelTasksByStatusNeedsNoListing(t *testing.T) {
	e := newEnv(t, call("cancel_tasks", map[string]any{"status": "active"}), say("Cancelled."))
	e.tasks.tasks = []*domain.Task{mkTask(e.user, "RESEARCH: x", domain.TaskStatusRunning), mkTask(e.user, "APPLY: y", domain.TaskStatusQueued), mkTask(e.user, "Done", domain.TaskStatusCompleted)}
	res, err := e.run("stop everything running")
	if err != nil || len(e.tasks.cancelled) != 2 || res.Actions[0].UndoID == "" {
		t.Fatalf("err %v cancelled %d", err, len(e.tasks.cancelled))
	}
	e2 := newEnv(t, call("cancel_tasks", map[string]any{"status": "running", "title_contains": "research"}), say("ok"))
	e2.tasks.tasks = []*domain.Task{mkTask(e2.user, "RESEARCH: x", domain.TaskStatusRunning), mkTask(e2.user, "APPLY: y", domain.TaskStatusRunning)}
	if _, err := e2.run("stop research"); err != nil || len(e2.tasks.cancelled) != 1 {
		t.Fatalf("err %v cancelled %d", err, len(e2.tasks.cancelled))
	}
}

func TestPlaceCallOwnerChatQueuesTheSameTaskShapeAndScheduledOnlyAsks(t *testing.T) {
	e, _, _, _ := workEnv(t)
	e.model.replies = replies(call("place_call", map[string]any{"contact": "Hiten", "kind": "sim"}), say("Calling Hiten."))
	if _, err := e.run("call hiten"); err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.created) != 1 {
		t.Fatalf("created %d", len(e.tasks.created))
	}
	p := string(e.tasks.created[0].InputPayload)
	for _, want := range []string{`"requires":["calls"]`, `"action":"direct_call"`, `"number":"Hiten"`, `"channel":"cellular"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("payload %s lacks %s", p, want)
		}
	}

	s, _, _, _ := workEnv(t)
	s.model.replies = replies(call("place_call", map[string]any{"contact": "Hiten", "kind": "spoken", "message": "I will be late"}), say("Waiting for approval."))
	if _, err := s.runAs("call hiten and say I will be late", OriginScheduled); err != nil {
		t.Fatal(err)
	}
	if len(s.tasks.created) != 0 || len(s.apps.holdKind) != 1 || s.apps.holdKind[0] != "place_call" {
		t.Fatalf("scheduled call must only create an approval: created %d holds %v", len(s.tasks.created), s.apps.holdKind)
	}
}

func TestPlaceCallRefusesAPronounAndNeverGuessesAContact(t *testing.T) {
	e, _, _, _ := workEnv(t)
	e.model.replies = replies(call("place_call", map[string]any{"contact": "him", "kind": "sim"}), say("Who should I call?"))
	if _, err := e.run("call him instead"); err != nil || len(e.tasks.created) != 0 {
		t.Fatalf("err %v created %d", err, len(e.tasks.created))
	}
}

func TestLaptopActions(t *testing.T) {
	e, _, _, _ := workEnv(t)
	e.model.replies = replies(call("laptop", map[string]any{"action": "play", "target": "lofi beats"}), say("Playing."))
	if _, err := e.run("play lofi beats"); err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.created) != 2 {
		t.Fatalf("play X is an open_url then a play, got %d tasks", len(e.tasks.created))
	}
	open, err := domain.ParseDesktopAction(e.tasks.created[0].InputPayload)
	if err != nil || open.Op != domain.DesktopOpOpenURL || !strings.HasPrefix(open.URL, "https://www.youtube.com/results?search_query=lofi") {
		t.Fatalf("first task %+v %v", open, err)
	}
	if got := domain.RequiredCapabilities(e.tasks.created[1].InputPayload); len(got) != 1 || got[0] != "desktop" {
		t.Fatalf("requires %v", got)
	}

	b, _, _, _ := workEnv(t)
	b.model.replies = replies(call("laptop", map[string]any{"action": "open_url", "target": "file:///etc/passwd"}), say("I cannot open that."))
	if _, err := b.run("open that file"); err != nil || len(b.tasks.created) != 0 {
		t.Fatalf("a non-http address must not run: %v %d", err, len(b.tasks.created))
	}
}

type fakeVault struct{}

func (fakeVault) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	b, _ := json.Marshal(domain.VaultMeta{Kind: domain.VaultKind, Label: "PAN card", Category: domain.VaultID})
	name := "pan-secret-filename.pdf"
	return []*domain.Artifact{{ID: uuid.New(), Metadata: b, Filename: &name}}, nil
}

func TestListVaultShowsNamesOnly(t *testing.T) {
	e := newEnv(t, call("list_vault", nil), say("Your vault holds a PAN card."))
	tools := NewTools(Deps{Tasks: e.tasks, Store: e.store, Vault: fakeVault{}})
	e.agent.Registry, _ = NewRegistry(tools...)
	if _, err := e.run("what is in my vault"); err != nil {
		t.Fatal(err)
	}
	out := responseOf(t, e.model, 1, 0)["result"].(string)
	if !strings.Contains(out, "PAN card | id") || strings.Contains(out, "secret") {
		t.Fatalf("vault output = %q", out)
	}
}

func TestLastRoundOffersNoToolsSoTheOwnerGetsAnAnswer(t *testing.T) {
	e := newEnv(t, call("list_tasks", nil), say("Nothing is running right now."))
	e.agent.MaxRounds = 1
	var sawDecls []int
	e.agent.Model = declCounter{e.model, &sawDecls}
	res, err := e.run("what is running")
	if err != nil || res.Reply == "" {
		t.Fatalf("err %v result %+v", err, res)
	}
	if len(sawDecls) != 2 || sawDecls[0] == 0 || sawDecls[1] != 0 {
		t.Fatalf("tool declarations per round = %v, the final round must offer none", sawDecls)
	}
}

type declCounter struct {
	Model
	seen *[]int
}

func (d declCounter) Generate(ctx context.Context, system string, c []gemini.Content, decls []gemini.FunctionDecl) (gemini.ToolReply, error) {
	*d.seen = append(*d.seen, len(decls))
	return d.Model.Generate(ctx, system, c, decls)
}

func TestPlaceCallWithoutContactAsks(t *testing.T) {
	e, _, _, _ := workEnv(t)
	e.model.replies = replies(call("place_call", map[string]any{"kind": "sim"}), say("Who should I call?"))
	if _, err := e.run("do a normal sim call instead"); err != nil || len(e.tasks.created) != 0 {
		t.Fatalf("err %v created %d", err, len(e.tasks.created))
	}
}
