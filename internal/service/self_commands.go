package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// Self commands: the owner changes Griffin itself from chat ("approve all pending", "cancel the Coy task", "pause
// the daily scan routine"). The owner typed it, so it is done at once, with no confirmation step and no model.
//
// ParseSelfCommand reads the message with fixed rules and returns one of a small, closed set of commands. Run then
// carries it out through the same service methods the web buttons use, on the owner's own items only. A named
// target that fits more than one item changes nothing; the reply lists the candidates instead. Anything that does
// not read clearly as one of these commands (a question, a negation, a condition, a long sentence) is left to the
// normal chat path.

// SelfCommandKind is one of the closed set of things the owner can command in chat.
type SelfCommandKind string

const (
	SelfNone              SelfCommandKind = ""
	SelfApproveAllPending SelfCommandKind = "approve_all_pending"
	SelfApprove           SelfCommandKind = "approve"
	SelfReject            SelfCommandKind = "reject"
	SelfCancelTask        SelfCommandKind = "cancel_task"
	SelfRetryTask         SelfCommandKind = "retry_task"
	SelfPauseRoutine      SelfCommandKind = "pause_routine"
	SelfResumeRoutine     SelfCommandKind = "resume_routine"
)

// SelfCommand is a parsed command. Ref is the words that name the target ("coy"), empty for approve-all.
type SelfCommand struct {
	Kind SelfCommandKind
	Ref  string
}

const selfMaxWords = 16

var (
	reSelfModalYou = regexp.MustCompile(`(?i)^(?:please |pls )?(?:can|could|would|will) you\b`)
	reSelfTokens   = regexp.MustCompile(`[\p{L}\p{N}]+`)
)

// selfQuestionLeads start a question, never a command.
var selfQuestionLeads = map[string]bool{
	"what": true, "why": true, "how": true, "should": true, "shall": true, "is": true, "are": true, "was": true,
	"were": true, "does": true, "do": true, "did": true, "which": true, "who": true, "whom": true, "when": true,
	"where": true, "whether": true, "if": true, "may": true, "might": true, "have": true, "has": true, "am": true,
}

// selfFillerPrefixes and selfFillerSuffixes are politeness around a command; they are stripped before reading it.
var selfFillerPrefixes = []string{
	"please", "pls", "plz", "kindly", "ok", "okay", "yes", "yeah", "yep", "yup", "hey", "so", "now", "just", "alright",
	"then", "go ahead and", "go ahead", "you can", "can you", "could you", "would you", "will you", "i want you to",
	"i want to", "i d like you to", "i would like you to", "i need you to", "let s", "lets",
}

var selfFillerSuffixes = []string{
	"right now", "please", "pls", "plz", "now", "karo", "kar do", "kardo", "krdo", "kar de", "for me", "thanks",
	"thank you", "thx", "immediately", "asap", "today",
}

// selfGuards make a sentence conditional or negative; such a sentence is never a command.
var selfGuards = map[string]bool{
	"except": true, "but": true, "unless": true, "without": true, "not": true, "don": true, "never": true, "if": true,
	"when": true, "until": true, "before": true, "after": true, "once": true, "whenever": true, "cannot": true, "nothing": true, "none": true, "nobody": true, "no": true,
	"can": true, "shouldn": true, "wouldn": true, "won": true,
}

// selfNoise are words that point at a target without naming it. They are dropped from a reference; what is left is
// what must appear in the item's own text.
var selfNoise = map[string]bool{
	"the": true, "a": true, "an": true, "my": true, "me": true, "one": true, "ones": true, "of": true, "for": true,
	"to": true, "that": true, "this": true, "it": true, "s": true, "task": true, "tasks": true, "approval": true,
	"approvals": true, "request": true, "requests": true, "message": true, "messages": true, "dm": true, "dms": true,
	"draft": true, "drafts": true, "action": true, "actions": true, "item": true, "items": true, "thing": true,
	"things": true, "pending": true, "waiting": true, "open": true, "are": true, "is": true, "currently": true,
	"still": true, "there": true, "and": true, "send": true, "go": true, "out": true, "only": true, "please": true,
	"named": true, "called": true, "titled": true, "from": true, "with": true, "about": true, "on": true, "in": true,
	"routine": true, "routines": true, "schedule": true, "schedules": true, "scheduled": true, "which": true,
	"them": true, "up": true, "again": true, "down": true,
}

