package service

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

// trackerLists keeps one text per tracker, like the living lists in storage.
type trackerLists struct{ text map[string]string }

func newTrackerLists(initial map[string]string) *trackerLists { return &trackerLists{text: initial} }

func (s *trackerLists) id(tracker string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(tracker))
}
func (s *trackerLists) SaveArtifact(_ context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error) {
	b, _ := io.ReadAll(in.Body)
	var m struct{ Tracker string }
	m.Tracker = strings.Trim(strings.Split(strings.Split(string(in.Metadata), `"tracker":"`)[1], `"`)[0], " ")
	s.text[m.Tracker] = string(b)
	return &domain.Artifact{ID: s.id(m.Tracker)}, nil
}
func (s *trackerLists) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	for tracker := range s.text {
		out = append(out, &domain.Artifact{ID: s.id(tracker), Metadata: domain.TrackerListMetadata("list", tracker)})
	}
	return out, nil
}
func (s *trackerLists) ReadText(_ context.Context, _ uuid.UUID, id uuid.UUID, _ int64) (string, error) {
	for tracker, text := range s.text {
		if s.id(tracker) == id {
			return text, nil
		}
	}
	return "", errors.New("no such list")
}
func (s *trackerLists) UpdateText(_ context.Context, id, _ uuid.UUID, text string, _ bool, _ int64) (*domain.Artifact, error) {
	for tracker := range s.text {
		if s.id(tracker) == id {
			s.text[tracker] = text
			return &domain.Artifact{ID: id}, nil
		}
	}
	return nil, errors.New("no such list")
}

func syncFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../sources/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var syncNow = func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) }

func routineTask(title string) *domain.Task {
	return &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: title}
}

func TestOnlyTheThreeRoutinesAreMatchedByTheirTitles(t *testing.T) {
	for title, is := range map[string]func(string) bool{
		"HACKATHON TRACKER (daily 08:30)":  IsHackathonTracker,
		"hackathon tracker":                IsHackathonTracker,
		"OPEN-SOURCE WATCH (Monday 09:00)": IsOpenSourceWatch,
		"REPO HYGIENE (monthly)":           IsRepoHygiene,
		"  Repo hygiene (monthly)":         IsRepoHygiene,
	} {
		if !is(title) {
			t.Errorf("%q is a routine", title)
		}
	}
	for _, title := range []string{"Which hackathons are easy wins that I should target", "Clean up my repo hygiene and README", "APPLY - Qdrant - SDE Intern", "Open source ideas"} {
		if IsHackathonTracker(title) || IsOpenSourceWatch(title) || IsRepoHygiene(title) {
			t.Errorf("%q is not a routine and must reach the normal path", title)
		}
	}
}

func TestTheHackathonRoutineReplacesTheListWithRealHackathonsInCode(t *testing.T) {
	junk := "| Site | What it is |\n|---|---|\n| Devfolio | a website |\n| Unstop | a website |\n"
	store := newTrackerLists(map[string]string{"hackathons": junk})
	w := &TrackerLists{Store: store, Hackathons: &HackathonSync{
		Now: syncNow,
		Get: func(_ context.Context, u string) ([]byte, error) {
			if strings.Contains(u, "devpost") {
				return syncFixture(t, "devpost.json"), nil
			}
			return syncFixture(t, "unstop_hackathons.json"), nil
		},
		Post: func(context.Context, string, []byte) ([]byte, error) { return syncFixture(t, "devfolio.json"), nil },
	}}
	answer, how := w.RunInCode(context.Background(), routineTask("HACKATHON TRACKER (daily 08:30)"), nil, nil)
	if how != Answered {
		t.Fatalf("answered in code: %v %s", how, answer)
	}
	list := store.text["hackathons"]
	if strings.Contains(list, "a website") || !strings.Contains(list, "| Name | Organizer | Mode & Location | Prize | Themes | Deadline | Days left | Source | Link |") {
		t.Fatalf("the old list is replaced:\n%s", list)
	}
	for _, want := range []string{"| Kiroween 2026 | Kiro | Online | $100,000 |", "| PromptWars x Quad Patna | Quad AI School of Technology & Management | In person, Patna |", "| promptwaves |"} {
		if !strings.Contains(list, want) {
			t.Errorf("list lacks %q:\n%s", want, list)
		}
	}
	rows := strings.Count(list, "\n| ") - 1
	if !strings.Contains(answer, " open hackathons fit you (Devpost ") || strings.Contains(answer, "could not find") || rows < 5 {
		t.Fatalf("%d rows, summary: %s", rows, answer)
	}
}

func TestWhenEverySiteFailsTheHackathonListIsLeftAlone(t *testing.T) {
	store := newTrackerLists(map[string]string{"hackathons": "keep me"})
	w := &TrackerLists{Store: store, Hackathons: &HackathonSync{Now: syncNow,
		Get:  func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") },
		Post: func(context.Context, string, []byte) ([]byte, error) { return nil, errors.New("offline") }}}
	answer, how := w.RunInCode(context.Background(), routineTask(HackathonTrackerTitle), nil, nil)
	if how != Answered || store.text["hackathons"] != "keep me" || !strings.Contains(answer, "unchanged") || !strings.Contains(answer, "Devfolio") {
		t.Fatalf("%v %s %q", how, answer, store.text["hackathons"])
	}
}

