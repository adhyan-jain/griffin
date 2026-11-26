package tests

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// failingDNC is the real person store with a do-not-contact lookup that errors.
type failingDNC struct{ service.PersonStore }

func (failingDNC) DoNotContactProfiles(context.Context, uuid.UUID) (map[string]bool, error) {
	return nil, errors.New("db down")
}

func approvedDMTask(user uuid.UUID, link string) *domain.Task {
	c := domain.OutreachContact{Name: "Meera Das", Company: "Razorpay", Link: link, Message: "Hi Meera"}
	return &domain.Task{ID: uuid.New(), UserID: user, Title: domain.AlumniDMPrefix + "Meera Das - Razorpay", Goal: domain.OutreachManualGoal(c, "")}
}

func TestApprovedAlumniDMIsCancelledWhenPersonBecameDoNotContact(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, jwt := p.setup(t)
	p.saveAlumniList(t, user, alumniFixture)
	doJSON(t, p.ts, "POST", "/api/v1/alumni/import", jwt, nil, nil)
	meera := p.list(t, jwt, "?company=razorpay&grad_year_max=2020")[0]

	var cancelled []string
	cancel := func(_ context.Context, _ uuid.UUID, why string) error { cancelled = append(cancelled, why); return nil }
	lists := &service.TrackerLists{Store: p.storage, Alumni: &service.AlumniPeople{Lists: p.storage, People: p.people}}
	task := approvedDMTask(user, "https://www.linkedin.com/in/meera")

	// approved and still fine: it is not dropped
	_, out := lists.RunInCode(ctx, task, cancel, nil)
	require.NotEqual(t, service.Dropped, out)
	require.Empty(t, cancelled)

	// marked do-not-contact after approval: cancelled, never sent
	code, body := doJSON(t, p.ts, "PATCH", "/api/v1/alumni/"+meera.ID.String(), jwt, nil, map[string]any{"do_not_contact": true})
	require.Equal(t, http.StatusOK, code, string(body))
	_, out = lists.RunInCode(ctx, task, cancel, nil)
	require.Equal(t, service.Dropped, out)
	require.Len(t, cancelled, 1)
	require.Contains(t, cancelled[0], "do-not-contact")
}

func TestApprovedAlumniDMIsNotSentWhenTheDoNotContactLookupFails(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)

	var cancelled []string
	cancel := func(_ context.Context, _ uuid.UUID, why string) error { cancelled = append(cancelled, why); return nil }
	lists := &service.TrackerLists{Store: p.storage, Alumni: &service.AlumniPeople{Lists: p.storage, People: failingDNC{p.people}}}

	_, out := lists.RunInCode(ctx, approvedDMTask(user, "https://www.linkedin.com/in/meera"), cancel, nil)
	require.Equal(t, service.Dropped, out, "fail closed: an unchecked message is not sent")
	require.Len(t, cancelled, 1)
	require.Contains(t, cancelled[0], "could not be read")
}
