package service

import (
	"context"
	"testing"

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
		{"make an api call to fetch users", false},
		{"check the function call stack", false},
		{"send a message on whatsapp to hiten", false},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, wantsCall(c.input), "input: %s", c.input)
	}
}

func TestCallStepsDecomposition(t *testing.T) {
	req := &domain.UserRequest{
		RequestText: "Call Hiten and tell him that I was building an agent and was testing out if it works or not",
		Priority:    domain.TaskPriorityNormalP2,
	}

	planner := NewRuleBasedPlanner()
	reg := manager.NewDefaultRegistry()
	plan, err := planner.GeneratePlan(context.Background(), req, reg)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.Len(t, plan.ChildTasks, 1)

	task := plan.ChildTasks[0]
	assert.Equal(t, "cellular_call", task.Key)
	assert.Contains(t, task.Title, "Hiten")
	assert.Equal(t, "personal_ops", task.ManagerDomain)
	assert.Contains(t, task.CapabilitiesRequired, domain.HostCapCalls)
	assert.Contains(t, string(task.InputPayload), "automated_call")
	assert.Contains(t, string(task.InputPayload), "calls")
}
