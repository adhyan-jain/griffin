package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

type outboxFake struct {
	rows  []*domain.Notification
	acked []uuid.UUID
}

func (f *outboxFake) Record(context.Context, *domain.Notification) (bool, error) { return true, nil }
func (f *outboxFake) After(_ context.Context, _ uuid.UUID, after int64, _ int) ([]*domain.Notification, error) {
	out := []*domain.Notification{}
	for _, n := range f.rows {
		if n.Seq > after {
			out = append(out, n)
		}
	}
	return out, nil
}
func (f *outboxFake) LatestSeq(context.Context, uuid.UUID) (int64, error) {
	return int64(len(f.rows)), nil
}
func (f *outboxFake) Ack(_ context.Context, _ uuid.UUID, ids []uuid.UUID) error {
	f.acked = append(f.acked, ids...)
	return nil
}

func asOwner(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, uuid.New()))
}

func TestADeviceGetsOnlyWhatCameAfterItsCursor(t *testing.T) {
	f := &outboxFake{rows: []*domain.Notification{{ID: uuid.New(), Seq: 1, Title: "old"}, {ID: uuid.New(), Seq: 2, Title: "new"}}}
	h := NewNotificationsHandler(f)

	type page struct {
		Cursor int64                  `json:"cursor"`
		Data   []*domain.Notification `json:"data"`
	}
	var env struct {
		Data page `json:"data"`
	}
	body := &env.Data
	w := httptest.NewRecorder()
	h.List(w, asOwner(httptest.NewRequest(http.MethodGet, "/api/v1/notifications?after=1", nil)))
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &env) != nil || len(body.Data) != 1 || body.Data[0].Title != "new" || body.Cursor != 2 {
		t.Fatalf("after=1 returns only seq 2 and the new cursor: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	h.List(w, asOwner(httptest.NewRequest(http.MethodGet, "/api/v1/notifications?after=latest", nil)))
	body.Data = nil
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &env) != nil || len(body.Data) != 0 || body.Cursor != 2 {
		t.Fatalf("a new device starts at the latest cursor and replays nothing: %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	h.List(w, asOwner(httptest.NewRequest(http.MethodGet, "/api/v1/notifications?after=-3", nil)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a bad cursor is refused: %d", w.Code)
	}
}

func TestADeviceAcknowledgesWhatItShowed(t *testing.T) {
	f := &outboxFake{}
	h := NewNotificationsHandler(f)
	id := uuid.New()
	w := httptest.NewRecorder()
	h.Ack(w, asOwner(httptest.NewRequest(http.MethodPost, "/api/v1/notifications/ack", strings.NewReader(`{"ids":["`+id.String()+`"]}`))))
	if w.Code != http.StatusNoContent || len(f.acked) != 1 || f.acked[0] != id {
		t.Fatalf("ack: %d %v", w.Code, f.acked)
	}
}
