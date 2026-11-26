package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// PersonStore is the person records the Alumni page reads and the owner edits.
type PersonStore interface {
	List(ctx context.Context, userID uuid.UUID, f repository.PersonFilter) ([]*domain.Person, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*domain.Person, error)
	Update(ctx context.Context, userID, id uuid.UUID, state *domain.ContactState, dnc *bool) (*domain.Person, error)
	MarkDrafted(ctx context.Context, userID, id uuid.UUID) (bool, error)
	ApplyVerification(ctx context.Context, userID, id uuid.UUID, v *domain.VerifiedFields) (*domain.Person, error)
}

// AlumniImporter imports the Alumni tracker list into person records.
type AlumniImporter interface {
	Import(ctx context.Context, userID uuid.UUID) (repository.ImportResult, error)
}

// OutreachHolder puts an outreach draft on the owner's approvals page. Approving it sends nothing.
type OutreachHolder interface {
	HoldAlumniOutreach(ctx context.Context, userID uuid.UUID, name string, payload json.RawMessage) (*domain.Approval, error)
}

// AlumniHandler serves the Alumni page: the people Griffin knows, with research attached. Griffin never sends a
// message: a draft is approved, and the owner sends it by hand and records that here.
type AlumniHandler struct {
	People   PersonStore
	Importer AlumniImporter
	Holder   OutreachHolder
	// Profiles supplies the sender's facts for drafts; nil leaves placeholders.
	Profiles service.OwnerProfileSource
	// GradCutoff is the configured graduation-year cutoff shown as meets_cutoff on each person.
	GradCutoff int
}

// NewAlumniHandler creates an AlumniHandler.
func NewAlumniHandler(p PersonStore, i AlumniImporter, h OutreachHolder, gradCutoff int) *AlumniHandler {
	if gradCutoff <= 0 {
		gradCutoff = domain.DefaultGraduationCutoff
	}
	return &AlumniHandler{People: p, Importer: i, Holder: h, GradCutoff: gradCutoff}
}

type personView struct {
	*domain.Person
	// MeetsCutoff is true only when graduation_eligibility is "eligible". A year equal to the cutoff year with no
	// exact date is borderline_2022, which does not meet the cutoff.
	MeetsCutoff bool `json:"meets_cutoff"`
}

// view shows the person under the one graduation rule, domain.EvaluateGraduationEligibility, with the cutoff date
// taken from the configured cutoff year.
func (h *AlumniHandler) view(p *domain.Person) personView {
	p.GraduationEligibility = domain.EvaluateGraduationEligibility(p.GraduationYear, p.GraduationDate, domain.GraduationCutoffDate(h.GradCutoff))
	return personView{Person: p, MeetsCutoff: p.GraduationEligibility == domain.CohortEligible}
}

// List returns the owner's people. Filters (all server-side): grad_year_max (people whose known year is this or
// earlier; an unknown year never matches), role_family (tech or operations), company (part of the name), country,
// campus, acm_vit_affiliation, priority_tier, seniority, graduation_eligibility, professional_function,
// contactability_status, min_classification_confidence (0 to 100), do_not_contact (true or false), limit (at most
// 200). Filters combine with AND and run on the stored columns. It always returns an array.
func (h *AlumniHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	limit, err := listLimit(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	q := r.URL.Query()
	f := PersonFilterFrom(q.Get("company"), q.Get("country"), limit)
	if s := q.Get("grad_year_max"); s != "" {
		y, err := strconv.Atoi(s)
		if err != nil || y < 1950 || y > 2100 {
			httputil.ErrorJSON(w, fmt.Errorf("%w: grad_year_max must be a year", domain.ErrValidation))
			return
		}
		f.GradYearMax = &y
	}
	if s := q.Get("role_family"); s != "" {
		if s != domain.RoleFamilyTech && s != domain.RoleFamilyOperations {
			httputil.ErrorJSON(w, fmt.Errorf("%w: role_family must be tech or operations", domain.ErrValidation))
			return
		}
		f.RoleFamily = s
	}
	if s := q.Get("campus"); s != "" {
		f.Campus = s
	}
	if s := q.Get("acm_vit_affiliation"); s != "" {
		f.ACMAffiliation = s
	}
	if s := q.Get("priority_tier"); s != "" {
		f.PriorityTier = s
	}
	if s := q.Get("seniority"); s != "" {
		f.Seniority = s
	}
	if s := q.Get("graduation_eligibility"); s != "" {
		f.GradEligibility = s
	} else if s := q.Get("grad_eligibility"); s != "" {
		f.GradEligibility = s
	}
	if s := q.Get("professional_function"); s != "" {
		f.ProfessionalFunction = s
	}
	if s := q.Get("contactability_status"); s != "" {
		f.ContactabilityStatus = s
	}
	if s := q.Get("min_classification_confidence"); s != "" {
		c, err := strconv.Atoi(s)
		if err != nil || c < 0 || c > 100 {
			httputil.ErrorJSON(w, fmt.Errorf("%w: min_classification_confidence must be 0 to 100", domain.ErrValidation))
			return
		}
		f.MinClassificationConfidence = &c
	}
	if s := q.Get("search"); s != "" {
		f.Search = s
	}
	if q.Get("sort_by_priority") == "true" {
		f.SortByPriority = true
	}
	if s := q.Get("do_not_contact"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			httputil.ErrorJSON(w, fmt.Errorf("%w: do_not_contact must be true or false", domain.ErrValidation))
			return
		}
		f.DoNotContact = &b
	}
	people, err := h.People.List(r.Context(), userID, f)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	out := make([]personView, 0, len(people))
	for _, p := range people {
		out = append(out, h.view(p))
	}
	httputil.JSON(w, http.StatusOK, out)
}

