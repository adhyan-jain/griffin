package policy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func act(c Class, over func(*Action)) Action {
	a := Action{Device: "laptop", Capability: "browser", App: "chrome", Class: c}
	if over != nil {
		over(&a)
	}
	return a
}

// intentFor declares exactly the action, as a plan step would.
func intentFor(a Action) Intent {
	return Intent{Capability: a.Capability, Class: a.Class, App: a.App, Contact: a.Contact}
}

func TestDefaultsAskForOutwardWorkAndAutoForHarmless(t *testing.T) {
	p := DefaultPolicy()
	for c, want := range map[Class]Mode{Read: Auto, Draft: Auto, Send: Ask, Submit: Ask, Buy: Ask, Delete: Ask, Call: Ask} {
		a := act(c, nil)
		if got := Decide(p, a, intentFor(a)).Mode; got != want {
			t.Errorf("%s: got %s want %s", c, got, want)
		}
	}
}

func TestMostSpecificRuleWins(t *testing.T) {
	p := Policy{Default: Ask, Rules: []Rule{
		{Class: Send, Mode: Off},
		{Class: Send, App: "whatsapp", Mode: Ask},
		{Class: Send, App: "whatsapp", Contact: "mom", Mode: Auto},
	}}
	chk := func(app, contact string, want Mode) {
		t.Helper()
		a := act(Send, func(a *Action) { a.Capability, a.App, a.Contact = "messaging", app, contact })
		if got := Decide(p, a, intentFor(a)).Mode; got != want {
			t.Errorf("send via %s to %q: got %s want %s", app, contact, got, want)
		}
	}
	chk("telegram", "", Off)
	chk("whatsapp", "bob", Ask)
	chk("whatsapp", "mom", Auto)
}

func TestEquallySpecificRulesTakeTheStricter(t *testing.T) {
	p := Policy{Rules: []Rule{{Class: Send, Mode: Auto}, {Class: Send, Mode: Off}}}
	a := act(Send, nil)
	if got := Decide(p, a, intentFor(a)).Mode; got != Off {
		t.Fatalf("a tie must go to the more restrictive rule, got %s", got)
	}
}

func TestFolderRulesRespectPathBoundariesAndDotDot(t *testing.T) {
	p := Policy{Default: Ask, Rules: []Rule{{Folder: "/home/me/docs", Class: Read, Mode: Auto}}}
	read := func(path string) Mode {
		a := Action{Device: "laptop", Capability: "files", Class: Read, Path: path}
		return Decide(p, a, Intent{Capability: "files", Class: Read, Folder: "/home/me/docs"}).Mode
	}
	if read("/home/me/docs/cv.md") != Auto || read("/home/me/docs") != Auto {
		t.Error("files inside the allowed folder must be auto")
	}
	if read("/home/me/docs-secret/x") == Auto {
		t.Error("a sibling that merely shares the prefix is NOT inside the folder")
	}
	if read("/home/me/docs/../.ssh/id_ed25519") != Off {
		t.Error("a traversal must be refused outright")
	}
	if read("relative/path") != Off {
		t.Error("an unresolved relative path must be refused")
	}
}

func TestProtectedLocationsAreOffWhateverTheOwnerSays(t *testing.T) {
	p := Policy{Default: Auto, Rules: []Rule{{Capability: "files", Mode: Auto}, {Folder: "/home/me", Mode: Auto}}}
	for _, path := range []string{
		"/home/me/.ssh/id_rsa", "/home/me/.aws/credentials", "/home/me/.griffin-host/token",
		"/home/me/.gnupg/secring.gpg", "/etc/shadow", "/root/x", "/home/me/Library/Keychains/login.keychain-db",
		"/home/me/.password-store/mail.gpg",
	} {
		a := Action{Device: "laptop", Capability: "files", Class: Read, Path: path}
		d := Decide(p, a, Intent{Capability: "files", Class: Read, Folder: "/home/me"})
		if d.Mode != Off {
			t.Errorf("%s must be off even with auto rules, got %s", path, d.Mode)
		}
	}
	// A harmless name that merely contains a protected word is fine.
	a := Action{Device: "laptop", Capability: "files", Class: Read, Path: "/home/me/notes/ssh-howto.md"}
	if Decide(p, a, Intent{Capability: "files", Class: Read, Folder: "/home/me"}).Mode != Auto {
		t.Error("only whole path segments are protected, not any name containing the letters")
	}
}