var selfAllWords = map[string]bool{
	"all": true, "everything": true, "every": true, "everyone": true, "sab": true, "saare": true, "saari": true, "sabhi": true,
}

var (
	selfApproveVerbs = map[string]bool{"approve": true, "accept": true, "allow": true, "authorize": true, "authorise": true, "confirm": true}
	selfRejectVerbs  = map[string]bool{"reject": true, "decline": true, "deny": true, "dismiss": true, "refuse": true}
	selfCancelVerbs  = map[string]bool{"cancel": true, "abort": true, "kill": true, "stop": true}
	selfRetryVerbs   = map[string]bool{"retry": true, "rerun": true, "redo": true, "re-run": true, "re-try": true}
	selfPauseVerbs   = map[string]bool{"pause": true, "suspend": true, "disable": true, "halt": true, "stop": true, "deactivate": true}
	selfResumeVerbs  = map[string]bool{"resume": true, "unpause": true, "restart": true, "enable": true, "reactivate": true, "start": true}
)

func selfWords(s string) []string { return reSelfTokens.FindAllString(strings.ToLower(s), -1) }

// stripFillers removes polite lead-ins and tails ("please", "can you", "karo") until none are left.
func stripFillers(s string) string {
	for changed := true; changed; {
		changed = false
		for _, p := range selfFillerPrefixes {
			if strings.HasPrefix(s+" ", p+" ") && len(s) > len(p) {
				s, changed = strings.TrimSpace(s[len(p):]), true
			}
		}
		for _, p := range selfFillerSuffixes {
			if strings.HasSuffix(" "+s, " "+p) && len(s) > len(p) {
				s, changed = strings.TrimSpace(s[:len(s)-len(p)]), true
			}
		}
	}
	return s
}

// refOf is the words left after the pointing words are removed; they name the target.
func refOf(words []string) string {
	var keep []string
	for _, w := range words {
		if !selfNoise[w] && !selfAllWords[w] {
			keep = append(keep, w)
		}
	}
	return strings.Join(keep, " ")
}

func hasAllWord(words []string) bool {
	for _, w := range words {
		if selfAllWords[w] {
			return true
		}
	}
	return false
}

func hasWord(words []string, set ...string) bool {
	for _, w := range words {
		for _, x := range set {
			if w == x {
				return true
			}
		}
	}
	return false
}

