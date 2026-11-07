package worker

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// Outcome is what to report for an executed task: a result, a failure, or a request for approval.
// It exists for runners that report over HTTP (the laptop and phone hosts) and so cannot use
// Worker.ProcessNext, which talks to the database directly. The rules are the same as ProcessNext's.
type Outcome struct {
	// Output is the task's output payload when it succeeded.
	Output json.RawMessage
	// FailReason is non-empty when the task failed.
	FailReason string
	// Retryable says whether a failure may be retried.
	Retryable bool
	// NeedsApproval is set when the run paused for the owner's approval.
	NeedsApproval *domain.Approval
}

// Failed reports whether the outcome is a failure.
func (o Outcome) Failed() bool { return o.FailReason != "" }

// WithDependencyOutputs returns a copy of task whose prompt also carries the outputs of its completed
// prerequisites (for this run only; nothing is persisted).
func WithDependencyOutputs(task *domain.Task, deps []repository.DependencyOutput) *domain.Task {
	return withDependencyOutputs(task, deps, 0)
}

// InterpretResult decides what to report for an execution, with the same guards as ProcessNext:
// a "successful" run with no result is a retryable failure, never a completion; a chat turn must
// carry assistant text; an authentication failure is never retried.
func InterpretResult(task *domain.Task, exec TaskExecutor, res *ExecutionResult, execErr error) Outcome {
	if execErr == nil && (res == nil || (res.Error == nil && res.RequiresApproval == nil && domain.IsEmptyOutput(res.Output))) {
		if res == nil {
			res = &ExecutionResult{}
		}
		res.Error = ErrEmptyOutput
		res.Retryable = true
	}

	chat := isChatTask(task)
	var chatText string
	if chat && execErr == nil && res != nil && res.Error == nil && res.RequiresApproval == nil {
		if chatText = chatResponseText(res); strings.TrimSpace(chatText) == "" {
			res = &ExecutionResult{ExecutionID: res.ExecutionID, ProviderName: res.ProviderName, Status: "FAILED", Error: ErrEmptyChatResponse, Retryable: true}
		}
	}

	if execErr != nil || (res != nil && res.Error != nil) {
		failErr := execErr
		retryable := false
		if res != nil {
			if res.Error != nil {
				failErr = res.Error
			}
			retryable = res.Retryable
		}
		msg := failErr.Error()
		if errors.Is(failErr, domain.ErrAuthenticationRequired) || strings.Contains(strings.ToLower(msg), "authentication required") {
			retryable = false
		}
		return Outcome{FailReason: msg, Retryable: retryable}
	}

	if res.RequiresApproval != nil {
		return Outcome{NeedsApproval: res.RequiresApproval}
	}

	output := res.Output
	if chat {
		var facts RuntimeFacts
		if rp, ok := exec.(RuntimeFactsProvider); ok {
			facts = rp.RuntimeFacts()
		}
		output = buildChatOutput(chatText, exec.Name(), facts)
	}
	return Outcome{Output: output}
}
