package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// AuditEntry is one step in a task's story: the instruction, each thing the system did, each approval and its
// decision, and the result. Entries are chained: Hash covers the entry and the previous entry's hash, so a
// replay can prove that nothing was removed or reordered after the fact.
type AuditEntry struct {
	Seq  int       `json:"seq"`
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // instruction, event, attempt, approval_requested, approval_decided, result
	// Actor is who did it: "owner", "system", or a worker id.
	Actor string `json:"actor"`
	Text  string `json:"text"`
	// Cause points at the entry this one answers to (an approval decision answers its request; everything else
	// answers to the instruction, seq 0).
	Cause int    `json:"cause"`
	Hash  string `json:"hash"`
}

// BuildAudit lays a task's history out in order and chains it. It only reads what it is given.
func BuildAudit(t *Task, events []*TaskEvent, attempts []*TaskAttempt, approvals []*Approval) []AuditEntry {
	var out []AuditEntry
	add := func(at time.Time, kind, actor, text string) int {
		out = append(out, AuditEntry{At: at, Kind: kind, Actor: actor, Text: text})
		return len(out) - 1
	}
	add(t.CreatedAt, "instruction", "owner", t.Goal)
	for _, e := range events {
		if e.EventType == "HEARTBEAT" {
			continue // noise: proves liveness, not an action
		}
		add(e.CreatedAt, "event", "system", e.EventType+": "+e.Message)
	}
	for _, a := range attempts {
		text := fmt.Sprintf("attempt %d %s", a.AttemptNumber, a.Status)
		if a.ErrorMessage != nil && *a.ErrorMessage != "" {
			text += ": " + *a.ErrorMessage
		}
		add(a.StartedAt, "attempt", a.WorkerID, text)
	}
	// Approval requests and decisions keep a link: decisions are tied to their request below.
	reqIdx := map[string]int{}
	for _, ap := range approvals {
		reqIdx[ap.ID.String()] = add(ap.CreatedAt, "approval_requested", "system", ap.ActionType+": "+ap.Summary)
	}
	type decided struct {
		ap  *Approval
		idx int
	}
	var decisions []decided
	for _, ap := range approvals {
		if ap.DecidedAt == nil {
			continue
		}
		txt := string(ap.Status)
		if ap.DecisionReason != nil && *ap.DecisionReason != "" {
			txt += ": " + *ap.DecisionReason
		}
		decisions = append(decisions, decided{ap, add(*ap.DecidedAt, "approval_decided", "owner", txt)})
	}
	if t.CompletedAt != nil {
		text := string(t.Status)
		if t.FailureReason != nil && *t.FailureReason != "" {
			text += ": " + *t.FailureReason
		}
		add(*t.CompletedAt, "result", "system", text)
	}

	order := make([]int, len(out))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := out[order[a]], out[order[b]]
		if x.Kind == "instruction" != (y.Kind == "instruction") {
			return x.Kind == "instruction" // the instruction always comes first
		}
		return x.At.Before(y.At)
	})
	seqOf := make([]int, len(out)) // original index -> final sequence number
	sorted := make([]AuditEntry, len(out))
	for pos, orig := range order {
		seqOf[orig] = pos
		sorted[pos] = out[orig]
	}
	for _, d := range decisions {
		sorted[seqOf[d.idx]].Cause = seqOf[reqIdx[d.ap.ID.String()]]
	}
	prev := ""
	for i := range sorted {
		sorted[i].Seq = i
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%s|%s|%d", prev, i, sorted[i].At.UTC().Format(time.RFC3339Nano), sorted[i].Kind, sorted[i].Actor, sorted[i].Text, sorted[i].Cause)))
		sorted[i].Hash = hex.EncodeToString(sum[:])
		prev = sorted[i].Hash
	}
	return sorted
}

// VerifyAudit recomputes the chain and reports the first entry that does not match, or -1 when it is intact.
func VerifyAudit(entries []AuditEntry) int {
	prev := ""
	for i, e := range entries {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s|%s|%s|%d", prev, e.Seq, e.At.UTC().Format(time.RFC3339Nano), e.Kind, e.Actor, e.Text, e.Cause)))
		if e.Seq != i || e.Hash != hex.EncodeToString(sum[:]) {
			return i
		}
		prev = e.Hash
	}
	return -1
}
