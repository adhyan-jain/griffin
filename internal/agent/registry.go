package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

// ApprovalHolder puts an exact payload on the owner's Approvals page. The engine implements it.
type ApprovalHolder interface {
	HoldAgentAction(ctx context.Context, userID uuid.UUID, actionType, summary string, payload []byte) (*domain.Approval, error)
}

// Registry is the set of typed tools.
type Registry struct {
	tools map[string]*Tool
	// Audit writes a line to a task's event log; may be nil.
	Audit func(ctx context.Context, taskID uuid.UUID, message string)
}

// NewRegistry validates and registers tools. External tools are registered as agent approval types, so an approved
// one is finished (never queued for a worker).
func NewRegistry(tools ...*Tool) (*Registry, error) {
	r := &Registry{tools: map[string]*Tool{}}
	for _, t := range tools {
		if t.Name == "" || t.Description == "" {
			return nil, fmt.Errorf("agent: a tool needs a name and a description")
		}
		if _, dup := r.tools[t.Name]; dup {
			return nil, fmt.Errorf("agent: tool %q registered twice", t.Name)
		}
		switch t.Effect {
		case EffectRead, EffectInternal:
			if t.Run == nil {
				return nil, fmt.Errorf("agent: tool %q has no Run", t.Name)
			}
		case EffectExternal:
			if t.Prepare == nil || t.Exec == nil {
				return nil, fmt.Errorf("agent: external tool %q needs Prepare and Exec", t.Name)
			}
			domain.RegisterAgentAction(t.Name)
		default:
			return nil, fmt.Errorf("agent: tool %q has no effect class", t.Name)
		}
		r.tools[t.Name] = t
	}
	return r, nil
}

// Get returns a tool by name.
func (r *Registry) Get(name string) *Tool { return r.tools[name] }

// Names lists the tool names in order.
func (r *Registry) Names() []string {
	n := make([]string, 0, len(r.tools))
	for k := range r.tools {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// Declarations is what the model is told it can call.
func (r *Registry) Declarations() []gemini.FunctionDecl {
	out := make([]gemini.FunctionDecl, 0, len(r.tools))
	for _, name := range r.Names() {
		t := r.tools[name]
		d := gemini.FunctionDecl{Name: t.Name, Description: t.Description}
		if props, ok := t.Schema["properties"].(map[string]any); ok && len(props) > 0 {
			d.Parameters = t.Schema
		}
		out = append(out, d)
	}
	return out
}

// Decided runs an approved agent action: exactly the payload stored on the approval, once (an approval can be
// decided only once). Rejection runs nothing. It ignores approvals that are not the agent's.
func (r *Registry) Decided(ctx context.Context, a *domain.Approval, approved bool) {
	t := r.tools[a.ActionType]
	if t == nil || t.Effect != EffectExternal {
		return
	}
	if !approved {
		r.audit(ctx, a.TaskID, "Agent action rejected by the owner: "+a.Summary)
		return
	}
	done, err := t.Exec(ctx, a.UserID, a.ActionPayload)
	if err != nil {
		slog.Error("agent: approved action failed", "tool", t.Name, "approval_id", a.ID, "error", err)
		r.audit(ctx, a.TaskID, fmt.Sprintf("Approved action %s failed: %v", t.Name, err))
		return
	}
	r.audit(ctx, a.TaskID, "Approved action done: "+done)
}

func (r *Registry) audit(ctx context.Context, taskID uuid.UUID, msg string) {
	if r.Audit != nil {
		r.Audit(ctx, taskID, msg)
	}
}

// Undo reverses one earlier action once. It returns what was undone.
func (r *Registry) undo(ctx context.Context, userID uuid.UUID, rec UndoRecord) (string, error) {
	t := r.tools[rec.Tool]
	if t == nil || t.Undo == nil {
		return "", ErrNoUndo
	}
	return t.Undo(ctx, userID, rec.Data)
}
