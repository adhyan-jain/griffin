package whatsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

type stubDriver struct {
	name, device string
	err          error
	calls        int
}

func (s *stubDriver) Name() string   { return s.name }
func (s *stubDriver) Device() string { return s.device }
func (s *stubDriver) Do(context.Context, *domain.MessagingAction) (*Result, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &Result{Sent: true}, nil
}

func act(prefer string) *domain.MessagingAction {
	a := &domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "hi", Prefer: prefer}
	_ = a.Validate()
	return a
}

func TestMessenger_LaptopFirstByDefault(t *testing.T) {
	web, phone := &stubDriver{name: "web", device: "laptop"}, &stubDriver{name: "android", device: "phone"}
	res, err := NewMessenger(nil, web, phone).Do(context.Background(), act(""))
	require.NoError(t, err)
	assert.Equal(t, "web", res.Via)
	assert.Zero(t, phone.calls)
}

func TestMessenger_PreferPhoneGoesToThePhoneFirst(t *testing.T) {
	web, phone := &stubDriver{name: "web", device: "laptop"}, &stubDriver{name: "android", device: "phone"}
	res, err := NewMessenger(nil, web, phone).Do(context.Background(), act("phone"))
	require.NoError(t, err)
	assert.Equal(t, "android", res.Via)
	assert.Zero(t, web.calls)
}

func TestMessenger_FallsBackOnlyWhenTheFirstTouchedNothing(t *testing.T) {
	web := &stubDriver{name: "web", device: "laptop", err: notReady("not signed in")}
	phone := &stubDriver{name: "android", device: "phone"}
	res, err := NewMessenger(nil, web, phone).Do(context.Background(), act(""))
	require.NoError(t, err)
	assert.Equal(t, "android", res.Via)
	require.NotEmpty(t, res.Notes)
	assert.Contains(t, res.Notes[0], "web was not available")
}

func TestMessenger_NeverFallsBackAfterSomethingMayHaveBeenSent(t *testing.T) {
	for name, e := range map[string]error{
		"unconfirmed": ErrUnconfirmed,
		"not found":   ErrContactNotFound,
		"ambiguous":   ErrAmbiguousContact,
		"unknown":     errors.New("boom"),
	} {
		t.Run(name, func(t *testing.T) {
			web := &stubDriver{name: "web", device: "laptop", err: e}
			phone := &stubDriver{name: "android", device: "phone"}
			_, err := NewMessenger(nil, web, phone).Do(context.Background(), act(""))
			require.Error(t, err)
			assert.ErrorIs(t, err, e)
			assert.Zero(t, phone.calls, "a second driver must not run, or the contact could be messaged twice")
		})
	}
}

func TestMessenger_NothingAvailableSaysWhy(t *testing.T) {
	web := &stubDriver{name: "web", device: "laptop", err: notReady("not signed in")}
	phone := &stubDriver{name: "android", device: "phone", err: notReady("no phone connected")}
	_, err := NewMessenger(nil, web, phone).Do(context.Background(), act(""))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotReady)
	assert.Contains(t, err.Error(), "not signed in")
	assert.Contains(t, err.Error(), "no phone connected")

	_, err = NewMessenger(nil).Do(context.Background(), act(""))
	assert.ErrorIs(t, err, ErrNotReady, "no drivers at all")
}