// ParseSelfCommand recognises a self command, or returns SelfNone. It is strict on purpose: the sentence must start
// with the command verb (after politeness), must not be a question, a negation or a condition, and must be short.
func ParseSelfCommand(text string) SelfCommand {
	t := strings.TrimSpace(text)
	if t == "" || len(strings.Fields(t)) > selfMaxWords {
		return SelfCommand{}
	}
	// A question is not a command. "Can you approve all?" is a request in question form; "should I approve all?" is not.
	if strings.HasSuffix(t, "?") && !reSelfModalYou.MatchString(t) {
		return SelfCommand{}
	}
	s := norm(t)
	if first, _, _ := strings.Cut(s, " "); selfQuestionLeads[first] {
		return SelfCommand{}
	}
	s = strings.NewReplacer("re-run", "rerun", "re-try", "retry").Replace(stripFillers(s))
	// "sab approve karo": the all-word leads in Hinglish.
	if first, rest, ok := strings.Cut(s, " "); ok && selfAllWords[first] && selfApproveVerbs[strings.Fields(rest)[0]] && len(strings.Fields(rest)) == 1 {
		return SelfCommand{Kind: SelfApproveAllPending}
	}
	words := selfWords(s)
	if len(words) < 2 {
		return SelfCommand{}
	}
	for _, w := range words {
		if selfGuards[w] {
			return SelfCommand{}
		}
	}
	verb, rest := words[0], words[1:]
	if (verb == "turn" || verb == "switch") && len(rest) > 0 && (rest[0] == "off" || rest[0] == "on") {
		if rest[0] == "off" {
			verb = "pause"
		} else {
			verb = "resume"
		}
		rest = rest[1:]
	}

	// Routines first: "pause the daily scan routine". The noun is required, so "pause the music" is not a command.
	if hasWord(rest, "routine", "routines", "schedule", "schedules") {
		ref := refOf(rest)
		if ref == "" || hasAllWord(rest) {
			return SelfCommand{}
		}
		switch {
		case selfPauseVerbs[verb]:
			return SelfCommand{Kind: SelfPauseRoutine, Ref: ref}
		case selfResumeVerbs[verb]:
			return SelfCommand{Kind: SelfResumeRoutine, Ref: ref}
		}
		return SelfCommand{}
	}

	switch {
	case selfApproveVerbs[verb]:
		if hasAllWord(rest) {
			if refOf(rest) == "" {
				return SelfCommand{Kind: SelfApproveAllPending}
			}
			return SelfCommand{} // "approve all the Coy ones" is not supported; the normal path handles it
		}
		if ref := refOf(rest); ref != "" {
			return SelfCommand{Kind: SelfApprove, Ref: ref}
		}
	case selfRejectVerbs[verb]:
		if hasAllWord(rest) {
			return SelfCommand{}
		}
		if ref := refOf(rest); ref != "" {
			return SelfCommand{Kind: SelfReject, Ref: ref}
		}
	case selfCancelVerbs[verb]:
		if hasAllWord(rest) || hasWord(rest, "approval", "approvals") {
			return SelfCommand{}
		}
		if ref := refOf(rest); ref != "" {
			return SelfCommand{Kind: SelfCancelTask, Ref: ref}
		}
	case selfRetryVerbs[verb]:
		if hasAllWord(rest) {
			return SelfCommand{}
		}
		if ref := refOf(rest); ref != "" {
			return SelfCommand{Kind: SelfRetryTask, Ref: ref}
		}
	case (verb == "run" || verb == "try") && rest[len(rest)-1] == "again":
		if hasAllWord(rest) {
			return SelfCommand{}
		}
		if ref := refOf(rest); ref != "" {
			return SelfCommand{Kind: SelfRetryTask, Ref: ref}
		}
	case (verb == "resume" || verb == "restart") && hasWord(rest, "task"):
		if hasAllWord(rest) {
			return SelfCommand{}
		}
		if ref := refOf(rest); ref != "" {
			return SelfCommand{Kind: SelfRetryTask, Ref: ref}
		}
	}
	return SelfCommand{}
}

// ---- running a command ----

// SelfTasks is what the commands need from the task engine; TaskEngine satisfies it.
type SelfTasks interface {
	ListTasks(ctx context.Context, f repository.TaskFilter) ([]*domain.Task, error)
	CancelTask(ctx context.Context, id uuid.UUID, reason string) error
	ResumeTask(ctx context.Context, id uuid.UUID) error
}

// SelfRoutines is the owner's recurring schedules; the schedule repository satisfies it. nil switches routine
// commands off.
type SelfRoutines interface {
	ListSchedules(ctx context.Context, userID uuid.UUID) ([]*domain.Schedule, error)
	SetPaused(ctx context.Context, id uuid.UUID, userID uuid.UUID, paused bool) error
}

// SelfCommands carries out parsed self commands for one owner.
type SelfCommands struct {
	Tasks     SelfTasks
	Approvals VoiceApprovals
	Routines  SelfRoutines
	// Audit writes one line to the task's event log for each change made on a task or approval; may be nil.
	Audit func(ctx context.Context, taskID uuid.UUID, message string)
}

