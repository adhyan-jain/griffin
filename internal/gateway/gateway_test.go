package gateway

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffin/core/internal/policy"
)

type fakeSource struct {
	p  policy.Policy
	v  int
	ok bool
}

func (f fakeSource) Policy() (policy.Policy, int, bool) { return f.p, f.v, f.ok }

type memAudit struct{ got []Entry }

func (m *memAudit) Record(e Entry) { m.got = append(m.got, e) }

var send = policy.Action{Capability: "messaging", Class: policy.Send, App: "whatsapp", Contact: "mom"}
var sendIntent = policy.Intent{Capability: "messaging", Class: policy.Send, App: "whatsapp", Contact: "mom"}

func TestNothingRunsBeforeThePolicyHasBeenReceived(t *testing.T) {
	g := New("phone", fakeSource{ok: false}, &memAudit{})
	// Even a harmless read is refused on a guess: the owner may have switched reading off.
	err := g.Require("t1", policy.Action{Capability: "files", Class: policy.Read, Path: "/home/me/a.md"},
		policy.Intent{Capability: "files", Class: policy.Read, Folder: "/home/me"})
	if !IsDenied(err) {
		t.Fatalf("with no policy synced every action must be denied, got %v", err)
	}
}

func TestAutoWithinIntentProceeds(t *testing.T) {
	au := &memAudit{}
	g := New("phone", fakeSource{p: policy.Policy{Default: policy.Auto}, v: 3, ok: true}, au)
	if err := g.Require("t1", send, sendIntent); err != nil {
		t.Fatalf("auto and within intent must proceed, got %v", err)
	}
	if len(au.got) != 1 || au.got[0].Mode != policy.Auto || au.got[0].PolicyVersion != 3 || !au.got[0].Synced {
		t.Fatalf("the decision must be audited with the policy version, got %+v", au.got)
	}
}

func TestAskStopsAndCarriesTheReasons(t *testing.T) {
	g := New("phone", fakeSource{p: policy.DefaultPolicy(), v: 1, ok: true}, &memAudit{})
	err := g.Require("t1", send, sendIntent)
	if !IsNeedsApproval(err) {
		t.Fatalf("sending under the default policy must ask, got %v", err)
	}
	var na *ErrNeedsApproval
	_ = asErr(err, &na)
	if len(na.Decision.Reasons) == 0 {
		t.Fatal("the approval request must say why")
	}
}

func asErr(err error, target **ErrNeedsApproval) bool {
	if e, ok := err.(*ErrNeedsApproval); ok {
		*target = e
		return true
	}
	return false
}

func TestOffIsRefused(t *testing.T) {
	g := New("phone", fakeSource{p: policy.Policy{Default: policy.Off}, v: 1, ok: true}, &memAudit{})
	if err := g.Require("t1", send, sendIntent); !IsDenied(err) {
		t.Fatalf("off must be a refusal, got %v", err)
	}
}

func TestAToolCannotClaimToBeOnAnotherDevice(t *testing.T) {
	// The owner allows sending only from the laptop.
	p := policy.Policy{Default: policy.Off, Rules: []policy.Rule{{Device: "laptop", Class: policy.Send, Mode: policy.Auto}}}
	g := New("phone", fakeSource{p: p, v: 1, ok: true}, &memAudit{})
	a := send
	a.Device = "laptop" // a buggy or malicious tool lies about where it runs
	if err := g.Require("t1", a, sendIntent); err == nil {
		t.Fatal("the gateway must stamp its own device; a tool cannot borrow the laptop's permissions")
	}
}

func TestAuditNeverContainsFreeTextAndIsOwnerReadableOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	fa, err := NewFileAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fa.Close()
	g := New("laptop", fakeSource{p: policy.Policy{Default: policy.Auto}, v: 2, ok: true}, fa)
	_ = g.Require("task-9", policy.Action{Capability: "browser", Class: policy.Submit, App: "chrome"},
		policy.Intent{Capability: "browser", Class: policy.Submit, App: "chrome"})
	_ = g.Require("task-9", policy.Action{Capability: "files", Class: policy.Read, Path: "/home/me/.ssh/id_rsa"},
		policy.Intent{Capability: "files", Class: policy.Read, Folder: "/home/me"})

	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the audit file must be 0600, got %v %v", st.Mode().Perm(), err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		var e map[string]any
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("every line must be valid JSON: %v", err)
		}
		for _, banned := range []string{"body", "content", "message", "text"} {
			if _, ok := e[banned]; ok {
				t.Errorf("the audit record must not carry %q", banned)
			}
		}
	}
	if n != 2 {
		t.Fatalf("want 2 audit lines, got %d", n)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"mode":"off"`) {
		t.Fatal("the refused credential read must be recorded as off")
	}
}