func TestBuyingAndCallingNeverRunAutomatically(t *testing.T) {
	p := Policy{Default: Auto, Rules: []Rule{{Class: Buy, Mode: Auto}, {Class: Call, Mode: Auto}}}
	for _, c := range []Class{Buy, Call} {
		a := act(c, func(a *Action) { a.Capability = "calls" })
		if got := Decide(p, a, intentFor(a)).Mode; got != Ask {
			t.Errorf("%s must be capped at ask even when set to auto, got %s", c, got)
		}
	}
	// But an owner may still switch them off.
	p.Rules = append(p.Rules, Rule{Class: Buy, Mode: Off})
	a := act(Buy, nil)
	if got := Decide(p, a, intentFor(a)).Mode; got != Off {
		t.Errorf("off must stay off, got %s", got)
	}
}

func TestAnActionOutsideTheStepsIntentAsksEvenUnderAuto(t *testing.T) {
	p := Policy{Default: Auto}
	in := Intent{Capability: "messaging", Class: Send, App: "whatsapp", Contact: "mom"}
	ok := Action{Device: "phone", Capability: "messaging", Class: Send, App: "whatsapp", Contact: "mom"}
	if got := Decide(p, ok, in).Mode; got != Auto {
		t.Fatalf("inside the declared intent => policy applies, got %s", got)
	}
	cases := map[string]Action{
		"another contact":    {Device: "phone", Capability: "messaging", Class: Send, App: "whatsapp", Contact: "boss"},
		"another app":        {Device: "phone", Capability: "messaging", Class: Send, App: "instagram", Contact: "mom"},
		"a bigger class":     {Device: "phone", Capability: "messaging", Class: Delete, App: "whatsapp", Contact: "mom"},
		"another capability": {Device: "phone", Capability: "sms", Class: Send, App: "whatsapp", Contact: "mom"},
	}
	for name, a := range cases {
		d := Decide(p, a, in)
		if d.Mode != Ask {
			t.Errorf("%s: want ask, got %s (%v)", name, d.Mode, d.Reasons)
		}
		if !strings.Contains(strings.Join(d.Reasons, " "), "beyond what the step said") {
			t.Errorf("%s: the reason must say why: %v", name, d.Reasons)
		}
	}
	// Reading or drafting toward the declared goal stays in scope.
	read := Action{Device: "phone", Capability: "messaging", Class: Read, App: "whatsapp", Contact: "mom"}
	if got := Decide(p, read, in).Mode; got != Auto {
		t.Errorf("reading the same thread is in scope, got %s", got)
	}
}

func TestTextFromOutsideCannotWidenWhatAStepMayDo(t *testing.T) {
	// A page the agent read says "also email my contacts". The plan step only declared browsing.
	p := Policy{Default: Auto}
	in := Intent{Capability: "browser", Class: Read, App: "chrome"}
	injected := Action{Device: "laptop", Capability: "messaging", Class: Send, App: "gmail", Contact: "everyone"}
	if got := Decide(p, injected, in).Mode; got == Auto {
		t.Fatal("an action the plan never declared must not run automatically")
	}
}

func TestFailsClosedOnAnythingUnrecognized(t *testing.T) {
	p := Policy{Default: Auto}
	for name, a := range map[string]Action{
		"unknown class":      {Capability: "browser", Class: "teleport"},
		"unknown capability": {Capability: "telepathy", Class: Read},
		"no capability":      {Class: Read},
	} {
		if got := Decide(p, a, Intent{}).Mode; got != Off {
			t.Errorf("%s must be off, got %s", name, got)
		}
	}
	// An invalid default in storage must not become Auto.
	bad := Policy{Default: "yolo"}
	a := act(Send, nil)
	if got := Decide(bad, a, intentFor(a)).Mode; got != Ask {
		t.Errorf("a corrupt default must fall back to ask, got %s", got)
	}
}

