package domain

import (
	"strings"
	"testing"
)

const replyFacts = "Adhyan is a CSE student at VIT. He built ComputeHive, a distributed task runner in Go with Redis and Docker. He also does AI agent work."

const replyThread = "How do you build a task queue with Redis and worker pools in Go?\nI run 3 worker nodes and jobs sometimes get lost."

const goodReply = "Worker pools on top of a Redis list are a solid start. The part that usually bites is a worker dying mid job, so I would add a visibility timeout and retries before anything else. " +
	"Are your jobs idempotent, and do they need strict ordering? That decides whether a plain queue is enough. With 3 nodes you can probably keep it simple for now and measure first."

func TestCommunityReplyPassesWhenEveryNumberAndNameIsFromTheThreadOrProfile(t *testing.T) {
	if p := CheckCommunityReply(goodReply, replyThread, replyFacts); len(p) != 0 {
		t.Fatalf("a reply that states only what the thread and profile state passes: %v", p)
	}
}

func TestCommunityReplyRejectsInventedNumbersNamesLinksAndNoise(t *testing.T) {
	cases := map[string]string{
		"an invented number":  strings.Replace(goodReply, "With 3 nodes", "With 12 nodes", 1),
		"an invented product": strings.Replace(goodReply, "a Redis list", "a Pulsarix stream", 1), // a niche or unknown product; well-known tools (Kafka) are vocabulary
		"a link":              goodReply + " See https://example.com/queues for more.",
		"a hashtag":           goodReply + " #golang",
		"an emoji":            goodReply + " 🚀",
		"invented experience": goodReply + " We ran this for years of experience at my company.",
		"too short":           "Try Redis and retries.",
		"too long":            strings.Repeat("word ", 95),
	}
	for name, reply := range cases {
		if p := CheckCommunityReply(reply, replyThread, replyFacts); len(p) == 0 {
			t.Errorf("%s must be rejected: %q", name, reply)
		}
	}
	if p := CheckCommunityReply(goodReply, replyThread, ""); len(p) != 1 || !strings.Contains(p[0], "no verified owner profile") {
		t.Fatalf("with no profile nothing is checkable, so nothing passes: %v", p)
	}
	if p := CheckCommunityReply("", replyThread, replyFacts); len(p) == 0 {
		t.Fatal("an empty reply is rejected")
	}
}

func TestCommunityMentionFollowsTheRowsFlag(t *testing.T) {
	plain := goodReply
	promo := goodReply + " I built ComputeHive for exactly this, a distributed task runner in Go."
	twice := promo + " My project ComputeHive also retries."
	if p := CheckCommunityMention(plain, false); len(p) != 0 {
		t.Fatalf("no mention, none flagged: %v", p)
	}
	if p := CheckCommunityMention(promo, false); len(p) == 0 {
		t.Fatal("promoting on an unflagged thread is rejected")
	}
	if p := CheckCommunityMention(promo, true); len(p) != 0 {
		t.Fatalf("one sentence on a flagged thread is fine: %v", p)
	}
	if p := CheckCommunityMention(twice, true); len(p) == 0 {
		t.Fatal("two sentences about a project is too many")
	}
}
