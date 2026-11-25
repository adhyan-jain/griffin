package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/research"
)

// AlumniHandler serves Alumni Explorer endpoints.
type AlumniHandler struct {
	Knowledge research.KnowledgeStore
	Approvals repository.ApprovalRepository
}

// NewAlumniHandler creates an AlumniHandler.
func NewAlumniHandler(k research.KnowledgeStore, a repository.ApprovalRepository) *AlumniHandler {
	return &AlumniHandler{Knowledge: k, Approvals: a}
}

// Explorer returns qualified alumni with filter support.
func (h *AlumniHandler) Explorer(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	entities, err := h.Knowledge.ListEntities(r.Context(), userID, domain.EntityPerson, limit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	type AlumniCard struct {
		ID             string         `json:"id"`
		Name           string         `json:"name"`
		Title          string         `json:"title"`
		Company        string         `json:"company"`
		Function       string         `json:"function"`
		GraduationYear int            `json:"graduation_year,omitempty"`
		Metadata       map[string]any `json:"metadata"`
	}

	var cards []AlumniCard
	for _, e := range entities {
		title, _ := e.Metadata["title"].(string)
		company, _ := e.Metadata["company"].(string)
		function, _ := e.Metadata["function"].(string)
		gradYear, _ := e.Metadata["grad_year"].(float64)

		cards = append(cards, AlumniCard{
			ID:             e.ID.String(),
			Name:           e.Name,
			Title:          title,
			Company:        company,
			Function:       function,
			GraduationYear: int(gradYear),
			Metadata:       e.Metadata,
		})
	}

	httputil.JSON(w, http.StatusOK, cards)
}

// Outreach creates a pending approval for an outreach draft (human-in-the-loop).
func (h *AlumniHandler) Outreach(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	entity, err := h.Knowledge.GetEntity(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	var body struct {
		DraftText string `json:"draft_text"`
		Channel   string `json:"channel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.DraftText == "" {
		body.DraftText = fmt.Sprintf("Hi %s,\n\nI am a VIT student interested in your work. Would love to connect!", entity.Name)
	}

	rawPayload, _ := json.Marshal(map[string]any{
		"alumni_id":  id.String(),
		"name":       entity.Name,
		"draft_text": body.DraftText,
		"channel":    body.Channel,
	})

	approval := &domain.Approval{
		ID:            uuid.New(),
		UserID:        userID,
		ActionType:    "alumni_outreach",
		RiskLevel:     domain.ApprovalRiskLow,
		Summary:       fmt.Sprintf("Send outreach message to %s", entity.Name),
		ActionPayload: rawPayload,
		Status:        domain.ApprovalStatusPending,
	}

	if h.Approvals != nil {
		if err := h.Approvals.CreateApproval(r.Context(), approval); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}

	httputil.JSON(w, http.StatusCreated, approval)
}
