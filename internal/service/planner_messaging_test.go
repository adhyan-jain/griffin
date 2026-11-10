package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
)

func TestWantsWhatsApp(t *testing.T) {
	yes := []string{
		"text mom on whatsapp that I'm running late",
		"Send a WhatsApp message to Sam Lee saying happy birthday",
		"check my unread whatsapp chats",
		"read the last 5 messages from Dad on WhatsApp",
		"tell dad on whats app that I reached",
		"whatsapp hello to bhai",
		"whatsapp bhai hello",
		"whatsapp to bhai saying hello",
		"can you whatsapp bhai?",
		"please whatsapp mom that dinner is ready",
		"whatsapp Sam Lee saying test 123",
	}
	no := []string{
		"write code for a WhatsApp bot",
		"build a whatsapp integration with the cloud API",
		"explain how whatsapp end to end encryption works", // no messaging verb
		"send an email to my professor",                    // not whatsapp
		"develop a webhook for whatsapp notifications",
		"whatsapp is safe to use",
		"whatsapp web is not working",
		"whatsapp status update",
		"",
	}
	for _, s := range yes {
		assert.True(t, wantsWhatsApp(s), s)
	}
	for _, s := range no {
		assert.False(t, wantsWhatsApp(s), s)
	}
}

func TestIntentContactFromTheOwnersWords(t *testing.T) {
	cases := map[string]string{
		"text mom on whatsapp that I'm running late":               "mom",
		"Send a WhatsApp message to Sam Lee saying happy birthday": "Sam Lee",
		"tell Dad I'll be late":                                    "Dad",
		"message my brother about dinner on whatsapp":              "brother",
		"read the last 5 messages from Dad on WhatsApp":            "", // names no recipient after a verb
		"whatsapp hello to bhai":                                   "bhai",
		"whatsapp bhai hello":                                      "bhai",
		"whatsapp to bhai saying hello":                            "bhai",
		"can you whatsapp bhai?":                                   "bhai",
		"please whatsapp mom that dinner is ready":                 "mom",
		"whatsapp Sam Lee saying test 123":                         "Sam Lee",
	}
	for in, want := range cases {
		assert.Equal(t, want, intentContactFrom(in), in)
	}
	// A request that names nobody never yields a recipient made of other words.
	for _, in := range []string{"check my unread whatsapp chats", "show whatsapp messages", ""} {
		got := intentContactFrom(in)
		assert.NotContains(t, strings.ToLower(got), "whatsapp", in)
	}
}

func TestWhatsAppRequestsPlanAsComposeThenDo(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	req := &domain.UserRequest{UserID: uuid.New(), RequestText: "text mom on whatsapp that I'm running late", Priority: domain.TaskPriorityNormalP2}
	plan, err := NewRuleBasedPlanner().GeneratePlan(context.Background(), req, reg)
	require.NoError(t, err)
	require.Len(t, plan.ChildTasks, 2)
	assert.Equal(t, "personal_ops", plan.ManagerID)
	assert.Empty(t, plan.ApprovalRequirements, "approval is the gateway's job at the device, not a plan-level gate")

	compose, do := plan.ChildTasks[0], plan.ChildTasks[1]
	assert.Contains(t, compose.Goal, "ONLY a JSON object")
	assert.Contains(t, compose.Goal, "running late")
	require.Len(t, plan.Dependencies, 1)
	assert.Equal(t, domain.PlanDependency{TaskKey: do.Key, DependsOnKey: compose.Key}, plan.Dependencies[0])

	// The second step needs a device with WhatsApp; the first is plain text and needs none.
	assert.NoError(t, domain.ValidateRequires(do.InputPayload))
	assert.NoError(t, domain.ValidateMessaging(do.InputPayload))
	var p struct {
		Requires      []string `json:"requires"`
		IntentContact string   `json:"intent_contact"`
	}
	require.NoError(t, json.Unmarshal(do.InputPayload, &p))
	assert.Equal(t, []string{domain.HostCapMessaging}, p.Requires)
	assert.Equal(t, "mom", p.IntentContact)
	assert.NotContains(t, string(compose.InputPayload), "requires")
}

func TestBuildingAWhatsAppBotIsNotAMessagingJob(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	req := &domain.UserRequest{UserID: uuid.New(), RequestText: "write code for a WhatsApp bot", Priority: domain.TaskPriorityNormalP2}
	plan, err := NewRuleBasedPlanner().GeneratePlan(context.Background(), req, reg)
	require.NoError(t, err)
	for _, c := range plan.ChildTasks {
		assert.NotContains(t, string(c.InputPayload), "messaging")
	}
}

