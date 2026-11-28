// Command griffin-mcp is a stdio MCP server that gives a model task running on a laptop or EC2 (the Claude CLI, agy)
// the same typed tools JJ uses in chat. It forwards each call to the Griffin API as the host that holds the task.
//
//	GRIFFIN_API_URL     API origin, e.g. https://api.example.com
//	GRIFFIN_HOST_TOKEN  the host token
//	GRIFFIN_TASK_ID     the task this run belongs to (the host must hold its lease)
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

func main() {
	api, tok, task := strings.TrimSpace(os.Getenv("GRIFFIN_API_URL")), strings.TrimSpace(os.Getenv("GRIFFIN_HOST_TOKEN")), strings.TrimSpace(os.Getenv("GRIFFIN_TASK_ID"))
	if api == "" || tok == "" || task == "" {
		fmt.Fprintln(os.Stderr, "griffin-mcp: GRIFFIN_API_URL, GRIFFIN_HOST_TOKEN and GRIFFIN_TASK_ID are required")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newServer(api, tok, task).serve(ctx, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "griffin-mcp:", err)
		os.Exit(1)
	}
}
