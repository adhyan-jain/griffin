package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/provider/gemini"
)

const (
	defaultJobRounds  = 60
	defaultJobMaxAge  = 2 * time.Hour
	maxJobErrors      = 5
	minJobRound       = 3 * time.Second // a round is not started with less time than this left
	toolGrace         = 3 * time.Second // a tool that has started may run this long past the budget
	maxJobContents    = 40              // the goal plus the newest model/tool turns are kept
	jobSystemNote     = "\n\nYou are running a background job for the owner, not chatting. Work through the goal with tools, one step at a time. Anything that leaves Griffin only becomes an approval the owner decides later: do not wait for it, carry on with other work. When nothing else remains, answer with a short report of what was done and what is still waiting."
	jobLastRoundNote  = "This is the last step you have. Report now, from what the tool results showed: what is done and what is not."
	jobPendingHeading = "Waiting for your approval:"
)

// JobState is a long job's progress, kept in its own conversation after every round so the next tick resumes it.
type JobState struct {
	Goal      string           `json:"goal"`
	Contents  []gemini.Content `json:"contents,omitempty"` // the model/tool exchange so far, goal first
	Actions   []Action         `json:"actions,omitempty"`
	Rounds    int              `json:"rounds"`
	Errors    int              `json:"errors,omitempty"`
	Retried   bool             `json:"retried,omitempty"` // the one grounding retry has been used
	StartedAt time.Time        `json:"started_at"`
	Done      bool             `json:"done,omitempty"`
	Failed    string           `json:"failed,omitempty"`
	Reply     string           `json:"reply,omitempty"`
}

// JobInput is one slice of work on a job. Deadline ends this slice, not the job.
type JobInput struct {
	UserID         uuid.UUID
	ConversationID uuid.UUID // the job's own conversation
	Goal           string
	Deadline       time.Time
	MaxRounds      int           // total over the job's life; zero means 60
	MaxAge         time.Duration // total wall time since the job started; zero means 2 hours
}

// JobResult says where the job stands. Neither Done nor Failed means "continue on the next tick".
type JobResult struct {
	Done    bool
	Failed  string // the reason, when the job gave up
	Reply   string
	Actions []Action
	Rounds  int
}

// RunJob works on a long job until it finishes, fails, or the slice's deadline nears. The job always runs as
// OriginScheduled: an outward tool only makes an Approval for the owner, and the job carries on with other work.
func (a *Agent) RunJob(ctx context.Context, in JobInput) (*JobResult, error) {
	maxRounds, maxAge := in.MaxRounds, in.MaxAge
	if maxRounds <= 0 {
		maxRounds = defaultJobRounds
	}
	if maxAge <= 0 {
		maxAge = defaultJobMaxAge
	}
	conv, err := a.Store.GetConversation(ctx, in.UserID, in.ConversationID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		conv = &Conversation{ID: in.ConversationID, UserID: in.UserID}
	}
	now := a.now()
	js := conv.WorkingSet.Job
	if js == nil {
		goal := strings.TrimSpace(in.Goal)
		js = &JobState{Goal: goal, StartedAt: now, Contents: []gemini.Content{{Role: "user", Parts: []gemini.Part{{Text: "Job goal: " + goal}}}}}
		conv.WorkingSet.Job = js
	}
	if js.Done || js.Failed != "" {
		return jobResult(js), nil
	}
	rs := &run{call: &Call{UserID: in.UserID, Origin: OriginScheduled, Now: now, TurnID: uuid.New(), conv: conv}, actions: js.Actions}
	system := a.systemPrompt(ctx, conv, rs, now) + jobSystemNote
	rs.evidence.WriteString(system + "\n" + jobEvidence(js))
	decls := a.Registry.Declarations()

	save := func() error {
		js.Actions = rs.actions
		js.Contents = trimJobContents(js.Contents)
		return a.Store.SaveConversation(context.WithoutCancel(ctx), conv)
	}
	fail := func(reason string) (*JobResult, error) {
		js.Failed = reason
		if err := save(); err != nil {
			return nil, err
		}
		return jobResult(js), nil
	}
	for {
		if now := a.now(); now.Sub(js.StartedAt) > maxAge {
			return fail(fmt.Sprintf("it ran for more than %s without finishing", maxAge))
		}
		if js.Rounds >= maxRounds {
			return fail(fmt.Sprintf("it used all %d steps without finishing", maxRounds))
		}
		if ctx.Err() != nil || time.Until(in.Deadline) < minJobRound {
			if err := save(); err != nil {
				return nil, err
			}
			return jobResult(js), nil
		}
		contents, roundDecls := js.Contents, decls
		if js.Rounds == maxRounds-1 {
			// The last step offers no tools, so the owner gets a report rather than a failure.
			roundDecls = nil
			contents = append(append([]gemini.Content(nil), contents...), gemini.Content{Role: "user", Parts: []gemini.Part{{Text: jobLastRoundNote}}})
		}
		rctx, cancel := context.WithDeadline(ctx, in.Deadline)
		rep, err := a.Model.Generate(rctx, system, contents, roundDecls)
		cancel()
		if err != nil {
			if ctx.Err() != nil || time.Until(in.Deadline) <= 0 {
				continue // out of time, not a model failure: save and stop at the top of the loop
			}
			js.Errors++
			a.log().Warn("agent job: the model failed", "conversation_id", conv.ID, "errors", js.Errors, "error", err)
			if js.Errors >= maxJobErrors {
				return fail("the model kept failing: " + err.Error())
			}
			if err := save(); err != nil {
				return nil, err
			}
			return jobResult(js), nil
		}
		js.Errors = 0
		if len(rep.Calls) > 0 {
			js.Contents = append(js.Contents, rep.Raw)
			// A tool that has started finishes even if the budget ends: a half-run step could not be resumed.
			tctx, tcancel := context.WithTimeout(context.WithoutCancel(ctx), max(time.Until(in.Deadline), 0)+toolGrace)
			var parts []gemini.Part
			for i, c := range rep.Calls {
				var resp map[string]any
				if i >= maxCallsPerRound {
					resp = map[string]any{"error": "too many tool calls in one step; ask again for the rest"}
				} else {
					resp = a.execute(tctx, rs, c)
				}
				parts = append(parts, gemini.Part{FunctionResponse: &gemini.FunctionResponse{Name: c.Name, Response: resp}})
			}
			tcancel()
			js.Contents = append(js.Contents, gemini.Content{Role: "user", Parts: parts})
			js.Rounds++
			if err := save(); err != nil {
				return nil, err
			}
			continue
		}
		reply := strings.TrimSpace(rep.Text)
		if vs := Check(reply, rs.actions, rs.evidence.String()); len(vs) > 0 {
			if !js.Retried {
				js.Retried = true
				js.Contents = append(js.Contents, rep.Raw, gemini.Content{Role: "user", Parts: []gemini.Part{{Text: retryNote(vs)}}})
				js.Rounds++
				if err := save(); err != nil {
					return nil, err
				}
				continue
			}
			a.log().Warn("agent job: dropping unsupported sentences", "violations", len(vs))
			reply = DropUnsupported(reply, vs)
		}
		if reply == "" {
			reply = "The job ended without a report."
		}
		js.Reply = reply + pendingNote(rs.actions)
		js.Done = true
		js.Actions = rs.actions
		if err := a.finishJobConversation(ctx, conv, js); err != nil {
			return nil, err
		}
		return jobResult(js), nil
	}
}

