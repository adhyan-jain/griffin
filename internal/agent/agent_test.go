package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

func responseOf(t *testing.T, m *scriptModel, round int, idx int) map[string]any {
	t.Helper()
	cs := m.seen[round]
	last := cs[len(cs)-1]
	return last.Parts[idx].FunctionResponse.Response
}

func TestSetReminderResolvesTimeInCode(t *testing.T) {
	e := newEnv(t,
		call("set_reminder", map[string]any{"what": "call mom", "when": "tomorrow at 12 pm"}),
		say("Done. I set a reminder to call mom."))
	res, err := e.run("remind me to call mom tomorrow at 12 pm")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.rem.at) != 1 || e.rem.at[0].In(IST()).Hour() != 12 {
		t.Fatalf("reminder times = %v", e.rem.at)
	}
	if len(res.Actions) != 1 || res.Actions[0].Tool != "set_reminder" || res.Actions[0].UndoID == "" {
		t.Fatalf("actions = %+v", res.Actions)
	}
}

func TestSetReminderAsksWhenTimeUnclear(t *testing.T) {
	e := newEnv(t, call("set_reminder", map[string]any{"what": "x", "when": "sometime"}), say("When should I remind you?"))
	res, err := e.run("remind me to x sometime")
	if err != nil || len(e.rem.at) != 0 || len(res.Actions) != 0 {
		t.Fatalf("err=%v created=%v actions=%v", err, e.rem.at, res.Actions)
	}
}

func seedTasks(e *env, n int) []*domain.Task {
	var ts []*domain.Task
	for i := 0; i < n; i++ {
		ts = append(ts, mkTask(e.user, "Task "+string(rune('A'+i)), domain.TaskStatusQueued))
	}
	e.tasks.tasks = ts
	return ts
}

func TestBulkCancelRunsAtOnceWithUndoOnce(t *testing.T) {
	e := newEnv(t, call("list_tasks", nil), nil2(), say("ok"))
	ts := seedTasks(e, 5)
	var ids []any
	for _, x := range ts {
		ids = append(ids, x.ID.String())
	}
	e.model.replies[1] = call("cancel_tasks", map[string]any{"ids": ids})
	e.model.replies[2] = say("Cancelled 5 tasks.")
	res, err := e.run("cancel all my tasks")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.cancelled) != 5 {
		t.Fatalf("cancelled %d, want 5 without any confirmation step", len(e.tasks.cancelled))
	}
	undo := res.Actions[0].UndoID
	if undo == "" {
		t.Fatal("no undo offered")
	}
	if _, err := e.agent.Undo(context.Background(), e.user, undo); err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.resumed) != 5 {
		t.Fatalf("resumed %d", len(e.tasks.resumed))
	}
	if _, err := e.agent.Undo(context.Background(), e.user, undo); err != ErrNoUndo {
		t.Fatalf("second undo = %v, want ErrNoUndo", err)
	}
	if len(e.tasks.resumed) != 5 {
		t.Fatal("undo ran twice")
	}
}

func nil2() gemini.ToolReply { return gemini.ToolReply{} }

func TestWriteToolRefusesUnseenID(t *testing.T) {
	e := newEnv(t, call("cancel_tasks", map[string]any{"ids": []any{uuid.NewString()}}), say("I could not cancel that."))
	e.tasks.tasks = []*domain.Task{mkTask(e.user, "A", domain.TaskStatusQueued)}
	if _, err := e.run("cancel it"); err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.cancelled) != 0 {
		t.Fatal("cancelled an id no list tool returned")
	}
	if r := responseOf(t, e.model, 1, 0); r["error"] == nil {
		t.Fatalf("model was not told: %v", r)
	}
}

func TestSeenIDsPersistAcrossTurns(t *testing.T) {
	e := newEnv(t, call("list_tasks", nil), say("Here are your tasks."))
	ts := seedTasks(e, 2)
	if _, err := e.run("what tasks do I have"); err != nil {
		t.Fatal(err)
	}
	e.model.replies = append(e.model.replies, call("cancel_tasks", map[string]any{"ids": []any{ts[0].ID.String()}}), say("Cancelled Task A."))
	if _, err := e.run("cancel the first"); err != nil {
		t.Fatal(err)
	}
	if len(e.tasks.cancelled) != 1 {
		t.Fatalf("cancelled = %d; the id seen in turn 1 must carry to turn 2", len(e.tasks.cancelled))
	}
}

