package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// Managing tasks and approvals by voice. The commands live here, on the server, so the phone and the web app
// behave the same and neither has its own copy of the rules. Reading is immediate. Anything that changes something
// (cancel, delete, approve, reject) first answers with a question and a short-lived signed token; the client asks the
// owner and sends the token back with their answer. Nothing is stored between the two requests.

// TaskCommandKind is what the owner asked for.
type TaskCommandKind string

const (
	CmdNone          TaskCommandKind = ""
	CmdListTasks     TaskCommandKind = "list_tasks"
	CmdCancelTasks   TaskCommandKind = "cancel_tasks"
	CmdDeleteTasks   TaskCommandKind = "delete_tasks"
	CmdListApprovals TaskCommandKind = "list_approvals"
	CmdApprove       TaskCommandKind = "approve"
	CmdReject        TaskCommandKind = "reject"
	CmdSteerTask     TaskCommandKind = "steer_task"
)

// TaskCommand is a parsed request. Scope is "all", "active", "done" or "failed" (or "old" for delete); Ref is the
// words the owner used to name one task ("the cover letter"), or "" when they named none.
type TaskCommand struct {
	Kind  TaskCommandKind
	Scope string
	Ref   string
}

var (
	reList          = regexp.MustCompile(`^(?:please )?(?:(?:give me|get me|fetch|show(?: me)?|tell me|read(?: out)?(?: me)?|list|check|display|bring up)(?: me)?(?: all)?(?: of)?(?: my| the| a)?(?: list of)?|(?:the |a )?list of(?: all)?(?: of)?(?: my| the)?)(?: (running|active|finished|completed|done|failed|pending|open|in progress|old))? (?:tasks?|task list|tasks list)$`)
	reListQuestions = regexp.MustCompile(`^(?:what|how|are there|is there|do i have|have i got)(?: is| are| were| many)?(?: the| any| all| of)?(?: my| the)?(?: list of)?(?: (running|active|finished|completed|done|failed|pending|open|in progress|old))? (?:tasks?|task list|tasks list)(?: (?:are )?(running|active|done|finished|completed|failed|open|left|pending|in progress))?(?: do i have)?$`)
	reTaskStatus    = regexp.MustCompile(`^(?:what is |check |tell me )?(?:the )?(?:status of (?:my |the )?tasks?|tasks? status)$`)
	reCancelAll     = regexp.MustCompile(`^(?:please )?(?:cancel|stop) (?:all|everything|all (?:of )?(?:my )?(?:running|active)(?: tasks)?|everything running)$`)
	reCancel        = regexp.MustCompile(`^(?:please )?cancel (?:the |my )?(.+?)(?: task)?$`)
	reDeleteSet     = regexp.MustCompile(`^(?:please )?(?:delete|remove|clear|clean up)(?: all)?(?: of)?(?: the| my)? (finished|completed|done|failed|old|all|cancelled|canceled)(?: tasks)?$`)
	reDelete        = regexp.MustCompile(`^(?:please )?(?:delete|remove) (?:the |my |task )?(.+?)(?: task)?$`)
	reApprList      = regexp.MustCompile(`^(?:(?:please )?(?:list|show|check|give me|tell me|read(?: out)?|any|what|do i have any|are there any|what are the)?(?: all)?(?: of)?(?: my)?(?: pending| open)? approvals?|what needs my approval|what needs approval|(?:any |what )?(?:things?|tasks?) (?:that )?(?:need|needs|waiting for) (?:my )?approval)(?: (?:are )?(?:waiting|pending))?(?: for me)?$`)
	reApprove       = regexp.MustCompile(`^(?:please )?(?:approve|confirm)(?: it| that| this| the (.+?)| (.+?))?(?: karo)?$`)
	reReject        = regexp.MustCompile(`^(?:please )?(?:reject|deny|decline)(?: it| that| this| the (.+?)| (.+?))?(?: karo)?$`)
)

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[,.!?']`).ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func scopeOf(word string) string {
	switch word {
	case "running", "active", "pending", "in progress", "open":
		return "active"
	case "finished", "completed", "done":
		return "done"
	case "failed", "cancelled", "canceled":
		return "failed"
	case "old":
		return "old"
	}
	return "all"
}

