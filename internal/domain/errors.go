package domain

import "errors"

var (
	ErrNotFound               = errors.New("resource not found")
	ErrConflict               = errors.New("resource conflict")
	ErrUnauthorized           = errors.New("unauthorized")
	ErrForbidden              = errors.New("forbidden: insufficient permissions")
	ErrInvalidTransition      = errors.New("invalid state transition")
	ErrLeaseExpired           = errors.New("lease has expired or was acquired by another worker")
	ErrIdempotentConflict     = errors.New("duplicate request: idempotency key conflict")
	ErrDependencyFailed       = errors.New("one or more prerequisite tasks failed or were cancelled")
	ErrValidation             = errors.New("validation failed")
	ErrApprovalNotPending     = errors.New("approval is not in pending state")
	ErrTaskAlreadyTerminal    = errors.New("task is already in a terminal state")
	ErrAuthenticationRequired = errors.New("ai execution provider authentication expired or required")
)
