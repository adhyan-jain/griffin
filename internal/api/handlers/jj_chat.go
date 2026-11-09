package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
)

const (
	chatMaxMessageChars = 8000
	chatMaxHistoryTurns = 10
	chatMaxTurnChars    = 2000
	chatMaxTitleChars   = 60
	chatMaxKeyChars     = 255

	capacityDetail = "AWS has no capacity for the worker instance right now; it will retry"
)

// ChatTaskStore is the slice of the task engine the chat endpoint needs.
type ChatTaskStore interface {
	CreateTask(ctx context.Context, task *domain.Task, dependencyIDs []uuid.UUID) (*domain.Task, error)
	GetTaskByIdempotencyKey(ctx context.Context, userID uuid.UUID, key string) (*domain.Task, error)
}

// ObjectiveSubmitter is the slice of the JJ orchestrator the chat endpoint needs.
type ObjectiveSubmitter interface {
	SubmitRequest(ctx context.Context, req *domain.UserRequest) (*domain.OrchestrationRecord, *domain.OrchestrationPlan, error)
	RunnableChildCount(ctx context.Context, orchID uuid.UUID) (int, error)
}

// outcomeWaker is implemented by runner.WakeupController; it lets the API say whether a start
// was signaled or skipped.
type outcomeWaker interface {
	Wake(ctx context.Context) (runner.WakeOutcome, error)
}

// JJChatHandler serves POST /api/v1/jj/messages: the single chat entry point. Conversational
// messages become one worker-executed chat task; objectives go through the orchestrator.
type JJChatHandler struct {
	tasks              ChatTaskStore
	objectives         ObjectiveSubmitter
	wakeup             RunnerWakeup
	scaleToZeroEnabled bool
	// profiles supplies the owner's chosen manager name; nil means the default name.
	profiles ProfileStore
	// threads enables follow-ups on an existing task; nil disables them.
	threads ThreadStore
}

// WithProfiles lets the handler address and answer as the manager the owner named.
func (h *JJChatHandler) WithProfiles(p ProfileStore) *JJChatHandler {
	h.profiles = p
	return h
}

// NewJJChatHandler constructs a JJChatHandler. objectives may be nil, in which case every
// message is handled as a conversation.
func NewJJChatHandler(tasks ChatTaskStore, objectives ObjectiveSubmitter, wakeup RunnerWakeup, scaleToZeroEnabled bool) *JJChatHandler {
	return &JJChatHandler{tasks: tasks, objectives: objectives, wakeup: wakeup, scaleToZeroEnabled: scaleToZeroEnabled}
}

type chatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// JJMessageRequest is the POST /api/v1/jj/messages body.
type JJMessageRequest struct {
	Message        string              `json:"message"`
	ConversationID string              `json:"conversation_id,omitempty"`
	History        []chatTurn          `json:"history,omitempty"`
	Priority       domain.TaskPriority `json:"priority,omitempty"`
	IdempotencyKey *string             `json:"idempotency_key,omitempty"`
}

type runnerOutcome struct {
	Wake   string `json:"wake"`
	Detail string `json:"detail,omitempty"`
}

