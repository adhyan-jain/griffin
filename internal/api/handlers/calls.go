package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
)

// CallLog is where calls are recorded.
type CallLog interface {
	Upsert(ctx context.Context, userID uuid.UUID, c repository.CallRecord) error
}

// OutboundCallState tracks an in-flight automated outbound call.
type OutboundCallState struct {
	ID            string    `json:"id"`
	UserID        uuid.UUID `json:"user_id"`
	To            string    `json:"to"`
	RecipientName string    `json:"recipient_name"`
	Message       string    `json:"message"`
	FirstResponse string    `json:"first_response,omitempty"`
	FollowupNotes string    `json:"followup_notes,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// CallsHandler manages inbound screening and automated outbound phone calls (native phone host or webhook fallback).
type CallsHandler struct {
	Log            CallLog
	Tasks          repository.TaskRepository // optional task repo for cellular call queuing to phone host
	Sender         notify.Sender             // may be nil
	AuthToken      string                    // provider auth token; signs every webhook
	PublicURL      string                    // the public base address the provider calls, e.g. https://api.example.com (no trailing slash)
	OwnerUserID    uuid.UUID
	OwnerName      string
	OwnerNumber    string   // where trusted calls are forwarded
	Trusted        []string // numbers that ring through without screening
	Logger         *slog.Logger
	InboundEnabled func(ctx context.Context) bool // reports whether inbound automated screening is toggled on

	outbound sync.Map // map[string]*OutboundCallState
}

const (
	callsInboundPath          = "/api/v1/calls/inbound"
	callsScreenedPath         = "/api/v1/calls/screened"
	callsOutboundTwiMLPath    = "/api/v1/calls/outbound/twiml"
	callsOutboundGatherPath   = "/api/v1/calls/outbound/gather"
	callsOutboundFollowupPath = "/api/v1/calls/outbound/followup"
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

// Inbound is the first webhook of an inbound call.
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
	// Check if inbound automated screening is toggled on; if disabled, dial owner directly or hang up cleanly.
	if h.InboundEnabled != nil && !h.InboundEnabled(r.Context()) {
		if h.OwnerNumber != "" {
			h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "forwarded"})
			h.xml(w, domain.TwiMLDial(h.OwnerNumber))
			return
		}
		h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "no_answer"})
		h.xml(w, domain.TwiMLHangup())
		return
	}
	h.record(r.Context(), repository.CallRecord{CallSID: sid, From: from, Outcome: "screening"})
	h.xml(w, domain.TwiMLScreen(h.name(), strings.TrimRight(h.PublicURL, "/")+callsScreenedPath))
}

// Screened receives the inbound caller's spoken answer.
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

// MakeOutboundCall places an autonomous outbound phone call to deliver a message and capture the recipient's response.
func (h *CallsHandler) MakeOutboundCall(ctx context.Context, userID uuid.UUID, to, recipientName, message string) (string, error) {
	return h.MakeOutboundCallWithOption(ctx, userID, to, recipientName, message, false)
}

// MakeOutboundCallWithOption places an autonomous outbound phone call with background execution options.
func (h *CallsHandler) MakeOutboundCallWithOption(ctx context.Context, userID uuid.UUID, to, recipientName, message string, inBackground bool) (string, error) {
	to = strings.TrimSpace(to)
	message = strings.TrimSpace(message)
	if to == "" {
		return "", fmt.Errorf("recipient phone number or name is required")
	}
	if message == "" {
		return "", fmt.Errorf("message is required")
	}
	if recipientName == "" {
		recipientName = to
	}

	callID := uuid.New().String()
	st := &OutboundCallState{
		ID:            callID,
		UserID:        userID,
		To:            to,
		RecipientName: recipientName,
		Message:       message,
		Status:        "initiated",
		CreatedAt:     time.Now(),
	}
	h.outbound.Store(callID, st)

	if h.Tasks != nil {
		// Queue as a cellular phone task requiring capability "calls"
		inputJSON, _ := json.Marshal(map[string]any{
			"requires": []string{domain.HostCapCalls},
			"phone": map[string]any{
				"action":         "automated_call",
				"number":         to,
				"recipient_name": recipientName,
				"message":        message,
				"call_id":        callID,
				"in_background":  inBackground,
			},
		})
		task := &domain.Task{
			UserID:        userID,
			Title:         fmt.Sprintf("Cellular call to %s", recipientName),
			Goal:          fmt.Sprintf("Autonomous cellular call to %s: %s", recipientName, message),
			ManagerDomain: "personal_ops",
			Priority:      domain.TaskPriorityHighP1,
			Status:        domain.TaskStatusQueued,
			InputPayload:  inputJSON,
		}
		_, err := h.Tasks.CreateTask(ctx, task)
		if err != nil {
			st.Status = "failed"
			return "", fmt.Errorf("queue cellular call task: %w", err)
		}
		st.Status = "queued_to_phone"
		if h.Logger != nil {
			h.Logger.Info("cellular outbound call queued for phone host", "call_id", callID, "to", to, "recipient", recipientName, "in_background", inBackground)
		}
	} else {
		st.Status = "simulated"
		if h.Logger != nil {
			h.Logger.Info("autonomous outbound call staged (simulated)", "call_id", callID, "to", to, "recipient", recipientName, "message", message, "in_background", inBackground)
		}
	}
	return callID, nil
}

// MakeOutboundCallHTTP handles POST /api/v1/calls/outbound.
func (h *CallsHandler) MakeOutboundCallHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	var req struct {
		To            string `json:"to"`
		RecipientName string `json:"recipient_name"`
		Message       string `json:"message"`
		InBackground  bool   `json:"in_background"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	callID, err := h.MakeOutboundCallWithOption(r.Context(), userID, req.To, req.RecipientName, req.Message, req.InBackground)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"call_id":        callID,
		"status":         "initiated",
		"to":             req.To,
		"recipient_name": req.RecipientName,
		"message":        req.Message,
		"in_background":  req.InBackground,
	})
}