func isApprovalListQuery(s string) bool {
	switch s {
	case "approvals", "my approvals", "all approvals", "pending approvals", "open approvals",
		"list approvals", "show approvals", "check approvals", "give me approvals",
		"tell me approvals", "any approvals", "what approvals", "approvals batao",
		"mere approvals batao", "kya koi approval hai", "kya koi approvals hai":
		return true
	}
	return false
}

func isNonListCommand(s string) bool {
	if s == "tasks" || s == "what is a task" || s == "what is task" || s == "explain task" || s == "define task" || s == "how do tasks work" {
		return true
	}
	for _, p := range []string{"cancel", "stop", "delete", "remove", "clear", "clean up", "create", "add task", "new task", "start task"} {
		if s == p || strings.HasPrefix(s, p+" ") {
			return true
		}
	}
	return false
}

func isTaskListQuery(s string) (bool, string) {
	// 1. Idiomatic queries for running state without "task"
	switch s {
	case "what s running", "what is running", "what s in progress", "what is in progress",
		"what s open", "what is open", "kya chal raha hai", "kya chal rha hai":
		return true, "active"
	}

	// 2. Exact phrases with scope
	switch s {
	case "open tasks", "running tasks", "active tasks", "pending tasks",
		"any open tasks", "any running tasks", "any active tasks", "any pending tasks",
		"open task", "running task", "active task", "pending task",
		"any open task", "any running task", "any active task", "any pending task":
		return true, "active"
	case "completed tasks", "finished tasks", "done tasks",
		"any completed tasks", "any finished tasks", "any done tasks",
		"completed task", "finished task", "done task",
		"any completed task", "any finished task", "any done task":
		return true, "done"
	case "failed tasks", "cancelled tasks", "canceled tasks",
		"any failed tasks", "any cancelled tasks", "any canceled tasks",
		"failed task", "cancelled task", "canceled task",
		"any failed task", "any cancelled task", "any canceled task":
		return true, "failed"
	case "my tasks", "all tasks", "the tasks", "task list", "tasks list",
		"list tasks", "list of tasks", "the list of tasks", "a list of tasks",
		"my task", "the task", "list task", "list of task", "the list of task", "a list of task",
		"give me the list of task", "give me the list of tasks", "give me a list of task", "give me a list of tasks",
		"show me the list of task", "show me the list of tasks", "show my task", "show my tasks",
		"any tasks", "any task", "my task list", "tasks batao", "mere tasks batao",
		"mere tasks kya hai", "tasks kya hai", "kya tasks hai", "task batao",
		"mere task batao", "task kya hai":
		return true, "all"
	case "kya tasks chal rahe hai", "chal rahe tasks":
		return true, "active"
	}

	// 3. Status queries
	if reTaskStatus.MatchString(s) {
		return true, "all"
	}

	// 4. Request queries
	if m := reList.FindStringSubmatch(s); m != nil {
		return true, scopeOf(m[1])
	}

	// 5. Question queries
	if m := reListQuestions.FindStringSubmatch(s); m != nil {
		w := m[1]
		if w == "" {
			w = m[2]
		}
		return true, scopeOf(w)
	}

	return false, ""
}

