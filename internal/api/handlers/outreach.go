package handlers

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// OutreachHandler serves campaigns and the people in them. The owner (JWT) can do everything. A host (token)
// can add people, write drafts, claim the next person to message within the limits, and report sent and
// replied, but it can NEVER approve: moving a draft to "approved" is the one step only the owner's sign-in allows.
type OutreachHandler struct {
	repo *repository.OutreachRepository
}

func NewOutreachHandler(repo *repository.OutreachRepository) *OutreachHandler {
	return &OutreachHandler{repo: repo}
}

// actor resolves who is calling: the owner, or a host acting for its owner.
func (h *OutreachHandler) actor(r *http.Request) (userID uuid.UUID, isHost bool, err error) {
	if host := middleware.GetHost(r.Context()); host != nil {
		return host.UserID, true, nil
	}
	userID, err = middleware.GetUserID(r.Context())
	return userID, false, err
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, errors.Join(domain.ErrValidation, fmt.Errorf("invalid %s", name))
	}
	return id, nil
}

type createCampaignRequest struct {
	Name    string                 `json:"name"`
	Goal    string                 `json:"goal"`
	Channel string                 `json:"channel"`
	Limits  *domain.OutreachLimits `json:"limits,omitempty"`
}

func (h *OutreachHandler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req createCampaignRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("name is required")))
		return
	}
	if req.Channel == "" {
		req.Channel = "email"
	}
	c := &domain.OutreachCampaign{UserID: userID, Name: req.Name, Goal: req.Goal, Channel: req.Channel, Limits: domain.DefaultOutreachLimits(req.Channel)}
	if req.Limits != nil {
		c.Limits = *req.Limits
	}
	if err := h.repo.CreateCampaign(r.Context(), c); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, c)
}

type campaignView struct {
	*domain.OutreachCampaign
	Counts map[string]int `json:"counts"`
}

func (h *OutreachHandler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	cs, err := h.repo.ListCampaigns(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	out := make([]campaignView, 0, len(cs))
	for _, c := range cs {
		counts, err := h.repo.Counts(r.Context(), userID, c.ID)
		if err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
		out = append(out, campaignView{c, counts})
	}
	httputil.JSON(w, http.StatusOK, out)
}

type updateCampaignRequest struct {
	Status *string                `json:"status,omitempty"`
	Limits *domain.OutreachLimits `json:"limits,omitempty"`
}

// UpdateCampaign pauses, resumes or finishes a campaign, or changes its limits. Owner only: a host cannot
// raise its own sending limits.
func (h *OutreachHandler) UpdateCampaign(w http.ResponseWriter, r *http.Request) {
	userID, isHost, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if isHost {
		httputil.ErrorJSON(w, domain.ErrForbidden)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req updateCampaignRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "paused" && *req.Status != "done" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("status must be active, paused or done")))
		return
	}
	c, err := h.repo.UpdateCampaign(r.Context(), userID, id, req.Status, req.Limits)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, c)
}

func (h *OutreachHandler) ListTargets(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var st *domain.OutreachStatus
	if q := r.URL.Query().Get("status"); q != "" {
		s := domain.OutreachStatus(q)
		if !s.Valid() {
			httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("unknown status")))
			return
		}
		st = &s
	}
	ts, err := h.repo.ListTargets(r.Context(), userID, id, st)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if ts == nil {
		ts = []*domain.OutreachTarget{}
	}
	httputil.JSON(w, http.StatusOK, ts)
}

type addTargetsRequest struct {
	Targets []domain.OutreachTarget `json:"targets"`
}

// AddTargets is how a researching agent (or the owner) adds people. Existing people keep their status.
func (h *OutreachHandler) AddTargets(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req addTargetsRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if len(req.Targets) == 0 || len(req.Targets) > 500 {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("send between 1 and 500 people at a time")))
		return
	}
	added, err := h.repo.UpsertTargets(r.Context(), userID, id, req.Targets)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]int{"added": added, "received": len(req.Targets)})
}

type updateTargetRequest struct {
	Status    *domain.OutreachStatus `json:"status,omitempty"`
	Draft     *string                `json:"draft,omitempty"`
	Notes     *string                `json:"notes,omitempty"`
	ReplyText *string                `json:"reply_text,omitempty"`
}

