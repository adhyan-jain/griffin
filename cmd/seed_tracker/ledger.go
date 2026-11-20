package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ledgerColumns maps the local ledger's fields to the shared list's columns, in order.
var ledgerColumns = []struct{ header, field string }{
	{"Tier", "tier"}, {"Company", "company"}, {"Role", "role"}, {"Mode & Location", "mode"}, {"Stipend", "stipend"},
	{"Duration", "duration"}, {"Deadline", "deadline"}, {"Posted", "posted"}, {"PPO", "ppo"}, {"2028 Eligible", "eligible_2028"},
	{"HM / Founder & Source", "contact_source"}, {"Application Link", "link"}, {"Status", "status"},
}

func cell(v any) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if v == nil || s == "<nil>" {
		return ""
	}
	s = strings.ReplaceAll(s, "|", "/")
	return strings.Join(strings.Fields(s), " ")
}

// ledgerTable turns the local ledger file ({"rows": [...]} or a bare list) into the list's markdown table.
func ledgerTable(raw []byte) (string, int, error) {
	var wrapped struct {
		Rows []map[string]any `json:"rows"`
	}
	rows := wrapped.Rows
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Rows) > 0 {
		rows = wrapped.Rows
	} else if err := json.Unmarshal(raw, &rows); err != nil {
		return "", 0, fmt.Errorf("the ledger is neither {\"rows\": [...]} nor a list: %w", err)
	}
	if len(rows) == 0 {
		return "", 0, fmt.Errorf("the ledger has no rows")
	}
	var head, sep []string
	for _, c := range ledgerColumns {
		head, sep = append(head, c.header), append(sep, "---")
	}
	lines := []string{"| " + strings.Join(head, " | ") + " |", "|" + strings.Join(sep, "|") + "|"}
	for _, r := range rows {
		var cs []string
		for _, c := range ledgerColumns {
			cs = append(cs, cell(r[c.field]))
		}
		lines = append(lines, "| "+strings.Join(cs, " | ")+" |")
	}
	return strings.Join(lines, "\n"), len(rows), nil
}
