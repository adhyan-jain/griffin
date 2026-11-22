package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// NotificationsHandler serves the notification outbox to the owner's devices. A device keeps a cursor (the
// last seq it showed) and asks only for what came after it, so nothing is shown twice.
type NotificationsHandler struct {
	repo repository.NotificationRepository
}

// NewNotificationsHandler builds a NotificationsHandler.
func NewNotificationsHandler(repo repository.NotificationRepository) *NotificationsHandler {
	return &NotificationsHandler{repo: repo}
}

// List returns {"cursor": n, "data": [...]}: the rows after ?after=<seq>, oldest first, and the cursor to send
// next time. ?after=latest returns no rows, only the current cursor: a newly installed device starts there,
// so it never replays old events.
func (h *NotificationsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	q := r.URL.Query()
	if q.Get("after") == "latest" {
		seq, err := h.repo.LatestSeq(r.Context(), userID)
		if err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"cursor": seq, "data": []*domain.Notification{}})
		return
	}
	after, err := strconv.ParseInt(q.Get("after"), 10, 64)
	if err != nil || after < 0 {
		httputil.ErrorJSON(w, fmt.Errorf("%w: after must be a non-negative number or \"latest\"", domain.ErrValidation))
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	rows, err := h.repo.After(r.Context(), userID, after, limit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	cursor := after
	for _, n := range rows {
		if n.Seq > cursor {
			cursor = n.Seq
		}
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"cursor": cursor, "data": rows})
}

type ackNotificationsRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

// Ack marks rows as shown on a device.
func (h *NotificationsHandler) Ack(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req ackNotificationsRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if len(req.IDs) > 100 {
		httputil.ErrorJSON(w, fmt.Errorf("%w: at most 100 ids at a time", domain.ErrValidation))
		return
	}
	if err := h.repo.Ack(r.Context(), userID, req.IDs); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
