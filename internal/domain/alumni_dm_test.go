package domain

import (
	"strings"
	"testing"
	"time"
)

func TestAlumniDMTitleAndResult(t *testing.T) {
	n, c, ok := AlumniDMOf("ALUMNI DM - Riya Sen - Google India")
	if !ok || n != "Riya Sen" || c != "Google India" {
		t.Fatalf("got %q %q %v", n, c, ok)
	}
	if _, _, ok := AlumniDMOf("APPLY - Google - Intern"); ok {
		t.Fatal("not a message task")
	}
	for in, want := range map[string]string{"DM: sent\nPROOF: Message delivered": "sent", "dm: Requested": "requested", "done!": "blocked"} {
		if got := AlumniDMResult(in); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
}

func TestWithAlumniStatusSetsOnlyThatPerson(t *testing.T) {
	tbl := AlumniHeader + "\n| Riya Sen | 2023 | Google | SDE | ACM | https://linkedin.com/in/riya | why | not contacted | 2026-11-22 |\n| Aman Rao | 2024 | Atlassian | SDE | ISTE |  | why | not contacted | 2026-11-22 |\n"
	out, ok := WithAlumniStatus(tbl, "riya  sen", "google", "sent", time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC))
	if !ok || !strings.Contains(out, "| sent 2026-11-23 |") || strings.Count(out, "not contacted") != 1 {
		t.Fatalf("ok=%v\n%s", ok, out)
	}
	if _, ok := WithAlumniStatus(tbl, "Nobody", "Google", "sent", time.Now()); ok {
		t.Fatal("no such person")
	}
}

func TestAlumniDMBatchNamesTheResumesAndNeverSendsItself(t *testing.T) {
	for _, want := range []string{"Adhyan_Jain_Resume_Backend.pdf", "Adhyan_Jain_Resume_AI_ML.pdf", "Adhyan_Jain_Resume_SDE.pdf", "Do NOT message anyone yourself", AlumniDMPrefix, "{RoleFocus}"} {
		if !strings.Contains(AlumniDMBatchGoal, want) {
			t.Errorf("goal missing %q", want)
		}
	}
	if !IsAlumniDMBatch("ALUMNI DM BATCH") || TrackerTypeOf("ALUMNI DM BATCH", AlumniDMBatchGoal, nil) == "alumni" {
		t.Error("the batch is its own routine, not the alumni list builder")
	}
}