// ParseTaskCommand recognises task and approval requests. It is strict: a sentence only matches when it starts like
// the command, so ordinary conversation is never turned into an action.
func ParseTaskCommand(text string) TaskCommand {
	s := norm(text)
	if s == "" {
		return TaskCommand{}
	}

	// Approvals queries and decisions
	if reApprList.MatchString(s) || isApprovalListQuery(s) {
		return TaskCommand{Kind: CmdListApprovals}
	}
	if m := reApprove.FindStringSubmatch(s); m != nil {
		ref := strings.TrimSpace(m[1] + m[2])
		ref = strings.TrimSuffix(ref, " karo")
		if genericRef(ref) {
			ref = ""
		}
		return TaskCommand{Kind: CmdApprove, Ref: ref}
	}
	if m := reReject.FindStringSubmatch(s); m != nil {
		ref := strings.TrimSpace(m[1] + m[2])
		ref = strings.TrimSuffix(ref, " karo")
		if genericRef(ref) {
			ref = ""
		}
		return TaskCommand{Kind: CmdReject, Ref: ref}
	}

	// Task cancellations and deletions
	if reCancelAll.MatchString(s) {
		return TaskCommand{Kind: CmdCancelTasks, Scope: "active"}
	}
	if m := reDeleteSet.FindStringSubmatch(s); m != nil {
		return TaskCommand{Kind: CmdDeleteTasks, Scope: scopeOf(m[1])}
	}
	if m := reCancel.FindStringSubmatch(s); m != nil && !genericRef(m[1]) {
		return TaskCommand{Kind: CmdCancelTasks, Ref: m[1]}
	}
	if m := reDelete.FindStringSubmatch(s); m != nil && !genericRef(m[1]) {
		return TaskCommand{Kind: CmdDeleteTasks, Ref: m[1]}
	}

	// Negative guard: definitions or mutations are not listing commands
	if isNonListCommand(s) {
		return TaskCommand{}
	}

	// Task listing queries
	if ok, scope := isTaskListQuery(s); ok {
		return TaskCommand{Kind: CmdListTasks, Scope: scope}
	}

	return TaskCommand{}
}

// genericRef is a reference that names nothing ("it", "that"), which must not delete or cancel a guess.
func genericRef(ref string) bool {
	switch norm(ref) {
	case "", "it", "that", "this", "them", "me", "my", "everything", "all", "all tasks", "tasks", "task", "the task", "the tasks", "the", "a", "up", "approval", "the approval", "approvals", "karo":
		return true
	}
	return false
}

// ---- running a command ----

// VoiceStore is what the commands need from the task engine and the approvals.
type VoiceStore interface {
	ListTasks(ctx context.Context, f repository.TaskFilter) ([]*domain.Task, error)
	CancelTask(ctx context.Context, id uuid.UUID, reason string) error
	DeleteTask(ctx context.Context, id uuid.UUID) error
	UpdateTask(ctx context.Context, task *domain.Task) error
}

// VoiceApprovals is the approvals side.
type VoiceApprovals interface {
	ListApprovals(ctx context.Context, userID uuid.UUID, status *domain.ApprovalStatus) ([]*domain.Approval, error)
	RespondApproval(ctx context.Context, id, userID uuid.UUID, approved bool, reason string) (*domain.Approval, error)
}

// VoiceCommands runs parsed commands for one owner.
type VoiceCommands struct {
	Tasks     VoiceStore
	Approvals VoiceApprovals
	// Secret signs confirmation tokens. Empty means a random one for this process (tokens then do not survive a restart).
	Secret []byte
	Now    func() time.Time
	secret []byte
}

// Result is what to say, and, for a change, the token to send back with the owner's yes or no.
type Result struct {
	Reply        string
	ConfirmToken string
	Draft        string
	// NotHandled means the sentence only looked like a task command (it named no task of the owner's), so the caller
	// should treat it as ordinary conversation or work instead of answering "I could not find it".
	NotHandled bool
}

const confirmTTL = 2 * time.Minute

func (v *VoiceCommands) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

func (v *VoiceCommands) key() []byte {
	if len(v.Secret) > 0 {
		return v.Secret
	}
	if v.secret == nil {
		v.secret = make([]byte, 32)
		_, _ = rand.Read(v.secret)
	}
	return v.secret
}

type confirmPayload struct {
	User   string   `json:"u"`
	Action string   `json:"a"`
	IDs    []string `json:"i"`
	Exp    int64    `json:"e"`
}

