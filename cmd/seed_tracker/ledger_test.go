package main

import (
	"strings"
	"testing"
)

func TestLedgerTableKeepsEveryRowAndTier(t *testing.T) {
	raw := []byte(`{"updated":"x","rows":[
	 {"tier":"Tier A","company":"Acme","role":"SDE | Intern","stipend":"₹30,000/mo","link":"https://a","status":"not_applied"},
	 {"tier":"Tier B","company":"Beta","role":"ML Intern","deadline":null}]}`)
	table, n, err := ledgerTable(raw)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	lines := strings.Split(table, "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "| Tier | Company | Role |") || !strings.Contains(lines[1], "---") {
		t.Fatalf("bad table:\n%s", table)
	}
	if !strings.Contains(table, "Tier A | Acme | SDE / Intern") || !strings.Contains(table, "not_applied") {
		t.Fatalf("a pipe in a cell must not break the table, and status must be kept:\n%s", table)
	}
	if strings.Contains(table, "<nil>") {
		t.Fatal("a missing field must be an empty cell")
	}
}

func TestLedgerTableRefusesAnEmptyLedger(t *testing.T) {
	if _, _, err := ledgerTable([]byte(`{"rows":[]}`)); err == nil {
		t.Fatal("an empty ledger must not replace a list")
	}
}
