package domain

import (
	"strings"
	"testing"
)

const rowsFixture = "# Hackathons\n\n| Name | Status | Link |\n|---|---|---|\n| Alpha | open | https://a |\n| Beta | open | https://b |\n| Beta | open | https://b |\n"

func TestTrackerTableKeysAreStableAndUnique(t *testing.T) {
	h, rows := TrackerTable(rowsFixture)
	if len(h) != 3 || len(rows) != 3 {
		t.Fatalf("header %v rows %d", h, len(rows))
	}
	if rows[1].Key == rows[2].Key || !strings.HasSuffix(rows[2].Key, "-2") {
		t.Fatalf("identical rows need distinct keys: %s %s", rows[1].Key, rows[2].Key)
	}
	_, again := TrackerTable(rowsFixture)
	if again[0].Key != rows[0].Key {
		t.Fatal("keys must be stable")
	}
}

func TestRemoveTrackerRows(t *testing.T) {
	_, rows := TrackerTable(rowsFixture)
	out, n := RemoveTrackerRows(rowsFixture, []string{rows[0].Key, rows[2].Key, "nope"})
	if n != 2 || strings.Contains(out, "Alpha") || strings.Count(out, "Beta") != 1 || !strings.Contains(out, "|---|") {
		t.Fatalf("removed %d: %q", n, out)
	}
}

func TestSetTrackerCell(t *testing.T) {
	_, rows := TrackerTable(rowsFixture)
	out, key, err := SetTrackerCell(rowsFixture, rows[0].Key, "status", "closed | gone")
	if err != nil || !strings.Contains(out, "| Alpha | closed / gone | https://a |") || key == "" || key == rows[0].Key {
		t.Fatalf("%v %q %q", err, out, key)
	}
	if _, _, err := SetTrackerCell(rowsFixture, rows[0].Key, "nonsense", "x"); err == nil {
		t.Fatal("unknown column must fail")
	}
	if _, _, err := SetTrackerCell(rowsFixture, "r00000000", "status", "x"); err == nil {
		t.Fatal("unknown row must fail")
	}
}