func (v *VoiceCommands) sign(p confirmPayload) string {
	raw, _ := json.Marshal(p)
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, v.key())
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (v *VoiceCommands) verify(token string, user uuid.UUID) (*confirmPayload, error) {
	i := strings.LastIndex(token, ".")
	if i < 0 {
		return nil, errors.New("that confirmation is not valid")
	}
	mac := hmac.New(sha256.New, v.key())
	mac.Write([]byte(token[:i]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(token[i+1:])) {
		return nil, errors.New("that confirmation is not valid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[:i])
	var p confirmPayload
	if err != nil || json.Unmarshal(raw, &p) != nil {
		return nil, errors.New("that confirmation is not valid")
	}
	if p.User != user.String() {
		return nil, errors.New("that confirmation is not valid")
	}
	if v.now().Unix() > p.Exp {
		return nil, errors.New("that took too long, so I did not do it. Ask again")
	}
	return &p, nil
}

func tidyTitle(t *domain.Task) string {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = strings.TrimSpace(t.Goal)
	}
	for _, p := range []string{"Chat: ", "Follow-up: "} {
		title = strings.TrimPrefix(title, p)
	}
	if i := strings.Index(title, " Orchestration: "); i > 0 && i <= 31 {
		title = title[i+len(" Orchestration: "):]
	}
	r := []rune(title)
	if len(r) > 60 {
		title = string(r[:60]) + "…"
	}
	return title
}

// topLevel are the tasks the owner sees in their list: not plan steps and not follow-ups.
func topLevel(tasks []*domain.Task) []*domain.Task {
	var out []*domain.Task
	for _, t := range tasks {
		if t.ParentID == nil && !domain.IsFollowUp(t) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func inScope(t *domain.Task, scope string, now time.Time) bool {
	switch scope {
	case "active":
		return !t.IsTerminal()
	case "done":
		return t.Status == domain.TaskStatusCompleted
	case "failed":
		return t.Status == domain.TaskStatusFailed || t.Status == domain.TaskStatusCancelled
	case "old":
		return t.IsTerminal() && now.Sub(t.UpdatedAt) > 7*24*time.Hour
	case "all":
		return true
	}
	return false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func spokenList(tasks []*domain.Task, limit int) string {
	if len(tasks) == 0 {
		return ""
	}
	var parts []string
	for i, t := range tasks {
		if i == limit {
			parts = append(parts, fmt.Sprintf("%d more", len(tasks)-limit))
			break
		}
		parts = append(parts, tidyTitle(t))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) == 2 {
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// match picks the tasks a reference names: every word of it appears in the title or goal. "last" and "latest" mean
// the newest. It returns all that fit, newest first, so the caller can ask when more than one does.
func match(ref string, tasks []*domain.Task) []*domain.Task {
	ref = norm(ref)
	if ref == "" {
		return nil
	}
	if ref == "last" || ref == "latest" || ref == "last one" || ref == "latest one" || ref == "newest" || ref == "newest one" {
		if len(tasks) > 0 {
			return tasks[:1]
		}
		return nil
	}
	words := strings.Fields(ref)
	var out []*domain.Task
	for _, t := range tasks {
		hay := norm(t.Title + " " + t.Goal)
		all := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				all = false
				break
			}
		}
		if all {
			out = append(out, t)
		}
	}
	return out
}

// Run executes a parsed command. unlocked says the owner is at an unlocked device: changes are refused otherwise,
// because a spoken yes carries no proof of who said it.
func (v *VoiceCommands) Run(ctx context.Context, user uuid.UUID, c TaskCommand, unlocked bool) (Result, error) {
	switch c.Kind {
	case CmdListTasks:
		return v.listTasks(ctx, user, c)
	case CmdListApprovals:
		return v.listApprovals(ctx, user)
	case CmdCancelTasks, CmdDeleteTasks:
		return v.changeTasks(ctx, user, c, unlocked)
	case CmdApprove, CmdReject:
		return v.decideApproval(ctx, user, c, unlocked)
	case CmdSteerTask:
		return v.steerTask(ctx, user, c)
	}
	return Result{}, errors.New("not a task command")
}

func isDraftRequest(s string) bool {
	return domain.DraftFirstRegex.MatchString(s)
}

func (v *VoiceCommands) steerTask(ctx context.Context, user uuid.UUID, c TaskCommand) (Result, error) {
	all, err := v.tasksOf(ctx, user)
	if err != nil {
		return Result{}, err
	}
	if len(all) == 0 {
		return Result{Reply: "You don't have any tasks right now."}, nil
	}

	var target *domain.Task
	if c.Scope != "" && c.Scope != "active" {
		if matched := match(c.Scope, all); len(matched) > 0 {
			target = matched[0]
		}
	}
	if target == nil && c.Ref != "" {
		if matched := match(c.Ref, all); len(matched) > 0 {
			target = matched[0]
		}
	}
	// Prefer active non-terminal task
	if target == nil {
		for _, t := range all {
			if !t.IsTerminal() {
				target = t
				break
			}
		}
	}
	// Fall back to newest task
	if target == nil {
		target = all[0]
	}

	isDraft := isDraftRequest(c.Ref) || isDraftRequest(c.Scope)
	if isDraft {
		if draft := domain.ResultText(target.OutputPayload); draft != "" {
			snip := draft
			if runes := []rune(snip); len(runes) > 160 {
				snip = string(runes[:157]) + "..."
			}
			return Result{
				Reply: fmt.Sprintf("Here is the draft: %s. Would you like me to approve and send it?", snip),
				Draft: draft,
			}, nil
		}

		target.AppendSteeringNote("User requested draft first: prepare draft only and hold outward send for approval.")
		target.SetDraftOnly()
		if v.Tasks != nil {
			_ = v.Tasks.UpdateTask(ctx, target)
			children, _ := v.Tasks.ListTasks(ctx, repository.TaskFilter{UserID: &user, ParentID: &target.ID})
			for _, ch := range children {
				if strings.Contains(strings.ToLower(ch.Title), "send") || strings.Contains(string(ch.InputPayload), domain.HostCapMessaging) || strings.Contains(ch.Title, "WhatsApp:") || strings.Contains(ch.Title, "Instagram:") {
					ch.Title = "[Draft Only] " + ch.Title
					ch.SetDraftOnly()
					_ = v.Tasks.UpdateTask(ctx, ch)
				}
			}
		}
		return Result{
			Reply: "Understood. I have updated the ongoing task to draft it first and hold the send. I will show you the draft before anything is sent.",
		}, nil
	}

	note := c.Ref
	if note == "" {
		note = c.Scope
	}
	if note != "" {
		target.AppendSteeringNote(note)
		if v.Tasks != nil {
			_ = v.Tasks.UpdateTask(ctx, target)
		}
		return Result{
			Reply: fmt.Sprintf("Got it. I've updated the ongoing task with: %q.", note),
		}, nil
	}

	return Result{Reply: "Understood."}, nil
}

func (v *VoiceCommands) tasksOf(ctx context.Context, user uuid.UUID) ([]*domain.Task, error) {
	all, err := v.Tasks.ListTasks(ctx, repository.TaskFilter{UserID: &user, Limit: 300})
	if err != nil {
		return nil, err
	}
	return topLevel(all), nil
}

func (v *VoiceCommands) listTasks(ctx context.Context, user uuid.UUID, c TaskCommand) (Result, error) {
	tasks, err := v.tasksOf(ctx, user)
	if err != nil {
		return Result{}, err
	}
	now := v.now()
	if c.Scope == "all" {
		var active, done, failed []*domain.Task
		for _, t := range tasks {
			switch {
			case !t.IsTerminal():
				active = append(active, t)
			case t.Status == domain.TaskStatusCompleted:
				done = append(done, t)
			default:
				failed = append(failed, t)
			}
		}
		if len(tasks) == 0 {
			return Result{Reply: "You have no tasks."}, nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "You have %s.", plural(len(tasks), "task", "tasks"))
		if len(active) > 0 {
			fmt.Fprintf(&b, " Running: %s.", spokenList(active, 4))
		}
		fmt.Fprintf(&b, " %d finished", len(done))
		if len(failed) > 0 {
			fmt.Fprintf(&b, " and %d failed or cancelled", len(failed))
		}
		b.WriteString(".")
		return Result{Reply: b.String()}, nil
	}
	var picked []*domain.Task
	for _, t := range tasks {
		if inScope(t, c.Scope, now) {
			picked = append(picked, t)
		}
	}
	word := map[string]string{"active": "running", "done": "finished", "failed": "failed or cancelled", "old": "old"}[c.Scope]
	if len(picked) == 0 {
		return Result{Reply: fmt.Sprintf("You have no %s tasks.", word)}, nil
	}
	return Result{Reply: fmt.Sprintf("You have %s %s: %s.", plural(len(picked), "task", "tasks"), word, spokenList(picked, 5))}, nil
}

func (v *VoiceCommands) changeTasks(ctx context.Context, user uuid.UUID, c TaskCommand, unlocked bool) (Result, error) {
	cancel := c.Kind == CmdCancelTasks
	verb, action := "Delete", "delete"
	if cancel {
		verb, action = "Cancel", "cancel"
	}
	if !unlocked {
		return Result{Reply: fmt.Sprintf("Unlock your phone first. I only %s tasks when it is unlocked.", action)}, nil
	}
	tasks, err := v.tasksOf(ctx, user)
	if err != nil {
		return Result{}, err
	}
	now := v.now()
	var targets []*domain.Task
	desc := ""
	if c.Ref != "" {
		found := match(c.Ref, tasks)
		var eligible []*domain.Task
		for _, t := range found {
			if (cancel && !t.IsTerminal()) || (!cancel && t.IsTerminal()) {
				eligible = append(eligible, t)
			}
		}
		switch {
		case len(found) == 0:
			return Result{NotHandled: true}, nil
		case len(eligible) == 0 && cancel:
			return Result{Reply: fmt.Sprintf("%s has already finished, so there is nothing to cancel.", tidyTitle(found[0]))}, nil
		case len(eligible) == 0:
			return Result{Reply: fmt.Sprintf("%s is still running. Cancel it first, then delete it.", tidyTitle(found[0]))}, nil
		case len(eligible) > 1:
			return Result{Reply: fmt.Sprintf("I found %d tasks like that: %s. Say which one.", len(eligible), spokenList(eligible, 3))}, nil
		}
		targets, desc = eligible, tidyTitle(eligible[0])
	} else {
		for _, t := range tasks {
			if inScope(t, c.Scope, now) && ((cancel && !t.IsTerminal()) || (!cancel && t.IsTerminal())) {
				targets = append(targets, t)
			}
		}
		if len(targets) == 0 {
			return Result{Reply: "There is nothing like that to " + action + "."}, nil
		}
		desc = plural(len(targets), "task", "tasks")
		if c.Scope != "all" && c.Scope != "" {
			desc = plural(len(targets), map[string]string{"active": "running", "done": "finished", "failed": "failed", "old": "old"}[c.Scope]+" task", map[string]string{"active": "running", "done": "finished", "failed": "failed", "old": "old"}[c.Scope]+" tasks")
		}
	}
	ids := make([]string, len(targets))
	for i, t := range targets {
		ids[i] = t.ID.String()
	}
	token := v.sign(confirmPayload{User: user.String(), Action: action, IDs: ids, Exp: v.now().Add(confirmTTL).Unix()})
	return Result{Reply: fmt.Sprintf("%s %s? Say yes to confirm.", verb, desc), ConfirmToken: token}, nil
}

func (v *VoiceCommands) pending(ctx context.Context, user uuid.UUID) ([]*domain.Approval, error) {
	st := domain.ApprovalStatusPending
	return v.Approvals.ListApprovals(ctx, user, &st)
}

func (v *VoiceCommands) listApprovals(ctx context.Context, user uuid.UUID) (Result, error) {
	a, err := v.pending(ctx, user)
	if err != nil {
		return Result{}, err
	}
	if len(a) == 0 {
		return Result{Reply: "Nothing is waiting for your approval."}, nil
	}
	parts := make([]string, 0, 3)
	for i, x := range a {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("%d more", len(a)-3))
			break
		}
		parts = append(parts, strings.TrimSpace(x.Summary))
	}
	var formatted string
	if len(parts) == 1 {
		formatted = parts[0]
	} else if len(parts) == 2 {
		formatted = parts[0] + " and " + parts[1]
	} else {
		formatted = strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
	return Result{Reply: fmt.Sprintf("%s waiting for your approval: %s.", plural(len(a), "approval is", "approvals are"), formatted)}, nil
}

func (v *VoiceCommands) decideApproval(ctx context.Context, user uuid.UUID, c TaskCommand, unlocked bool) (Result, error) {
	verb, action := "Approve", "approve"
	if c.Kind == CmdReject {
		verb, action = "Reject", "reject"
	}
	if !unlocked {
		return Result{Reply: fmt.Sprintf("Unlock your phone first. I only %s things when it is unlocked.", action)}, nil
	}
	a, err := v.pending(ctx, user)
	if err != nil {
		return Result{}, err
	}
	if len(a) == 0 {
		return Result{Reply: "Nothing is waiting for your approval."}, nil
	}
	var chosen []*domain.Approval
	if c.Ref == "" {
		chosen = a
	} else {
		words := strings.Fields(norm(c.Ref))
		for _, x := range a {
			hay := norm(x.Summary)
			ok := true
			for _, w := range words {
				if !strings.Contains(hay, w) {
					ok = false
				}
			}
			if ok {
				chosen = append(chosen, x)
			}
		}
	}
	switch {
	case len(chosen) == 0:
		return Result{Reply: fmt.Sprintf("Nothing waiting matches %s.", c.Ref)}, nil
	case len(chosen) > 1:
		titles := make([]string, 0, 3)
		for i, x := range chosen {
			if i == 3 {
				break
			}
			titles = append(titles, strings.TrimSpace(x.Summary))
		}
		return Result{Reply: fmt.Sprintf("%d things are waiting: %s. Say which one.", len(chosen), strings.Join(titles, "; "))}, nil
	}
	var draft string
	if len(chosen[0].ActionPayload) > 0 {
		var ap map[string]any
		if err := json.Unmarshal(chosen[0].ActionPayload, &ap); err == nil {
			if t, ok := ap["text"].(string); ok && t != "" {
				draft = t
			}
		}
	}
	if draft == "" {
		draft = strings.TrimSpace(chosen[0].Summary)
	}
	token := v.sign(confirmPayload{User: user.String(), Action: action, IDs: []string{chosen[0].ID.String()}, Exp: v.now().Add(confirmTTL).Unix()})
	return Result{
		Reply:        fmt.Sprintf("%s: %s? Say yes to confirm.", verb, strings.TrimSpace(chosen[0].Summary)),
		ConfirmToken: token,
		Draft:        draft,
	}, nil
}

// Confirm carries out a change the owner agreed to, or drops it. The token must be genuine, theirs and fresh.
func (v *VoiceCommands) Confirm(ctx context.Context, user uuid.UUID, token string, yes, unlocked bool) (Result, error) {
	p, err := v.verify(token, user)
	if err != nil {
		return Result{Reply: err.Error() + "."}, nil
	}
	if !yes {
		return Result{Reply: "Okay, I will leave it."}, nil
	}
	if !unlocked {
		return Result{Reply: "Unlock your phone first, then ask again."}, nil
	}
	done, failed := 0, 0
	for _, id := range p.IDs {
		u, err := uuid.Parse(id)
		if err != nil {
			failed++
			continue
		}
		switch p.Action {
		case "cancel":
			err = v.Tasks.CancelTask(ctx, u, "cancelled by voice")
		case "delete":
			err = v.Tasks.DeleteTask(ctx, u)
		case "approve", "reject":
			_, err = v.Approvals.RespondApproval(ctx, u, user, p.Action == "approve", "decided by voice")
		default:
			err = errors.New("unknown action")
		}
		if err != nil {
			failed++
		} else {
			done++
		}
	}
	verb := map[string]string{"cancel": "Cancelled", "delete": "Deleted", "approve": "Approved", "reject": "Rejected"}[p.Action]
	switch {
	case done == 0:
		return Result{Reply: "I could not do that."}, nil
	case failed > 0:
		return Result{Reply: fmt.Sprintf("%s %d, but %d could not be changed.", verb, done, failed)}, nil
	case done == 1:
		return Result{Reply: verb + "."}, nil
	}
	return Result{Reply: fmt.Sprintf("%s %d.", verb, done)}, nil
}
