package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

type mockClassifierBrain struct {
	enabled bool
	reply   string
	err     error
	calls   int
	system  string
	turns   []gemini.Turn
}

func (m *mockClassifierBrain) Enabled() bool { return m.enabled }
func (m *mockClassifierBrain) Chat(_ context.Context, system string, turns []gemini.Turn) (string, error) {
	m.calls++
	m.system = system
	m.turns = turns
	return m.reply, m.err
}

func TestSmartClassifier_DirectControlCommandsBypassLLM(t *testing.T) {
	brain := &mockClassifierBrain{enabled: true, reply: `{"intent":"conversation"}`}
	classifier := NewSmartClassifier(brain)

	commands := []struct {
		text     string
		wantKind IntentKind
	}{
		{"list my tasks", IntentListTasks},
		{"what are my tasks", IntentListTasks},
		{"kya chal raha hai", IntentListTasks},
		{"list approvals", IntentListApprovals},
		{"approve it", IntentApprove},
		{"reject", IntentReject},
		{"cancel active tasks", IntentCancelTasks},
		{"delete finished tasks", IntentDeleteTasks},
	}

	for _, tc := range commands {
		intent := classifier.Classify(context.Background(), tc.text)
		assert.Equal(t, tc.wantKind, intent.Kind, "command %q should match without LLM call", tc.text)
		cmd, ok := intent.ToTaskCommand()
		assert.True(t, ok, "should convert to task command")
		assert.NotEmpty(t, cmd.Kind)
	}
	assert.Zero(t, brain.calls, "deterministic database commands must not call LLM")
}

func TestSmartClassifier_LLMDecomposesMultipleIndependentTasks(t *testing.T) {
	mockResponse := `{
		"intent": "create_tasks",
		"tasks": [
			{
				"title": "Research quantum computing",
				"goal": "Research quantum computing architectures and recent breakthroughs",
				"manager_domain": "research",
				"priority": "NORMAL_P2"
			},
			{
				"title": "Buy groceries",
				"goal": "Buy groceries: milk, eggs, bread",
				"manager_domain": "personal_ops",
				"priority": "NORMAL_P2"
			},
			{
				"title": "Fix auth bug",
				"goal": "Fix the authentication bug on github repository",
				"manager_domain": "engineering",
				"priority": "HIGH_P1"
			}
		],
		"reason": "decomposed 3 independent user tasks"
	}`

	brain := &mockClassifierBrain{enabled: true, reply: mockResponse}
	classifier := NewSmartClassifier(brain)

	prompt := "1. Research quantum computing\n2. Buy groceries\n3. Fix auth bug on github"
	intent := classifier.Classify(context.Background(), prompt)

	assert.Equal(t, 1, brain.calls, "must consult LLM for task decomposition")
	assert.Equal(t, IntentCreateTasks, intent.Kind)
	require.Len(t, intent.Tasks, 3, "must decompose into 3 independent tasks")

	assert.Equal(t, "Research quantum computing", intent.Tasks[0].Title)
	assert.Equal(t, "research", intent.Tasks[0].ManagerDomain)
	assert.Equal(t, domain.TaskPriorityNormalP2, intent.Tasks[0].Priority)

	assert.Equal(t, "Buy groceries", intent.Tasks[1].Title)
	assert.Equal(t, "personal_ops", intent.Tasks[1].ManagerDomain)
	assert.Equal(t, domain.TaskPriorityNormalP2, intent.Tasks[1].Priority)

	assert.Equal(t, "Fix auth bug", intent.Tasks[2].Title)
	assert.Equal(t, "engineering", intent.Tasks[2].ManagerDomain)
	assert.Equal(t, domain.TaskPriorityHighP1, intent.Tasks[2].Priority)
}

func TestSmartClassifier_LLMClassifiesAutomatedCall(t *testing.T) {
	mockResponse := `{
		"intent": "automated_call",
		"ref": "mom",
		"target": "I will be 15 minutes late for dinner",
		"reason": "user wants an automated outbound phone call"
	}`

	brain := &mockClassifierBrain{enabled: true, reply: mockResponse}
	classifier := NewSmartClassifier(brain)

	prompt := "make an automated call to mom and tell her I will be 15 minutes late for dinner"
	intent := classifier.Classify(context.Background(), prompt)

	assert.Equal(t, 1, brain.calls)
	assert.Equal(t, IntentAutomatedCall, intent.Kind)
	assert.Equal(t, "mom", intent.Ref)
	assert.Equal(t, "I will be 15 minutes late for dinner", intent.Target)

	cmd, ok := intent.ToTaskCommand()
	assert.True(t, ok)
	assert.Equal(t, CmdAutomatedCall, cmd.Kind)
	assert.Equal(t, "mom", cmd.Ref)
	assert.Equal(t, "I will be 15 minutes late for dinner", cmd.Target)
}

func TestSmartClassifier_OfflineRegexFallbackMultiTask(t *testing.T) {
	// Brain is disabled/offline
	classifier := NewSmartClassifier(nil)

	// Numbered list
	text := "1. Research top 5 laptops under 60000\n2. Send a whatsapp to mom saying I'll be home late\n3. Check open github issues on the repo"
	intent := classifier.Classify(context.Background(), text)

	assert.Equal(t, IntentCreateTasks, intent.Kind)
	require.Len(t, intent.Tasks, 3, "offline fallback decomposes numbered lists")
	assert.Equal(t, "research", intent.Tasks[0].ManagerDomain)
	assert.Equal(t, "personal_ops", intent.Tasks[1].ManagerDomain)
	assert.Equal(t, "engineering", intent.Tasks[2].ManagerDomain)
}

func TestSmartClassifier_OfflineRegexFallbackCompoundTask(t *testing.T) {
	classifier := NewSmartClassifier(nil)

	text := "make a task to buy groceries and another task to fix the docker build"
	intent := classifier.Classify(context.Background(), text)

	assert.Equal(t, IntentCreateTasks, intent.Kind)
	require.Len(t, intent.Tasks, 2, "offline fallback decomposes compound tasks")
	assert.Equal(t, "personal_ops", intent.Tasks[0].ManagerDomain)
	assert.Equal(t, "engineering", intent.Tasks[1].ManagerDomain)
}

func TestSmartClassifier_OfflineRegexFallbackSingleTask(t *testing.T) {
	classifier := NewSmartClassifier(nil)

	text := "create a task to audit AWS spend"
	intent := classifier.Classify(context.Background(), text)

	assert.Equal(t, IntentCreateTasks, intent.Kind)
	require.Len(t, intent.Tasks, 1)
	assert.True(t, strings.Contains(intent.Tasks[0].Goal, "audit AWS spend"))
	assert.Equal(t, "finance", intent.Tasks[0].ManagerDomain)
}
