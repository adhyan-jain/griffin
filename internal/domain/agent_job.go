package domain

import "strings"

// AgentJobTitlePrefix starts the title of every long agent job task. These run in code on the scheduler tick, so the
// EC2 runner and the hosts never claim them and they do not wake EC2.
const AgentJobTitlePrefix = "AGENT JOB - "

// IsAgentJob reports whether a task title is a long agent job.
func IsAgentJob(title string) bool { return strings.HasPrefix(title, AgentJobTitlePrefix) }

// AgentJobPayload is the input_payload of an agent job task.
type AgentJobPayload struct {
	Kind               string `json:"kind"` // always "agent_job"
	Goal               string `json:"goal"`
	ConversationID     string `json:"conversation_id"`     // the job's own conversation in the agent store
	OriginConversation string `json:"origin_conversation"` // the chat that started it; the result is posted there
}
