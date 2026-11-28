package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// server is the smallest MCP server that serves tools over stdio: newline-delimited JSON-RPC 2.0 with initialize,
// tools/list and tools/call. Every tool lives in the Griffin API; this only forwards, with the host token and task id.
type server struct {
	apiURL, token, taskID string
	http                  *http.Client
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const defaultProtocol = "2024-11-05"

// serve reads requests from in and writes responses to out until in ends.
func (s *server) serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if err := enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 { // a notification (such as notifications/initialized) gets no reply
			continue
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		result, rerr := s.handle(ctx, req)
		if rerr != nil {
			resp.Error = rerr
		} else {
			resp.Result = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *server) handle(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = defaultProtocol
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "griffin-mcp", "version": "1"},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools, err := s.listTools(ctx)
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{-32602, "tools/call needs a name"}
		}
		return s.callTool(ctx, p.Name, p.Arguments), nil
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}

// do sends one API request and returns the "data" of the envelope.
func (s *server) do(ctx context.Context, method, path string, body any) (json.RawMessage, int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.apiURL, "/")+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("the Griffin API answered %d", resp.StatusCode)
		if env.Error != nil && env.Error.Message != "" {
			msg += ": " + env.Error.Message
		}
		return nil, resp.StatusCode, fmt.Errorf("%s", msg)
	}
	return env.Data, resp.StatusCode, nil
}

func (s *server) listTools(ctx context.Context) ([]map[string]any, error) {
	data, _, err := s.do(ctx, http.MethodGet, "/api/v1/host/agent/tools", nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(d.Tools))
	for _, t := range d.Tools {
		out = append(out, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
	}
	return out, nil
}

// callTool returns an MCP tool result; a failure is a result with isError set, so the model can read it and correct itself.
func (s *server) callTool(ctx context.Context, name string, args json.RawMessage) map[string]any {
	text := func(msg string, isErr bool) map[string]any {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}, "isError": isErr}
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}
	data, _, err := s.do(ctx, http.MethodPost, "/api/v1/host/agent/tools/"+url.PathEscape(name), map[string]any{"task_id": s.taskID, "args": args})
	if err != nil {
		return text(err.Error(), true)
	}
	var r struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return text("unreadable answer from the Griffin API", true)
	}
	if r.Error != "" {
		return text(r.Error, true)
	}
	return text(r.Result, false)
}

func newServer(apiURL, token, taskID string) *server {
	return &server{apiURL: apiURL, token: token, taskID: taskID, http: &http.Client{Timeout: 60 * time.Second}}
}