// finishJobConversation records the job's goal and report as an ordinary exchange in its conversation.
func (a *Agent) finishJobConversation(ctx context.Context, conv *Conversation, js *JobState) error {
	ctx = context.WithoutCancel(ctx)
	js.Contents = nil // the exchange is no longer needed once the job is over
	if err := a.Store.SaveConversation(ctx, conv); err != nil {
		return err
	}
	if err := a.Store.AppendTurn(ctx, &Turn{ID: uuid.New(), ConversationID: conv.ID, UserID: conv.UserID, Role: "user", Text: js.Goal}); err != nil {
		return err
	}
	return a.Store.AppendTurn(ctx, &Turn{ID: uuid.New(), ConversationID: conv.ID, UserID: conv.UserID, Role: "assistant", Text: js.Reply})
}

func jobResult(js *JobState) *JobResult {
	return &JobResult{Done: js.Done, Failed: js.Failed, Reply: js.Reply, Actions: js.Actions, Rounds: js.Rounds}
}

// pendingNote lists, in code, what is still waiting for the owner, so the report cannot leave it out.
func pendingNote(actions []Action) string {
	var b strings.Builder
	for _, a := range actions {
		if a.Pending {
			b.WriteString("\n- " + a.Summary)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\n" + jobPendingHeading + b.String()
}

// jobEvidence is the text the grounding check may rely on: everything the job's tools showed so far.
func jobEvidence(js *JobState) string {
	var b strings.Builder
	for _, c := range js.Contents {
		for _, p := range c.Parts {
			b.WriteString(p.Text + "\n")
			if p.FunctionCall != nil {
				raw, _ := json.Marshal(p.FunctionCall.Args)
				b.WriteString(string(raw) + "\n")
			}
			if p.FunctionResponse != nil {
				raw, _ := json.Marshal(p.FunctionResponse.Response)
				b.WriteString(string(raw) + "\n")
			}
		}
	}
	for _, a := range js.Actions {
		b.WriteString(a.Summary + "\n")
	}
	return b.String()
}

// trimJobContents keeps the goal and the newest turns, starting the kept turns at a model turn so each function
// call stays followed by its response.
func trimJobContents(cs []gemini.Content) []gemini.Content {
	if len(cs) <= maxJobContents {
		return cs
	}
	tail := cs[len(cs)-(maxJobContents-1):]
	for len(tail) > 0 && (tail[0].Role != "model" || hasResponse(tail[0])) {
		tail = tail[1:]
	}
	return append([]gemini.Content{cs[0]}, tail...)
}

func hasResponse(c gemini.Content) bool {
	for _, p := range c.Parts {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
}
