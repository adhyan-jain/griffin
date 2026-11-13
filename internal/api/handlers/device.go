package handlers

import (
	"errors"
	"net/http"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/browser"
)

// DeviceHandler lets a device that cannot hold a model key (the phone) ask the server what to do next on its
// screen. The model call and its key stay on the server; the phone sends what it sees and runs the one step it
// is given. Only a registered host can call it.
type DeviceHandler struct{ decider browser.Decider }

func NewDeviceHandler(d browser.Decider) *DeviceHandler { return &DeviceHandler{decider: d} }

type deviceNextRequest struct {
	Goal string               `json:"goal"`
	Page browser.Page         `json:"page"`
	Done []domain.BrowserStep `json:"done"`
}

type deviceNextReply struct {
	Done   bool               `json:"done"`
	Result string             `json:"result,omitempty"`
	Step   domain.BrowserStep `json:"step"`
}

// Next returns the next step for the screen the device is showing.
func (h *DeviceHandler) Next(w http.ResponseWriter, r *http.Request) {
	if middleware.GetHost(r.Context()) == nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.decider == nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("no model is configured on the server (GEMINI_API_KEY)")))
		return
	}
	var req deviceNextRequest
	if err := decodeBody(r, &req); err != nil || req.Goal == "" || len(req.Goal) > 2000 || len(req.Done) > 60 {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("a goal, the screen and the steps so far are required")))
		return
	}
	d, err := h.decider.Next(r.Context(), req.Goal, &req.Page, req.Done)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, deviceNextReply{Done: d.Done, Result: d.Result, Step: d.Step})
}
