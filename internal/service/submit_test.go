package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestApprovingADraftStartsOneNonRetryingSubmitTask(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"company": "Anyscale", "role": "LLM Intern", "portal_url": "https://x.test", "resume_path": "/r.pdf", "text": "FORM:\n- Name: A"})
	var made []*domain.Task
	create := func(_ context.Context, task *domain.Task) error { made = append(made, task); return nil }
	w := &TrackerLists{}
	w.StartSubmit(context.Background(), &domain.Approval{UserID: uuid.New(), ActionPayload: payload}, create)
	if len(made) != 1 || made[0].MaxRetries != 0 || !strings.HasPrefix(made[0].Title, "SUBMIT - Anyscale") || !strings.Contains(made[0].Goal, "- Name: A") {
		t.Fatalf("made %+v", made)
	}
	made = nil
	w.StartSubmit(context.Background(), &domain.Approval{UserID: uuid.New(), ActionPayload: json.RawMessage(`{"company":"X"}`)}, create)
	if len(made) != 0 {
		t.Fatal("an approval that names no role starts no submission")
	}
}
