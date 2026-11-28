package service

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
)

func TestWantsCall(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"Call Hiten and tell him that I was building an agent and was testing out if it works or not", true},
		{"i am going to sleep so actually make a task and call hiten", true},
		{"call mitul in background and tell him that i'll be late for dinner", true},
		{"make an automated call to xyz and tell them abc", true},
		{"phone mom", true},
		{"dial 9876543210", true},
		{"whatsapp call hiten", true},
		{"call hiten on whatsapp", true},
		{"whatsapp call mom for me", true},
		{"make an api call to fetch users", false},
		{"check the function call stack", false},
		{"send a message on whatsapp to hiten", false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, wantsCall(c.input), "input: %s", c.input)
	}
}

func TestCallStepsDecomposition(t *testing.T) {
	planner := NewRuleBasedPlanner()
	reg := manager.NewDefaultRegistry()

	t.Run("supervised_listen", func(t *testing.T) {
		req := &domain.UserRequest{
			RequestText: "Call Hiten and tell him that I was building an agent and was testing out if it works or not and you talk and i can listen",
			Priority:    domain.TaskPriorityNormalP2,
		}
		plan, err := planner.GeneratePlan(context.Background(), req, reg)
		require.NoError(t, err)
		require.Len(t, plan.ChildTasks, 1)

		task := plan.ChildTasks[0]
		assert.Equal(t, "cellular_call", task.Key)
		assert.Contains(t, task.Title, "Hiten")
		assert.Contains(t, task.Title, "supervised speaker")
		assert.Contains(t, string(task.InputPayload), `"call_mode":"supervised_listen"`)
		assert.Contains(t, string(task.InputPayload), `"channel":"cellular"`)
	})

	t.Run("automated_background", func(t *testing.T) {
		req := &domain.UserRequest{
			RequestText: "call mitul in background and tell him that i'll be late for dinner",
			Priority:    domain.TaskPriorityNormalP2,
		}
		plan, err := planner.GeneratePlan(context.Background(), req, reg)
		require.NoError(t, err)
		require.Len(t, plan.ChildTasks, 1)

		task := plan.ChildTasks[0]
		assert.Contains(t, task.Title, "background agent")
		assert.Contains(t, string(task.InputPayload), `"call_mode":"automated_bg"`)
		assert.Contains(t, string(task.InputPayload), `"in_background":true`)
	})

	t.Run("direct_talk_for_me", func(t *testing.T) {
		req := &domain.UserRequest{
			RequestText: "call hiten for me like in that you just open it and i'll talk",
			Priority:    domain.TaskPriorityNormalP2,
		}
		plan, err := planner.GeneratePlan(context.Background(), req, reg)
		require.NoError(t, err)
		require.Len(t, plan.ChildTasks, 1)

		task := plan.ChildTasks[0]
		assert.Contains(t, task.Title, "direct handoff")
		assert.Contains(t, string(task.InputPayload), `"call_mode":"direct_talk"`)
		assert.Contains(t, string(task.InputPayload), `"action":"direct_call"`)
	})

	t.Run("whatsapp_call", func(t *testing.T) {
		req := &domain.UserRequest{
			RequestText: "whatsapp call hiten for me",
			Priority:    domain.TaskPriorityNormalP2,
		}
		plan, err := planner.GeneratePlan(context.Background(), req, reg)
		require.NoError(t, err)
		require.Len(t, plan.ChildTasks, 1)

		task := plan.ChildTasks[0]
		assert.Contains(t, task.Title, "WhatsApp")
		assert.Contains(t, string(task.InputPayload), `"channel":"whatsapp"`)
		assert.Contains(t, string(task.InputPayload), `"action":"whatsapp_call"`)
	})

	t.Run("automated_call_with_speech_message", func(t *testing.T) {
		req := &domain.UserRequest{
			RequestText: "call anish at 8:30am and say wake up bitch",
			Priority:    domain.TaskPriorityNormalP2,
		}
		plan, err := planner.GeneratePlan(context.Background(), req, reg)
		require.NoError(t, err)
		require.Len(t, plan.ChildTasks, 1)

		task := plan.ChildTasks[0]
		assert.Contains(t, strings.ToLower(task.Title), "anish")
		assert.Contains(t, string(task.InputPayload), `"message":"wake up bitch"`)
		assert.Contains(t, string(task.InputPayload), `"requires":["calls"]`)
	})
}

func TestCallRecipientSkipsFillerWords(t *testing.T) {
	cases := map[string]string{
		"call him instead":                        "",
		"call them now":                           "",
		"do a normal sim call again":              "",
		"call back":                               "",
		"call normal sim call to hiten":           "hiten",
		"calling hiten now":                       "hiten",
		"make a sim call to mitul":                "mitul",
		"sim call hiten":                          "hiten",
		"call her back and tell her i'll be late": "",
		"call mom":                                "mom",
	}
	for in, want := range cases {
		assert.Equal(t, want, callRecipient(in), in)
	}
	for _, in := range []string{"calling hiten", "do a sim call", "call him instead"} {
		assert.True(t, wantsCall(in), in)
	}
}

func TestCallWithoutRecipientAsksInsteadOfGuessing(t *testing.T) {
	planner := NewRuleBasedPlanner()
	reg := manager.NewDefaultRegistry()
	_, err := planner.GeneratePlan(context.Background(), &domain.UserRequest{RequestText: "call him instead", Priority: domain.TaskPriorityNormalP2}, reg)
	require.ErrorIs(t, err, ErrCallRecipientMissing)

	plan, err := planner.GeneratePlan(context.Background(), &domain.UserRequest{RequestText: "calling anish and say wake up", Priority: domain.TaskPriorityNormalP2}, reg)
	require.NoError(t, err)
	require.Len(t, plan.ChildTasks, 1)
	assert.NotContains(t, plan.ChildTasks[0].Title, "Hiten")
	assert.Contains(t, strings.ToLower(plan.ChildTasks[0].Title), "anish")
}

type nilCaller struct{}

func (*nilCaller) MakeOutboundCall(context.Context, uuid.UUID, string, string, string) (string, error) {
	return "", nil
}

func TestVoiceCommandsTypedNilCallerIsNotConfigured(t *testing.T) {
	var c *nilCaller // a nil pointer inside an interface value is not == nil
	v := &VoiceCommands{Caller: c}
	res, err := v.makeAutomatedCall(context.Background(), uuid.New(), TaskCommand{Ref: "hiten", Target: "hi"})
	require.NoError(t, err)
	assert.Contains(t, res.Reply, "not configured")
}

func TestPrepListingIDs(t *testing.T) {
	id := uuid.New()
	assert.Equal(t, []uuid.UUID{id}, PrepListingIDs([]byte(`{"prepare_listings":["`+id.String()+`"]}`)))
	assert.Empty(t, PrepListingIDs([]byte(`{}`)))
	assert.Empty(t, PrepListingIDs([]byte(`not json`)))
	assert.False(t, ValidCallContact("instead"))
	assert.True(t, ValidCallContact("Hiten"))
}
