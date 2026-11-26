package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func TestAFailedCancelOfADoNotContactMessageStillHoldsItUnsent(t *testing.T) {
	const link = "https://www.linkedin.com/in/meera"
	lists := &TrackerLists{Alumni: &AlumniPeople{People: &fakePersonStore{dnc: map[string]bool{link + "/": true}}}}
	c := domain.OutreachContact{Name: "Meera Das", Company: "Razorpay", Link: link, Message: "Hi Meera"}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: domain.AlumniDMPrefix + "Meera Das - Razorpay", Goal: domain.OutreachManualGoal(c, "")}

	cancel := func(context.Context, uuid.UUID, string) error { return errors.New("db down") }
	require.True(t, lists.SkipDoNotContact(context.Background(), task, cancel))
}