// PersonFilterFrom builds the part of a filter that needs no parsing.
func PersonFilterFrom(company, country string, limit int) repository.PersonFilter {
	return repository.PersonFilter{Company: strings.TrimSpace(company), Country: strings.TrimSpace(country), Limit: limit}
}

// Import imports the Alumni tracker list into person records. It is safe to run again: it changes nothing it
// already wrote.
func (h *AlumniHandler) Import(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.Importer == nil {
		httputil.ErrorJSON(w, errors.New("alumni import is not set up on this server"))
		return
	}
	res, err := h.Importer.Import(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, res)
}

// ownerStates are the contact states the owner may set by hand. drafted and approved come only from the outreach
// and approval flow.
var ownerStates = []domain.ContactState{domain.ContactNotContacted, domain.ContactSentManually, domain.ContactReplied, domain.ContactOptedOut}

// Update lets the owner record what happened: contact_state (not_contacted, sent_manually, replied, opted_out) and
// do_not_contact. Opting out also turns do_not_contact on.
func (h *AlumniHandler) Update(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		ContactState *domain.ContactState `json:"contact_state"`
		DoNotContact *bool                `json:"do_not_contact"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: invalid JSON body", domain.ErrValidation))
		return
	}
	if body.ContactState == nil && body.DoNotContact == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: give contact_state or do_not_contact", domain.ErrValidation))
		return
	}
	if body.ContactState != nil {
		if !slices.Contains(ownerStates, *body.ContactState) {
			httputil.ErrorJSON(w, fmt.Errorf("%w: contact_state must be one of not_contacted, sent_manually, replied, opted_out", domain.ErrValidation))
			return
		}
		if *body.ContactState == domain.ContactOptedOut && body.DoNotContact == nil {
			t := true
			body.DoNotContact = &t
		}
	}
	p, err := h.People.Update(r.Context(), userID, id, body.ContactState, body.DoNotContact)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, h.view(p))
}

// Verify records the owner's evidence for a person's campus, graduation, ACM role and public contact route. Each
// claim needs an http(s) source_url and an excerpt (422 otherwise). Only a non-LinkedIn source confirms a campus or
// an ACM role; the stored affiliation, eligibility and tier follow from the evidence.
func (h *AlumniHandler) Verify(w http.ResponseWriter, r *http.Request) {
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
	var body domain.PersonVerification
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: invalid JSON body", domain.ErrValidation))
		return
	}
	fields, err := body.Resolve(time.Now())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	p, err := h.People.ApplyVerification(r.Context(), userID, id, fields)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, h.view(p))
}

// Outreach files a draft for the owner to approve. It creates a task that only carries the approval, then the
// approval, and moves the person to "drafted". Approving it sends nothing: the owner sends the message by hand
// and records it with PATCH /alumni/{id}. A do-not-contact person is refused with 409.
func (h *AlumniHandler) Outreach(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.Holder == nil {
		httputil.ErrorJSON(w, errors.New("alumni outreach is not set up on this server"))
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}
	var body struct {
		DraftText string `json:"draft_text"`
		Channel   string `json:"channel"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: invalid JSON body", domain.ErrValidation))
		return
	}
	body.DraftText = strings.TrimSpace(body.DraftText)
	switch {
	case body.DraftText == "":
		httputil.ErrorJSON(w, fmt.Errorf("%w: draft_text is required", domain.ErrValidation))
		return
	case utf8.RuneCountInString(body.DraftText) > domain.MaxOutreachDraftRunes:
		httputil.ErrorJSON(w, fmt.Errorf("%w: draft_text is longer than %d characters", domain.ErrValidation, domain.MaxOutreachDraftRunes))
		return
	case !slices.Contains(domain.OutreachChannels, body.Channel):
		httputil.ErrorJSON(w, fmt.Errorf("%w: channel must be one of %s", domain.ErrValidation, strings.Join(domain.OutreachChannels, ", ")))
		return
	}

	person, err := h.People.Get(r.Context(), userID, id) // not found for any id that is not the owner's person
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if person.DoNotContact || person.ContactState == domain.ContactOptedOut {
		httputil.ErrorJSON(w, fmt.Errorf("%w: %s is marked do not contact", domain.ErrConflict, person.Name))
		return
	}

	payload, err := json.Marshal(map[string]any{
		"alumni_id": id.String(), "name": person.Name, "profile_url": person.ProfileURL,
		"draft_text": body.DraftText, "channel": body.Channel, "sends_nothing": true,
	})
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	approval, err := h.Holder.HoldAlumniOutreach(r.Context(), userID, person.Name, payload)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	ok, err := h.People.MarkDrafted(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if !ok {
		httputil.ErrorJSON(w, fmt.Errorf("%w: %s can no longer be contacted", domain.ErrConflict, person.Name))
		return
	}
	person.ContactState = domain.ContactDrafted
	httputil.JSON(w, http.StatusCreated, map[string]any{"approval": approval, "person": h.view(person)})
}

// GenerateDraft generates evidence-backed dual-variant drafts for an alumnus without sending.
func (h *AlumniHandler) GenerateDraft(w http.ResponseWriter, r *http.Request) {
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
	person, err := h.People.Get(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	sender, err := service.OwnerProfileOrEmpty(r.Context(), h.Profiles, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	draft, err := service.GenerateVITAlumniDraft(person, sender)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, draft)
}
