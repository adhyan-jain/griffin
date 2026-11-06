package antigravity

import (
	"errors"

	"github.com/griffin/core/internal/domain"
)

var (
	ErrAuthenticationRequired = domain.ErrAuthenticationRequired
	ErrBinaryNotFound         = errors.New("antigravity CLI binary not found")
	ErrProcessTimeout         = errors.New("antigravity process execution timed out")
	ErrProcessCancelled       = errors.New("antigravity process execution was cancelled")
	ErrInvalidOutput          = errors.New("malformed or unparseable JSON output from antigravity CLI")
	ErrWorkspacePathTraversal = errors.New("workspace path traversal attempt detected")
	ErrRateLimitExceeded      = errors.New("antigravity provider rate limit or quota exceeded")
	ErrExecutionFailed        = errors.New("antigravity execution returned an error")
	ErrEmptyResult            = errors.New("antigravity returned an empty result")
	ErrToolPermissionDenied   = errors.New("antigravity model attempted a tool the headless runner cannot approve")
)