func TestPauseRoutineUntilMovesNextRunAndUndoRestores(t *testing.T) {
	e := newEnv(t, call("list_routines", nil), nil2(), say("Paused."))
	rid := uuid.New()
	orig := time.Date(2026, 11, 28, 9, 0, 0, 0, IST())
	e.routs.list = []*domain.Schedule{{ID: rid, UserID: e.user, Name: "Listings sync", CronExpression: "daily 09:00 Asia/Kolkata", NextRunAt: orig}}
	e.model.replies[1] = call("pause_routine", map[string]any{"id": rid.String(), "until": "2026-11-30"})
	res, err := e.run("pause listings sync till 30 Nov")
	if err != nil {
		t.Fatal(err)
	}
	got := e.routs.list[0]
	want := time.Date(2026, 11, 30, 9, 0, 0, 0, IST())
	if !got.NextRunAt.Equal(want) || got.IsPaused {
		t.Fatalf("next run = %v paused=%v, want %v", got.NextRunAt, got.IsPaused, want)
	}
	if _, err := e.agent.Undo(context.Background(), e.user, res.Actions[0].UndoID); err != nil {
		t.Fatal(err)
	}
	if !e.routs.list[0].NextRunAt.Equal(orig) {
		t.Fatalf("undo left next run at %v", e.routs.list[0].NextRunAt)
	}
}

func TestPauseResumeEditRunRoutine(t *testing.T) {
	e := newEnv(t, call("list_routines", nil), call("pause_routine", map[string]any{"id": "X"}), nil2(), nil2(), nil2(), say("ok"))
	rid := uuid.New()
	e.routs.list = []*domain.Schedule{{ID: rid, UserID: e.user, Name: "Brief", CronExpression: "daily 09:00 Asia/Kolkata", ManagerDomain: "JJ",
		NextRunAt: e.now.Add(time.Hour), TaskSpecTemplate: []byte(`{"title":"Brief","goal":"old goal","priority":"NORMAL_P2"}`)}}
	rs := rid.String()
	e.model.replies[1] = call("pause_routine", map[string]any{"id": rs})
	e.model.replies[2] = call("resume_routine", map[string]any{"id": rs})
	e.model.replies[3] = call("edit_routine", map[string]any{"id": rs, "goal": "new goal"})
	e.model.replies[4] = call("run_routine_now", map[string]any{"id": rs})
	res, err := e.run("do things")
	if err != nil {
		t.Fatal(err)
	}
	if e.routs.list[0].IsPaused || !strings.Contains(string(e.routs.list[0].TaskSpecTemplate), `"goal":"new goal"`) ||
		!strings.Contains(string(e.routs.list[0].TaskSpecTemplate), `"title":"Brief"`) {
		t.Fatalf("routine = %+v %s", e.routs.list[0], e.routs.list[0].TaskSpecTemplate)
	}
	if len(e.tasks.created) != 1 || e.tasks.created[0].Goal != "new goal" {
		t.Fatalf("created = %+v", e.tasks.created)
	}
	// undo the edit: the old spec comes back
	if _, err := e.agent.Undo(context.Background(), e.user, res.Actions[2].UndoID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(e.routs.list[0].TaskSpecTemplate), "old goal") {
		t.Fatalf("spec after undo = %s", e.routs.list[0].TaskSpecTemplate)
	}
}

func TestRoutineOfAnotherUserIsRefused(t *testing.T) {
	e := newEnv(t, call("list_routines", nil), nil2(), say("no"))
	rid := uuid.New()
	e.routs.list = []*domain.Schedule{{ID: rid, UserID: uuid.New(), Name: "Theirs", CronExpression: "daily 09:00"}}
	e.model.replies[1] = call("pause_routine", map[string]any{"id": rid.String()})
	if _, err := e.run("pause it"); err != nil {
		t.Fatal(err)
	}
	if e.routs.list[0].IsPaused {
		t.Fatal("paused someone else's routine")
	}
}

func TestRetryTask(t *testing.T) {
	e := newEnv(t, call("list_tasks", nil), nil2(), say("Retrying."))
	f := mkTask(e.user, "Failed one", domain.TaskStatusFailed)
	e.tasks.tasks = []*domain.Task{f}
	e.model.replies[1] = call("retry_task", map[string]any{"id": f.ID.String()})
	res, err := e.run("retry the failed task")
	if err != nil || len(e.tasks.resumed) != 1 || !res.Wake {
		t.Fatalf("err=%v resumed=%v wake=%v", err, e.tasks.resumed, res != nil && res.Wake)
	}
}