func chatValidation(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrValidation, fmt.Sprintf(format, args...))
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// PostMessage handles POST /api/v1/jj/messages.
func (h *JJChatHandler) PostMessage(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req JJMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	if headerKey := r.Header.Get("Idempotency-Key"); headerKey != "" && req.IdempotencyKey == nil {
		req.IdempotencyKey = &headerKey
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
	if err := domain.ValidatePriority(req.Priority); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if req.IdempotencyKey != nil && len(*req.IdempotencyKey) > chatMaxKeyChars {
		httputil.ErrorJSON(w, chatValidation("idempotency_key must be at most %d characters", chatMaxKeyChars))
		return
	}
	conversationID := uuid.New()
	if req.ConversationID != "" {
		if conversationID, err = uuid.Parse(req.ConversationID); err != nil {
			httputil.ErrorJSON(w, chatValidation("conversation_id must be a UUID"))
			return
		}
	}
	history, err := normalizeHistory(req.History)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	managerName := ResolveManagerName(r.Context(), h.profiles, userID)
	// "Jarvis research X" is addressed to the manager; classify what was asked, but store the owner's own words.
	toClassify := message
	if stripped := service.StripLeadingName(message, managerName); stripped != "" {
		toClassify = stripped
	}

	kind, reason := service.ClassifyMessageWithReason(toClassify)
	if h.objectives == nil {
		kind, reason = service.MessageKindConversation, "no-orchestrator"
	}
	slog.Info("jj chat: message classified", "user_id", userID, "conversation_id", conversationID, "kind", kind, "reason", reason)

	if kind == service.MessageKindObjective {
		h.postObjective(w, r, userID, conversationID, message, req, history, managerName)
		return
	}
	h.postConversation(w, r, userID, conversationID, message, history, req.IdempotencyKey, managerName)
}

func normalizeHistory(in []chatTurn) ([]chatTurn, error) {
	out := make([]chatTurn, 0, len(in))
	for i, t := range in {
		if t.Role != "user" && t.Role != "assistant" {
			return nil, chatValidation(`history[%d]: role %q is not valid; use "user" or "assistant"`, i, t.Role)
		}
		text := truncateRunes(strings.TrimSpace(t.Text), chatMaxTurnChars)
		if text == "" {
			continue
		}
		out = append(out, chatTurn{Role: t.Role, Text: text})
	}
	if len(out) > chatMaxHistoryTurns {
		out = out[len(out)-chatMaxHistoryTurns:]
	}
	return out, nil
}

func (h *JJChatHandler) postConversation(w http.ResponseWriter, r *http.Request, userID, conversationID uuid.UUID, message string, history []chatTurn, key *string, managerName string) {
	h.postConversationIn(w, r, userID, conversationID, message, history, key, managerName, nil, "Chat: ")
}

// postConversationIn is postConversation for a message that may belong to a thread (extra holds the thread
// fields) and with its own title prefix.
func (h *JJChatHandler) postConversationIn(w http.ResponseWriter, r *http.Request, userID, conversationID uuid.UUID, message string, history []chatTurn, key *string, managerName string, extra map[string]any, titlePrefix string) {
	// A repeated idempotency key returns the original task; never a second one.
	if key != nil {
		if existing, err := h.tasks.GetTaskByIdempotencyKey(r.Context(), userID, *key); err == nil {
			h.writeDuplicate(w, existing, conversationID)
			return
		} else if !errors.Is(err, domain.ErrNotFound) {
			httputil.ErrorJSON(w, err)
			return
		}
	}

	body := map[string]any{
		"kind":            "chat",
		"conversation_id": conversationID.String(),
		"history":         history,
		"manager_name":    managerName,
	}
	for k, v := range extra {
		body[k] = v
	}
	payload, _ := json.Marshal(body)
	title := titlePrefix + truncateRunes(strings.Join(strings.Fields(message), " "), chatMaxTitleChars)
	task := &domain.Task{
		UserID:         userID,
		ManagerDomain:  "JJ",
		Title:          title,
		Goal:           message,
		InputPayload:   payload,
		Priority:       domain.TaskPriorityHighP1,
		MaxRetries:     domain.DefaultMaxRetries,
		IdempotencyKey: key,
	}
	created, err := h.tasks.CreateTask(r.Context(), task, nil)
	if err != nil {
		if key != nil && errors.Is(err, domain.ErrIdempotentConflict) { // lost a race with the same key
			if existing, gerr := h.tasks.GetTaskByIdempotencyKey(r.Context(), userID, *key); gerr == nil {
				h.writeDuplicate(w, existing, conversationID)
				return
			}
		}
		httputil.ErrorJSON(w, err)
		return
	}

	// The task is persisted; now wake the runner (never fails the request).
	runnerOut := h.wakeRunner(created.ID, 1)
	httputil.JSON(w, http.StatusCreated, map[string]any{
		"kind":            string(service.MessageKindConversation),
		"conversation_id": conversationID.String(),
		"task_id":         created.ID.String(),
		"runner":          runnerOut,
	})
}

func (h *JJChatHandler) writeDuplicate(w http.ResponseWriter, existing *domain.Task, fallbackConv uuid.UUID) {
	conv := fallbackConv.String()
	var p struct {
		ConversationID string `json:"conversation_id"`
	}
	if json.Unmarshal(existing.InputPayload, &p) == nil && p.ConversationID != "" {
		conv = p.ConversationID
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"kind":            string(service.MessageKindConversation),
		"conversation_id": conv,
		"task_id":         existing.ID.String(),
		"runner":          runnerOutcome{Wake: "skipped", Detail: "duplicate request; original task returned"},
	})
}

