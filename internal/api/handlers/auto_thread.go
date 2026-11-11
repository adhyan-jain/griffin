package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// finishedAt is when a task reached its final state.
func finishedAt(t *domain.Task) time.Time {
	if t.CompletedAt != nil {
		return *t.CompletedAt
	}
	return t.UpdatedAt
}

func conversationOf(t *domain.Task) string {
	var p struct {
		Conversation string `json:"conversation_id"`
	}
	if len(t.InputPayload) > 0 {
		_ = json.Unmarshal(t.InputPayload, &p)
	}
	return p.Conversation
}

// threadGroup is one thread: its root and every task in it, newest last.
type threadGroup struct {
	Root  *domain.Task
	Tasks []*domain.Task
}

// groupThreads gathers the owner's recent top-level tasks into threads. Steps of a plan belong to their plan,
// and a thread whose root is no longer in the recent list is left out.
func groupThreads(tasks []*domain.Task) []*threadGroup {
	byID := map[uuid.UUID]*domain.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	groups := map[uuid.UUID]*threadGroup{}
	for _, t := range tasks {
		if t.ParentID != nil {
			continue
		}
		rootID, _ := domain.ThreadOf(t)
		root, ok := byID[rootID]
		if !ok {
			continue
		}
		g := groups[rootID]
		if g == nil {
			g = &threadGroup{Root: root}
			groups[rootID] = g
		}
		g.Tasks = append(g.Tasks, t)
	}
	out := make([]*threadGroup, 0, len(groups))
	for _, g := range groups {
		sort.SliceStable(g.Tasks, func(i, j int) bool { return g.Tasks[i].CreatedAt.Before(g.Tasks[j].CreatedAt) })
		out = append(out, g)
	}
	return out
}

// settled is the newest task of a thread when every task in it has finished; nil while any is still running,
// because a message must not be added to a thread that is mid-answer.
func (g *threadGroup) settled() *domain.Task {
	var latest *domain.Task
	for _, t := range g.Tasks {
		if !t.IsTerminal() {
			return nil
		}
		if latest == nil || finishedAt(t).After(finishedAt(latest)) {
			latest = t
		}
	}
	return latest
}

// inFlight returns the newest active, non-terminal task of a thread, or nil if all have finished.
func (g *threadGroup) inFlight() *domain.Task {
	var active *domain.Task
	for _, t := range g.Tasks {
		if !t.IsTerminal() {
			if active == nil || t.CreatedAt.After(active.CreatedAt) {
				active = t
			}
		}
	}
	return active
}

type steeringResult struct {
	Matched bool
	Note    string
	IsDraft bool
}

func isSteeringIntent(s string) bool {
	return domain.DraftFirstRegex.MatchString(s) || domain.SteeringAddRegex.MatchString(s)
}

func (h *JJChatHandler) isSteering(ctx context.Context, s string) steeringResult {
	if domain.DraftFirstRegex.MatchString(s) {
		return steeringResult{
			Matched: true,
			Note:    "User requested draft first: prepare draft only and hold outward send for approval.",
			IsDraft: true,
		}
	}
	if domain.SteeringAddRegex.MatchString(s) {
		return steeringResult{
			Matched: true,
			Note:    s,
			IsDraft: false,
		}
	}
	if h.classifier != nil {
		intent := h.classifier.Classify(ctx, s)
		if intent.Kind == service.IntentSteerTask {
			note := intent.Steering
			if note == "" {
				note = s
			}
			isDraft := domain.DraftFirstRegex.MatchString(s) || domain.DraftFirstRegex.MatchString(note)
			return steeringResult{
				Matched: true,
				Note:    note,
				IsDraft: isDraft,
			}
		}
	}
	return steeringResult{}
}

func (h *JJChatHandler) steerTaskWith(w http.ResponseWriter, r *http.Request, userID uuid.UUID, g *threadGroup, active *domain.Task, note string, isDraft bool, convID string) bool {
	if convID == "" {
		convID = conversationOf(active)
	}

	if isDraft {
		if draft := domain.ResultText(active.OutputPayload); draft != "" {
			httputil.JSON(w, http.StatusOK, map[string]any{
				"kind":            string(service.MessageKindConversation),
				"conversation_id": convID,
				"task_id":         active.ID.String(),
				"response":        fmt.Sprintf("Here is the draft: %s. Would you like me to approve and send it?", draft),
				"draft":           draft,
				"steered":         true,
				"steered_task_id": active.ID.String(),
			})
			return true
		}

		active.AppendSteeringNote("User requested draft first: prepare draft only and hold outward send for approval.")
		active.SetDraftOnly()
		_ = h.threads.UpdateTask(r.Context(), active)

		// Also find downstream children if this is an orchestration plan root
		children, _ := h.threads.ListTasks(r.Context(), repository.TaskFilter{UserID: &userID, ParentID: &g.Root.ID})
		for _, child := range children {
			if strings.Contains(strings.ToLower(child.Title), "send") || strings.Contains(string(child.InputPayload), domain.HostCapMessaging) {
				child.Title = "[Draft Only] " + child.Title
				child.SetDraftOnly()
				_ = h.threads.UpdateTask(r.Context(), child)
			}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"kind":            string(service.MessageKindConversation),
			"conversation_id": convID,
			"task_id":         active.ID.String(),
			"response":        "Understood. I have updated the ongoing task to draft it first and hold the send. I will show you the draft before anything is sent.",
			"steered":         true,
			"steered_task_id": active.ID.String(),
		})
		return true
	}

	active.AppendSteeringNote(note)
	_ = h.threads.UpdateTask(r.Context(), active)
	if g.Root.ID != active.ID {
		g.Root.AppendSteeringNote(note)
		_ = h.threads.UpdateTask(r.Context(), g.Root)
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"kind":            string(service.MessageKindConversation),
		"conversation_id": convID,
		"task_id":         active.ID.String(),
		"response":        fmt.Sprintf("Got it. I've updated the ongoing task with: %q.", note),
		"steered":         true,
		"steered_task_id": active.ID.String(),
	})
	return true
}