func TestApprovalsListAndDecide(t *testing.T) {
	e := newEnv(t, call("list_approvals", nil), nil2(), say("Approved."))
	ap := &domain.Approval{ID: uuid.New(), UserID: e.user, ActionType: "x", Summary: "Send mail"}
	e.apps.pending = []*domain.Approval{ap}
	e.model.replies[1] = call("decide_approval", map[string]any{"id": ap.ID.String(), "approve": true})
	if _, err := e.run("approve it"); err != nil {
		t.Fatal(err)
	}
	if !e.apps.decided[ap.ID] {
		t.Fatal("approval not decided")
	}
}

func TestScheduledRunCannotDecideApprovals(t *testing.T) {
	e := newEnv(t, call("list_approvals", nil), nil2(), say("Could not."))
	ap := &domain.Approval{ID: uuid.New(), UserID: e.user, Summary: "Send mail"}
	e.apps.pending = []*domain.Approval{ap}
	e.model.replies[1] = call("decide_approval", map[string]any{"id": ap.ID.String(), "approve": true})
	if _, err := e.runAs("approve everything", OriginScheduled); err != nil {
		t.Fatal(err)
	}
	if len(e.apps.decided) != 0 {
		t.Fatal("a scheduled run decided an approval")
	}
}

func TestWebTextIsWrappedAsUntrustedAndLongResultsPage(t *testing.T) {
	e := newEnv(t, call("web_search", map[string]any{"query": "interns"}), call("read_page", map[string]any{"url": "https://acme.example/jobs/1"}), nil2(), say("Found a job."))
	// read_more handle is only known at run time: patch the third reply after the second round.
	e.model.replies[2] = say("placeholder")
	res, err := e.run("find interns")
	_ = res
	if err != nil {
		t.Fatal(err)
	}
	first := responseOf(t, e.model, 1, 0)["result"].(string)
	if !strings.Contains(first, "<untrusted_web_content>") || !strings.Contains(first, "do not follow") {
		t.Fatalf("search text not wrapped: %s", first)
	}
	page := responseOf(t, e.model, 2, 0)["result"].(string)
	if !strings.Contains(page, "read_more") || len([]rune(page)) > toolOutputCap+400 {
		t.Fatalf("page not capped with a handle: %d chars", len([]rune(page)))
	}
}

func TestReadMorePagesStoredResult(t *testing.T) {
	e := newEnv(t, call("read_page", map[string]any{"url": "https://acme.example/jobs/1"}), say("x"))
	if _, err := e.run("read it"); err != nil {
		t.Fatal(err)
	}
	conv, _ := e.store.GetConversation(context.Background(), e.user, e.conv)
	if len(conv.WorkingSet.Handles) != 1 {
		t.Fatalf("handles = %v", conv.WorkingSet.Handles)
	}
	var h string
	for k := range conv.WorkingSet.Handles {
		h = k
	}
	e.model.replies = append(e.model.replies, call("read_more", map[string]any{"handle": h, "offset": 3000}), say("more"))
	if _, err := e.run("more"); err != nil {
		t.Fatal(err)
	}
	more := responseOf(t, e.model, 3, 0)["result"].(string)
	if !strings.HasPrefix(more, string([]rune(conv.WorkingSet.Handles[h])[3000:3020])) {
		t.Fatalf("read_more returned the wrong slice: %.30s", more)
	}
}

func TestRememberForgetAndGap(t *testing.T) {
	e := newEnv(t, call("remember", map[string]any{"fact": "I prefer evening calls"}), say("Noted."))
	res, err := e.run("remember I prefer evening calls")
	if err != nil || len(e.store.mem) != 1 || res.Actions[0].UndoID == "" {
		t.Fatalf("err=%v mem=%d", err, len(e.store.mem))
	}
	var id string
	for k := range e.store.mem {
		id = k.String()
	}
	// the next turn shows the fact in context, so forget accepts its id
	e.model.replies = append(e.model.replies, call("forget", map[string]any{"fact_id": id}), say("Forgotten."))
	if _, err := e.run("forget that"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.model.systems[2], "I prefer evening calls") {
		t.Fatal("memory missing from the context")
	}
	if len(e.store.del) != 1 {
		t.Fatal("not forgotten")
	}
	e.model.replies = append(e.model.replies, call("forget", map[string]any{"fact_id": uuid.NewString()}), say("I could not."))
	if _, err := e.run("forget something else"); err != nil {
		t.Fatal(err)
	}
	e.model.replies = append(e.model.replies, call("no_tool_fits", map[string]any{"request": "book a flight"}), say("I can't do that yet."))
	if _, err := e.run("book a flight"); err != nil || len(e.store.gaps) != 1 {
		t.Fatalf("err=%v gaps=%v", err, e.store.gaps)
	}
}

