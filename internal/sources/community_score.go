package sources

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A thread's score is plain arithmetic over its own words and numbers; no model reads it.

type topic struct {
	label  string
	weight int
	re     *regexp.Regexp
}

// topics are the subjects the owner can learn from, help with and be seen in: the whole AI field, plus Go and
// distributed systems (ComputeHive), plus internships and getting in.
var topics = []topic{
	{"agents", 3, regexp.MustCompile(`(?i)\bagents?\b|agentic|\bmcp\b|tool[- ]calling|multi-agent`)},
	{"LLMs", 3, regexp.MustCompile(`(?i)\bllms?\b|language models?|fine-?tun|\bgpt|\bclaude\b|\bllama|\bqwen|\bgemini\b|transformers?\b|prompt(ing| engineering)`)},
	{"RAG", 3, regexp.MustCompile(`(?i)\brag\b|retrieval|embeddings?|vector (db|database|store|search)|reranker`)},
	{"inference", 2, regexp.MustCompile(`(?i)inference|\bvllm\b|quantiz|llama\.cpp|\bgguf\b|\bollama\b|model serving|\bgpus?\b|\bcuda\b`)},
	{"ML", 2, regexp.MustCompile(`(?i)machine learning|deep learning|\bmlops\b|neural net|\bpytorch\b|\bml (engineer|system|model)`)},
	{"Go", 3, regexp.MustCompile(`(?i)\bgolang\b|\bgoroutines?\b|\bgo (backend|service|concurrency|channels?|modules?|routines?)\b|\bin go\b|\bwritten in go\b`)},
	{"distributed systems", 3, regexp.MustCompile(`(?i)distributed|consensus|\braft\b|sharding|microservices?|message (queue|broker)|\bkafka\b|\bgrpc\b`)},
	{"task queues", 3, regexp.MustCompile(`(?i)task (queue|runner|scheduler)|job (queue|scheduler|runner)|worker (pool|queue|nodes?)|background (jobs?|tasks?|workers?)|\bcelery\b|\basynq\b|\btemporal\b|workflow (engine|orchestrat)|\bcron\b`)},
	{"Redis", 2, regexp.MustCompile(`(?i)\bredis\b`)},
	{"Docker", 2, regexp.MustCompile(`(?i)\bdocker\b|kubernetes|\bk8s\b|container(s|ized|ised)?\b`)},
	{"internships", 3, regexp.MustCompile(`(?i)\binterns?(hips?)?\b`)},
	{"India", 2, regexp.MustCompile(`(?i)\bindia\b|\bindian\b|\bvit\b|bangalore|bengaluru|\biits?\b|\bnits?\b|\btier[- ]?[123] college`)},
	{"interviews", 2, regexp.MustCompile(`(?i)interview|resume|\bcv\b|referral|placement|new grad|campus`)},
}

// mentions are the owner's projects and the problems each one answers. A thread whose own words describe such a
// problem is flagged; the flag is only that, never a draft.
var mentions = []struct {
	project string
	re      *regexp.Regexp
}{
	{"ComputeHive (his distributed task runner in Go with Redis and Docker)", regexp.MustCompile(`(?i)task (queue|runner|scheduler|distribution)|job (queue|scheduler|runner)|worker (pool|queue|nodes?)|distributed (task|job|worker|queue|scheduler|execution)|background (jobs?|tasks?|workers?)|\bcelery\b|\basynq\b|\bsidekiq\b|\bcron\b|workflow (engine|orchestrat)|run(ning)? (jobs|tasks) (across|on) (many|multiple)`)},
	{"his AI agent work", regexp.MustCompile(`(?i)agent (framework|orchestrat|memory|reliab|eval|harness|architecture|infrastructure|sandbox)|multi-agent|tool[- ]calling`)},
}

var questionish = regexp.MustCompile(`(?i)\?\s*$|\bhow (do|can|should|would) (i|we|you)\b|\bhow to\b|\bhelp\b|\badvice\b|\banyone\b|\brecommend|\bsuggest|\btips\b|\bwhat (is|are|should)\b|\bany (idea|way)`)

// Window is how old a thread may be.
const Window = 48 * time.Hour

// Scored is a thread that fits, with the reasons, from code.
type Scored struct {
	Thread  Thread
	Score   int
	Topics  []string
	Why     string
	Mention string // the project worth mentioning, or ""
}

// ScoreThread rates one thread; ok is false for a thread that is not for the owner: adult, locked, archived,
// pinned, too old, or with nothing about his topics.
func ScoreThread(t Thread, sub *RedditSub, now time.Time) (Scored, bool) {
	age := now.Sub(t.Posted)
	if t.NSFW || t.Locked || t.Archived || t.Stickied || t.Title == "" || t.URL == "" || age > Window {
		return Scored{}, false
	}
	if age < 0 {
		age = 0
	}
	text := t.Title + " " + t.Text
	var labels []string
	score := 0
	for _, tp := range topics {
		if tp.re.MatchString(text) {
			labels = append(labels, tp.label)
			score += tp.weight
		}
	}
	if sub != nil && sub.Topic != "" && !has(labels, sub.Topic) {
		labels = append(labels, sub.Topic)
		score += 2
	}
	if len(labels) == 0 {
		return Scored{}, false
	}
	if score > 8 {
		score = 8
	}
	var notes []string
	if questionish.MatchString(t.Title) {
		score += 2
		notes = append(notes, "a question")
	}
	switch {
	case t.Comments > 100 || t.Score > 1000:
		score -= 3
		notes = append(notes, "already huge, a reply may be lost")
	case t.Comments >= 1 && t.Comments <= 30:
		score += 2
		notes = append(notes, fmt.Sprintf("%s so far, a reply is seen", plural(t.Comments, "comment")))
	case t.Comments == 0:
		score++
		notes = append(notes, "no replies yet")
	}
	switch {
	case age < 12*time.Hour:
		score += 2
	case age < 24*time.Hour:
		score++
	}
	notes = append(notes, "posted "+ago(age))
	s := Scored{Thread: t, Score: score, Topics: labels, Why: "About " + strings.Join(labels, ", ") + "; " + strings.Join(notes, "; ")}
	for _, m := range mentions {
		if m.re.MatchString(text) {
			s.Mention = m.project
			break
		}
	}
	return s, true
}

// MinScore is the least a thread scores to be listed.
const MinScore = 4

// PickCommunityThreads scores every thread, drops the ones that do not fit or were already listed (known holds
// lower-cased URLs), keeps one entry per URL and returns the best max, best first.
func PickCommunityThreads(threads []Thread, known map[string]bool, now time.Time, max int) []Scored {
	subs := map[string]*RedditSub{}
	for i := range RedditSubs {
		subs["r/"+strings.ToLower(RedditSubs[i].Name)] = &RedditSubs[i]
	}
	seen := map[string]bool{}
	var out []Scored
	for _, t := range threads {
		key := strings.ToLower(t.URL)
		if seen[key] || known[key] {
			continue
		}
		seen[key] = true
		var sub *RedditSub
		if t.Platform == Reddit {
			sub = subs[strings.ToLower(t.Community)]
		}
		if s, ok := ScoreThread(t, sub, now); ok && s.Score >= MinScore {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Thread.Posted.After(out[j].Thread.Posted)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func has(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func ago(d time.Duration) string {
	if d < time.Hour {
		return "under an hour ago"
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
