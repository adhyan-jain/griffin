package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
)

// CallLog is where calls are recorded.
type CallLog interface {
	Upsert(ctx context.Context, userID uuid.UUID, c repository.CallRecord) error
}

// CallsHandler answers inbound calls from the phone provider (Twilio-style webhooks). Calls from numbers on the
// owner's trusted list ring through; everyone else is asked who they are and why they call, and the owner gets a
// push with the answer. Griffin never dials out here. Every request must carry a valid provider signature.
type CallsHandler struct {
	Log         CallLog
	Sender      notify.Sender // may be nil
	AuthToken   string        // provider auth token; signs every webhook
	PublicURL   string        // the public base address the provider calls, e.g. https://api.example.com (no trailing slash)
	OwnerUserID uuid.UUID
	OwnerName   string
	OwnerNumber string   // where trusted calls are forwarded
	Trusted     []string // numbers that ring through without screening
	Logger      *slog.Logger
}

const (
	callsInboundPath  = "/api/v1/calls/inbound"
	callsScreenedPath = "/api/v1/calls/screened"
)

// verified checks the provider signature over the public URL and the form fields.
func (h *CallsHandler) verified(r *http.Request, path string) bool {
	if err := r.ParseForm(); err != nil {
		return false
	}
	return domain.ValidTwilioSignature(h.AuthToken, strings.TrimRight(h.PublicURL, "/")+path, r.PostForm, r.Header.Get("X-Twilio-Signature"))
}

func (h *CallsHandler) xml(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (h *CallsHandler) name() string {
	if h.OwnerName != "" {
		return h.OwnerName
	}
	return "the owner"
}

// Inbound is the first webhook of a call.
func (h *CallsHandler) Inbound(w http.ResponseWriter, r *http.Request) {
	if !h.verified(r, callsInboundPath) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	sid, from := r.PostForm.Get("CallSid"), r.PostForm.Get("From")
	if sid == "" {
		http.Error(w, "missing CallSid", http.StatusBadRequest)
		return
	}
	for _, n := range h.Trusted {
		if h.OwnerNumber != "" && domain.SameNumber(n, from) {
			h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "forwarded"})
			h.xml(w, domain.TwiMLDial(h.OwnerNumber))
			return
		}
	}
	h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "screening"})
	h.xml(w, domain.TwiMLScreen(h.name(), strings.TrimRight(h.PublicURL, "/")+callsScreenedPath))
}

// Screened receives the caller's spoken answer.
func (h *CallsHandler) Screened(w http.ResponseWriter, r *http.Request) {
	if !h.verified(r, callsScreenedPath) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	sid, from := r.PostForm.Get("CallSid"), r.PostForm.Get("From")
	said := strings.TrimSpace(r.PostForm.Get("SpeechResult"))
	if sid == "" {
		http.Error(w, "missing CallSid", http.StatusBadRequest)
		return
	}
	if said == "" {
		h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "no_answer"})
		h.xml(w, domain.TwiMLThanks(h.name()))
		return
	}
	h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "message", Message: said})
	if h.Sender != nil {
		if err := h.Sender.Send(r.Context(), notify.Message{Title: "Call from " + from, Body: said, Priority: 4, Tags: []string{"telephone_receiver"}}); err != nil && h.Logger != nil {
			h.Logger.Warn("call push failed", "error", err)
		}
	}
	h.xml(w, domain.TwiMLThanks(h.name()))
}

func (h *CallsHandler) record(ctx context.Context, c repository.CallRecord) {
	if err := h.Log.Upsert(ctx, h.OwnerUserID, c); err != nil && h.Logger != nil {
		h.Logger.Warn("recording call failed", "error", err)
	}
}
