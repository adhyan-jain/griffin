package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var ist = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return l
	}
	return time.FixedZone("IST", 5*3600+1800)
}()

// IST is the owner's time zone.
func IST() *time.Location { return ist }

// systemPrompt is the context the model sees every turn: the time in IST, who the owner is, what JJ remembers,
// the conversation summary and the ids already shown. It states facts; the guards are in code.
func (a *Agent) systemPrompt(ctx context.Context, conv *Conversation, rs *run, now time.Time) string {
	var b strings.Builder
	b.WriteString("You are JJ, the owner's assistant inside Griffin. Do things by calling tools; answer in a few short plain sentences. ")
	b.WriteString("Say only what tool results show. If no tool fits the request, call no_tool_fits and tell the owner you cannot do that yet.\n\n")
	b.WriteString("Now (IST): " + now.In(ist).Format("Monday 2 January 2006, 3:04 pm") + " (" + now.In(ist).Format("2006-01-02") + ")\n")
	if a.Profile != nil {
		if p := strings.TrimSpace(a.Profile(ctx, conv.UserID)); p != "" {
			b.WriteString("\nOwner profile:\n" + truncate(p, 2500) + "\n")
		}
	}
	if mem, err := a.Store.ListMemory(ctx, conv.UserID, 20); err == nil && len(mem) > 0 {
		b.WriteString("\nThings the owner told you to remember (id: fact):\n")
		for _, m := range mem {
			b.WriteString(fmt.Sprintf("- %s: %s\n", m.ID, m.Fact))
			rs.call.conv.WorkingSet.mark(m.ID.String())
		}
	}
	if s := strings.TrimSpace(conv.Summary); s != "" {
		b.WriteString("\nSummary of the earlier conversation:\n" + s + "\n")
	}
	return b.String()
}
