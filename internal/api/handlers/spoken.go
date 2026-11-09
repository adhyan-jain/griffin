package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// SpokenHandler serves GET /api/v1/tasks/{id}/spoken-summary: a few seconds of speech about a task, which the
// Listen page reads aloud when a notification is tapped.
type SpokenHandler struct {
	tasks    ThreadStore
	summ     *service.Summarizer
	profiles ProfileStore
}

// NewSpokenHandler constructs a SpokenHandler.
func NewSpokenHandler(tasks ThreadStore, summ *service.Summarizer, profiles ProfileStore) *SpokenHandler {
	return &SpokenHandler{tasks: tasks, summ: summ, profiles: profiles}
}

// Get handles the request.
func (h *SpokenHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, chatValidation("task id must be a UUID"))
		return
	}
	task, err := h.tasks.GetTaskForUser(r.Context(), id, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	sum := h.summ.Spoken(r.Context(), task, ResolveManagerName(r.Context(), h.profiles, userID))
	httputil.JSON(w, http.StatusOK, map[string]any{"text": sum.Text, "case": sum.Case, "task_id": task.ID, "title": task.Title, "status": task.Status})
}
