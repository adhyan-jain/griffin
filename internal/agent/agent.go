// Package agent is JJ's tool-using brain for chat: the model only picks typed tools from a registry, and every
// guard (seen ids, who may send what, grounding of the reply) lives here in code, never in prompt text.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/provider/gemini"
)

// Effect says what a tool can change.
type Effect string

const (
	// EffectRead only reads.
	EffectRead Effect = "read"
	// EffectInternal changes Griffin's own data and returns an undo record. It never asks.
	EffectInternal Effect = "internal"
	// EffectExternal leaves Griffin (a message, a mail, a call, a submit, a push).
	EffectExternal Effect = "external"
)

// Origin says who started the run. The model cannot set it: the caller of Agent.Run does.
type Origin string

const (
	// OriginOwnerChat is the owner's own message in this turn: an external tool runs at once.
	OriginOwnerChat Origin = "owner_chat"
	// OriginScheduled is a routine or long job acting alone: an external tool only makes an Approval.
	OriginScheduled Origin = "scheduled"
)

// Handoff asks the caller to start a long job from Goal instead of answering inline.
type Handoff struct {
	Goal string `json:"goal"`
}

// Output is what a tool returns.
type Output struct {
	// Text goes back to the model. Large text is capped and the rest kept behind a read_more handle.
	Text string
	// Summary words what a write did, for the owner and for the grounding check. Empty means "not a write".
	Summary string
	// UndoData, when set, makes the action undoable: it is handed back to the tool's Undo.
	UndoData json.RawMessage
	// IDs are the ids this output shows the model; later write tools accept only these.
	IDs []string
	// Untrusted marks text from the open web: it is wrapped as data.
	Untrusted bool
	// NoStash keeps the output from being capped into a handle (read_more itself).
	NoStash bool
	// Handoff ends the run and hands the goal to the caller.
	Handoff *Handoff
	// Wake says work was queued and the runner should be signalled.
	Wake bool
}

// Tool is one typed capability.
type Tool struct {
	Name        string
	Description string
	// Schema is an OpenAPI-style object schema (nil for no arguments).
	Schema map[string]any
	Effect Effect
	// OwnerOnly tools refuse to run in a scheduled run (the owner alone decides approvals, for one).
	OwnerOnly bool
	// HoldAfterUntrusted makes an external tool wait for approval, even in the owner's chat, when an earlier tool in the
	// same turn returned untrusted text (a prompt-injection guard for tools that run commands).
	HoldAfterUntrusted bool
	// Run executes a read or internal tool.
	Run func(ctx context.Context, c *Call, args json.RawMessage) (Output, error)
	// Undo reverses an internal tool's action from the data its Run returned.
	Undo func(ctx context.Context, userID uuid.UUID, data json.RawMessage) (string, error)
	// Prepare validates an external tool's arguments and returns the exact payload to execute and a summary of it.
	Prepare func(args json.RawMessage) (payload json.RawMessage, summary string, err error)
	// Exec carries out an external payload and says what was done. It runs the payload and nothing else.
	Exec func(ctx context.Context, userID uuid.UUID, payload json.RawMessage) (string, error)
}

// Call is the per-run environment a tool sees.
type Call struct {
	UserID uuid.UUID
	Origin Origin
	Now    time.Time
	TurnID uuid.UUID
	conv   *Conversation
}

// Seen reports whether a read tool (or the context) showed this id in this conversation.
func (c *Call) Seen(id string) bool { return c.conv.WorkingSet.has(id) }

// RequireSeen refuses an id the conversation never saw.
func (c *Call) RequireSeen(id string) error {
	if !c.Seen(id) {
		return fmt.Errorf("id %q was not returned by a list tool in this conversation; list first and use an id from the list", id)
	}
	return nil
}

// ParseSeenID parses an id and requires that it was seen.
func (c *Call) ParseSeenID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if err := c.RequireSeen(raw); err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("id %q is not a valid id", raw)
	}
	return id, nil
}

// Handle returns the stored text behind a read_more handle.
func (c *Call) Handle(h string) (string, bool) {
	s, ok := c.conv.WorkingSet.Handles[h]
	return s, ok
}

const (
	maxSeenIDs     = 500
	maxHandles     = 5
	maxHandleChars = 40000
)

// WorkingSet is the ids a conversation has seen and the large results kept for read_more.
type WorkingSet struct {
	IDs           []string          `json:"ids,omitempty"`
	Handles       map[string]string `json:"handles,omitempty"`
	HandleOrder   []string          `json:"handle_order,omitempty"`
	SummarizedSeq int64             `json:"summarized_seq,omitempty"`
	// Job is the state of a long job running in this conversation (see jobs.go); chats never set it.
	Job *JobState `json:"job,omitempty"`
}