func TestTheOpenSourceRoutineWritesTheOpenIssues(t *testing.T) {
	store := newTrackerLists(map[string]string{"opensource": "areas, repos and issue types in prose"})
	w := &TrackerLists{Store: store, OpenSource: &OpenSourceSync{
		Now: func() time.Time { return time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC) },
		Get: func(_ context.Context, u string) ([]byte, error) { return syncFixture(t, "github_issues.json"), nil },
	}}
	answer, how := w.RunInCode(context.Background(), routineTask("OPEN-SOURCE WATCH (Monday 09:00)"), nil, nil)
	list := store.text["opensource"]
	if how != Answered || strings.Contains(list, "prose") || !strings.Contains(list, "| vllm-project/vllm | #38175 [RFC]: Support ViT Full CUDA Graph (Tracker) |") {
		t.Fatalf("%v\n%s", how, list)
	}
	if !strings.HasPrefix(answer, "2 open unassigned issues across 1 projects. Newest: vllm-project/vllm #38175") {
		t.Fatalf("summary: %s", answer)
	}
}

func TestAnOpenSourceRateLimitIsReportedAndTheListKept(t *testing.T) {
	store := newTrackerLists(map[string]string{"opensource": "keep me"})
	w := &TrackerLists{Store: store, OpenSource: &OpenSourceSync{Now: syncNow,
		Get: func(context.Context, string) ([]byte, error) { return nil, errors.New("answered HTTP 403") }}}
	answer, how := w.RunInCode(context.Background(), routineTask(OpenSourceWatchTitle), nil, nil)
	if how != Answered || store.text["opensource"] != "keep me" || !strings.Contains(answer, "rate limit") {
		t.Fatalf("%v %s", how, answer)
	}
}

func TestRepoHygieneNeedsALoginAndChangesNothingWithoutOne(t *testing.T) {
	store := newTrackerLists(map[string]string{"hygiene": "keep me"})
	for _, w := range []*TrackerLists{{Store: store}, {Store: store, Hygiene: &HygieneSync{}}} {
		answer, how := w.RunInCode(context.Background(), routineTask("REPO HYGIENE (monthly)"), nil, nil)
		if how != Answered || !strings.Contains(answer, "GITHUB_LOGIN") || store.text["hygiene"] != "keep me" {
			t.Fatalf("%v %s", how, answer)
		}
	}
}

func TestRepoHygieneWritesOneRowPerRepoWithRuleMadeFixes(t *testing.T) {
	store := newTrackerLists(map[string]string{"hygiene": "generic advice for every repo"})
	w := &TrackerLists{Store: store, Hygiene: &HygieneSync{Login: "someone", Now: syncNow,
		Get: func(_ context.Context, u string) ([]byte, error) {
			switch {
			case strings.Contains(u, "/users/someone/repos"):
				return syncFixture(t, "github_repos.json"), nil
			case strings.HasSuffix(u, "/contents/.github"):
				return syncFixture(t, "github_dotgithub.json"), nil
			case strings.Contains(u, "/Spoon-Knife/"):
				return []byte(`[{"name":"README.md","type":"file"},{"name":".github","type":"dir"}]`), nil
			}
			return syncFixture(t, "github_root.json"), nil
		}}}
	answer, how := w.RunInCode(context.Background(), routineTask("REPO HYGIENE (monthly)"), nil, nil)
	list := store.text["hygiene"]
	if how != Answered || strings.Contains(list, "generic") || !strings.Contains(list, "| Repo | Stack | Last push | README | License | CI | Open issues | Fix next | Link |") {
		t.Fatalf("%v\n%s", how, list)
	}
	if !strings.Contains(list, "| Hello-World |") || !strings.Contains(list, "Add a LICENSE") || !strings.Contains(list, "| Spoon-Knife |") || strings.Contains(list, "linguist") {
		t.Fatalf("forks are skipped and fixes come from the checks:\n%s", list)
	}
	if !strings.HasPrefix(answer, "Checked 4 public repos: 0 pass every check, 4 need a fix. First: ") {
		t.Fatalf("summary: %s", answer)
	}
}

func TestAHackathonTaskThatIsNotTheRoutineIsLeftToTheNormalPath(t *testing.T) {
	w := &TrackerLists{Store: newTrackerLists(map[string]string{}), Hackathons: &HackathonSync{}, OpenSource: &OpenSourceSync{}, Hygiene: &HygieneSync{Login: "x"}}
	for _, title := range []string{"Which hackathons are easy wins", "Clean up my repo hygiene", "open source ideas"} {
		if _, how := w.RunInCode(context.Background(), routineTask(title), nil, nil); how != NotInCode {
			t.Errorf("%q must not be answered by the syncs", title)
		}
	}
}
