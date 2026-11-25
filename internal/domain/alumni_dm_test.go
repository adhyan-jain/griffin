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
	for in, want := range map[string]string{"DM: sent\nPROOF: Message delivered": "sent", "dm: Requested": "requested", "done!": "blocked", "DM: by hand\nPROOF: nothing was sent": AlumniStatusByHand} {
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

func TestAlumniDMBatchIsItsOwnRoutine(t *testing.T) {
	if !IsAlumniDMBatch("ALUMNI DM BATCH") || TrackerTypeOf("ALUMNI DM BATCH", AlumniDMBatchGoal, nil) == "alumni" {
		t.Error("the batch is its own routine, not the alumni list builder")
	}
}

func TestTheNextAlumniToMessageArePickedInCodeACMFirst(t *testing.T) {
	list := AlumniHeader + "\n" +
		"| Ravi Kumar | 2023 | Atlassian | Backend Engineer | ISTE | https://www.linkedin.com/in/ravi | intern ₹1L | not contacted | 2026-11-20 |\n" +
		"| Sara Iyer | 2024 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | intern ₹1.5L | not contacted | 2026-11-20 |\n" +
		"| Old Contact | 2022 | Uber | SDE | ACM VIT | https://www.linkedin.com/in/old | x | sent 2026-11-21 | 2026-11-19 |\n" +
		"| No Link | 2023 | Uber | SDE | | | x | not contacted | 2026-11-19 |\n" +
		"| Meera Das | 2025 | Razorpay | Software Engineer | | https://www.linkedin.com/in/meera | x | not contacted | 2026-11-20 |"
	ps := AlumniDMProposals(list, 8)
	if len(ps) != 3 {
		t.Fatalf("only not-contacted people with a profile link: %d %+v", len(ps), ps)
	}
	if ps[0].Contact.Name != "Sara Iyer" || ps[1].Contact.Name != "Ravi Kumar" || ps[2].Contact.Name != "Meera Das" {
		t.Fatalf("ACM first, then other clubs, then the rest: %s, %s, %s", ps[0].Contact.Name, ps[1].Contact.Name, ps[2].Contact.Name)
	}
	if !strings.Contains(ps[0].Contact.Message, "ACM") || strings.Contains(ps[1].Contact.Message, "ACM") || strings.Contains(ps[2].Contact.Message, "ACM") {
		t.Fatal("ACM is named only to the ACM alum")
	}
	if ps[0].Resume != ResumeFiles["AI/ML"] || ps[1].Resume != ResumeFiles["backend"] || ps[2].Resume != ResumeFiles["software engineering"] {
		t.Fatalf("the resume follows the alum's role: %s %s %s", ps[0].Resume, ps[1].Resume, ps[2].Resume)
	}
	if !strings.HasPrefix(ps[0].Title, AlumniDMPrefix) || !ps[0].Act {
		t.Fatalf("title/act: %+v", ps[0])
	}
	if got := ParseProposals(FormatProposals(ps)); len(got) != 3 || got[0].Contact == nil || got[0].Contact.Link != "https://www.linkedin.com/in/sara" {
		t.Fatalf("the block round-trips through the parser: %+v", got)
	}
	if got := AlumniDMProposals(list, 1); len(got) != 1 {
		t.Fatalf("the limit holds: %d", len(got))
	}
}