func (h *JJChatHandler) steerActiveTask(w http.ResponseWriter, r *http.Request, userID uuid.UUID, g *threadGroup, active *domain.Task, message string, convID string) bool {
	res := h.isSteering(r.Context(), message)
	if !res.Matched {
		return false
	}
	return h.steerTaskWith(w, r, userID, g, active, res.Note, res.IsDraft, convID)
}

func continuedInfo(g *threadGroup, latest *domain.Task, reason string) map[string]any {
	return map[string]any{"continued": map[string]any{"task_id": latest.ID, "root_id": g.Root.ID, "title": g.Root.Title, "reason": reason}}
}

// tryContinue decides whether message continues an earlier thread. It returns handled=true when it has answered
// the request by continuing one. Otherwise respExtra may carry a suggestion ("this might continue ...") that the
// caller adds to its own response. It never fails a message: anything unexpected means "treat it as new".
func (h *JJChatHandler) tryContinue(w http.ResponseWriter, r *http.Request, userID uuid.UUID, message string, req JJMessageRequest, managerName string, _ []chatTurn) (bool, map[string]any) {
	// 1. The owner chose to continue a specific task.
	if req.ContinueTaskID != "" {
		id, err := uuid.Parse(req.ContinueTaskID)
		if err != nil {
			httputil.ErrorJSON(w, chatValidation("continue_task_id must be a UUID"))
			return true, nil
		}
		target, err := h.threads.GetTaskForUser(r.Context(), id, userID)
		if err != nil {
			httputil.ErrorJSON(w, err)
			return true, nil
		}
		if !target.IsTerminal() {
			if isSteeringIntent(message) {
				tasks, _ := h.threads.ListTasks(r.Context(), repository.TaskFilter{UserID: &userID, Limit: threadScanLimit})
				groups := groupThreads(tasks)
				root, _ := domain.ThreadOf(target)
				for _, g := range groups {
					if g.Root.ID == root {
						if h.steerActiveTask(w, r, userID, g, target, message, req.ConversationID) {
							return true, nil
						}
					}
				}
			}
			httputil.ErrorJSON(w, fmt.Errorf("%w: wait for that task to finish before continuing it", domain.ErrConflict))
			return true, nil
		}
		root, _ := domain.ThreadOf(target)
		h.continueTask(w, r, userID, target, message, req.IdempotencyKey, managerName,
			map[string]any{"continued": map[string]any{"task_id": target.ID, "root_id": root, "title": target.Title, "reason": "you chose to continue it"}})
		return true, nil
	}

	tasks, err := h.threads.ListTasks(r.Context(), repository.TaskFilter{UserID: &userID, Limit: threadScanLimit})
	if err != nil {
		return false, nil
	}
	groups := groupThreads(tasks)

	// 2. A conversation the chat page is already holding is one thread.
	if conv := strings.TrimSpace(req.ConversationID); conv != "" {
		var newest *domain.Task
		for _, t := range tasks {
			if t.ParentID == nil && conversationOf(t) == conv && (newest == nil || t.CreatedAt.After(newest.CreatedAt)) {
				newest = t
			}
		}
		if newest != nil {
			rootID, _ := domain.ThreadOf(newest)
			for _, g := range groups {
				if g.Root.ID == rootID {
					if latest := g.settled(); latest != nil {
						h.continueTask(w, r, userID, latest, message, req.IdempotencyKey, managerName, continuedInfo(g, latest, "the same conversation"))
						return true, nil
					}
					// If the task is currently active and the user is steering it mid-flight:
					if active := g.inFlight(); active != nil {
						if h.steerActiveTask(w, r, userID, g, active, message, conv) {
							return true, nil
						}
					}
				}
			}
		}
		return false, nil // a new conversation, or one still answering: a task of its own, as before
	}

	// 2.5 Mid-flight steering of an active task even when conversation ID is omitted:
	if res := h.isSteering(r.Context(), message); res.Matched {
		for _, g := range groups {
			if active := g.inFlight(); active != nil {
				if h.steerTaskWith(w, r, userID, g, active, res.Note, res.IsDraft, "") {
					return true, nil
				}
			}
		}
	}

	// 3. Otherwise judge it from what was said and how recently.
	var cands []service.ThreadCandidate
	for _, g := range groups {
		if latest := g.settled(); latest != nil {
			cands = append(cands, service.ThreadCandidate{Root: g.Root, Latest: latest, Answer: answerOf(latest), Finished: finishedAt(latest)})
		}
	}
	d := service.DecideThread(message, time.Now(), cands)
	switch d.Action {
	case service.ThreadContinue:
		for _, g := range groups {
			if g.Root.ID == d.Candidate.Root.ID {
				h.continueTask(w, r, userID, d.Candidate.Latest, message, req.IdempotencyKey, managerName, continuedInfo(g, d.Candidate.Latest, strings.Join(d.Reasons, "; ")))
				return true, nil
			}
		}
	case service.ThreadMaybe:
		return false, map[string]any{"suggestion": map[string]any{"task_id": d.Candidate.Latest.ID, "title": d.Candidate.Root.Title, "reason": strings.Join(d.Reasons, "; ")}}
	}
	return false, nil
}