func (w *WorkingSet) has(id string) bool {
	for _, x := range w.IDs {
		if x == id {
			return true
		}
	}
	return false
}

func (w *WorkingSet) mark(ids ...string) {
	for _, id := range ids {
		if id != "" && !w.has(id) {
			w.IDs = append(w.IDs, id)
		}
	}
	if len(w.IDs) > maxSeenIDs {
		w.IDs = w.IDs[len(w.IDs)-maxSeenIDs:]
	}
}

func (w *WorkingSet) stash(text string) string {
	if w.Handles == nil {
		w.Handles = map[string]string{}
	}
	h := "h" + uuid.NewString()[:8]
	r := []rune(text)
	if len(r) > maxHandleChars {
		text = string(r[:maxHandleChars])
	}
	w.Handles[h] = text
	w.HandleOrder = append(w.HandleOrder, h)
	for len(w.HandleOrder) > maxHandles {
		delete(w.Handles, w.HandleOrder[0])
		w.HandleOrder = w.HandleOrder[1:]
	}
	return h
}

// Conversation is the server-side state of one chat.
type Conversation struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Summary    string
	WorkingSet WorkingSet
}

// UndoRecord is what is needed to reverse one action once.
type UndoRecord struct {
	Tool string          `json:"tool"`
	Data json.RawMessage `json:"data"`
	Used bool            `json:"used,omitempty"`
}

// Action is one thing the agent did in a turn.
type Action struct {
	ID      string      `json:"id"`
	Tool    string      `json:"tool"`
	Effect  Effect      `json:"effect"`
	Summary string      `json:"summary"`
	Pending bool        `json:"pending,omitempty"` // waiting for the owner's approval; nothing has happened yet
	Undo    *UndoRecord `json:"undo,omitempty"`
}

// ActionView is an Action as the client sees it.
type ActionView struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	UndoID  string `json:"undo_id,omitempty"`
	Pending bool   `json:"pending,omitempty"`
}

// View hides the undo data and offers Undo only while it can still run.
func (a Action) View() ActionView {
	v := ActionView{Tool: a.Tool, Summary: a.Summary, Pending: a.Pending}
	if a.Undo != nil && !a.Undo.Used {
		v.UndoID = a.ID
	}
	return v
}

// Turn is one stored message.
type Turn struct {
	ID             uuid.UUID
	Seq            int64
	ConversationID uuid.UUID
	UserID         uuid.UUID
	Role           string // "user" or "assistant"
	Text           string
	ToolCalls      json.RawMessage
	Actions        []Action
	CreatedAt      time.Time
}

// Memory is a fact the owner told JJ.
type Memory struct {
	ID        uuid.UUID
	Fact      string
	CreatedAt time.Time
}

// Store keeps conversations, turns, memory and gaps.
type Store interface {
	GetConversation(ctx context.Context, user, id uuid.UUID) (*Conversation, error) // nil when absent
	SaveConversation(ctx context.Context, c *Conversation) error
	AppendTurn(ctx context.Context, t *Turn) error
	RecentTurns(ctx context.Context, user, conv uuid.UUID, n int) ([]Turn, error)
	TurnsAfter(ctx context.Context, user, conv uuid.UUID, afterSeq int64) ([]Turn, error)
	ListMemory(ctx context.Context, user uuid.UUID, n int) ([]Memory, error)
	AddMemory(ctx context.Context, user uuid.UUID, fact string, sourceTurn uuid.UUID) (uuid.UUID, error)
	SetMemoryDeleted(ctx context.Context, user, id uuid.UUID, deleted bool) error
	AddGap(ctx context.Context, user uuid.UUID, request string) error
	// ClaimUndo marks the action's undo as used and returns it; ok is false when it is unknown, not undoable or
	// already used. ReleaseUndo reverts the claim when the undo failed.
	ClaimUndo(ctx context.Context, user uuid.UUID, actionID string) (rec UndoRecord, ok bool, err error)
	ReleaseUndo(ctx context.Context, user uuid.UUID, actionID string) error
}

// Model is the function-calling model. *gemini.Client satisfies it through ModelFunc.
type Model interface {
	Generate(ctx context.Context, system string, contents []gemini.Content, decls []gemini.FunctionDecl) (gemini.ToolReply, error)
}

// GeminiModel adapts a Gemini client.
type GeminiModel struct{ Client *gemini.Client }

// Generate implements Model.
func (g GeminiModel) Generate(ctx context.Context, system string, contents []gemini.Content, decls []gemini.FunctionDecl) (gemini.ToolReply, error) {
	return g.Client.ChatWithTools(ctx, system, contents, decls)
}

// Errors the caller can tell apart.
var (
	ErrTooManyRounds = errors.New("agent: the model kept calling tools without answering")
	ErrNoUndo        = errors.New("agent: nothing to undo for that id (unknown, not undoable, or already undone)")
)