func TestStartJobHandsOff(t *testing.T) {
	e := newEnv(t, call("start_job", map[string]any{"goal": "check every listing link"}))
	res, err := e.run("check every listing link")
	if err != nil || res.Handoff == nil || res.Handoff.Goal != "check every listing link" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestExternalFromOwnerChatExecutesAtOnce(t *testing.T) {
	e := newEnv(t, call("send_test_message", map[string]any{"to": "Asha", "text": "hello"}), say("Sent the message to Asha."))
	res, err := e.run("message Asha hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.sent) != 1 || len(e.apps.holdKind) != 0 {
		t.Fatalf("sent=%v holds=%v", e.sent, e.apps.holdKind)
	}
	if !strings.Contains(res.Actions[0].Summary, `"To":"Asha"`) || res.Actions[0].Pending {
		t.Fatalf("action must record exactly what was sent: %+v", res.Actions[0])
	}
}

func TestExternalFromScheduledRunOnlyAsksThenRunsExactPayloadOnce(t *testing.T) {
	e := newEnv(t, call("send_test_message", map[string]any{"to": "Asha", "text": "hello"}), say("It is waiting for your approval; nothing was sent."))
	res, err := e.runAs("send the weekly note", OriginScheduled)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.sent) != 0 || len(e.apps.pending) != 1 || !res.Actions[0].Pending {
		t.Fatalf("sent=%v pending=%d", e.sent, len(e.apps.pending))
	}
	ap := e.apps.pending[0]
	if ap.ActionType != "send_test_message" || !domain.IsAgentAction("send_test_message") {
		t.Fatalf("approval type = %s", ap.ActionType)
	}
	e.reg.Decided(context.Background(), ap, false)
	if len(e.sent) != 0 {
		t.Fatal("a rejection ran the action")
	}
	e.reg.Decided(context.Background(), ap, true)
	if len(e.sent) != 1 || e.sent[0] != string(ap.ActionPayload) {
		t.Fatalf("sent = %v, want exactly the stored payload %s", e.sent, ap.ActionPayload)
	}
	// an approval of another kind is ignored
	e.reg.Decided(context.Background(), &domain.Approval{ActionType: "alumni_outreach"}, true)
	if len(e.sent) != 1 {
		t.Fatal("ran for a foreign approval")
	}
}

func TestExternalBadArgsExecuteNothing(t *testing.T) {
	e := newEnv(t, call("send_test_message", map[string]any{"text": "hello"}), say("I could not send it."))
	if _, err := e.run("message someone"); err != nil || len(e.sent) != 0 {
		t.Fatalf("err=%v sent=%v", err, e.sent)
	}
}

func TestGroundingRetriesAnInventedDoneClaimThenAccepts(t *testing.T) {
	e := newEnv(t, say("Done, I've paused the routine."), say("I could not pause anything; no routine was changed."))
	res, err := e.run("pause my routine")
	if err != nil {
		t.Fatal(err)
	}
	if e.model.calls != 2 || strings.Contains(res.Reply, "Done") {
		t.Fatalf("calls=%d reply=%q", e.model.calls, res.Reply)
	}
	retry := e.model.seen[1][len(e.model.seen[1])-1].Parts[0].Text
	if !strings.Contains(retry, "no tool call did it") {
		t.Fatalf("retry note = %q", retry)
	}
}

func TestGroundingDropsUnsupportedSentencesAfterOneRetry(t *testing.T) {
	e := newEnv(t, say("Done, I removed it. You have 3 tasks."), say("Done, I removed it. You have 3 tasks."))
	res, err := e.run("remove it")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Reply, "removed") || !strings.Contains(res.Reply, "couldn't verify") || !strings.Contains(res.Reply, "3 tasks") {
		t.Fatalf("reply = %q", res.Reply)
	}
}

func TestGroundingCatchesInventedURL(t *testing.T) {
	e := newEnv(t, call("web_search", map[string]any{"query": "x"}), say("Apply at https://made-up.example/apply today."), say("See https://acme.example/jobs/1 for the posting."))
	res, err := e.run("find the posting")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Reply, "https://acme.example/jobs/1") || strings.Contains(res.Reply, "made-up") {
		t.Fatalf("reply = %q", res.Reply)
	}
}

func TestAgentErrorReturnsErrorAndStoresNothing(t *testing.T) {
	e := newEnv(t)
	e.model.err = context.DeadlineExceeded
	if _, err := e.run("hi"); err == nil {
		t.Fatal("want the error so the caller can use the old path")
	}
	if len(e.store.turns) != 0 {
		t.Fatal("a failed run stored turns")
	}
}