// UpdateTarget edits one person: a draft, a note, or a status move. When a person is marked sent, their
// follow-up date is set from the campaign's limits.
func (h *OutreachHandler) UpdateTarget(w http.ResponseWriter, r *http.Request) {
	userID, isHost, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req updateTargetRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if isHost && req.Status != nil && *req.Status == domain.OutreachApproved {
		httputil.ErrorJSON(w, errors.Join(domain.ErrForbidden, errors.New("only the owner can approve a message")))
		return
	}
	if req.Draft != nil && len([]rune(*req.Draft)) > 8000 {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("draft is too long")))
		return
	}
	u := repository.TargetUpdate{To: req.Status, Draft: req.Draft, Notes: req.Notes, ReplyText: req.ReplyText}
	who := "owner"
	if isHost {
		who = "agent"
	}
	if req.Status != nil {
		u.Event = fmt.Sprintf("%s: %s", who, *req.Status)
	} else if req.Draft != nil {
		u.Event = who + ": edited draft"
	}
	if req.Status != nil && *req.Status == domain.OutreachSent {
		after, err := h.followupDelay(r, userID, id)
		if err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
		next := time.Now().UTC().Add(after)
		u.NextFollowupAt = &next
	}
	t, err := h.repo.UpdateTarget(r.Context(), userID, id, u)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, t)
}

func (h *OutreachHandler) followupDelay(r *http.Request, userID, targetID uuid.UUID) (time.Duration, error) {
	cid, err := h.repo.CampaignOf(r.Context(), userID, targetID)
	if err != nil {
		return 0, err
	}
	c, err := h.repo.GetCampaign(r.Context(), userID, cid)
	if err != nil {
		return 0, err
	}
	return time.Duration(c.Limits.FollowupAfterDay) * 24 * time.Hour, nil
}

// Next answers "who should I message now?" within the campaign's limits. The optional tz query is the owner's
// IANA time zone, for the daily cap and quiet hours (default UTC).
func (h *OutreachHandler) Next(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	loc := time.UTC
	if tz := r.URL.Query().Get("tz"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	claim, err := h.repo.ClaimNextToSend(r.Context(), userID, id, time.Now().In(loc))
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, claim)
}

type replyRequest struct {
	Handle string `json:"handle"`
	// Name is used by a phone, whose notifications show a contact's name and not an address.
	Name string `json:"name"`
	Text string `json:"text"`
}

// Awaiting lists the people who have been messaged and not answered (names and addresses only).
func (h *OutreachHandler) Awaiting(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	list, err := h.repo.AwaitingReply(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, list)
}

// Reply records an answer seen in an inbox or a notification and moves the matching person to "replied".
func (h *OutreachHandler) Reply(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req replyRequest
	if err := decodeBody(r, &req); err != nil || (strings.TrimSpace(req.Handle) == "" && strings.TrimSpace(req.Name) == "") {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("handle or name is required")))
		return
	}
	var t *domain.OutreachTarget
	if strings.TrimSpace(req.Handle) != "" {
		t, err = h.repo.RecordReplyByHandle(r.Context(), userID, strings.TrimSpace(req.Handle), req.Text)
	} else {
		t, err = h.repo.RecordReplyByName(r.Context(), userID, strings.TrimSpace(req.Name), req.Text)
	}
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"matched": t != nil, "target": t})
}

// Export streams a campaign as CSV, for a spreadsheet.
func (h *OutreachHandler) Export(w http.ResponseWriter, r *http.Request) {
	userID, _, err := h.actor(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	ts, err := h.repo.ListTargets(r.Context(), userID, id, nil)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="outreach.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "handle", "status", "sent_at", "replied_at", "reply", "next_followup_at", "followups_sent", "notes"})
	ts2 := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	for _, t := range ts {
		_ = cw.Write([]string{csvSafe(t.Name), csvSafe(t.Handle), string(t.Status), ts2(t.SentAt), ts2(t.RepliedAt), csvSafe(t.ReplyText), ts2(t.NextFollowupAt), fmt.Sprint(t.FollowupsSent), csvSafe(t.Notes)})
	}
	cw.Flush()
}

// csvSafe stops a cell that starts with = + - or @ from being run as a formula when the file is opened in a
// spreadsheet (people write these fields, so they are untrusted).
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
