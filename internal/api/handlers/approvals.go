package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"fmt"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type ApprovalHandler struct {
	engine       *service.TaskEngine
	approvalRepo repository.ApprovalRepository
	wakeup       RunnerWakeup
	brain        VoiceBrain
}

func NewApprovalHandler(engine *service.TaskEngine, approvalRepo repository.ApprovalRepository, wakeup RunnerWakeup) *ApprovalHandler {
	return &ApprovalHandler{
		engine:       engine,
		approvalRepo: approvalRepo,
		wakeup:       wakeup,
	}
}

func (h *ApprovalHandler) WithBrain(brain VoiceBrain) *ApprovalHandler {
	h.brain = brain
	return h
}

func (h *ApprovalHandler) ListApprovals(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var status *domain.ApprovalStatus
	if s := r.URL.Query().Get("status"); s != "" {
		st := domain.ApprovalStatus(s)
		status = &st
	}

	approvals, err := h.approvalRepo.ListApprovals(r.Context(), userID, status)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if approvals == nil {
		approvals = []*domain.Approval{}
	}

	httputil.JSON(w, http.StatusOK, approvals)
}

type RespondApprovalRequest struct {
	Decision string `json:"decision"` // "APPROVE" or "REJECT"
	Reason   string `json:"reason"`
}

func (h *ApprovalHandler) RespondApproval(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	approvalID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid approval id format")))
		return
	}

	var req RespondApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	approved := false
	if strings.EqualFold(req.Decision, "APPROVE") || strings.EqualFold(req.Decision, "APPROVED") {
		approved = true
	} else if !strings.EqualFold(req.Decision, "REJECT") && !strings.EqualFold(req.Decision, "REJECTED") {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("decision must be APPROVE or REJECT")))
		return
	}

	app, err := h.engine.RespondApproval(r.Context(), approvalID, userID, approved, req.Reason)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	if approved && h.wakeup != nil {
		// Synchronous (bounded) for Lambda; best-effort, the scheduler tick retries.
		wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = h.wakeup.TriggerWakeup(wakeCtx)
		cancel()
	}

	httputil.JSON(w, http.StatusOK, app)
}

type EditApprovalRequest struct {
	Text string `json:"text"`
}

// EditApproval changes the text of a message that is waiting for approval; the owner then approves the new text.
func (h *ApprovalHandler) EditApproval(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	approvalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid approval id format")))
		return
	}
	var req EditApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("text is required")))
		return
	}
	app, err := h.engine.EditApproval(r.Context(), approvalID, userID, req.Text)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, app)
}

type ReviseApprovalRequest struct {
	Instruction string `json:"instruction"`
}

// ReviseApproval enables interactive AI chat to improve and revise approval text/pitches in place.
func (h *ApprovalHandler) ReviseApproval(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	approvalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid approval id format")))
		return
	}
	var req ReviseApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Instruction) == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("instruction is required")))
		return
	}

	app, err := h.approvalRepo.GetApprovalByID(r.Context(), approvalID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if app.UserID != userID {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	if app.Status != domain.ApprovalStatusPending {
		httputil.ErrorJSON(w, domain.ErrApprovalNotPending)
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(app.ActionPayload, &payload); err != nil || payload == nil {
		payload = make(map[string]any)
	}

	currentText := ""
	textField := "cover_note"
	if cn, ok := payload["cover_note"].(string); ok && cn != "" {
		currentText = cn
		textField = "cover_note"
	} else if dr, ok := payload["draft"].(string); ok && dr != "" {
		currentText = dr
		textField = "draft"
	} else if tx, ok := payload["text"].(string); ok && tx != "" {
		currentText = tx
		textField = "text"
	}

	if h.brain == nil || !h.brain.Enabled() || currentText == "" {
		httputil.ErrorJSON(w, fmt.Errorf("%w: revision model is unavailable or disabled", domain.ErrValidation))
		return
	}

	sysPrompt := "You are JJ, Adhyan Jain's AI career and outreach assistant. Adhyan is a 3rd-year CSE student at VIT Vellore. His core systems and applied AI projects are ComputeHive (Go distributed task runner, Redis Lua atomic leasing, sub-15s dead worker recovery), EchoTales (bitemporal narrative knowledge graph), Griffin (personal AI assistant), and SGLang documentation contributions. Rewrite the provided draft or cover note based strictly on the user's instructions. Maintain a warm, polite, humble tone, strictly zero emojis, and authentic engineering claims. Return ONLY the revised draft with no preamble."
	turns := []gemini.Turn{
		{Role: "user", Text: fmt.Sprintf("Current draft:\n%s\n\nRevision request:\n%s", currentText, req.Instruction)},
	}
	out, err := h.brain.Chat(r.Context(), sysPrompt, turns)
	if err != nil || strings.TrimSpace(out) == "" {
		httputil.ErrorJSON(w, fmt.Errorf("%w: revision model failed to generate response", domain.ErrValidation))
		return
	}
	revisedText := strings.TrimSpace(out)

	payload[textField] = revisedText
	if textField == "draft" || textField == "cover_note" {
		payload["text"] = revisedText
	}
	newPayload, err := json.Marshal(payload)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	updatedApp, err := h.engine.ReviseApprovalWithPayload(r.Context(), approvalID, userID, newPayload, app.Summary, req.Instruction)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, updatedApp)
}

type BatchRespondApprovalRequest struct {
	Decision string      `json:"decision"` // "APPROVE" or "REJECT"
	Reason   string      `json:"reason,omitempty"`
	IDs      []uuid.UUID `json:"ids,omitempty"`
}

type BatchRespondApprovalResponse struct {
	Approved int                `json:"approved"`
	Rejected int                `json:"rejected"`
	Failed   int                `json:"failed"`
	Total    int                `json:"total"`
	Items    []*domain.Approval `json:"items"`
}

func (h *ApprovalHandler) BatchRespondApprovals(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req BatchRespondApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	approved := false
	if strings.EqualFold(req.Decision, "APPROVE") || strings.EqualFold(req.Decision, "APPROVED") {
		approved = true
	} else if !strings.EqualFold(req.Decision, "REJECT") && !strings.EqualFold(req.Decision, "REJECTED") {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("decision must be APPROVE or REJECT")))
		return
	}

	targetIDs := req.IDs
	if len(targetIDs) == 0 {
		statusPending := domain.ApprovalStatusPending
		pending, err := h.approvalRepo.ListApprovals(r.Context(), userID, &statusPending)
		if err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
		for _, a := range pending {
			targetIDs = append(targetIDs, a.ID)
		}
	}

	res := BatchRespondApprovalResponse{
		Total: len(targetIDs),
		Items: make([]*domain.Approval, 0, len(targetIDs)),
	}

	reason := req.Reason
	if reason == "" {
		if approved {
			reason = "Bulk approved"
		} else {
			reason = "Bulk rejected"
		}
	}

	for _, id := range targetIDs {
		app, err := h.engine.RespondApproval(r.Context(), id, userID, approved, reason)
		if err != nil {
			res.Failed++
			continue
		}
		if approved {
			res.Approved++
		} else {
			res.Rejected++
		}
		res.Items = append(res.Items, app)
	}

	if res.Approved > 0 && h.wakeup != nil {
		wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = h.wakeup.TriggerWakeup(wakeCtx)
		cancel()
	}

	httputil.JSON(w, http.StatusOK, res)
}
