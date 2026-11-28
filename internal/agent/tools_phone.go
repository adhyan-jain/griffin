package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// The phone tool runs things that only touch the owner's own phone (an alarm, a timer, an app, a link), so it
// is internal: it runs straight away from chat and from a scheduled job alike. The phone does the work; this
// side resolves the time words in code and refuses when no phone is online.
var (
	phoneWait = 15 * time.Second
	phonePoll = 500 * time.Millisecond
)

// phoneTaskReader reads one task back (service.TaskEngine does); the tool uses it to relay the phone's answer.
type phoneTaskReader interface {
	GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error)
}

var (
	reBareClock = regexp.MustCompile(`^\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*$`)
	reDuration  = regexp.MustCompile(`(\d+)\s*(second|sec|s|minute|min|m|hour|hr|h)s?\b`)
	reRelative  = regexp.MustCompile(`^\s*in\s+`)
	dayNumbers  = map[string]int{"sun": 1, "mon": 2, "tue": 3, "wed": 4, "thu": 5, "fri": 6, "sat": 7}
)

// durationSeconds reads "10 minutes", "in 2 hours", "an hour", "half an hour", "90 seconds".
func durationSeconds(text string) (int, bool) {
	s := strings.ToLower(text)
	switch {
	case strings.Contains(s, "half an hour"), strings.Contains(s, "half hour"):
		return 1800, true
	case regexp.MustCompile(`\b(an|one)\s+hour\b`).MatchString(s):
		return 3600, true
	case regexp.MustCompile(`\ba\s+minute\b`).MatchString(s):
		return 60, true
	}
	total := 0
	for _, m := range reDuration.FindAllStringSubmatch(s, -1) {
		n, _ := strconv.Atoi(m[1])
		switch m[2][0] {
		case 'h':
			total += n * 3600
		case 'm':
			total += n * 60
		default:
			total += n
		}
	}
	return total, total > 0
}

// alarmClock turns the owner's words into an hour and minute. A bare "7" or "6:30" is the morning (an alarm for
// 7 pm says so); anything else goes through the shared time reader.
func alarmClock(now time.Time, text string) (h, m int, ok bool, msg string) {
	if g := reBareClock.FindStringSubmatch(text); g != nil {
		h, _ = strconv.Atoi(g[1])
		if g[2] != "" {
			m, _ = strconv.Atoi(g[2])
		}
		if h > 23 || m > 59 {
			return 0, 0, false, ""
		}
		return h, m, true, ""
	}
	at, found := domain.ResolveWhen(now, text)
	if !found {
		return 0, 0, false, ""
	}
	if at.Sub(now) > 24*time.Hour {
		return 0, 0, false, "An alarm rings at the next time the clock shows that hour, so it cannot be set for a later day. Give a time within a day, or days of the week for a repeating alarm."
	}
	at = at.In(ist)
	return at.Hour(), at.Minute(), true, ""
}

func alarmDays(in []string) ([]int, error) {
	seen := map[int]bool{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		switch d {
		case "daily", "everyday", "every day":
			for i := 1; i <= 7; i++ {
				seen[i] = true
			}
		case "weekdays":
			for i := 2; i <= 6; i++ {
				seen[i] = true
			}
		case "weekends":
			seen[1], seen[7] = true, true
		default:
			n, ok := dayNumbers[strings.TrimSuffix(d[:min(3, len(d))], ".")]
			if !ok || len(d) < 3 {
				return nil, fmt.Errorf("%q is not a day of the week", d)
			}
			seen[n] = true
		}
	}
	var out []int
	for i := 1; i <= 7; i++ {
		if seen[i] {
			out = append(out, i)
		}
	}
	return out, nil
}

type phoneArgs struct {
	Action  string   `json:"action"`
	Time    string   `json:"time,omitempty"`
	Seconds int      `json:"seconds,omitempty"`
	Name    string   `json:"name,omitempty"`
	URL     string   `json:"url,omitempty"`
	Label   string   `json:"label,omitempty"`
	Days    []string `json:"days,omitempty"`
}

// phoneJob is the typed job the phone claims (input_payload.phone_control).
type phoneJob struct {
	Action  string `json:"action"`
	Hour    *int   `json:"hour,omitempty"`
	Minute  *int   `json:"minute,omitempty"`
	Seconds int    `json:"seconds,omitempty"`
	Label   string `json:"label,omitempty"`
	Days    []int  `json:"days,omitempty"`
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
}