// SelfResult is what to tell the owner. Handled is false when the sentence only looked like a command (it named
// nothing of the owner's), so the caller should treat it as ordinary conversation. Wake says that work was queued
// and the runner should be signalled.
type SelfResult struct {
	Reply   string
	Handled bool
	Wake    bool
}

func (s *SelfCommands) audit(ctx context.Context, user, taskID uuid.UUID, kind SelfCommandKind, detail string) {
	slog.Info("self command", "user_id", user, "command", string(kind), "task_id", taskID, "detail", detail)
	if s.Audit != nil && taskID != uuid.Nil {
		s.Audit(ctx, taskID, fmt.Sprintf("Owner chat command (%s): %s", kind, detail))
	}
}

// refMatches reports whether every word of ref is a whole word of text. Whole words, so "coy" never matches "coyote".
func refMatches(ref, text string) bool {
	have := map[string]bool{}
	for _, w := range selfWords(text) {
		have[w] = true
	}
	words := selfWords(ref)
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if !have[w] {
			return false
		}
	}
	return true
}

func joinNames(names []string, limit int) string {
	if len(names) > limit {
		return strings.Join(names[:limit], "; ") + fmt.Sprintf("; and %d more", len(names)-limit)
	}
	return strings.Join(names, "; ")
}

func approvalName(a *domain.Approval) string {
	if s := strings.TrimSpace(a.Summary); s != "" {
		return s
	}
	return a.ActionType
}

// approvalText is what a reference is matched against: the summary and the text fields of the action payload (the
// recipient, the company), so "the Coy one" finds a message whose summary does not spell the name out.
func approvalText(a *domain.Approval) string {
	var b strings.Builder
	b.WriteString(a.Summary)
	collectStrings(a.ActionPayload, &b)
	return b.String()
}

func collectStrings(raw []byte, b *strings.Builder) {
	if len(raw) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return
	}
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			b.WriteString(" " + t)
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
}

// Run carries out a parsed command for user.
func (s *SelfCommands) Run(ctx context.Context, user uuid.UUID, c SelfCommand) (SelfResult, error) {
	switch c.Kind {
	case SelfApproveAllPending:
		return s.approveAll(ctx, user)
	case SelfApprove, SelfReject:
		return s.decideOne(ctx, user, c)
	case SelfCancelTask, SelfRetryTask:
		return s.changeTask(ctx, user, c)
	case SelfPauseRoutine, SelfResumeRoutine:
		return s.changeRoutine(ctx, user, c)
	}
	return SelfResult{}, errors.New("not a self command")
}

func (s *SelfCommands) pending(ctx context.Context, user uuid.UUID) ([]*domain.Approval, error) {
	st := domain.ApprovalStatusPending
	return s.Approvals.ListApprovals(ctx, user, &st)
}

func (s *SelfCommands) approveAll(ctx context.Context, user uuid.UUID) (SelfResult, error) {
	list, err := s.pending(ctx, user)
	if err != nil {
		return SelfResult{}, err
	}
	if len(list) == 0 {
		return SelfResult{Handled: true, Reply: "Nothing is waiting for your approval."}, nil
	}
	var done, failed []string
	for _, a := range list {
		if _, err := s.Approvals.RespondApproval(ctx, a.ID, user, true, "Approved by the owner in chat: approve all pending"); err != nil {
			failed = append(failed, approvalName(a))
			continue
		}
		done = append(done, approvalName(a))
		s.audit(ctx, user, a.TaskID, SelfApproveAllPending, "approved "+approvalName(a))
	}
	var reply string
	switch {
	case len(done) == 0:
		reply = fmt.Sprintf("I could not approve any of the %s: %s.", plural(len(failed), "pending approval", "pending approvals"), joinNames(failed, 10))
	default:
		reply = fmt.Sprintf("Approved %s: %s.", plural(len(done), "pending approval", "pending approvals"), joinNames(done, 10))
		if len(failed) > 0 {
			reply += fmt.Sprintf(" %d could not be approved: %s.", len(failed), joinNames(failed, 10))
		}
	}
	return SelfResult{Handled: true, Reply: reply, Wake: len(done) > 0}, nil
}

