package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type phoneTasks struct {
	created []*domain.Task
	status  domain.TaskStatus
	output  string
	reason  string
}

func (f *phoneTasks) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return nil, nil
}
func (f *phoneTasks) CancelTask(context.Context, uuid.UUID, string) error { return nil }
func (f *phoneTasks) ResumeTask(context.Context, uuid.UUID) error         { return nil }
func (f *phoneTasks) CreateTask(_ context.Context, t *domain.Task, _ []uuid.UUID) (*domain.Task, error) {
	t.ID = uuid.New()
	f.created = append(f.created, t)
	return t, nil
}
func (f *phoneTasks) GetTask(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	t := &domain.Task{ID: id, Status: f.status}
	if f.output != "" {
		t.OutputPayload, _ = json.Marshal(map[string]string{"response": f.output})
	}
	if f.reason != "" {
		t.FailureReason = &f.reason
	}
	return t, nil
}

func runPhone(t *testing.T, live bool, ft *phoneTasks, args string) string {
	t.Helper()
	phoneWait, phonePoll = 80*time.Millisecond, 5*time.Millisecond
	tool := phoneTool(Deps{Tasks: ft, PhoneLive: func(context.Context, uuid.UUID) bool { return live }})
	now := time.Date(2026, 11, 28, 20, 0, 0, 0, ist)
	out, err := tool.Run(context.Background(), &Call{UserID: uuid.New(), Origin: OriginOwnerChat, Now: now}, json.RawMessage(args))
	if err != nil {
		return "ERR: " + err.Error()
	}
	return out.Text
}

func jobOf(t *testing.T, ft *phoneTasks) map[string]any {
	t.Helper()
	if len(ft.created) != 1 {
		t.Fatalf("created %d tasks", len(ft.created))
	}
	var p struct {
		Requires []string       `json:"requires"`
		Job      map[string]any `json:"phone_control"`
	}
	_ = json.Unmarshal(ft.created[0].InputPayload, &p)
	if len(p.Requires) != 1 || p.Requires[0] != domain.HostCapPhoneApps {
		t.Fatalf("requires = %v", p.Requires)
	}
	return p.Job
}

func TestPhoneAlarmWords(t *testing.T) {
	for in, want := range map[string][2]float64{"7": {7, 0}, "6:30": {6, 30}, "7 am": {7, 0}, "9 pm": {21, 0}} {
		ft := &phoneTasks{status: domain.TaskStatusCompleted, output: "Alarm set for 7 AM in your Clock app."}
		out := runPhone(t, true, ft, `{"action":"set_alarm","time":"`+in+`","days":["Mon","weekends"],"label":"gym"}`)
		j := jobOf(t, ft)
		if j["action"] != "set_alarm" || j["hour"] != want[0] || j["minute"] != want[1] || j["label"] != "gym" {
			t.Fatalf("%q -> %v", in, j)
		}
		if d, _ := json.Marshal(j["days"]); string(d) != "[1,2,7]" {
			t.Fatalf("days = %s", d)
		}
		if !strings.Contains(out, "The phone says: Alarm set") {
			t.Fatalf("out = %q", out)
		}
	}
}

func TestPhoneTimerFromWordsAndRelativeAlarm(t *testing.T) {
	ft := &phoneTasks{}
	runPhone(t, true, ft, `{"action":"set_timer","time":"10 minutes"}`)
	if j := jobOf(t, ft); j["action"] != "set_timer" || j["seconds"] != float64(600) {
		t.Fatalf("job = %v", j)
	}
	ft = &phoneTasks{}
	runPhone(t, true, ft, `{"action":"set_alarm","time":"in 2 hours"}`)
	if j := jobOf(t, ft); j["action"] != "set_timer" || j["seconds"] != float64(7200) {
		t.Fatalf("job = %v", j)
	}
}

func TestPhoneOpenAppAndURL(t *testing.T) {
	ft := &phoneTasks{}
	runPhone(t, true, ft, `{"action":"open_app","name":"Spotify"}`)
	if j := jobOf(t, ft); j["name"] != "Spotify" {
		t.Fatalf("job = %v", j)
	}
	ft = &phoneTasks{}
	runPhone(t, true, ft, `{"action":"open_url","url":"https://example.com/x"}`)
	if j := jobOf(t, ft); j["url"] != "https://example.com/x" {
		t.Fatalf("job = %v", j)
	}
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "intent://x", "https://a b.com"} {
		ft = &phoneTasks{}
		if out := runPhone(t, true, ft, `{"action":"open_url","url":"`+bad+`"}`); !strings.HasPrefix(out, "ERR") || len(ft.created) != 0 {
			t.Fatalf("%q accepted: %q", bad, out)
		}
	}
}

func TestPhoneRefusals(t *testing.T) {
	ft := &phoneTasks{}
	if out := runPhone(t, false, ft, `{"action":"open_app","name":"Spotify"}`); !strings.Contains(out, "No phone") || len(ft.created) != 0 {
		t.Fatalf("offline: %q", out)
	}
	for _, args := range []string{`{"action":"set_alarm","time":"sometime"}`, `{"action":"set_timer"}`, `{"action":"set_alarm","time":"friday 7am"}`} {
		ft = &phoneTasks{}
		if out := runPhone(t, true, ft, args); len(ft.created) != 0 || out == "" || strings.HasPrefix(out, "Sent") {
			t.Fatalf("%s -> %q created=%d", args, out, len(ft.created))
		}
	}
}

func TestPhoneRelaysFailureAndTimeout(t *testing.T) {
	ft := &phoneTasks{status: domain.TaskStatusFailed, reason: "I could not find an app called Nope."}
	if out := runPhone(t, true, ft, `{"action":"open_app","name":"Nope"}`); !strings.Contains(out, "could not find an app called Nope") {
		t.Fatalf("out = %q", out)
	}
	ft = &phoneTasks{status: domain.TaskStatusQueued}
	if out := runPhone(t, true, ft, `{"action":"open_app","name":"Spotify"}`); !strings.Contains(out, "Sent to the phone (task ") {
		t.Fatalf("out = %q", out)
	}
}
