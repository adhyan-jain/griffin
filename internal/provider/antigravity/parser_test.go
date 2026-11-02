package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOutput_SuccessStructured(t *testing.T) {
	rawStdout := []byte(`{
		"conversation_id": "c1f7a0de-fbb0-4d45-be02-401738739958",
		"status": "SUCCESS",
		"response": "Summary text",
		"duration_seconds": 3.45,
		"num_turns": 1,
		"structured_output": {
			"score": 95,
			"verdict": "pass"
		},
		"usage": {
			"input_tokens": 120,
			"output_tokens": 45,
			"total_tokens": 165
		}
	}`)
	rawStderr := []byte("")

	res, err := ParseOutput(rawStdout, rawStderr, 0)
	require.NoError(t, err)
	assert.Equal(t, "c1f7a0de-fbb0-4d45-be02-401738739958", res.ConversationID)
	assert.Equal(t, "SUCCESS", res.Status)
	assert.Equal(t, "Summary text", res.ResponseText)
	assert.Equal(t, 3.45, res.DurationSeconds)
	assert.Equal(t, 165, res.Usage["total_tokens"])

	var structured struct {
		Score   int    `json:"score"`
		Verdict string `json:"verdict"`
	}
	err = json.Unmarshal(res.StructuredOutput, &structured)
	require.NoError(t, err)
	assert.Equal(t, 95, structured.Score)
	assert.Equal(t, "pass", structured.Verdict)
}

func TestParseOutput_BannerLogsBeforeJSON(t *testing.T) {
	rawStdout := []byte("Info: starting run...\n{\"conversation_id\":\"conv-123\",\"status\":\"SUCCESS\",\"response\":\"done\",\"usage\":{}}\n")
	res, err := ParseOutput(rawStdout, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, "conv-123", res.ConversationID)
	assert.Equal(t, "done", res.ResponseText)
}

func TestParseOutput_AuthFailure(t *testing.T) {
	cases := []struct {
		name   string
		stdout []byte
		stderr []byte
	}{
		{
			name:   "stderr contains auth required",
			stdout: []byte(""),
			stderr: []byte("error: authentication required. Please run `agy auth login`."),
		},
		{
			name:   "stderr contains token expired",
			stdout: []byte(""),
			stderr: []byte("Token expired: please re-authenticate"),
		},
		{
			name:   "stdout contains unauthenticated",
			stdout: []byte("User is unauthenticated"),
			stderr: []byte(""),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOutput(tc.stdout, tc.stderr, 1)
			assert.Error(t, err)
			assert.ErrorIs(t, err, ErrAuthenticationRequired)
		})
	}
}

func TestParseOutput_RateLimitExceeded(t *testing.T) {
	stderr := []byte("error: quota exceeded: too many requests in 1 minute window")
	_, err := ParseOutput([]byte(""), stderr, 1)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrRateLimitExceeded)
}

func TestParseOutput_CLIErrorStatus(t *testing.T) {
	rawStdout := []byte(`{
		"conversation_id": "conv-err",
		"status": "ERROR",
		"error": "Failed to compile generated code",
		"usage": {}
	}`)
	_, err := ParseOutput(rawStdout, nil, 0)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrExecutionFailed)
	assert.Contains(t, err.Error(), "Failed to compile generated code")
}

func TestParseOutput_MalformedJSON(t *testing.T) {
	rawStdout := []byte("Invalid output text with no JSON")
	_, err := ParseOutput(rawStdout, nil, 0)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOutput)
}
