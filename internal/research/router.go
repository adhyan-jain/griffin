package research

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/provider/gemini"
)

// LLMClient is the interface needed by the research engine for model calls.
type LLMClient interface {
	Chat(ctx context.Context, system string, turns []gemini.Turn) (string, error)
}

// Router routes reasoning tasks to appropriate models and enforces prompt security.
type Router struct {
	StrongClient LLMClient // Strong model for planning, conflict resolution and synthesis
	LiteClient   LLMClient // Fast/lite model for structured extraction and classification
}

// NewRouter creates a new ModelRouter.
func NewRouter(strong, lite LLMClient) *Router {
	return &Router{StrongClient: strong, LiteClient: lite}
}

// SanitizeExternalContent wraps untrusted external web data in clear boundary tags
// and strips potential prompt injection vectors.
func SanitizeExternalContent(raw string) string {
	cleaned := strings.ReplaceAll(raw, "<script>", "")
	cleaned = strings.ReplaceAll(cleaned, "</script>", "")
	cleaned = strings.ReplaceAll(cleaned, "SYSTEM INSTRUCTION:", "EXTERNAL CONTENT:")
	cleaned = strings.ReplaceAll(cleaned, "Ignore previous instructions", "[redacted instruction phrase]")
	return fmt.Sprintf("\n<UNTRUSTED_EXTERNAL_DATA>\n%s\n</UNTRUSTED_EXTERNAL_DATA>\n", strings.TrimSpace(cleaned))
}

// AskPlan asks the strong model to break an objective into research questions and initial queries.
func (r *Router) AskPlan(ctx context.Context, objective string, constraints []string, depth string) (*PlanResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("no router configured")
	}
	system := `You are an elite principal research investigator. Your job is to convert a research objective into a thorough, iterative research plan.
You must return only valid JSON matching the following schema:
{
  "questions": [
    {"id": "q1", "question": "What is ...?", "category": "overview"}
  ],
  "initial_queries": ["search query 1", "search query 2"]
}
Strictly adhere to:
1. Generate specific, high-yield questions covering foundational facts, entities, traction/metrics, and risks.
2. Formulate 3-6 distinct, keyword-optimized search queries using varied terminology and operator syntax if helpful.
3. No commentary or markdown backticks, only JSON.`

	prompt := fmt.Sprintf("Objective: %s\nResearch Depth: %s\nConstraints: %s", objective, depth, strings.Join(constraints, "; "))
	client := r.StrongClient
	if client == nil {
		client = r.LiteClient
	}
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured")
	}

	reply, err := client.Chat(ctx, system, []gemini.Turn{{Role: "user", Text: prompt}})
	if err != nil {
		return nil, fmt.Errorf("ask plan LLM error: %w", err)
	}

	reply = cleanJSON(reply)
	var resp PlanResponse
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal plan response: %w (raw: %s)", err, reply)
	}
	return &resp, nil
}

// PlanResponse is the expected JSON response from planning.
type PlanResponse struct {
	Questions []struct {
		ID       string `json:"id"`
		Question string `json:"question"`
		Category string `json:"category"`
	} `json:"questions"`
	InitialQueries []string `json:"initial_queries"`
}

// ExtractClaims asks the lite model to extract structured factual claims from page content.
func (r *Router) ExtractClaims(ctx context.Context, topic, pageURL, pageText string, questions []string) (*ExtractionResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("no router configured")
	}
	system := `You are an evidence extraction engine. Extract factual claims from the provided untrusted web content that help answer the target research questions.
Rules:
1. Every claim MUST be supported by an exact verbatim quote from the text.
2. Never invent or hallucinate information not present in the excerpt.
3. Rate confidence between 0 and 100 based on source authority and clarity.
4. If a claim contradicts common knowledge or is company self-reported, note it.
Output schema:
{
  "findings": [
    {
      "topic": "...",
      "claim": "...",
      "extracted_value": "...",
      "quote": "exact quote from text",
      "confidence": 85,
      "is_contradiction": false
    }
  ],
  "new_leads": ["company name or keyword discovered to research next"]
}`

	userPrompt := fmt.Sprintf("Topic: %s\nSource URL: %s\nQuestions to Answer:\n%s\nContent:%s",
		topic, pageURL, strings.Join(questions, "\n- "), SanitizeExternalContent(pageText),
	)

	client := r.LiteClient
	if client == nil {
		client = r.StrongClient
	}
	if client == nil {
		return nil, fmt.Errorf("no LLM client configured")
	}

	reply, err := client.Chat(ctx, system, []gemini.Turn{{Role: "user", Text: userPrompt}})
	if err != nil {
		return nil, fmt.Errorf("extract claims LLM error: %w", err)
	}

	reply = cleanJSON(reply)
	var resp ExtractionResponse
	if err := json.Unmarshal([]byte(reply), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal extraction response: %w", err)
	}
	return &resp, nil
}

// ExtractionResponse holds findings and discovered leads from a page.
type ExtractionResponse struct {
	Findings []struct {
		Topic           string `json:"topic"`
		Claim           string `json:"claim"`
		ExtractedValue  string `json:"extracted_value"`
		Quote           string `json:"quote"`
		Confidence      int    `json:"confidence"`
		IsContradiction bool   `json:"is_contradiction"`
	} `json:"findings"`
	NewLeads []string `json:"new_leads"`
}

// SynthesizeReport asks the strong model to produce a final markdown report grounded in findings.
func (r *Router) SynthesizeReport(ctx context.Context, objective string, findingsJSON []byte, unresolved []string) (string, map[string]any, error) {
	if r == nil {
		return "", nil, fmt.Errorf("no router configured")
	}
	system := `You are a Principal Analyst. Synthesize the collected research findings into a comprehensive, structured markdown report and a summary payload.
Requirements:
1. Group findings by key themes or entities.
2. Ground conclusions directly in the cited evidence.
3. Explicitly report what remains unknown or weakly supported.
4. Do not invent information to fill gaps.`

	prompt := fmt.Sprintf("Objective: %s\nFindings:\n%s\nUnresolved Questions:\n%s",
		objective, string(findingsJSON), strings.Join(unresolved, "\n- "),
	)

	client := r.StrongClient
	if client == nil {
		client = r.LiteClient
	}
	if client == nil {
		return "", nil, fmt.Errorf("no LLM client configured")
	}

	reply, err := client.Chat(ctx, system, []gemini.Turn{{Role: "user", Text: prompt}})
	if err != nil {
		return "", nil, fmt.Errorf("synthesize report LLM error: %w", err)
	}

	structured := map[string]any{
		"objective":           objective,
		"unresolved_count":    len(unresolved),
		"synthesis_timestamp": "now",
	}
	return reply, structured, nil
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