// RecordCallHTTP handles POST /api/v1/calls/record from the phone companion app.
func (h *CallsHandler) RecordCallHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	var req struct {
		CallID        string `json:"call_id,omitempty"`
		To            string `json:"to"`
		RecipientName string `json:"recipient_name,omitempty"`
		Outcome       string `json:"outcome"` // "delivered", "message", "no_answer", "failed"
		Message       string `json:"message"`
		Reply         string `json:"reply,omitempty"`
		Followup      string `json:"followup,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	if req.To == "" {
		httputil.ErrorJSON(w, fmt.Errorf("%w: to is required", domain.ErrValidation))
		return
	}
	if req.Outcome == "" {
		req.Outcome = "delivered"
	}
	callID := req.CallID
	if callID == "" {
		callID = uuid.New().String()
	}

	fullMsg := req.Message
	if req.Reply != "" {
		fullMsg += fmt.Sprintf(" | Reply: %s", req.Reply)
	}
	if req.Followup != "" {
		fullMsg += fmt.Sprintf(" | Notes: %s", req.Followup)
	}

	h.recordForUser(r.Context(), userID, repository.CallRecord{
		CallSID: callID,
		From:    req.To,
		Outcome: req.Outcome,
		Message: fullMsg,
	})

	if h.Sender != nil {
		title := fmt.Sprintf("Cellular call to %s", req.To)
		if req.RecipientName != "" {
			title = fmt.Sprintf("Cellular call to %s (%s)", req.RecipientName, req.To)
		}
		body := fmt.Sprintf("Outcome: %s\n%s", req.Outcome, fullMsg)
		_ = h.Sender.Send(r.Context(), notify.Message{
			Title:    title,
			Body:     body,
			Priority: 4,
			Tags:     []string{"telephone_receiver"},
		})
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"call_id": callID,
		"status":  "recorded",
	})
}

// OutboundTwiML renders the TwiML for an answered outbound call.
func (h *CallsHandler) OutboundTwiML(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	val, ok := h.outbound.Load(id)
	if !ok {
		h.xml(w, domain.TwiMLHangup())
		return
	}
	st := val.(*OutboundCallState)
	st.Status = "in_progress"
	gatherURL := strings.TrimRight(h.PublicURL, "/") + callsOutboundGatherPath + "?id=" + id
	h.xml(w, domain.TwiMLOutboundDeliver(h.name(), st.Message, gatherURL))
}

// OutboundGather processes the recipient's speech on an outbound call.
func (h *CallsHandler) OutboundGather(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	val, ok := h.outbound.Load(id)
	if !ok {
		h.xml(w, domain.TwiMLHangup())
		return
	}
	st := val.(*OutboundCallState)
	_ = r.ParseForm()
	said := strings.TrimSpace(r.PostForm.Get("SpeechResult"))
	if said == "" {
		st.Status = "delivered"
		h.record(r.Context(), repository.CallRecord{
			CallSID: id,
			From:    st.To,
			Outcome: "no_answer",
			Message: "Delivered: " + st.Message,
		})
		h.xml(w, domain.TwiMLHangup())
		return
	}
	st.FirstResponse = said
	st.Status = "replied"
	followupURL := strings.TrimRight(h.PublicURL, "/") + callsOutboundFollowupPath + "?id=" + id
	h.xml(w, domain.TwiMLOutboundContextFallback(h.name(), followupURL))
}

// OutboundFollowup captures any additional notes and closes the call.
func (h *CallsHandler) OutboundFollowup(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	val, ok := h.outbound.Load(id)
	if !ok {
		h.xml(w, domain.TwiMLHangup())
		return
	}
	st := val.(*OutboundCallState)
	_ = r.ParseForm()
	followup := strings.TrimSpace(r.PostForm.Get("SpeechResult"))
	st.FollowupNotes = followup
	st.Status = "completed"

	fullMsg := fmt.Sprintf("Delivered: %s | Reply: %s", st.Message, st.FirstResponse)
	if followup != "" {
		fullMsg += " | Notes: " + followup
	}
	h.record(r.Context(), repository.CallRecord{
		CallSID: id,
		From:    st.To,
		Outcome: "message",
		Message: fullMsg,
	})

	if h.Sender != nil {
		body := fmt.Sprintf("Delivered: %q\nThey replied: %q", st.Message, st.FirstResponse)
		if followup != "" {
			body += fmt.Sprintf("\nAdditional notes: %q", followup)
		}
		title := fmt.Sprintf("Call response from %s", st.To)
		if st.RecipientName != "" {
			title = fmt.Sprintf("Call response from %s (%s)", st.RecipientName, st.To)
		}
		_ = h.Sender.Send(r.Context(), notify.Message{
			Title:    title,
			Body:     body,
			Priority: 4,
			Tags:     []string{"telephone_receiver"},
		})
	}

	h.xml(w, domain.TwiMLOutboundFinalThanks(h.name()))
}

func (h *CallsHandler) recordForUser(ctx context.Context, userID uuid.UUID, c repository.CallRecord) {
	targetUser := h.OwnerUserID
	if userID != uuid.Nil {
		targetUser = userID
	}
	if err := h.Log.Upsert(ctx, targetUser, c); err != nil && h.Logger != nil {
		h.Logger.Warn("recording call failed", "error", err)
	}
}

func (h *CallsHandler) record(ctx context.Context, c repository.CallRecord) {
	h.recordForUser(ctx, h.OwnerUserID, c)
}
