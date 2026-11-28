package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// ---- fakes ----

type fakePeople struct {
	list    []*domain.Person
	drafted []uuid.UUID
}

func (f *fakePeople) List(context.Context, uuid.UUID, repository.PersonFilter) ([]*domain.Person, error) {
	return f.list, nil
}
func (f *fakePeople) Get(_ context.Context, _, id uuid.UUID) (*domain.Person, error) {
	for _, p := range f.list {
		if p.ID == id {
			cp := *p
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakePeople) Update(_ context.Context, _, id uuid.UUID, s *domain.ContactState, dnc *bool) (*domain.Person, error) {
	for _, p := range f.list {
		if p.ID == id {
			if s != nil {
				p.ContactState = *s
			}
			if dnc != nil {
				p.DoNotContact = *dnc
			}
			return p, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakePeople) MarkDrafted(_ context.Context, _, id uuid.UUID) (bool, error) {
	f.drafted = append(f.drafted, id)
	return true, nil
}
func (f *fakePeople) ApplyVerification(_ context.Context, _, id uuid.UUID, _ *domain.VerifiedFields) (*domain.Person, error) {
	return f.Get(context.Background(), uuid.Nil, id)
}

type fakeHolder struct{ held []string }

func (f *fakeHolder) HoldAlumniOutreach(_ context.Context, u uuid.UUID, name string, payload json.RawMessage) (*domain.Approval, error) {
	f.held = append(f.held, name+" "+string(payload))
	return &domain.Approval{ID: uuid.New(), UserID: u}, nil
}

type fakeOutreach struct {
	OutreachAPI
	camp    *domain.OutreachCampaign
	targets []*domain.OutreachTarget
	updates []repository.TargetUpdate
}

func (f *fakeOutreach) GetCampaign(context.Context, uuid.UUID, uuid.UUID) (*domain.OutreachCampaign, error) {
	return f.camp, nil
}
func (f *fakeOutreach) UpdateCampaign(_ context.Context, _, _ uuid.UUID, st *string, _ *domain.OutreachLimits) (*domain.OutreachCampaign, error) {
	if st != nil {
		f.camp.Status = *st
	}
	return f.camp, nil
}
func (f *fakeOutreach) CampaignOf(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return f.camp.ID, nil
}
func (f *fakeOutreach) ListTargets(context.Context, uuid.UUID, uuid.UUID, *domain.OutreachStatus) ([]*domain.OutreachTarget, error) {
	return f.targets, nil
}
func (f *fakeOutreach) UpdateTarget(_ context.Context, _, id uuid.UUID, u repository.TargetUpdate) (*domain.OutreachTarget, error) {
	f.updates = append(f.updates, u)
	for _, t := range f.targets {
		if t.ID == id {
			if u.To != nil {
				t.Status = *u.To
			}
			if u.Draft != nil {
				t.Draft = *u.Draft
			}
			return t, nil
		}
	}
	return nil, domain.ErrNotFound
}

type fakeLibrary struct {
	text    map[uuid.UUID]string
	deleted []uuid.UUID
}

func (f *fakeLibrary) ListArtifacts(context.Context, uuid.UUID, *uuid.UUID) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	for id := range f.text {
		out = append(out, &domain.Artifact{ID: id, Title: "Notes", ArtifactType: "MARKDOWN"})
	}
	return out, nil
}
func (f *fakeLibrary) ReadText(_ context.Context, _, id uuid.UUID, _ int64) (string, error) {
	return f.text[id], nil
}
func (f *fakeLibrary) UpdateText(_ context.Context, id, _ uuid.UUID, text string, app bool, _ int64) (*domain.Artifact, error) {
	if app {
		text = f.text[id] + text
	}
	f.text[id] = text
	return &domain.Artifact{ID: id, Title: "Notes"}, nil
}
func (f *fakeLibrary) CreateList(_ context.Context, _ uuid.UUID, title, content string) (*domain.Artifact, error) {
	id := uuid.New()
	f.text[id] = content
	return &domain.Artifact{ID: id, Title: title}, nil
}
func (f *fakeLibrary) DeleteArtifact(_ context.Context, id, _ uuid.UUID) error {
	delete(f.text, id)
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeHosts struct {
	host   *domain.Host
	revoke []uuid.UUID
}

func (f *fakeHosts) Overview(context.Context, uuid.UUID) (*service.HostOverview, error) {
	return &service.HostOverview{Hosts: []service.HostView{{Host: f.host, Live: true}}}, nil
}
func (f *fakeHosts) SetPaused(_ context.Context, _, _ uuid.UUID, p bool) error {
	f.host.Paused = p
	return nil
}
func (f *fakeHosts) SetGranted(_ context.Context, _, _ uuid.UUID, c []string) error {
	f.host.Capabilities = c
	return nil
}
func (f *fakeHosts) Revoke(_ context.Context, _, id uuid.UUID) error {
	f.revoke = append(f.revoke, id)
	return nil
}

type fakeProfile struct {
	name    string
	inbound bool
}

func (f *fakeProfile) ManagerName(context.Context, uuid.UUID) (string, error) { return f.name, nil }
func (f *fakeProfile) SetManagerName(_ context.Context, _ uuid.UUID, n string) error {
	f.name = n
	return nil
}
func (f *fakeProfile) InboundCallsEnabled(context.Context, uuid.UUID) (bool, error) {
	return f.inbound, nil
}
func (f *fakeProfile) SetInboundCallsEnabled(_ context.Context, _ uuid.UUID, e bool) error {
	f.inbound = e
	return nil
}

type fakeAliases map[string]string

func (f fakeAliases) List(context.Context, uuid.UUID) (map[string]string, error) { return f, nil }
func (f fakeAliases) Put(_ context.Context, _ uuid.UUID, a, t string) error {
	f[a] = t
	return nil
}
func (f fakeAliases) Delete(_ context.Context, _ uuid.UUID, a string) (bool, error) {
	_, ok := f[a]
	delete(f, a)
	return ok, nil
}

type fakeResearch struct {
	ResearchAPI
	runs    map[uuid.UUID]*domain.ResearchRun
	paused  []uuid.UUID
	resumed []uuid.UUID
}

func (f *fakeResearch) StartRun(_ context.Context, u uuid.UUID, req domain.ResearchRequest) (*domain.ResearchRun, error) {
	r := &domain.ResearchRun{ID: uuid.New(), UserID: u, Objective: req.Objective}
	f.runs[r.ID] = r
	return r, nil
}
func (f *fakeResearch) PauseRun(_ context.Context, _, id uuid.UUID) error {
	f.paused = append(f.paused, id)
	return nil
}
func (f *fakeResearch) ResumeRun(_ context.Context, _, id uuid.UUID) (*domain.ResearchRun, error) {
	f.resumed = append(f.resumed, id)
	return &domain.ResearchRun{ID: id, Status: domain.PhasePaused}, nil
}

type fakeArchive struct {
	archived, unarchived []uuid.UUID
	deletedAll           bool
}

func (f *fakeArchive) ArchiveTasks(_ context.Context, _ uuid.UUID, ids []uuid.UUID, _ time.Time) (int64, error) {
	f.archived = append(f.archived, ids...)
	return int64(len(ids)), nil
}
func (f *fakeArchive) UnarchiveTask(_ context.Context, _, id uuid.UUID) (int64, error) {
	f.unarchived = append(f.unarchived, id)
	return 1, nil
}
func (f *fakeArchive) DeleteArchived(context.Context, uuid.UUID) (int64, error) {
	f.deletedAll = true
	return 3, nil
}

type fakeDSA struct{ solved []string }

func (f *fakeDSA) Today(context.Context, uuid.UUID) ([]*domain.DsaItem, error) {
	return []*domain.DsaItem{{Slug: "two-sum", Title: "Two Sum", State: "assigned"}}, nil
}
func (f *fakeDSA) Sync(context.Context, uuid.UUID) ([]string, error) { return nil, nil }
func (f *fakeDSA) MarkSolved(_ context.Context, _ uuid.UUID, s string) error {
	f.solved = append(f.solved, s)
	return nil
}

type fakeApprovalEdit struct{ edited []uuid.UUID }

func (f *fakeApprovalEdit) EditApproval(_ context.Context, id, _ uuid.UUID, _ string) (*domain.Approval, error) {
	f.edited = append(f.edited, id)
	return &domain.Approval{ID: id}, nil
}

type fakeVIT struct {
	VITAPI
	actions []string
}

func (f *fakeVIT) UpdateTargetAction(_ context.Context, _, _ uuid.UUID, a string) error {
	f.actions = append(f.actions, a)
	return nil
}

// ---- harness ----

type more struct {
	t     *testing.T
	user  uuid.UUID
	tools map[string]*Tool
	d     Deps
}

func newMore(t *testing.T, m MoreDeps, tasks ...*domain.Task) *more {
	t.Helper()
	h := &more{t: t, user: uuid.New(), tools: map[string]*Tool{}}
	h.d = Deps{Tasks: &fakeTasks{tasks: tasks}, More: m}
	for _, tl := range newMoreTools(h.d) {
		h.tools[tl.Name] = tl
	}
	return h
}

func (h *more) call(origin Origin, seen ...uuid.UUID) *Call {
	c := &Call{UserID: h.user, Origin: origin, Now: time.Date(2026, 11, 28, 10, 0, 0, 0, IST()), conv: &Conversation{}}
	for _, s := range seen {
		c.conv.WorkingSet.mark(s.String())
	}
	return c
}

func (h *more) run(name string, c *Call, args map[string]any) (Output, error) {
	h.t.Helper()
	tl := h.tools[name]
	if tl == nil {
		h.t.Fatalf("tool %s is not registered", name)
	}
	raw, _ := json.Marshal(args)
	return tl.Run(context.Background(), c, raw)
}

func (h *more) mustRun(name string, c *Call, args map[string]any) Output {
	h.t.Helper()
	out, err := h.run(name, c, args)
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	return out
}

func (h *more) refused(name string, args map[string]any) {
	h.t.Helper()
	_, err := h.run(name, h.call(OriginOwnerChat), args)
	if err == nil || !strings.Contains(err.Error(), "was not returned by a list tool") {
		h.t.Fatalf("%s with an unseen id: err = %v, want refusal", name, err)
	}
}

func (h *more) undo(name string, out Output) {
	h.t.Helper()
	if len(out.UndoData) == 0 {
		h.t.Fatalf("%s returned no undo data", name)
	}
	if _, err := h.tools[name].Undo(context.Background(), h.user, out.UndoData); err != nil {
		h.t.Fatalf("%s undo: %v", name, err)
	}
}

// ---- tests ----

func TestAlumniTools(t *testing.T) {
	p := &domain.Person{ID: uuid.New(), Name: "Asha Rao", Company: "Acme", ContactState: domain.ContactNotContacted}
	dnc := &domain.Person{ID: uuid.New(), Name: "Blocked Bo", DoNotContact: true}
	people := &fakePeople{list: []*domain.Person{p, dnc}}
	hold := &fakeHolder{}
	h := newMore(t, MoreDeps{People: people, Holder: hold})

	out := h.mustRun("list_alumni", h.call(OriginOwnerChat), nil)
	if len(out.IDs) != 2 || !strings.Contains(out.Text, "Asha Rao") {
		t.Fatalf("list_alumni = %+v", out)
	}
	h.refused("update_alumnus", map[string]any{"id": p.ID.String(), "contact_state": "replied"})
	h.refused("verify_alumnus", map[string]any{"id": p.ID.String()})
	h.refused("file_alumni_outreach", map[string]any{"id": p.ID.String(), "draft_text": "hi", "channel": "email"})
	h.refused("show_alumnus", map[string]any{"id": p.ID.String()})

	c := h.call(OriginOwnerChat, p.ID, dnc.ID)
	if _, err := h.run("update_alumnus", c, map[string]any{"id": p.ID.String(), "contact_state": "drafted"}); err == nil {
		t.Fatal("drafted is not a state the owner may set by hand")
	}
	up := h.mustRun("update_alumnus", c, map[string]any{"id": p.ID.String(), "contact_state": "opted_out"})
	if p.ContactState != domain.ContactOptedOut || !p.DoNotContact {
		t.Fatalf("opting out must also set do_not_contact: %+v", p)
	}
	h.undo("update_alumnus", up)
	if p.ContactState != domain.ContactNotContacted || p.DoNotContact {
		t.Fatalf("undo left %+v", p)
	}

	if _, err := h.run("file_alumni_outreach", c, map[string]any{"id": dnc.ID.String(), "draft_text": "hi", "channel": "email"}); err == nil || len(hold.held) != 0 {
		t.Fatalf("a do-not-contact person must be refused: %v", err)
	}
	if _, err := h.run("file_alumni_outreach", c, map[string]any{"id": p.ID.String(), "draft_text": "hi", "channel": "carrier-pigeon"}); err == nil {
		t.Fatal("bad channel accepted")
	}
	h.mustRun("file_alumni_outreach", c, map[string]any{"id": p.ID.String(), "draft_text": "Hello Asha", "channel": "linkedin"})
	if len(hold.held) != 1 || !strings.Contains(hold.held[0], `"sends_nothing":true`) || len(people.drafted) != 1 {
		t.Fatalf("held = %v drafted = %v", hold.held, people.drafted)
	}
}

func TestOutreachTools(t *testing.T) {
	camp := &domain.OutreachCampaign{ID: uuid.New(), Name: "Founders", Channel: "email", Status: "active", Limits: domain.DefaultOutreachLimits("email")}
	tg := &domain.OutreachTarget{ID: uuid.New(), CampaignID: camp.ID, Name: "Pat", Handle: "pat@x.example", Status: domain.OutreachDrafted, Draft: "v1"}
	o := &fakeOutreach{camp: camp, targets: []*domain.OutreachTarget{tg}}
	h := newMore(t, MoreDeps{Outreach: o})

	h.refused("update_campaign", map[string]any{"id": camp.ID.String(), "status": "paused"})
	h.refused("update_outreach_target", map[string]any{"id": tg.ID.String(), "draft": "x"})
	h.refused("list_campaign_targets", map[string]any{"campaign_id": camp.ID.String()})
	h.refused("next_outreach_target", map[string]any{"campaign_id": camp.ID.String()})

	c := h.call(OriginOwnerChat, camp.ID, tg.ID)
	up := h.mustRun("update_campaign", c, map[string]any{"id": camp.ID.String(), "status": "paused"})
	if camp.Status != "paused" {
		t.Fatal("not paused")
	}
	h.undo("update_campaign", up)
	if camp.Status != "active" {
		t.Fatal("undo did not resume")
	}

	ed := h.mustRun("update_outreach_target", c, map[string]any{"id": tg.ID.String(), "draft": "v2"})
	if tg.Draft != "v2" {
		t.Fatal("draft not edited")
	}
	h.undo("update_outreach_target", ed)
	if tg.Draft != "v1" {
		t.Fatalf("undo draft = %q", tg.Draft)
	}

	sched := h.call(OriginScheduled, camp.ID, tg.ID)
	if _, err := h.run("update_outreach_target", sched, map[string]any{"id": tg.ID.String(), "status": "approved"}); err == nil {
		t.Fatal("a scheduled run must not approve a message")
	}
	h.mustRun("update_outreach_target", c, map[string]any{"id": tg.ID.String(), "status": "approved"})
	h.mustRun("update_outreach_target", c, map[string]any{"id": tg.ID.String(), "status": "sent"})
	last := o.updates[len(o.updates)-1]
	if last.NextFollowupAt == nil || !last.NextFollowupAt.After(c.Now.Add(4*24*time.Hour)) {
		t.Fatalf("marking sent must schedule the follow-up from the campaign limits: %+v", last)
	}
}

func TestVITTargetApprovalIsOwnerOnly(t *testing.T) {
	v := &fakeVIT{}
	h := newMore(t, MoreDeps{VIT: v})
	id := uuid.New()
	h.refused("update_vit_target", map[string]any{"id": id.String(), "action": "approve"})
	if _, err := h.run("update_vit_target", h.call(OriginScheduled, id), map[string]any{"id": id.String(), "action": "approve"}); err == nil || len(v.actions) != 0 {
		t.Fatal("a scheduled run must not approve")
	}
	h.mustRun("update_vit_target", h.call(OriginOwnerChat, id), map[string]any{"id": id.String(), "action": "sent_manually"})
	if len(v.actions) != 1 {
		t.Fatalf("actions = %v", v.actions)
	}
}

func TestLibraryTools(t *testing.T) {
	id := uuid.New()
	lib := &fakeLibrary{text: map[uuid.UUID]string{id: "old"}}
	h := newMore(t, MoreDeps{Library: lib})
	h.refused("update_artifact_text", map[string]any{"id": id.String(), "text": "new"})
	h.refused("read_artifact_text", map[string]any{"id": id.String()})

	if out := h.mustRun("list_library", h.call(OriginOwnerChat), nil); len(out.IDs) != 1 {
		t.Fatalf("list_library = %+v", out)
	}
	c := h.call(OriginOwnerChat, id)
	up := h.mustRun("update_artifact_text", c, map[string]any{"id": id.String(), "text": "new"})
	if lib.text[id] != "new" {
		t.Fatal("not updated")
	}
	h.undo("update_artifact_text", up)
	if lib.text[id] != "old" {
		t.Fatalf("undo = %q", lib.text[id])
	}
	cr := h.mustRun("create_list", c, map[string]any{"title": "Ideas", "content": "# Ideas"})
	h.undo("create_list", cr)
	if len(lib.deleted) != 1 {
		t.Fatal("undo of create_list must remove the list")
	}

	del := h.tools["delete_artifact"]
	if del.Effect != EffectExternal {
		t.Fatal("a permanent delete is external")
	}
	if _, _, err := del.Prepare(json.RawMessage(`{"id":"nope"}`)); err == nil {
		t.Fatal("a bad id must not prepare")
	}
	payload, _, err := del.Prepare(json.RawMessage(`{"id":"` + id.String() + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := del.Exec(context.Background(), h.user, payload); err != nil || len(lib.deleted) != 2 {
		t.Fatalf("exec: %v %v", err, lib.deleted)
	}
}

func TestDeviceTools(t *testing.T) {
	host := &domain.Host{ID: uuid.New(), Name: "laptop", Capabilities: []string{"files"}}
	hs := &fakeHosts{host: host}
	h := newMore(t, MoreDeps{Hosts: hs})
	h.refused("update_device", map[string]any{"id": host.ID.String(), "paused": true})

	out := h.mustRun("list_devices", h.call(OriginOwnerChat), nil)
	if len(out.IDs) != 1 {
		t.Fatalf("list_devices = %+v", out)
	}
	c := h.call(OriginOwnerChat, host.ID)
	up := h.mustRun("update_device", c, map[string]any{"id": host.ID.String(), "paused": true, "capabilities": []string{"files", "desktop"}})
	if !host.Paused || len(host.Capabilities) != 2 {
		t.Fatalf("host = %+v", host)
	}
	h.undo("update_device", up)
	if host.Paused || len(host.Capabilities) != 1 {
		t.Fatalf("undo host = %+v", host)
	}
	rev := h.tools["revoke_device"]
	payload, _, err := rev.Prepare(json.RawMessage(`{"id":"` + host.ID.String() + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rev.Exec(context.Background(), h.user, payload); err != nil || len(hs.revoke) != 1 {
		t.Fatalf("revoke: %v", err)
	}
}

func TestSettingsAndAliasTools(t *testing.T) {
	pr := &fakeProfile{name: "Kit"}
	al := fakeAliases{"mummy": "mom"}
	h := newMore(t, MoreDeps{Profile: pr, Aliases: al})
	c := h.call(OriginOwnerChat)

	nm := h.mustRun("set_manager_name", c, map[string]any{"name": "Jarvis"})
	if pr.name != "Jarvis" {
		t.Fatalf("name = %q", pr.name)
	}
	h.undo("set_manager_name", nm)
	if pr.name != "Kit" {
		t.Fatalf("undo name = %q", pr.name)
	}
	ib := h.mustRun("set_inbound_calls", c, map[string]any{"enabled": true})
	h.undo("set_inbound_calls", ib)
	if pr.inbound {
		t.Fatal("undo did not turn inbound calls off")
	}
	if _, err := h.run("set_inbound_calls", c, nil); err == nil {
		t.Fatal("enabled is required")
	}

	sa := h.mustRun("set_alias", c, map[string]any{"alias": "papa", "target": "dad"})
	h.undo("set_alias", sa)
	if _, ok := al["papa"]; ok {
		t.Fatal("undo must remove a new alias")
	}
	ra := h.mustRun("remove_alias", c, map[string]any{"alias": "mummy"})
	if _, ok := al["mummy"]; ok {
		t.Fatal("alias not removed")
	}
	h.undo("remove_alias", ra)
	if al["mummy"] != "mom" {
		t.Fatal("undo must put the alias back")
	}
	if out := h.mustRun("show_settings", c, nil); !strings.Contains(out.Text, "mummy=mom") || !strings.Contains(out.Text, "Kit") {
		t.Fatalf("show_settings = %q", out.Text)
	}
}

func TestResearchTools(t *testing.T) {
	rs := &fakeResearch{runs: map[uuid.UUID]*domain.ResearchRun{}}
	h := newMore(t, MoreDeps{Research: rs})
	h.refused("control_research", map[string]any{"id": uuid.NewString(), "action": "pause"})

	st := h.mustRun("start_research", h.call(OriginOwnerChat), map[string]any{"objective": "Find pay at Acme"})
	if len(rs.runs) != 1 || len(h.d.Tasks.(*fakeTasks).created) != 1 {
		t.Fatalf("start_research must create the run and the task that advances it")
	}
	id := st.IDs[0]
	rid, _ := uuid.Parse(id)
	c := h.call(OriginOwnerChat, rid)
	p := h.mustRun("control_research", c, map[string]any{"id": id, "action": "pause"})
	h.undo("control_research", p)
	if len(rs.paused) != 1 || len(rs.resumed) != 1 {
		t.Fatalf("paused %v resumed %v", rs.paused, rs.resumed)
	}
	if _, err := h.run("control_research", c, map[string]any{"id": id, "action": "explode"}); err == nil {
		t.Fatal("bad action accepted")
	}
}

func TestTaskTools(t *testing.T) {
	u := uuid.New()
	done := mkTask(u, "Old run", domain.TaskStatusCompleted)
	other := mkTask(uuid.New(), "Not mine", domain.TaskStatusCompleted)
	ar := &fakeArchive{}
	deleted := []uuid.UUID{}
	h := newMore(t, MoreDeps{Archive: ar, DeleteTask: func(_ context.Context, id uuid.UUID) error { deleted = append(deleted, id); return nil }}, done, other)
	h.user = u
	h.refused("archive_tasks", map[string]any{"ids": []string{done.ID.String()}})

	c := h.call(OriginOwnerChat, done.ID, other.ID)
	if _, err := h.run("archive_tasks", c, map[string]any{"ids": []string{other.ID.String()}}); err == nil {
		t.Fatal("another owner's task must not be archived")
	}
	a := h.mustRun("archive_tasks", c, map[string]any{"ids": []string{done.ID.String()}})
	h.undo("archive_tasks", a)
	if len(ar.archived) != 1 || len(ar.unarchived) != 1 {
		t.Fatalf("archive %v unarchive %v", ar.archived, ar.unarchived)
	}

	del := h.tools["delete_tasks"]
	if del.Effect != EffectExternal {
		t.Fatal("deleting tasks is external")
	}
	if _, _, err := del.Prepare(json.RawMessage(`{}`)); err == nil {
		t.Fatal("neither ids nor archived must be refused")
	}
	if _, _, err := del.Prepare(json.RawMessage(`{"ids":["` + done.ID.String() + `"],"archived":true}`)); err == nil {
		t.Fatal("both ids and archived must be refused")
	}
	payload, _, err := del.Prepare(json.RawMessage(`{"ids":["` + done.ID.String() + `","` + other.ID.String() + `"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := del.Exec(context.Background(), u, payload); err != nil || len(deleted) != 1 || deleted[0] != done.ID {
		t.Fatalf("only the owner's own task may be deleted: %v %v", err, deleted)
	}
	payload, _, _ = del.Prepare(json.RawMessage(`{"archived":true}`))
	if _, err := del.Exec(context.Background(), u, payload); err != nil || !ar.deletedAll {
		t.Fatalf("delete archived: %v", err)
	}
}

func TestDsaAndApprovalEditNeedSeenIds(t *testing.T) {
	d := &fakeDSA{}
	ae := &fakeApprovalEdit{}
	h := newMore(t, MoreDeps{DSA: d, ApprovalEdit: ae})
	h.refused("mark_dsa_solved", map[string]any{"slug": "two-sum"})
	h.refused("edit_approval", map[string]any{"id": uuid.NewString(), "text": "x"})
	if len(d.solved) != 0 || len(ae.edited) != 0 {
		t.Fatal("an unseen id must change nothing")
	}
	out := h.mustRun("dsa_today", h.call(OriginOwnerChat), nil)
	if len(out.IDs) != 1 || out.IDs[0] != "two-sum" {
		t.Fatalf("dsa_today = %+v", out)
	}
	c := h.call(OriginOwnerChat)
	c.conv.WorkingSet.mark("two-sum")
	h.mustRun("mark_dsa_solved", c, map[string]any{"slug": "two-sum"})
	if len(d.solved) != 1 {
		t.Fatal("not marked")
	}
}

func TestReadOnlyPagesAndErrors(t *testing.T) {
	h := newMore(t, MoreDeps{
		SourceHealth: func(context.Context, uuid.UUID) ([]domain.SourceHealth, error) {
			return []domain.SourceHealth{{Source: "greenhouse", Target: "acme", LastStatus: "ok"}}, nil
		},
		InboundEvents: func(context.Context, uuid.UUID, int, bool) ([]*domain.InboundEvent, error) {
			return nil, errors.New("db down")
		},
	})
	if out := h.mustRun("sources_health", h.call(OriginOwnerChat), nil); !strings.Contains(out.Text, "greenhouse") {
		t.Fatalf("sources_health = %q", out.Text)
	}
	if _, err := h.run("list_inbound_events", h.call(OriginOwnerChat), nil); err == nil {
		t.Fatal("a store error must reach the model")
	}
}
