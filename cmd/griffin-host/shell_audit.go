package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/shell"
)

// shellAuditEntry is one laptop shell, terminal or app op as a line of audit.log. It holds the command or
// text the owner's assistant ran, because that is the point of the record.
type shellAuditEntry struct {
	Time    time.Time      `json:"time"`
	Kind    string         `json:"kind"`
	Op      domain.ShellOp `json:"op"`
	Session string         `json:"session,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	OK      bool           `json:"ok"`
	Note    string         `json:"note,omitempty"`
}

// newShellAudit appends to the same audit.log the permission gateway uses (mode 0600, one JSON object a line).
// If the file cannot be opened the host still runs, but the failure is logged on every op.
func newShellAudit(path string, logger *slog.Logger) shell.AuditFunc {
	var mu sync.Mutex
	return func(op domain.ShellOp, session, detail string, ok bool, note string) {
		mu.Lock()
		defer mu.Unlock()
		if len(detail) > 2000 {
			detail = detail[:2000] + "..."
		}
		b, _ := json.Marshal(shellAuditEntry{Time: time.Now().UTC(), Kind: "laptop_shell", Op: op, Session: session, Detail: detail, OK: ok, Note: note})
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			logger.Error("shell audit: could not open audit.log", "error", err)
			return
		}
		defer f.Close()
		if _, err := f.Write(append(b, '\n')); err != nil {
			logger.Error("shell audit: could not write audit.log", "error", err)
		}
	}
}
