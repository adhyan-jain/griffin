package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type fakePeople struct {
	people map[uuid.UUID]*domain.Person
	filter repository.PersonFilter
	listed []*domain.Person
}

func (f *fakePeople) List(_ context.Context, _ uuid.UUID, flt repository.PersonFilter) ([]*domain.Person, error) {
	f.filter = flt
	return f.listed, nil // a nil slice when nothing is set: the handler must still answer []
}
func (f *fakePeople) Get(_ context.Context, _, id uuid.UUID) (*domain.Person, error) {
	if p, ok := f.people[id]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}
func (f *fakePeople) Update(_ context.Context, _, id uuid.UUID, st *domain.ContactState, dnc *bool) (*domain.Person, error) {
	p, ok := f.people[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if st != nil {
		p.ContactState = *st
	}
	if dnc != nil {
		p.DoNotContact = *dnc
	}
	return p, nil
}
func (f *fakePeople) MarkDrafted(_ context.Context, _, id uuid.UUID) (bool, error) {
	p := f.people[id]
	if p == nil || p.DoNotContact {
		return false, nil
	}
	p.ContactState = domain.ContactDrafted
	return true, nil
}

func (f *fakePeople) ApplyVerification(_ context.Context, _, id uuid.UUID, v *domain.VerifiedFields) (*domain.Person, error) {
	p, ok := f.people[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if v.VITAffiliation != nil {
		p.VITAffiliation = *v.VITAffiliation
	}
	return p, nil
}

type fakeHolder struct{ held []json.RawMessage }

func (h *fakeHolder) HoldAlumniOutreach(_ context.Context, userID uuid.UUID, _ string, payload json.RawMessage) (*domain.Approval, error) {
	h.held = append(h.held, payload)
	return &domain.Approval{ID: uuid.New(), UserID: userID, TaskID: uuid.New(), ActionType: domain.AlumniOutreachApprovalAction, Status: domain.ApprovalStatusPending}, nil
}

func TestAlumniHandler(t *testing.T) {
	userID := uuid.New()
	year := 2021
	url := "https://www.linkedin.com/in/karan/"
	karan := &domain.Person{ID: uuid.New(), Name: "Karan Mehta", ProfileURL: &url, GraduationYear: &year, ContactState: domain.ContactNotContacted}
	dnc := &domain.Person{ID: uuid.New(), Name: "Opted Out", DoNotContact: true, ContactState: domain.ContactOptedOut}
	people := &fakePeople{people: map[uuid.UUID]*domain.Person{karan.ID: karan, dnc.ID: dnc}}
	holder := &fakeHolder{}
	handler := NewAlumniHandler(people, nil, holder, 2022)

	do := func(fn http.HandlerFunc, method, target string, body any, id uuid.UUID) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			require.NoError(t, json.NewEncoder(&buf).Encode(body))
		}
		req := withUserContext(httptest.NewRequest(method, target, &buf), userID)
		if id != uuid.Nil {
			req.SetPathValue("id", id.String())
		}
		w := httptest.NewRecorder()
		fn(w, req)
		return w
	}

	// 1. List: filters are parsed and passed to the store; an empty result is [] not null; the limit is capped
	w := do(handler.List, http.MethodGet, "/api/v1/alumni?grad_year_max=2022&role_family=tech&company=Razor&country=India&do_not_contact=false&limit=9999", nil, uuid.Nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"data":[]`)
	require.Equal(t, 2022, *people.filter.GradYearMax)
	require.Equal(t, "tech", people.filter.RoleFamily)
	require.Equal(t, "Razor", people.filter.Company)
	require.Equal(t, "India", people.filter.Country)
	require.False(t, *people.filter.DoNotContact)
	require.Equal(t, 200, people.filter.Limit)
	for _, bad := range []string{"grad_year_max=soon", "role_family=chef", "do_not_contact=maybe", "limit=0"} {
		w = do(handler.List, http.MethodGet, "/api/v1/alumni?"+bad, nil, uuid.Nil)
		require.Equal(t, http.StatusBadRequest, w.Code, bad)
	}

	// 2. Outreach: files an approval and moves the person to drafted
	w = do(handler.Outreach, http.MethodPost, "/x", map[string]string{"draft_text": "Hi Karan, loved your talk.", "channel": "linkedin"}, karan.ID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp struct {
		Data struct {
			Approval *domain.Approval `json:"approval"`
			Person   struct {
				ContactState string `json:"contact_state"`
			} `json:"person"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "alumni_outreach", resp.Data.Approval.ActionType)
	require.Equal(t, domain.ApprovalStatusPending, resp.Data.Approval.Status)
	require.Equal(t, "drafted", resp.Data.Person.ContactState)
	require.Len(t, holder.held, 1)

	// 3. Refusals: do-not-contact is 409; bad channel, empty or too long draft, unknown person are not drafted
	w = do(handler.Outreach, http.MethodPost, "/x", map[string]string{"draft_text": "Hi", "channel": "linkedin"}, dnc.ID)
	require.Equal(t, http.StatusConflict, w.Code)
	w = do(handler.Outreach, http.MethodPost, "/x", map[string]string{"draft_text": "Hi", "channel": "carrier pigeon"}, karan.ID)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = do(handler.Outreach, http.MethodPost, "/x", map[string]string{"draft_text": "  ", "channel": "email"}, karan.ID)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = do(handler.Outreach, http.MethodPost, "/x", map[string]string{"draft_text": strings.Repeat("a", 2001), "channel": "email"}, karan.ID)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = do(handler.Outreach, http.MethodPost, "/x", map[string]string{"draft_text": "Hi", "channel": "email"}, uuid.New())
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Len(t, holder.held, 1, "nothing was held for a refused draft")

	// 4. With no holder the call is an error, not a silent success
	w = do(NewAlumniHandler(people, nil, nil, 2022).Outreach, http.MethodPost, "/x", map[string]string{"draft_text": "Hi", "channel": "email"}, karan.ID)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	// 5. PATCH: the owner records what happened; approved/drafted cannot be set by hand; opting out sets DNC
	w = do(handler.Update, http.MethodPatch, "/x", map[string]string{"contact_state": "sent_manually"}, karan.ID)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, domain.ContactSentManually, karan.ContactState)
	w = do(handler.Update, http.MethodPatch, "/x", map[string]string{"contact_state": "approved"}, karan.ID)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = do(handler.Update, http.MethodPatch, "/x", map[string]string{"contact_state": "opted_out"}, karan.ID)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, karan.DoNotContact)
	w = do(handler.Update, http.MethodPatch, "/x", map[string]string{}, karan.ID)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// 6. Import with no importer is an error
	w = do(handler.Import, http.MethodPost, "/api/v1/alumni/import", nil, uuid.Nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAlumniListUsesTheOneGraduationRuleAndParsesTheNewFilters(t *testing.T) {
	y := func(v int) *int { return &v }
	mk := func(name string, year *int) *domain.Person {
		return &domain.Person{ID: uuid.New(), Name: name, GraduationYear: year, ContactState: domain.ContactNotContacted}
	}
	people := &fakePeople{listed: []*domain.Person{mk("a", y(2021)), mk("b", y(2022)), mk("c", y(2023)), mk("d", nil)}}
	h := NewAlumniHandler(people, nil, nil, 2022)

	req := withUserContext(httptest.NewRequest(http.MethodGet,
		"/api/v1/alumni?graduation_eligibility=eligible&professional_function=technology&contactability_status=profile_only&min_classification_confidence=70&campus=vellore", nil), uuid.New())
	w := httptest.NewRecorder()
	h.List(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "eligible", people.filter.GradEligibility)
	require.Equal(t, "technology", people.filter.ProfessionalFunction)
	require.Equal(t, "profile_only", people.filter.ContactabilityStatus)
	require.Equal(t, 70, *people.filter.MinClassificationConfidence)
	require.Equal(t, "vellore", people.filter.Campus)

	var env struct {
		Data []struct {
			Name                  string `json:"name"`
			MeetsCutoff           bool   `json:"meets_cutoff"`
			GraduationEligibility string `json:"graduation_eligibility"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	want := map[string]struct {
		meets bool
		elig  string
	}{"a": {true, "eligible"}, "b": {false, "borderline_2022"}, "c": {false, "ineligible"}, "d": {false, "unknown"}}
	require.Len(t, env.Data, 4)
	for _, p := range env.Data {
		require.Equal(t, want[p.Name].meets, p.MeetsCutoff, p.Name)
		require.Equal(t, want[p.Name].elig, p.GraduationEligibility, p.Name)
	}

	w = httptest.NewRecorder()
	h.List(w, withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/alumni?min_classification_confidence=101", nil), uuid.New()))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAlumniVerifyValidatesAndScopes(t *testing.T) {
	userID, id := uuid.New(), uuid.New()
	people := &fakePeople{people: map[uuid.UUID]*domain.Person{id: {ID: id, Name: "A"}}}
	h := NewAlumniHandler(people, nil, nil, 2022)
	do := func(target uuid.UUID, body string) *httptest.ResponseRecorder {
		req := withUserContext(httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(body)), userID)
		req.SetPathValue("id", target.String())
		w := httptest.NewRecorder()
		h.Verify(w, req)
		return w
	}
	require.Equal(t, http.StatusBadRequest, do(id, `{"campus":{"value":"vellore","source_url":"https://a.org","excerpt":""}}`).Code)
	require.Equal(t, http.StatusBadRequest, do(id, `{}`).Code)
	require.Equal(t, http.StatusNotFound, do(uuid.New(), `{"campus":{"value":"vellore","source_url":"https://a.org","excerpt":"x"}}`).Code)
	w := do(id, `{"campus":{"value":"vellore","source_url":"https://a.org","excerpt":"x"}}`)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "confirmed_vellore")
}
