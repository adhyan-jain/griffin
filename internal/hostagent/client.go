// Package hostagent is the device side of the Host API: it keeps a laptop or phone present, claims work
// it is allowed to do, runs it and reports back. It talks only to the Griffin API with a per-host token;
// it never has database credentials.
package hostagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/repository"
)

// ErrUnauthorized means the API refused the host token (revoked or wrong). It is not retryable.
var ErrUnauthorized = errors.New("host token refused by the API (revoked or invalid)")

// APIError is a non-success API response.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("api %d %s: %s", e.Status, e.Code, e.Message) }

// Client calls the Host API.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// NewClient builds a client. baseURL is the API origin (for example https://api.example.com).
func NewClient(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do sends a request and decodes the envelope's data into out (when non-nil). It returns (false, nil)
// for 204 No Content. The token is only ever sent as a header and never appears in an error.
func (c *Client) do(ctx context.Context, method, path string, body, out any) (bool, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return false, err
		}
		rd = bytes.NewReader(b)
	}
	return c.doBody(ctx, method, path, rd, "application/json", out)
}

// doBody is do with a ready-made body and content type (a file upload, for example).
func (c *Client) doBody(ctx context.Context, method, path string, rd io.Reader, contentType string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", method, path, scrub(err, c.token))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return false, nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return false, ErrUnauthorized
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env envelope
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode >= 400 || !env.Success && env.Error != nil {
		e := &APIError{Status: resp.StatusCode}
		if env.Error != nil {
			e.Code, e.Message = env.Error.Code, env.Error.Message
		}
		return false, e
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return false, fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return true, nil
}

func scrub(err error, token string) error {
	if token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "[redacted]"))
}

// HeartbeatReply is what the owner currently allows this host.
type HeartbeatReply struct {
	Paused            bool     `json:"paused"`
	Granted           []string `json:"granted"`
	Effective         []string `json:"effective_capabilities"`
	LiveWindowSeconds int      `json:"live_window_seconds"`
	// PolicyVersion is the version of the owner's permission policy; refetch it when it changes.
	PolicyVersion int `json:"policy_version"`
}

// Heartbeat reports presence and what this host can currently do.
func (c *Client) Heartbeat(ctx context.Context, capabilities []string, version string) (*HeartbeatReply, error) {
	if capabilities == nil {
		capabilities = []string{}
	}
	var out HeartbeatReply
	if _, err := c.do(ctx, http.MethodPost, "/api/v1/host/heartbeat", map[string]any{"capabilities": capabilities, "version": version}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Claimed is a task leased to this host, with the outputs of its completed prerequisites.
type Claimed struct {
	Task         *domain.Task
	Dependencies []repository.DependencyOutput
	// ApprovedActions are the approval action types the owner has already granted for this task.
	ApprovedActions []string
}

// Claim leases the next task this host may do. It returns (nil, nil) when there is none.
func (c *Client) Claim(ctx context.Context) (*Claimed, error) {
	var out struct {
		Task         *domain.Task `json:"task"`
		Dependencies []struct {
			Title  string          `json:"title"`
			Output json.RawMessage `json:"output"`
		} `json:"dependency_outputs"`
		ApprovedActions []string `json:"approved_actions"`
	}
	ok, err := c.do(ctx, http.MethodPost, "/api/v1/host/claim", map[string]any{}, &out)
	if err != nil || !ok || out.Task == nil {
		return nil, err
	}
	cl := &Claimed{Task: out.Task, ApprovedActions: out.ApprovedActions}
	for _, d := range out.Dependencies {
		cl.Dependencies = append(cl.Dependencies, repository.DependencyOutput{Title: d.Title, Output: d.Output})
	}
	return cl, nil
}

// RenewLease extends the lease on a running task.
func (c *Client) RenewLease(ctx context.Context, taskID string) error {
	_, err := c.do(ctx, http.MethodPost, "/api/v1/host/tasks/"+taskID+"/heartbeat", map[string]any{}, nil)
	return err
}

// Complete reports a finished task.
func (c *Client) Complete(ctx context.Context, taskID string, output json.RawMessage) error {
	_, err := c.do(ctx, http.MethodPost, "/api/v1/host/tasks/"+taskID+"/complete", map[string]any{"output": output}, nil)
	return err
}

// Fail reports a failed task.
func (c *Client) Fail(ctx context.Context, taskID, reason string, retryable bool) error {
	_, err := c.do(ctx, http.MethodPost, "/api/v1/host/tasks/"+taskID+"/fail", map[string]any{"reason": reason, "retryable": retryable}, nil)
	return err
}

// RequestApproval pauses a running task until the owner decides on one outward action. The API creates the
// approval; the owner sees it in the app and the task resumes (or is cancelled) when they answer.
func (c *Client) RequestApproval(ctx context.Context, taskID string, a *domain.Approval) error {
	_, err := c.do(ctx, http.MethodPost, "/api/v1/host/tasks/"+taskID+"/approval", map[string]any{
		"action_type": a.ActionType, "summary": a.Summary, "risk_level": a.RiskLevel, "payload": a.ActionPayload,
	}, nil)
	return err
}

// PolicyView is the owner's permission policy with its version (0 means none saved: the default applies).
type PolicyView struct {
	Policy  policy.Policy `json:"policy"`
	Version int           `json:"version"`
}

// Policy fetches the owner's permission policy. Hosts can read it and never write it.
func (c *Client) Policy(ctx context.Context) (*PolicyView, error) {
	var out PolicyView
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/host/policy", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
