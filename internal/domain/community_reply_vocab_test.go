package domain

import "testing"

func TestOrdinaryTechVocabularyIsNotAClaim(t *testing.T) {
	facts := "Adhyan Jain, B.Tech Computer Science, VIT. ComputeHive: distributed task runner in Go, Redis, Docker."
	thread := "Ask HN: How does one build an LLM router with a labelled set of 100 tasks?"
	reply := "Have you tried a small classifier on your embeddings before reaching for another API call? With a hundred labelled examples, a gradient boosted tree on top of the embedding vectors is cheap to train, and you can keep the GPU for the actual model. Log every misroute and retrain weekly so the router keeps up with drift."
	if p := CheckCommunityReply(reply, thread, facts); len(p) != 0 {
		t.Fatalf("API, GPU and similar words are vocabulary, not claims: %v", p)
	}
	invented := reply + " Acme Corp saw a 40% drop in cost."
	if p := CheckCommunityReply(invented, thread, facts); len(p) == 0 {
		t.Fatal("an invented company and number are still rejected")
	}
}
