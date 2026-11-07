package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func ptr(t time.Time) *time.Time { return &t }

func liveHost(kind HostKind, granted, advertised []string, seen time.Time) *Host {
	return &Host{ID: uuid.New(), Kind: kind, Capabilities: granted, Advertised: advertised, LastSeenAt: ptr(seen)}
}

func TestEffectiveCapabilitiesIsTheIntersection(t *testing.T) {
	h := &Host{Capabilities: []string{"agent", "browser", "shell"}, Advertised: []string{"agent", "browser", "calls"}}
	got := h.EffectiveCapabilities()
	if len(got) != 2 || got[0] != "agent" || got[1] != "browser" {
		t.Fatalf("a host must get only what is granted AND advertised, got %v", got)
	}
	// A host cannot grant itself anything.
	h2 := &Host{Capabilities: nil, Advertised: []string{"shell", "calls"}}
	if got := h2.EffectiveCapabilities(); len(got) != 0 {
		t.Fatalf("an ungranted host must have no capabilities, got %v", got)
	}
}

func TestIsLive(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		h    *Host
		want bool
	}{
		{"fresh", liveHost(HostKindLaptop, nil, nil, now.Add(-10*time.Second)), true},
		{"at the edge", liveHost(HostKindLaptop, nil, nil, now.Add(-HostLiveWindow)), true},
		{"stale", liveHost(HostKindLaptop, nil, nil, now.Add(-HostLiveWindow-time.Second)), false},
		{"never seen", &Host{}, false},
	}
	for _, c := range cases {
		if got := c.h.IsLive(now); got != c.want {
			t.Errorf("%s: IsLive=%v want %v", c.name, got, c.want)
		}
	}
	paused := liveHost(HostKindLaptop, nil, nil, now)
	paused.Paused = true
	if paused.IsLive(now) {
		t.Error("a paused host must not be live")
	}
	revoked := liveHost(HostKindLaptop, nil, nil, now)
	revoked.RevokedAt = ptr(now)
	if revoked.IsLive(now) {
		t.Error("a revoked host must not be live")
	}
}

func TestCovers(t *testing.T) {
	laptop := liveHost(HostKindLaptop, []string{"agent", "browser"}, []string{"agent", "browser"}, time.Now())
	phone := liveHost(HostKindPhone, []string{"phone_apps", "sms"}, []string{"phone_apps", "sms"}, time.Now())
	if !laptop.Covers(nil) {
		t.Error("a plain step needs an agent: the laptop has one")
	}
	if phone.Covers(nil) {
		t.Error("a phone without the agent capability must not take plain steps")
	}
	if !laptop.Covers([]string{"browser"}) || laptop.Covers([]string{"browser", "shell"}) {
		t.Error("every required capability must be present")
	}
	if !phone.Covers([]string{"phone_apps"}) || phone.Covers([]string{"browser"}) {
		t.Error("the phone covers phone_apps only")
	}
}

func TestElectMode(t *testing.T) {
	now := time.Now()
	laptop := liveHost(HostKindLaptop, []string{"agent"}, []string{"agent"}, now)
	phone := liveHost(HostKindPhone, []string{"phone_apps"}, []string{"phone_apps"}, now)
	staleLaptop := liveHost(HostKindLaptop, []string{"agent"}, []string{"agent"}, now.Add(-time.Hour))
	noAgentLaptop := liveHost(HostKindLaptop, []string{"browser"}, []string{"browser"}, now)

	if m := ElectMode([]*Host{phone, laptop}, now); m != ModeLaptop {
		t.Errorf("laptop on => laptop mode, got %s", m)
	}
	if m := ElectMode([]*Host{phone, staleLaptop}, now); m != ModePhone {
		t.Errorf("laptop off, phone on => phone mode, got %s", m)
	}
	if m := ElectMode([]*Host{staleLaptop}, now); m != ModeNone {
		t.Errorf("nothing live => none, got %s", m)
	}
	if m := ElectMode([]*Host{noAgentLaptop, phone}, now); m != ModePhone {
		t.Errorf("a laptop that cannot run an agent is not the brain, got %s", m)
	}
	if m := ElectMode(nil, now); m != ModeNone {
		t.Errorf("no hosts => none, got %s", m)
	}
}

func TestRequiredCapabilities(t *testing.T) {
	if got := RequiredCapabilities(json.RawMessage(`{"requires":["Browser"," shell ","browser"]}`)); len(got) != 2 || got[0] != "browser" || got[1] != "shell" {
		t.Errorf("normalize, dedupe and sort, got %v", got)
	}
	for _, p := range []string{``, `{}`, `{"requires":null}`, `{"requires":[]}`, `not json`, `[1,2]`} {
		if got := RequiredCapabilities(json.RawMessage(p)); got != nil {
			t.Errorf("%q declares nothing, got %v", p, got)
		}
	}
}

func TestValidateRequiresRejectsWhatCouldRunAsPlainText(t *testing.T) {
	for _, p := range []string{`{"requires":["telepathy"]}`, `{"requires":"browser"}`, `{"requires":[1]}`} {
		err := ValidateRequires(json.RawMessage(p))
		if !errors.Is(err, ErrValidation) {
			t.Errorf("%s must be rejected as a validation error, got %v", p, err)
		}
	}
	if err := ValidateRequires(json.RawMessage(`{"requires":["browser","phone_apps"]}`)); err != nil {
		t.Errorf("known capabilities must pass: %v", err)
	}
}

func TestTaskValidateChecksRequires(t *testing.T) {
	task := &Task{Title: "t", Goal: "g", InputPayload: json.RawMessage(`{"requires":["nope"]}`)}
	if err := task.Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("a task needing an unknown capability must not be creatable, got %v", err)
	}
}

func TestWorkerID(t *testing.T) {
	h := &Host{ID: uuid.New()}
	if got := h.WorkerID(); got != HostWorkerPrefix+h.ID.String() {
		t.Fatalf("unexpected worker id %q", got)
	}
}
