package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/griffin/core/internal/domain"
)

func TestProviderHintFor_ChatTask(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		title    string
		goal     string
		expected domain.ProviderHint
	}{
		{
			name:     "chat task with explicit claude hint",
			payload:  `{"kind":"chat","provider_hint":"claude"}`,
			title:    "Chat",
			goal:     "Explain distributed consensus",
			expected: domain.ProviderClaude,
		},
		{
			name:     "chat task with explicit agy hint",
			payload:  `{"kind":"chat","provider_hint":"agy"}`,
			title:    "Chat",
			goal:     "Write a poem",
			expected: domain.ProviderAgy,
		},
		{
			name:     "chat task with pure language/reasoning goal and no explicit hint",
			payload:  `{"kind":"chat"}`,
			title:    "Chat: Architecture Review",
			goal:     "Analyze and compare microservices vs monolithic architecture tradeoffs",
			expected: domain.ProviderClaude,
		},
		{
			name:     "chat task needing machine/tools",
			payload:  `{"kind":"chat"}`,
			title:    "Chat: Browse web",
			goal:     "Browse the web and download the latest releases",
			expected: domain.ProviderAgy,
		},
		{
			name:     "non-chat task with resume tailoring",
			payload:  `{}`,
			title:    "Tailor resume",
			goal:     "Format and polish resume for distributed systems engineer role",
			expected: domain.ProviderClaude,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &domain.Task{
				Title:        tt.title,
				Goal:         tt.goal,
				InputPayload: json.RawMessage(tt.payload),
			}
			got := domain.ProviderHintFor(task)
			if got != tt.expected {
				t.Errorf("ProviderHintFor() = %v, want %v", got, tt.expected)
			}
		})
	}
}