func (h *JJChatHandler) postObjective(w http.ResponseWriter, r *http.Request, userID, conversationID uuid.UUID, message string, req JJMessageRequest, history []chatTurn, managerName string) {
	h.postObjectiveIn(w, r, userID, conversationID, message, req, history, managerName, nil)
}

// postObjectiveIn is postObjective for a request that may belong to a thread (extra holds the thread fields).
func (h *JJChatHandler) postObjectiveIn(w http.ResponseWriter, r *http.Request, userID, conversationID uuid.UUID, message string, req JJMessageRequest, history []chatTurn, managerName string, extra map[string]any) {
	ctxMap := map[string]any{"conversation_id": conversationID.String(), "source": "jj_messages", "manager_name": managerName}
	if len(history) > 0 {
		// A conversation that became work (for example a voice chat) carries what was already said.
		ctxMap["history"] = history
	}
	for k, v := range extra {
		ctxMap[k] = v
	}
	ctxPayload, _ := json.Marshal(ctxMap)
	orch, plan, err := h.objectives.SubmitRequest(r.Context(), &domain.UserRequest{
		UserID:      userID,
		RequestText: message,
		Context:     ctxPayload,
		Priority:    req.Priority,
	})
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	n, err := h.objectives.RunnableChildCount(r.Context(), orch.ID)
	var out runnerOutcome
	switch {
	case err != nil:
		slog.Warn("jj chat: could not determine runnable children; skipping wake", "orchestration_id", orch.ID, "error", err)
		out = runnerOutcome{Wake: "skipped", Detail: "could not determine runnable work; the scheduler will pick it up"}
	case n == 0:
		out = runnerOutcome{Wake: "skipped", Detail: "no runnable work was created"}
	default:
		out = h.wakeRunner(orch.ID, n)
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{
		"kind":            string(service.MessageKindObjective),
		"conversation_id": conversationID.String(),
		"orchestration":   orch,
		"plan":            plan,
		"runner":          out,
	})
}

// wakeRunner synchronously (bounded; Lambda freezes after the response) wakes a scale-to-zero
// runner. Failure is reported in the result, never as an error.
func (h *JJChatHandler) wakeRunner(subjectID uuid.UUID, runnable int) runnerOutcome {
	if !h.scaleToZeroEnabled || h.wakeup == nil {
		return runnerOutcome{Wake: "disabled", Detail: "scale-to-zero wake is not enabled"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	outcome := runner.WakeSignaled
	var err error
	if ow, ok := h.wakeup.(outcomeWaker); ok {
		outcome, err = ow.Wake(ctx)
	} else {
		err = h.wakeup.TriggerWakeup(ctx)
	}
	if err != nil {
		slog.Error("jj chat: runner wake failed; scheduler tick will retry", "subject_id", subjectID, "runnable", runnable, "error", err)
		if errors.Is(err, runner.ErrInsufficientCapacity) || strings.Contains(err.Error(), "InsufficientInstanceCapacity") {
			return runnerOutcome{Wake: "failed", Detail: capacityDetail}
		}
		return runnerOutcome{Wake: "failed", Detail: "could not signal the worker; it will be retried"}
	}
	if outcome == runner.WakeSkipped {
		return runnerOutcome{Wake: "skipped", Detail: "worker is already running or was recently signaled"}
	}
	return runnerOutcome{Wake: "signaled"}
}