func TestWantsWeb(t *testing.T) {
	yes := []string{
		"search the web for the best way to learn rust",
		"Google the latest go release notes",
		"read https://go.dev/doc/ and summarize it",
		"look it up online: how tall is the eiffel tower",
	}
	no := []string{
		"explain how a web search engine works",
		"build a web search tool in go",
		"write a script to google things",
		"help me plan my week",
	}
	for _, s := range yes {
		assert.True(t, wantsWeb(s), s)
	}
	for _, s := range no {
		assert.False(t, wantsWeb(s), s)
	}
}

func TestWebRequestsPlanSearchReadAnswer(t *testing.T) {
	req := &domain.UserRequest{UserID: uuid.New(), RequestText: "search the web for best rust learning resources", Priority: domain.TaskPriorityNormalP2}
	plan, err := NewRuleBasedPlanner().GeneratePlan(context.Background(), req, manager.NewDefaultRegistry())
	require.NoError(t, err)
	require.Len(t, plan.ChildTasks, 3)
	search, read, answer := plan.ChildTasks[0], plan.ChildTasks[1], plan.ChildTasks[2]
	for _, c := range []domain.ChildTaskPlan{search, read} {
		assert.NoError(t, domain.ValidateRequires(c.InputPayload))
		assert.NoError(t, domain.ValidateWeb(c.InputPayload))
		assert.Contains(t, string(c.InputPayload), `"requires":["web"]`)
	}
	w, err := domain.ParseWebAction(search.InputPayload)
	require.NoError(t, err)
	assert.Equal(t, "best rust learning resources", w.Query, "the lead-in is stripped from the query")
	assert.NotContains(t, string(answer.InputPayload), "requires", "the answer is plain text and runs anywhere")
	assert.Contains(t, answer.Goal, "ONLY the web content")

	req.RequestText = "read https://go.dev/doc/ and tell me what is new."
	plan, err = NewRuleBasedPlanner().GeneratePlan(context.Background(), req, manager.NewDefaultRegistry())
	require.NoError(t, err)
	require.Len(t, plan.ChildTasks, 2)
	w, err = domain.ParseWebAction(plan.ChildTasks[0].InputPayload)
	require.NoError(t, err)
	assert.Equal(t, "https://go.dev/doc/", w.URL)
}

func TestWhatsAppStillWinsOverWeb(t *testing.T) {
	req := &domain.UserRequest{UserID: uuid.New(), RequestText: "text mom on whatsapp the link https://example.com/ticket", Priority: domain.TaskPriorityNormalP2}
	plan, err := NewRuleBasedPlanner().GeneratePlan(context.Background(), req, manager.NewDefaultRegistry())
	require.NoError(t, err)
	assert.Equal(t, "personal_ops", plan.ManagerID)
	assert.Contains(t, string(plan.ChildTasks[len(plan.ChildTasks)-1].InputPayload), `"messaging"`)
}

// "Google" in a request is usually a company or product, not an instruction to search.
func TestGoogleAsAProductNameIsNotASearchCommand(t *testing.T) {
	assert.False(t, wantsWeb("Research best practices for Google Antigravity CLI deployment on Linux"))
	assert.False(t, wantsWeb("compare Google Cloud and AWS pricing"))
	assert.True(t, wantsWeb("Google the best rust tutorials"))
	assert.True(t, wantsWeb("please google how tall the eiffel tower is"))
}

func TestWantsInstagram(t *testing.T) {
	yes := []string{
		"dm sam on insta saying hello",
		"message bro on instagram saying wassup",
		"send an instagram message to mom",
		"dm rahul on ig saying dinner is ready",
		"check my unread instagram chats",
	}
	no := []string{
		"write a bot for instagram",
		"explain how instagram reels work",
		"send an email to my professor",
	}
	for _, s := range yes {
		assert.True(t, wantsInstagram(s), s)
	}
	for _, s := range no {
		assert.False(t, wantsInstagram(s), s)
	}
}

func TestInstagramRequestsPlanAsComposeThenDo(t *testing.T) {
	req := &domain.UserRequest{UserID: uuid.New(), RequestText: "dm sam on insta saying running late", Priority: domain.TaskPriorityNormalP2}
	plan, err := NewRuleBasedPlanner().GeneratePlan(context.Background(), req, manager.NewDefaultRegistry())
	require.NoError(t, err)
	require.Len(t, plan.ChildTasks, 2)
	assert.Equal(t, "personal_ops", plan.ManagerID)

	compose, do := plan.ChildTasks[0], plan.ChildTasks[1]
	assert.Equal(t, "insta_compose", compose.Key)
	assert.Equal(t, "insta_do", do.Key)
	assert.Contains(t, compose.Goal, "ONLY a JSON object")
	assert.Contains(t, compose.Goal, "instagram")
	assert.NoError(t, domain.ValidateRequires(do.InputPayload))
	assert.NoError(t, domain.ValidateMessaging(do.InputPayload))
}
