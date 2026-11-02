package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type ApprovalHandler struct {
	engine       *service.TaskEngine
	approvalRepo repository.ApprovalRepository
	wakeup       RunnerWakeup
}

func NewApprovalHandler(engine *service.TaskEngine, approvalRepo repository.ApprovalRepository, wakeup RunnerWakeup) *ApprovalHandler {
	return &ApprovalHandler{
		engine:       engine,
		approvalRepo: approvalRepo,
		wakeup:       wakeup,
	}
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
