package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// ThreadStore is the part of the task engine follow-ups need.
type ThreadStore interface {
	GetTaskForUser(ctx context.Context, taskID, userID uuid.UUID) (*domain.Task, error)
	ListTasks(ctx context.Context, filter repository.TaskFilter) ([]*domain.Task, error)
}

// WithThreads enables POST /api/v1/tasks/{id}/follow-ups.
func (h *JJChatHandler) WithThreads(t ThreadStore) *JJChatHandler {
	h.threads = t
	return h
}

const threadScanLimit = 500

// failedNote is what the continuation sees in place of an answer when the earlier task failed.
func failedNote(t *domain.Task) string {
	if t.FailureReason != nil && *t.FailureReason != "" {
		return "(That attempt failed: " + *t.FailureReason + ")"
	}
	return "(That attempt did not finish.)"
}

func answerOf(t *domain.Task) string {
	if txt := domain.ResultText(t.OutputPayload); txt != "" {
		return txt
	}
	if t.Status == domain.TaskStatusFailed || t.Status == domain.TaskStatusCancelled {
		return failedNote(t)
	}
	return ""
}

// threadHistory builds what a follow-up starts from: the root task's request and answer, then each earlier
// follow-up's, in order. Long threads keep their beginning and their most recent turns.
func threadHistory(root *domain.Task, followUps []*domain.Task) []chatTurn {
	var turns []chatTurn
	add := func(t *domain.Task) {
		turns = append(turns, chatTurn{Role: "user", Text: t.Goal})
		if a := answerOf(t); a != "" {
			turns = append(turns, chatTurn{Role: "assistant", Text: a})
		}
	}
	add(root)
	for _, f := range followUps {
		if f.IsTerminal() {
			add(f)
		}
	}
	if len(turns) > chatMaxHistoryTurns {
		keep := append([]chatTurn{}, turns[:2]...)
		turns = append(keep, turns[len(turns)-(chatMaxHistoryTurns-2):]...)
	}
	return turns
}

// PostFollowUp handles POST /api/v1/tasks/{id}/follow-ups {"message": "..."}. The message continues that
// task: it joins its thread and starts with the transcript so far. Plain questions become a chat step;
// requests for work go through the planner like any other, carrying the same thread.
func (h *JJChatHandler) PostFollowUp(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.threads == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: follow-ups are not available on this server", domain.ErrNotFound))
		return
	}
	targetID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, chatValidation("task id must be a UUID"))
		return
	}
	var req struct {
		Message        string  `json:"message"`
		IdempotencyKey *string `json:"idempotency_key,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	if hk := r.Header.Get("Idempotency-Key"); hk != "" && req.IdempotencyKey == nil {
		req.IdempotencyKey = &hk
	}
	message := strings.TrimSpace(req.Message)
	switch {
	case message == "":
		httputil.ErrorJSON(w, chatValidation("message is required"))
		return
	case len([]rune(message)) > chatMaxMessageChars:
		httputil.ErrorJSON(w, chatValidation("message must be at most %d characters", chatMaxMessageChars))
		return
	}
	if req.IdempotencyKey != nil && len(*req.IdempotencyKey) > chatMaxKeyChars {
		httputil.ErrorJSON(w, chatValidation("idempotency_key must be at most %d characters", chatMaxKeyChars))
		return
	}

	target, err := h.threads.GetTaskForUser(r.Context(), targetID, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if !target.IsTerminal() {
		httputil.ErrorJSON(w, fmt.Errorf("%w: wait for this task to finish (or cancel it) before following up", domain.ErrConflict))
		return
	}

	// The thread's root: a plan's step continues its plan; a follow-up continues its own thread.
	rootID, _ := domain.ThreadOf(target)
	if target.ParentID != nil {
		rootID = *target.ParentID
	}
	root := target
	if rootID != target.ID {
		if root, err = h.threads.GetTaskForUser(r.Context(), rootID, userID); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}

	all, err := h.threads.ListTasks(r.Context(), repository.TaskFilter{UserID: &userID, Limit: threadScanLimit})
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var followUps []*domain.Task
	for _, t := range all {
		if t.ID != root.ID && domain.IsFollowUp(t) {
			if tr, _ := domain.ThreadOf(t); tr == root.ID {
				followUps = append(followUps, t)
			}
		}
	}
	sort.SliceStable(followUps, func(i, j int) bool { return followUps[i].CreatedAt.Before(followUps[j].CreatedAt) })
	history := threadHistory(root, followUps)

	managerName := ResolveManagerName(r.Context(), h.profiles, userID)
	toClassify := message
	if stripped := service.StripLeadingName(message, managerName); stripped != "" {
		toClassify = stripped
	}
	kind := service.ClassifyMessage(toClassify)
	if h.objectives == nil {
		kind = service.MessageKindConversation
	}
	thread := map[string]any{domain.ThreadIDKey: root.ID.String(), domain.FollowUpOfKey: target.ID.String()}

	if kind == service.MessageKindObjective {
		h.postObjectiveIn(w, r, userID, root.ID, message, JJMessageRequest{Message: message}, history, managerName, thread)
		return
	}
	h.postConversationIn(w, r, userID, root.ID, message, history, req.IdempotencyKey, managerName, thread, "Follow-up: ")
}

var _ = errors.New
