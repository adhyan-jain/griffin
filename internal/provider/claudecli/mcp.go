package claudecli

import (
	"encoding/json"
	"os"
	"strings"
)

// MCP points a Claude step at the griffin-mcp server, which gives it JJ's typed tools. The API address and host token
// are the ones of the host that holds the task's lease; the task id is added per step.
type MCP struct {
	Binary    string // path to griffin-mcp
	APIURL    string
	HostToken string
}

// MCPFromEnv reads GRIFFIN_MCP_BINARY, GRIFFIN_API_URL and GRIFFIN_HOST_TOKEN; it returns nil unless all three are set,
// so a step keeps its no-tools prompt where the tools cannot be reached.
func MCPFromEnv() *MCP {
	m := &MCP{Binary: strings.TrimSpace(os.Getenv("GRIFFIN_MCP_BINARY")), APIURL: strings.TrimSpace(os.Getenv("GRIFFIN_API_URL")), HostToken: strings.TrimSpace(os.Getenv("GRIFFIN_HOST_TOKEN"))}
	if m.Binary == "" || m.APIURL == "" || m.HostToken == "" {
		return nil
	}
	return m
}

// mcpServerName is the server's name, so its tools are mcp__griffin__<tool> and --allowedTools "mcp__griffin" allows them all.
const mcpServerName = "griffin"

// config is the --mcp-config JSON for one task.
func (m *MCP) config(taskID string) []byte {
	b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{mcpServerName: map[string]any{
		"command": m.Binary,
		"args":    []string{},
		"env":     map[string]string{"GRIFFIN_API_URL": m.APIURL, "GRIFFIN_HOST_TOKEN": m.HostToken, "GRIFFIN_TASK_ID": taskID},
	}}})
	return b
}

const noToolsLine = "You have NO tools: do not run commands, open files or browse."

// toolsLine replaces noToolsLine only where the Griffin tools are really provided.
const toolsLine = "You have only Griffin's own tools (mcp__griffin__*): use them to read or change the owner's Griffin data. A tool that would reach outside Griffin only asks the owner for approval. Do not run commands, open files or browse."

// leadToolsNote is added to a lead step, whose preamble lists other tools.
const leadToolsNote = "\nYou also have Griffin's own tools (mcp__griffin__*) for the owner's tasks, trackers, listings and approvals; a tool that would reach outside Griffin only asks the owner for approval."
