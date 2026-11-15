package domain

import (
	"strings"
	"testing"
	"time"
)

func TestAnalyzeReplyFindsAssessmentAndDeadline(t *testing.T) {
	text := "Hi! Thanks for reaching out. Please complete this coding challenge: https://app.codility.com/c/run/abc123, by Friday. " +
		"Also see our site https://acme.com/careers."
	r := AnalyzeReply(BriefReply{Name: "Priya"}, text)
	if len(r.Links) != 2 {
		t.Fatalf("links: %v", r.Links)
	}
	if len(r.Assessments) != 1 || !strings.Contains(r.Assessments[0], "codility") {
		t.Fatalf("assessments: %v (the careers page is not one, and the trailing comma must be stripped)", r.Assessments)
	}
	if strings.HasSuffix(r.Assessments[0], ",") {
		t.Fatal("trailing punctuation must be trimmed")
	}
	if len(r.Deadlines) != 1 || !strings.Contains(r.Deadlines[0], "Friday") {
		t.Fatalf("deadlines: %v", r.Deadlines)
	}
}

func TestAnalyzeReplyPlainThanksHasNothingToAct(t *testing.T) {
	r := AnalyzeReply(BriefReply{}, "Thanks, but we are not hiring right now.")
	if len(r.Links)+len(r.Assessments)+len(r.Deadlines) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestDeadlineVariants(t *testing.T) {
	for _, s := range []string{"Please reply within 48 hours", "The link expires tomorrow", "Submit by 20 Nov", "Last date is soon"} {
		if len(AnalyzeReply(BriefReply{}, s).Deadlines) != 1 {
			t.Errorf("should flag a deadline: %q", s)
		}
	}
	if len(AnalyzeReply(BriefReply{}, "Nice to meet you, call anytime").Deadlines) != 0 {
		t.Error("false deadline")
	}
}

func TestBriefOrdersByUrgencyAndSaysWhenEmpty(t *testing.T) {
	now := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	plain := AnalyzeReply(BriefReply{Name: "Amit"}, "Sounds good, let's talk.")
	test := AnalyzeReply(BriefReply{Name: "Priya"}, "Do this test https://www.hackerrank.com/test/xyz by Friday.")
	b := BuildBrief(BriefSource{
		Now: now, Replies: []BriefReply{plain, test}, PendingApprovals: []string{"email to Rahul"},
		FollowUpsDue: []string{"Neha"}, FailedTasks: []string{"apply to Acme"},
		DsaToday: []*DsaItem{{Title: "Two Sum", Difficulty: "easy", Slug: "two-sum", State: DsaAssigned}, {Title: "Done", State: DsaSolved}},
	})
	if len(b.Plan) != 6 {
		t.Fatalf("plan %d: %v", len(b.Plan), b.Plan)
	}
	if !strings.HasPrefix(b.Plan[0], "Reply from Priya") || !strings.HasPrefix(b.Plan[1], "Reply from Amit") {
		t.Fatalf("assessment reply must come first: %v", b.Plan[:2])
	}
	if !strings.HasPrefix(b.Plan[2], "Approve") || !strings.HasPrefix(b.Plan[3], "Follow-up") || !strings.HasPrefix(b.Plan[4], "DSA: Two Sum") || !strings.HasPrefix(b.Plan[5], "Failed") {
		t.Fatalf("order: %v", b.Plan)
	}
	if strings.Contains(b.Text, "Done") {
		t.Fatal("a solved DSA problem must not be listed")
	}
	e := BuildBrief(BriefSource{Now: now})
	if !e.Empty || !strings.Contains(e.Text, "Nothing needs you") {
		t.Fatalf("%+v", e)
	}
}
