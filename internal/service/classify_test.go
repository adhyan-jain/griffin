package service

import "testing"

func TestClassifyMessage(t *testing.T) {
	c, o := MessageKindConversation, MessageKindObjective
	cases := []struct {
		name string
		text string
		want MessageKind
	}{
		// greetings and small talk
		{"hi", "hi", c},
		{"hello jj", "Hello JJ!", c},
		{"hey how are you", "hey, how are you doing today", c},
		{"thanks", "thanks, that helped", c},
		{"good morning", "Good morning", c},
		{"empty-ish punctuation", "...", c},
		{"incident greeting", "Hi JJ. Reply with a short greeting and tell me which execution provider you are currently using.", c},
		// questions and explanations
		{"what is", "What is a goroutine", c},
		{"why", "Why does my deploy take so long", c},
		{"how", "How do I write a report in markdown", c},
		{"question mark only", "the sky is blue today, right?", c},
		{"explain bullets", "Explain what a Go worker does in three bullet points", c},
		{"describe", "Describe how leases work in a task queue", c},
		{"tell me", "Tell me about Postgres row level security", c},
		{"define", "Define idempotency", c},
		{"can you explain", "Can you explain context cancellation in Go?", c},
		{"question with research words", "What does research on task queues usually conclude?", c},
		{"question about compare", "How would I compare two libraries?", c},
		{"question mentions repo", "What does the repo contain?", c},
		{"technical question", "What is the difference between a mutex and a channel in Go?", c},
		{"is question", "Is Redis faster than Postgres for queues", c},
		{"do you", "Do you remember what we discussed?", c},
		{"write a haiku is cheap", "Write a haiku about spring", c},
		{"ambiguous noun", "task queues", c},
		{"empty", "   ", c},
		// objectives
		{"research and compare", "Research three open-source Go task queue libraries and compare them", o},
		{"investigate", "Investigate why our p99 latency doubled last week", o},
		{"analyse", "Analyse these three pricing pages", o},
		{"analyze us", "analyze the competitor landscape for AI notebooks", o},
		{"draft proposal", "Draft a proposal for the hackathon", o},
		{"write report", "Write a report on cgroup v2 limits", o},
		{"give me a report", "Give me a report on EC2 capacity errors", o},
		{"summarise with sources", "Summarise the latest Go release notes with sources", o},
		{"summarize no deliverable", "Summarize that in one line", c},
		{"build", "Build a CLI that lists my open pull requests", o},
		{"create", "Create a table of AWS regions and their latencies", o},
		{"plan", "Plan a 3 day trip to Lisbon", o},
		{"prepare", "Prepare a study checklist for the CKA exam", o},
		{"investigate and fix", "Investigate and fix the bug in the codebase", o},
		{"debug", "Debug the failing tests in the repo", o},
		{"polite can you", "Can you research three Go queue libraries and compare them?", o},
		{"please", "Please compile a comparison of Kafka and NATS", o},
		{"i need a report", "I need a report comparing Kafka and NATS", o},
		{"greeting then task", "Hi JJ. Research Go task queues and compare them.", o},
		{"report is not repo", "Write a report about the repo layout", o},
		{"multi sentence", "Thanks. Find the top 5 remote Go jobs and draft a cover letter for each.", o},
		{"apply for internship", "Apply for software engineering internships at Google", o},
		{"make a list", "Make a list of top 10 remote tech internships", o},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := ClassifyMessageWithReason(tc.text)
			if got != tc.want {
				t.Fatalf("ClassifyMessage(%q) = %s (%s), want %s", tc.text, got, reason, tc.want)
			}
			if reason == "" {
				t.Fatalf("empty reason for %q", tc.text)
			}
			if ClassifyMessage(tc.text) != tc.want {
				t.Fatalf("ClassifyMessage disagrees with ClassifyMessageWithReason for %q", tc.text)
			}
		})
	}
}

func TestClassifyMessageReportIsNotRepo(t *testing.T) {
	// planner.classifyIntent matches whole words; "report" must not be read as "repo".
	if classifyIntent("write a report") == "engineering" {
		t.Fatal("report must not route to engineering")
	}
}

// Requests for WhatsApp or a web search must reach the planner. They used to be classified as conversation, so
// the owner got "I cannot send messages on WhatsApp" from a chat answer and the WhatsApp worker never ran.
func TestDeviceRequestsAreWorkNotConversation(t *testing.T) {
	work := []string{
		"text bhai wassup on whatsapp",
		"Text bhai wassup on WhatsApp",
		"message bro on whatsapp saying I'm late",
		"send a whatsapp to mom",
		"check my unread whatsapp chats",
		"read my whatsapp messages from Sam",
		"tell Rahul on whatsapp that I will be late",
		"can you text bhai on whatsapp?",
		"please message mom on whatsapp that dinner is ready",
		"whatsapp hello to bhai",
		"whatsapp bhai hello",
		"whatsapp to bhai saying hello",
		"can you whatsapp bhai?",
		"please whatsapp mom that dinner is ready",
		"whatsapp Sam Lee saying test 123",
		"search the web for the best laptops under 60000",
		"google best pizza in Pune",
		"summarise https://example.com/article",
	}
	for _, s := range work {
		if k, why := ClassifyMessageWithReason(s); k != MessageKindObjective {
			t.Errorf("%q should be work, got %s (%s)", s, k, why)
		}
	}
	conversation := []string{
		"how does whatsapp web work?",
		"what is whatsapp",
		"why did whatsapp change its logo?",
		"explain how a web search engine works",
		"is whatsapp safe to use",
		"I like whatsapp",
		"hello",
		"what is the weather in Pune",
		"write a whatsapp bot in python", // building something, not sending a message
	}
	for _, s := range conversation {
		if k, why := ClassifyMessageWithReason(s); k == MessageKindObjective && why == "whatsapp-request" || k == MessageKindObjective && why == "web-request" {
			t.Errorf("%q must stay a conversation, got %s (%s)", s, k, why)
		}
	}
}