// buildPhoneJob validates the request and resolves times. A non-empty note is an answer for the owner, not a job.
func buildPhoneJob(now time.Time, a phoneArgs) (job phoneJob, title, note string, err error) {
	a.Action = strings.ToLower(strings.TrimSpace(a.Action))
	label := truncate(strings.TrimSpace(a.Label), 80)
	switch a.Action {
	case "set_alarm":
		if reRelative.MatchString(strings.ToLower(a.Time)) {
			if secs, ok := durationSeconds(a.Time); ok { // "in 10 minutes" is a timer
				return phoneJob{Action: "set_timer", Seconds: secs, Label: label}, "Timer for " + spokenSeconds(secs), "", nil
			}
		}
		h, m, ok, msg := alarmClock(now, a.Time)
		if msg != "" {
			return job, "", msg, nil
		}
		if !ok {
			return job, "", fmt.Sprintf("Could not read a time from %q. Nothing was set. Ask the owner what time the alarm should ring.", a.Time), nil
		}
		days, derr := alarmDays(a.Days)
		if derr != nil {
			return job, "", "", derr
		}
		return phoneJob{Action: a.Action, Hour: &h, Minute: &m, Label: label, Days: days}, fmt.Sprintf("Alarm at %02d:%02d", h, m), "", nil
	case "set_timer":
		secs := a.Seconds
		if secs == 0 {
			secs, _ = durationSeconds(a.Time)
		}
		if secs < 1 || secs > 86400 {
			return job, "", "A timer needs a length between 1 second and 24 hours. Ask the owner how long.", nil
		}
		return phoneJob{Action: a.Action, Seconds: secs, Label: label}, "Timer for " + spokenSeconds(secs), "", nil
	case "open_app":
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return job, "", "", errors.New("name is required: the app to open")
		}
		return phoneJob{Action: a.Action, Name: truncate(name, 80)}, "Open " + truncate(name, 60), "", nil
	case "open_url":
		if err := domain.ValidateBrowserURL(strings.TrimSpace(a.URL)); err != nil || strings.ContainsAny(a.URL, " \t\r\n") {
			return job, "", "", errors.New("url must be an http or https address")
		}
		u := strings.TrimSpace(a.URL)
		return phoneJob{Action: a.Action, URL: u}, "Open " + truncate(u, 80), "", nil
	}
	return job, "", "", errors.New("action must be set_alarm, set_timer, open_app or open_url")
}

func spokenSeconds(s int) string {
	switch {
	case s%3600 == 0:
		return fmt.Sprintf("%d h", s/3600)
	case s%60 == 0:
		return fmt.Sprintf("%d min", s/60)
	}
	return fmt.Sprintf("%d s", s)
}

func phoneTool(d Deps) *Tool {
	return &Tool{
		Name: "phone",
		Description: "Control the owner's own phone. action: set_alarm (time in the owner's words such as '7 am', '6:30', 'in 10 minutes'; optional days like ['mon','fri'], 'weekdays' or 'daily'; optional label), " +
			"set_timer (seconds, or time such as '10 minutes'), open_app (name of the app, for example Spotify), open_url (an http or https link). " +
			"It runs at once and reports what the phone did. If no phone is online it says so: tell the owner and do not claim it was done.",
		Effect: EffectInternal,
		Schema: obj(map[string]any{
			"action":  str("set_alarm, set_timer, open_app or open_url"),
			"time":    str("alarm time or timer length, in the owner's words"),
			"seconds": num("timer length in seconds"),
			"name":    str("app name, for open_app"),
			"url":     str("http or https link, for open_url"),
			"label":   str("optional label for an alarm or timer"),
			"days":    strs("optional repeat days for an alarm: mon..sun, weekdays, weekends, daily"),
		}, "action"),
		Run: func(ctx context.Context, c *Call, raw json.RawMessage) (Output, error) {
			var a phoneArgs
			if err := decode(raw, &a); err != nil {
				return Output{}, err
			}
			job, title, note, err := buildPhoneJob(c.Now, a)
			if err != nil {
				return Output{}, err
			}
			if note != "" {
				return Output{Text: note}, nil
			}
			if !d.PhoneLive(ctx, c.UserID) {
				return Output{Text: "No phone of the owner's is online right now, so nothing was done. Tell the owner to open the Griffin app on the phone and try again."}, nil
			}
			payload, _ := json.Marshal(map[string]any{
				"requires":      []string{domain.HostCapPhoneApps},
				"phone_control": job,
			})
			key := fmt.Sprintf("agentphone:%s:%x:%d", c.UserID, hashOf(string(payload)), c.Now.Unix()/60)
			task, err := d.Tasks.CreateTask(ctx, &domain.Task{UserID: c.UserID, ManagerDomain: "personal_ops", Title: "Phone: " + title,
				Goal: "Phone: " + title, Priority: domain.TaskPriorityHighP1, InputPayload: payload, IdempotencyKey: &key}, nil)
			if errors.Is(err, domain.ErrIdempotentConflict) {
				return Output{Text: "That was just sent to the phone already."}, nil
			}
			if err != nil {
				return Output{}, err
			}
			return Output{Text: waitForPhone(ctx, d.Tasks, task, title), Summary: title + " (phone)"}, nil
		},
	}
}

// waitForPhone gives the phone a few seconds to answer, then says what is known.
func waitForPhone(ctx context.Context, tasks TaskAPI, task *domain.Task, title string) string {
	sent := fmt.Sprintf("Sent to the phone (task %s). It has not answered yet; it will do it when it next checks in.", task.ID)
	rd, ok := tasks.(phoneTaskReader)
	if !ok {
		return sent
	}
	deadline := time.NewTimer(phoneWait)
	defer deadline.Stop()
	tick := time.NewTicker(phonePoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return sent
		case <-deadline.C:
			return sent
		case <-tick.C:
			t, err := rd.GetTask(ctx, task.ID)
			if err != nil || t == nil {
				continue
			}
			switch t.Status {
			case domain.TaskStatusCompleted:
				var out struct {
					Response string `json:"response"`
				}
				_ = json.Unmarshal(t.OutputPayload, &out)
				if out.Response == "" {
					out.Response = "Done."
				}
				return "The phone says: " + out.Response
			case domain.TaskStatusFailed, domain.TaskStatusCancelled:
				reason := "it did not say why"
				if t.FailureReason != nil && *t.FailureReason != "" {
					reason = *t.FailureReason
				}
				return fmt.Sprintf("The phone could not do it (%s): %s", title, reason)
			}
		}
	}
}