func TestRoundLimit(t *testing.T) {
	e := newEnv(t)
	l := call("list_tasks", nil)
	e.model.loop = &l
	if _, err := e.run("loop"); err != ErrTooManyRounds {
		t.Fatalf("err = %v", err)
	}
	if e.model.calls != 0 && len(e.model.seen) > defaultMaxRounds+1 {
		t.Fatalf("model called %d times", len(e.model.seen))
	}
}

func TestStoredHistoryUsedOnSecondTurnAndClientHistoryIgnored(t *testing.T) {
	e := newEnv(t, say("Your name is noted."), say("You told me earlier: Priya."))
	in := Input{UserID: e.user, ConversationID: e.conv, Message: "my cousin is Priya", Origin: OriginOwnerChat,
		ClientHistory: []HistoryTurn{{"user", "client-only-line"}}}
	if _, err := e.agent.Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(e.model.seen[0]), "client-only-line") {
		t.Fatal("client history should be used when the server has none")
	}
	in.Message = "who is my cousin?"
	in.ClientHistory = []HistoryTurn{{"user", "stale-client-line"}}
	if _, err := e.agent.Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	got := textOf(e.model.seen[1])
	if !strings.Contains(got, "my cousin is Priya") || strings.Contains(got, "stale-client-line") || strings.Contains(got, "client-only-line") {
		t.Fatalf("second turn contents = %s", got)
	}
}

func textOf(cs []gemini.Content) string {
	var b strings.Builder
	for _, c := range cs {
		for _, p := range c.Parts {
			b.WriteString(p.Text + "\n")
		}
	}
	return b.String()
}

func TestSummaryFoldsOlderTurnsAfterTheTurn(t *testing.T) {
	e := newEnv(t)
	var folded int
	e.agent.Summarize = func(_ context.Context, prev string, turns []Turn) (string, error) {
		folded = len(turns)
		return "owner talked about many things", nil
	}
	for i := 0; i < 12; i++ {
		e.model.replies = append(e.model.replies, say("ok"))
		if _, err := e.run("message number " + string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if folded == 0 {
		t.Fatal("never summarised")
	}
	e.model.replies = append(e.model.replies, say("ok"))
	_, _ = e.run("next")
	if !strings.Contains(e.model.systems[len(e.model.systems)-1], "owner talked about many things") {
		t.Fatal("summary missing from the context")
	}
}

func TestRegistryRejectsIncompleteTools(t *testing.T) {
	if _, err := NewRegistry(&Tool{Name: "x", Description: "d", Effect: EffectExternal}); err == nil {
		t.Fatal("external tool without Prepare/Exec accepted")
	}
	if _, err := NewRegistry(&Tool{Name: "x", Description: "d", Effect: EffectRead, Run: readMore().Run}, &Tool{Name: "x", Description: "d", Effect: EffectRead, Run: readMore().Run}); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestOriginMustBeSet(t *testing.T) {
	e := newEnv(t, say("hi"))
	if _, err := e.agent.Run(context.Background(), Input{UserID: e.user, ConversationID: e.conv, Message: "x"}); err == nil {
		t.Fatal("run without an origin")
	}
}

func TestCheckGrounding(t *testing.T) {
	ev := "Reminder set for Sat 28 Nov 2026 12:00pm. Pay ₹12,000 at https://x.example/a"
	acts := []Action{{Tool: "set_reminder", Effect: EffectInternal, Summary: "Reminder set"}}
	cases := []struct {
		reply string
		bad   int
	}{
		{"Done. I set the reminder for 28 Nov.", 0},
		{"I set the reminder for 2026-11-28.", 0},
		{"I set the reminder for 29 Nov.", 1},
		{"The pay is Rs 12000, see https://x.example/a.", 0},
		{"The pay is ₹15,000.", 1},
		{"See https://other.example/b", 1},
		{"I paused the routine.", 1},
		{"Should I pause the routine?", 0},
		{"I couldn't pause the routine.", 0},
	}
	for _, c := range cases {
		if got := len(Check(c.reply, acts, ev)); got != c.bad {
			t.Errorf("Check(%q) = %d violations, want %d", c.reply, got, c.bad)
		}
	}
	pending := []Action{{Tool: "send_mail", Effect: EffectExternal, Pending: true}}
	if len(Check("I sent the mail.", pending, "")) != 1 {
		t.Error("a pending approval must not justify 'sent'")
	}
}