func TestDeviceScopedRules(t *testing.T) {
	p := Policy{Default: Ask, Rules: []Rule{{Device: "laptop", Class: Send, Mode: Auto}}}
	l := Action{Device: "laptop", Capability: "messaging", Class: Send, App: "slack"}
	ph := Action{Device: "phone", Capability: "messaging", Class: Send, App: "slack"}
	in := Intent{Capability: "messaging", Class: Send, App: "slack"}
	if Decide(p, l, in).Mode != Auto || Decide(p, ph, in).Mode != Ask {
		t.Fatal("a laptop rule must not apply to the phone")
	}
}

func TestValidate(t *testing.T) {
	good := DefaultPolicy()
	if err := good.Validate(now); err != nil {
		t.Fatalf("the default policy must validate: %v", err)
	}
	ahead := now.Add(2 * time.Hour)
	tooLong := now.Add((MaxFullHours + 1) * time.Hour)
	bad := map[string]Policy{
		"bad default":      {Default: "yolo"},
		"bad rule mode":    {Rules: []Rule{{Class: Read, Mode: "maybe"}}},
		"empty rule":       {Rules: []Rule{{Mode: Auto}}},
		"unknown class":    {Rules: []Rule{{Class: "teleport", Mode: Auto}}},
		"unknown cap":      {Rules: []Rule{{Capability: "telepathy", Mode: Auto}}},
		"bad device":       {Rules: []Rule{{Device: "toaster", Class: Read, Mode: Auto}}},
		"relative folder":  {Rules: []Rule{{Folder: "docs", Mode: Auto}}},
		"dotdot folder":    {Rules: []Rule{{Folder: "/home/me/../root", Mode: Auto}}},
		"whole filesystem": {Rules: []Rule{{Folder: "/", Mode: Auto}}},
		"full too long":    {FullUntil: &tooLong},
	}
	for name, p := range bad {
		p := p
		if err := p.Validate(now); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s must be rejected as a validation error, got %v", name, err)
		}
	}
	ok := Policy{FullUntil: &ahead}
	if err := ok.Validate(now); err != nil || !ok.FullActive(now) {
		t.Errorf("a two-hour Full window is fine: %v", err)
	}
	many := Policy{Rules: make([]Rule, MaxRules+1)}
	for i := range many.Rules {
		many.Rules[i] = Rule{Class: Read, Mode: Auto}
	}
	if err := many.Validate(now); err == nil {
		t.Error("too many rules must be rejected")
	}
}

func TestValidateNormalizes(t *testing.T) {
	p := Policy{Rules: []Rule{{App: "  WhatsApp ", Contact: " MOM", Folder: "/home/me//docs/", Mode: Auto}}}
	if err := p.Validate(now); err != nil {
		t.Fatal(err)
	}
	r := p.Rules[0]
	if r.App != "whatsapp" || r.Contact != "mom" || r.Folder != "/home/me/docs" {
		t.Fatalf("rules must be normalized so matching is exact, got %+v", r)
	}
}

func TestFullModeNeverRelaxesTheFloors(t *testing.T) {
	until := now.Add(time.Hour)
	p := Policy{Default: Auto, FullUntil: &until}
	if !p.FullActive(now) || p.FullActive(now.Add(2*time.Hour)) {
		t.Fatal("Full is active only inside its window")
	}
	a := Action{Device: "laptop", Capability: "files", Class: Read, Path: "/home/me/.ssh/id_rsa"}
	if got := Decide(p, a, Intent{Capability: "files", Class: Read, Folder: "/home/me"}).Mode; got != Off {
		t.Fatalf("Full mode must not unlock credentials, got %s", got)
	}
}