func (s *SelfCommands) decideOne(ctx context.Context, user uuid.UUID, c SelfCommand) (SelfResult, error) {
	approve := c.Kind == SelfApprove
	verb, past := "approve", "Approved"
	if !approve {
		verb, past = "reject", "Rejected"
	}
	list, err := s.pending(ctx, user)
	if err != nil {
		return SelfResult{}, err
	}
	if len(list) == 0 {
		return SelfResult{Handled: true, Reply: "Nothing is waiting for your approval, so I did not " + verb + " anything."}, nil
	}
	var found []*domain.Approval
	for _, a := range list {
		if refMatches(c.Ref, approvalText(a)) {
			found = append(found, a)
		}
	}
	switch {
	case len(found) == 0:
		names := make([]string, len(list))
		for i, a := range list {
			names[i] = approvalName(a)
		}
		return SelfResult{Handled: true, Reply: fmt.Sprintf("No pending approval matches %q, so I did not %s anything. Waiting: %s.", c.Ref, verb, joinNames(names, 5))}, nil
	case len(found) > 1:
		names := make([]string, len(found))
		for i, a := range found {
			names[i] = approvalName(a)
		}
		return SelfResult{Handled: true, Reply: fmt.Sprintf("%d pending approvals match %q, so I changed nothing: %s. Say which one.", len(found), c.Ref, joinNames(names, 5))}, nil
	}
	a := found[0]
	if _, err := s.Approvals.RespondApproval(ctx, a.ID, user, approve, fmt.Sprintf("%s by the owner in chat: %s %s", past, verb, c.Ref)); err != nil {
		return SelfResult{Handled: true, Reply: fmt.Sprintf("I could not %s %s: %v.", verb, approvalName(a), err)}, nil
	}
	s.audit(ctx, user, a.TaskID, c.Kind, strings.ToLower(past)+" "+approvalName(a))
	return SelfResult{Handled: true, Reply: fmt.Sprintf("%s: %s.", past, approvalName(a)), Wake: approve}, nil
}

// matchTasks finds the owner's top-level tasks a reference names: by title first, and by goal only if no title fits.
func matchTasks(ref string, tasks []*domain.Task) []*domain.Task {
	var byTitle, byGoal []*domain.Task
	for _, t := range tasks {
		switch {
		case refMatches(ref, t.Title):
			byTitle = append(byTitle, t)
		case refMatches(ref, t.Goal):
			byGoal = append(byGoal, t)
		}
	}
	if len(byTitle) > 0 {
		return byTitle
	}
	return byGoal
}

