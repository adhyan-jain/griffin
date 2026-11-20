package linkedin

import (
	"context"
	"strings"
	"testing"
	"time"
)

const screen = `<hierarchy><node text="" content-desc=""><node text="Acme is hiring a Backend Intern for summer 2027. Apply at https://acme.example/jobs/42" content-desc=""/><node text="Great weekend everyone, here is a long photo caption about hiking" content-desc=""/></node></hierarchy>`

func TestCollectKeepsOnlyJobPostsAndDedupes(t *testing.T) {
	blocks, err := ParseScreen(screen)
	if err != nil || len(blocks) != 2 {
		t.Fatalf("blocks=%v err=%v", blocks, err)
	}
	seen := map[string]bool{}
	posts := Collect(seen, blocks)
	if len(posts) != 1 || posts[0].URLs[0] != "https://acme.example/jobs/42" {
		t.Fatalf("posts=%+v", posts)
	}
	if again := Collect(seen, blocks); len(again) != 0 {
		t.Fatalf("a repeated post must not be collected twice: %+v", again)
	}
}

type fakeADB struct{ calls []string }

func (f *fakeADB) Run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if args[0] == "exec-out" {
		return screen, nil
	}
	return "", nil
}

func TestScrollerOnlyReadsAndSwipes(t *testing.T) {
	f := &fakeADB{}
	posts, err := Scroller{ADB: f, Pause: time.Millisecond}.Run(context.Background(), 20*time.Millisecond)
	if err != nil || len(posts) != 1 {
		t.Fatalf("posts=%+v err=%v", posts, err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "input tap") || strings.Contains(c, "input text") {
			t.Fatalf("scroller must never tap or type: %s", c)
		}
	}
}
