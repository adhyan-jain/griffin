package antigravity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// OutputUsage records token consumption reported by the CLI.
type OutputUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// CLIOutput maps the structured JSON emitted by `agy --output-format json`.
type CLIOutput struct {
	ConversationID   string          `json:"conversation_id"`
	Status           string          `json:"status"`
	Response         string          `json:"response"`
	Error            string          `json:"error,omitempty"`
	DurationSeconds  float64         `json:"duration_seconds"`
	NumTurns         int             `json:"num_turns"`
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	Usage            OutputUsage     `json:"usage"`
}

// ParsedResult contains the normalized execution payload.
type ParsedResult struct {
	ConversationID   string
	Status           string
	ResponseText     string
	StructuredOutput json.RawMessage
	Usage            map[string]int
	DurationSeconds  float64
	RawStdout        string
	RawStderr        string
}

var authKeywords = []string{
	"authentication required",
	"unauthenticated",
	"login required",
	"not logged in",
	"token expired",
	"refresh token",
	"auth login",
	"invalid credentials",
}

var rateLimitKeywords = []string{
	"rate limit",
	"quota exceeded",
	"resource exhausted",
	"too many requests",
}

// CheckAuthError checks if either stdout or stderr indicates an authentication failure.
func CheckAuthError(combinedOutput string) bool {
	lower := strings.ToLower(combinedOutput)
	for _, kw := range authKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// CheckRateLimitError checks if output indicates a rate limit or quota exhaustion.
func CheckRateLimitError(combinedOutput string) bool {
	lower := strings.ToLower(combinedOutput)
	for _, kw := range rateLimitKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// ParseOutput extracts and validates CLIOutput from subprocess stdout and stderr.
func ParseOutput(stdoutBytes, stderrBytes []byte, exitCode int) (*ParsedResult, error) {
	stdoutStr := string(stdoutBytes)
	stderrStr := string(stderrBytes)
	combined := stdoutStr + "\n" + stderrStr

	// 1. Fail fast on authentication issues
	if CheckAuthError(combined) {
		return nil, ErrAuthenticationRequired
	}

	// 2. Check for rate limits / quota issues
	if CheckRateLimitError(combined) {
		return nil, ErrRateLimitExceeded
	}

	trimmedStdout := bytes.TrimSpace(stdoutBytes)
	if len(trimmedStdout) == 0 {
		if exitCode != 0 {
			return nil, fmt.Errorf("%w: exit code %d: %s", ErrExecutionFailed, exitCode, strings.TrimSpace(stderrStr))
		}
		return nil, fmt.Errorf("%w: empty stdout from CLI", ErrInvalidOutput)
	}

	// Try parsing directly first
	var cliOut CLIOutput
	err := json.Unmarshal(trimmedStdout, &cliOut)
	if err != nil {
		// Attempt to locate JSON boundaries {...} in case of preceding/trailing banner logs
		startIdx := bytes.IndexByte(trimmedStdout, '{')
		endIdx := bytes.LastIndexByte(trimmedStdout, '}')
		if startIdx >= 0 && endIdx > startIdx {
			err = json.Unmarshal(trimmedStdout[startIdx:endIdx+1], &cliOut)
		}
	}

	if err != nil {
		if exitCode != 0 {
			return nil, fmt.Errorf("%w: exit code %d (unparseable output: %v): %s", ErrExecutionFailed, exitCode, err, strings.TrimSpace(stderrStr))
		}
		return nil, fmt.Errorf("%w: %v (raw stdout: %s)", ErrInvalidOutput, err, string(trimmedStdout))
	}

	// Handle ERROR status inside valid JSON output
	if strings.EqualFold(cliOut.Status, "ERROR") {
		msg := cliOut.Error
		if msg == "" {
			msg = cliOut.Response
		}
		if msg == "" {
			msg = "unspecified error in CLI output"
		}
		return nil, fmt.Errorf("%w: %s", ErrExecutionFailed, msg)
	}

	if exitCode != 0 {
		return nil, fmt.Errorf("%w: exit code %d: %s", ErrExecutionFailed, exitCode, strings.TrimSpace(stderrStr))
	}

	usageMap := map[string]int{
		"input_tokens":  cliOut.Usage.InputTokens,
		"output_tokens": cliOut.Usage.OutputTokens,
		"total_tokens":  cliOut.Usage.TotalTokens,
	}

	var structOut json.RawMessage
	if len(cliOut.StructuredOutput) > 0 && string(cliOut.StructuredOutput) != "null" {
		structOut = cliOut.StructuredOutput
	}

	return &ParsedResult{
		ConversationID:   cliOut.ConversationID,
		Status:           cliOut.Status,
		ResponseText:     cliOut.Response,
		StructuredOutput: structOut,
		Usage:            usageMap,
		DurationSeconds:  cliOut.DurationSeconds,
		RawStdout:        stdoutStr,
		RawStderr:        stderrStr,
	}, nil
}