func (s *SelfCommands) changeTask(ctx context.Context, user uuid.UUID, c SelfCommand) (SelfResult, error) {
	all, err := s.Tasks.ListTasks(ctx, repository.TaskFilter{UserID: &user, Limit: 300})
	if err != nil {
		return SelfResult{}, err
	}
	cancel := c.Kind == SelfCancelTask
	found := matchTasks(c.Ref, topLevel(all))
	if len(found) == 0 {
		return SelfResult{}, nil // it named nothing of the owner's; ordinary conversation
	}
	var eligible []*domain.Task
	for _, t := range found {
		if (cancel && !t.IsTerminal()) || (!cancel && (t.Status == domain.TaskStatusFailed || t.Status == domain.TaskStatusCancelled)) {
			eligible = append(eligible, t)
		}
	}
	switch {
	case len(eligible) == 0 && cancel:
		return SelfResult{Handled: true, Reply: fmt.Sprintf("%s has already finished, so there is nothing to cancel.", tidyTitle(found[0]))}, nil
	case len(eligible) == 0:
		return SelfResult{Handled: true, Reply: fmt.Sprintf("%s is %s, not failed or cancelled, so there is nothing to retry.", tidyTitle(found[0]), strings.ToLower(string(found[0].Status)))}, nil
	case len(eligible) > 1:
		return SelfResult{Handled: true, Reply: fmt.Sprintf("%d tasks match %q, so I changed nothing: %s. Say which one.", len(eligible), c.Ref, spokenList(eligible, 5))}, nil
	}
	t := eligible[0]
	if cancel {
		if err := s.Tasks.CancelTask(ctx, t.ID, "cancelled by the owner in chat"); err != nil {
			return SelfResult{Handled: true, Reply: fmt.Sprintf("I could not cancel %s: %v.", tidyTitle(t), err)}, nil
		}
		s.audit(ctx, user, t.ID, c.Kind, "cancelled "+tidyTitle(t))
		return SelfResult{Handled: true, Reply: fmt.Sprintf("Cancelled task: %s.", tidyTitle(t))}, nil
	}
	if err := s.Tasks.ResumeTask(ctx, t.ID); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return SelfResult{Handled: true, Reply: fmt.Sprintf("I could not retry %s on its own: it is part of a larger plan or has already changed.", tidyTitle(t))}, nil
		}
		return SelfResult{Handled: true, Reply: fmt.Sprintf("I could not retry %s: %v.", tidyTitle(t), err)}, nil
	}
	s.audit(ctx, user, t.ID, c.Kind, "retried "+tidyTitle(t))
	return SelfResult{Handled: true, Reply: fmt.Sprintf("Retrying task: %s. It is queued again.", tidyTitle(t)), Wake: true}, nil
}

func (s *SelfCommands) changeRoutine(ctx context.Context, user uuid.UUID, c SelfCommand) (SelfResult, error) {
	if s.Routines == nil {
		return SelfResult{}, nil
	}
	pause := c.Kind == SelfPauseRoutine
	list, err := s.Routines.ListSchedules(ctx, user)
	if err != nil {
		return SelfResult{}, err
	}
	var found, eligible []*domain.Schedule
	for _, r := range list {
		if r.UserID == user && refMatches(c.Ref, r.Name) {
			found = append(found, r)
			if r.IsPaused != pause {
				eligible = append(eligible, r)
			}
		}
	}
	names := func(rs []*domain.Schedule) string {
		n := make([]string, len(rs))
		for i, r := range rs {
			n[i] = r.Name
		}
		return joinNames(n, 5)
	}
	state := map[bool]string{true: "paused", false: "running"}
	switch {
	case len(found) == 0:
		all := make([]string, 0, len(list))
		for _, r := range list {
			all = append(all, r.Name)
		}
		if len(all) == 0 {
			return SelfResult{Handled: true, Reply: "You have no routines."}, nil
		}
		return SelfResult{Handled: true, Reply: fmt.Sprintf("No routine matches %q. Your routines: %s.", c.Ref, joinNames(all, 8))}, nil
	case len(eligible) == 0:
		return SelfResult{Handled: true, Reply: fmt.Sprintf("%s is already %s.", found[0].Name, state[pause])}, nil
	case len(eligible) > 1:
		return SelfResult{Handled: true, Reply: fmt.Sprintf("%d routines match %q, so I changed nothing: %s. Say which one.", len(eligible), c.Ref, names(eligible))}, nil
	}
	r := eligible[0]
	if err := s.Routines.SetPaused(ctx, r.ID, user, pause); err != nil {
		return SelfResult{Handled: true, Reply: fmt.Sprintf("I could not change the routine %s: %v.", r.Name, err)}, nil
	}
	s.audit(ctx, user, uuid.Nil, c.Kind, map[bool]string{true: "paused ", false: "resumed "}[pause]+r.Name)
	if pause {
		return SelfResult{Handled: true, Reply: fmt.Sprintf("Paused routine: %s. It will not run until you resume it.", r.Name)}, nil
	}
	return SelfResult{Handled: true, Reply: fmt.Sprintf("Resumed routine: %s.", r.Name)}, nil
}
