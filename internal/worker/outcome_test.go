package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type stubExec struct{}

func (stubExec) Execute(context.Context, *domain.Task) (*ExecutionResult, error) { return nil, nil }
func (stubExec) Name() string                                                    { return "stub" }
func (stubExec) IsHealthy(context.Context) (bool, error)                         { return true, nil }

func plainTask() *domain.Task {
	return &domain.Task{ManagerDomain: "research", Title: "t", Goal: "g", InputPayload: json.RawMessage(`{}`)}
}

func chatTask() *domain.Task {
	return &domain.Task{ManagerDomain: "JJ", Title: "chat", Goal: "hi", InputPayload: json.RawMessage(`{"kind":"chat"}`)}
}

func TestInterpretResultSuccess(t *testing.T) {
	o := InterpretResult(plainTask(), stubExec{}, &ExecutionResult{Status: "SUCCESS", Output: json.RawMessage(`{"text":"ok"}`)}, nil)
	if o.Failed() || string(o.Output) != `{"text":"ok"}` {
		t.Fatalf("unexpected outcome %+v", o)
	}
}

func TestInterpretResultNeverCompletesWithNoResult(t *testing.T) {
	for name, res := range map[string]*ExecutionResult{
		"nil result":   nil,
		"empty output": {Status: "SUCCESS"},
		"empty object": {Status: "SUCCESS", Output: json.RawMessage(`{}`)},
	} {
		o := InterpretResult(plainTask(), stubExec{}, res, nil)
		if !o.Failed() || !o.Retryable {
			t.Errorf("%s: a run with no result must be a retryable failure, got %+v", name, o)
		}
	}
}

func TestInterpretResultFailures(t *testing.T) {
	o := InterpretResult(plainTask(), stubExec{}, nil, errors.New("boom"))
	if !o.Failed() || o.FailReason != "boom" || o.Retryable {
		t.Fatalf("an execution error is a non-retryable failure by default, got %+v", o)
	}
	o = InterpretResult(plainTask(), stubExec{}, &ExecutionResult{Error: errors.New("rate limited"), Retryable: true}, nil)
	if !o.Failed() || !o.Retryable {
		t.Fatalf("a retryable provider error stays retryable, got %+v", o)
	}
	o = InterpretResult(plainTask(), stubExec{}, &ExecutionResult{Error: errors.New("Authentication required: run agy auth login"), Retryable: true}, nil)
	if !o.Failed() || o.Retryable {
		t.Fatalf("an authentication failure must never be retried, got %+v", o)
	}
}

func TestInterpretResultApproval(t *testing.T) {
	ap := &domain.Approval{}
	o := InterpretResult(plainTask(), stubExec{}, &ExecutionResult{RequiresApproval: ap}, nil)
	if o.Failed() || o.NeedsApproval != ap {
		t.Fatalf("a paused run reports the approval, got %+v", o)
	}
}

func TestInterpretResultChat(t *testing.T) {
	o := InterpretResult(chatTask(), stubExec{}, &ExecutionResult{TextOutput: "hello there", Output: json.RawMessage(`{"response":"hello there"}`)}, nil)
	if o.Failed() || !strings.Contains(string(o.Output), `"response":"hello there"`) || !strings.Contains(string(o.Output), `"name":"stub"`) {
		t.Fatalf("chat output must carry the response and provider, got %+v", o)
	}
	o = InterpretResult(chatTask(), stubExec{}, &ExecutionResult{TextOutput: "   ", Output: json.RawMessage(`{"x":1}`)}, nil)
	if !o.Failed() || !o.Retryable {
		t.Fatalf("a blank chat reply is a retryable failure, got %+v", o)
	}
}

func TestWithDependencyOutputs(t *testing.T) {
	task := plainTask()
	if got := WithDependencyOutputs(task, nil); got != task {
		t.Fatal("with no prerequisites the task is returned unchanged")
	}
	got := WithDependencyOutputs(task, []repository.DependencyOutput{{Title: "gather", Output: json.RawMessage(`{"text":"facts"}`)}})
	if got == task || !strings.Contains(got.Goal, "gather") || !strings.Contains(got.Goal, "facts") {
		t.Fatalf("prerequisite output must reach the prompt, got %q", got.Goal)
	}
	if strings.Contains(task.Goal, "facts") {
		t.Fatal("the original task must not be modified")
	}
}
