package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

const libraryProfile = "- Full name: Adhyan Jain\n- Education: B.Tech in Computer Science and Engineering, Vellore Institute of Technology (VIT)\n- Expected graduation: 2028\n" +
	"- [computehive] ComputeHive: distributed task runner in Go, Redis, Docker\n- [palni] AI Engineer Intern, Palni\n" +
	"- [acm-recruitment] ACM - VIT Student Chapter (Senior Core Member)"

type fixedProfile struct{ p *domain.OwnerProfile }

func (f fixedProfile) Latest(context.Context, uuid.UUID) (*domain.OwnerProfile, error) {
	return f.p, nil
}

func TestTheLibraryProfileFillsTheDraftWhenThereIsNoStructuredOne(t *testing.T) {
	src := OwnerProfileWithLibrary{Structured: fixedProfile{}, Library: func(context.Context, uuid.UUID) string { return libraryProfile }}
	sender, err := OwnerProfileOrEmpty(context.Background(), src, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if sender.FullName != "Adhyan Jain" || sender.School != "Vellore Institute of Technology (VIT)" {
		t.Fatalf("got %+v", sender)
	}
	project, acm := senderFacts(sender)
	if project != "ComputeHive" || !acm {
		t.Fatalf("project=%q acm=%v", project, acm)
	}
	url := "https://acmvit.example.invalid/team"
	p := &domain.Person{Name: "Test Person", Company: "Example Startup", Title: "SDE", ACMAffiliation: "confirmed", ACMSource: url}
	d, err := GenerateVITAlumniDraft(p, sender)
	if err != nil {
		t.Fatal(err)
	}
	if d.NeedsUserReview || strings.Contains(d.ShortNote, "[your") || !strings.Contains(d.ShortNote, "Adhyan Jain") {
		t.Fatalf("no placeholders once the Library profile exists:\n%s", d.ShortNote)
	}
}

func TestTheStructuredProfileWinsAndNoProfileMeansPlaceholders(t *testing.T) {
	structured := &domain.OwnerProfile{FullName: "Structured Name", School: "Somewhere"}
	src := OwnerProfileWithLibrary{Structured: fixedProfile{structured}, Library: func(context.Context, uuid.UUID) string { return libraryProfile }}
	if s, _ := OwnerProfileOrEmpty(context.Background(), src, uuid.New()); s.FullName != "Structured Name" {
		t.Fatalf("structured profile must win: %+v", s)
	}
	empty := OwnerProfileWithLibrary{Library: func(context.Context, uuid.UUID) string { return "" }}
	s, err := OwnerProfileOrEmpty(context.Background(), empty, uuid.New())
	if err != nil || s.FullName != "" {
		t.Fatalf("no profile anywhere: %+v %v", s, err)
	}
}

func TestAClaimedAlumniDMIsAnsweredInCodeNeverRun(t *testing.T) {
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: domain.AlumniDMPrefix + "Test Person - Example",
		Goal: "Open https://www.linkedin.com/in/test-person in the owner's signed-in browser and send this message to Test Person, word for word."}
	answer, how := (&TrackerLists{}).RunInCode(context.Background(), task, nil, nil)
	if how != Answered || !strings.Contains(answer, domain.ManualSendMarker) || domain.AlumniDMResult(answer) != domain.AlumniStatusByHand {
		t.Fatalf("an old browser-send task must be handed to the owner: how=%v\n%s", how, answer)
	}
}
