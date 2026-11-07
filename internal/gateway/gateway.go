// Package gateway is the checkpoint every outward action on a host passes through (PRD 7.16). A tool
// (browser submit, message send, phone tap on a send button, file write, call) must call Require before
// it acts; it proceeds only on a nil result. The decision is policy.Decide; this package adds the two
// things around it that matter in practice: it fails closed until the owner's policy has been received,
// and it writes an audit record of every decision.
package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/griffin/core/internal/policy"
)

// ErrNeedsApproval means the action must wait for the owner. The caller stops and requests approval.
type ErrNeedsApproval struct{ Decision policy.Decision }

func (e *ErrNeedsApproval) Error() string { return "needs the owner's approval" }

// ErrDenied means the action is refused outright.
type ErrDenied struct{ Decision policy.Decision }

func (e *ErrDenied) Error() string { return "refused by the owner's permissions" }

// IsNeedsApproval reports whether err is an approval request.
func IsNeedsApproval(err error) bool { var e *ErrNeedsApproval; return errors.As(err, &e) }

// IsDenied reports whether err is a refusal.
func IsDenied(err error) bool { var e *ErrDenied; return errors.As(err, &e) }

// Source supplies the current policy snapshot. ok is false until a policy has been received.
type Source interface {
	Policy() (p policy.Policy, version int, ok bool)
}

// Entry is one audit record. It holds the shape of the action, never message bodies or file contents.
type Entry struct {
	Time          time.Time       `json:"time"`
	TaskID        string          `json:"task_id,omitempty"`
	Action        policy.Action   `json:"action"`
	Intent        policy.Intent   `json:"intent"`
	Mode          policy.Mode     `json:"mode"`
	Reasons       []string        `json:"reasons"`
	PolicyVersion int             `json:"policy_version"`
	Synced        bool            `json:"policy_synced"`
	Raw           json.RawMessage `json:"-"`
}

// Audit receives every decision.
type Audit interface{ Record(Entry) }

// Gateway authorizes actions for one host.
type Gateway struct {
	device string
	src    Source
	audit  Audit
	now    func() time.Time
}

// New builds a Gateway for a device ("laptop" or "phone"). audit may be nil only in tests.
func New(device string, src Source, audit Audit) *Gateway {
	return &Gateway{device: device, src: src, audit: audit, now: func() time.Time { return time.Now().UTC() }}
}

// failClosed is used until the owner's policy has been received: nothing outward runs on a guess.
var failClosed = policy.Policy{Default: policy.Off}

// Authorize decides and records. It always stamps the gateway's own device on the action, so a tool
// cannot claim to be running somewhere else.
func (g *Gateway) Authorize(taskID string, a policy.Action, in policy.Intent) policy.Decision {
	a.Device = g.device
	p, version, ok := g.src.Policy()
	if !ok {
		p = failClosed
	}
	d := policy.Decide(p, a, in)
	if !ok {
		d.Reasons = append(d.Reasons, "the owner's permissions have not been received yet")
	}
	if g.audit != nil {
		g.audit.Record(Entry{Time: g.now(), TaskID: taskID, Action: a, Intent: in, Mode: d.Mode, Reasons: d.Reasons, PolicyVersion: version, Synced: ok})
	}
	return d
}

// Require returns nil only when the action may proceed now.
func (g *Gateway) Require(taskID string, a policy.Action, in policy.Intent) error {
	d := g.Authorize(taskID, a, in)
	switch d.Mode {
	case policy.Auto:
		return nil
	case policy.Ask:
		return &ErrNeedsApproval{Decision: d}
	default:
		return &ErrDenied{Decision: d}
	}
}

// FileAudit appends audit entries as JSON lines to a file only the owner can read.
type FileAudit struct {
	mu sync.Mutex
	f  *os.File
}

// NewFileAudit opens (creating with mode 0600) an append-only audit file.
func NewFileAudit(path string) (*FileAudit, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &FileAudit{f: f}, nil
}

// Record implements Audit. A write failure is reported on stderr and never lets an action through that
// the policy refused (the decision is already made); it also never panics.
func (a *FileAudit) Record(e Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := json.Marshal(e)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit: could not encode an entry:", err)
		return
	}
	if _, err := a.f.Write(append(b, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "audit: could not write an entry:", err)
	}
}

// Close closes the file.
func (a *FileAudit) Close() error { a.mu.Lock(); defer a.mu.Unlock(); return a.f.Close() }
