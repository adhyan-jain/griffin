package httputil

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

// Response represents a standard JSON API envelope.
type Response struct {
	Success bool      `json:"success"`
	Data    any       `json:"data,omitempty"`
	Error   *APIError `json:"error,omitempty"`
}

// APIError encapsulates error details in the JSON envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSON writes a JSON payload with the specified HTTP status code.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{
		Success: true,
		Data:    data,
	})
}

// ErrorJSON maps domain and standard errors to consistent JSON API error envelopes.
func ErrorJSON(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL_ERROR"
	msg := "An unexpected internal server error occurred"

	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		status = http.StatusRequestEntityTooLarge
		code = "PAYLOAD_TOO_LARGE"
		msg = "Request body too large"

	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
		code = "NOT_FOUND"
		msg = err.Error()

	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusUnauthorized
		code = "UNAUTHORIZED"
		msg = err.Error()

	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
		code = "FORBIDDEN"
		msg = err.Error()

	case errors.Is(err, domain.ErrIdempotentConflict):
		status = http.StatusConflict
		code = "IDEMPOTENT_CONFLICT"
		msg = "A task with this idempotency key already exists"

	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
		code = "CONFLICT"
		msg = err.Error()

	case errors.Is(err, domain.ErrValidation):
		status = http.StatusBadRequest
		code = "VALIDATION_ERROR"
		msg = err.Error()

	case errors.Is(err, domain.ErrInvalidTransition):
		status = http.StatusBadRequest
		code = "INVALID_STATE_TRANSITION"
		msg = err.Error()

	case errors.Is(err, domain.ErrLeaseExpired):
		status = http.StatusGone
		code = "LEASE_EXPIRED"
		msg = "Task lease has expired or was reclaimed by another worker"

	case errors.Is(err, domain.ErrApprovalNotPending):
		status = http.StatusBadRequest
		code = "APPROVAL_NOT_PENDING"
		msg = "Approval request is no longer pending"

	case errors.Is(err, domain.ErrTaskAlreadyTerminal):
		status = http.StatusBadRequest
		code = "TASK_ALREADY_TERMINAL"
		msg = "Task is already in a terminal state (COMPLETED, FAILED, or CANCELLED)"

	case errors.Is(err, storage.ErrObjectNotFound):
		status = http.StatusNotFound
		code = "NOT_FOUND"
		msg = err.Error()

	case errors.Is(err, storage.ErrPermissionDenied):
		status = http.StatusForbidden
		code = "FORBIDDEN"
		msg = err.Error()

	case errors.Is(err, storage.ErrInvalidKey):
		status = http.StatusBadRequest
		code = "VALIDATION_ERROR"
		msg = err.Error()

	case errors.Is(err, storage.ErrBucketNotFound):
		status = http.StatusNotFound
		code = "NOT_FOUND"
		msg = err.Error()

	case errors.Is(err, storage.ErrAuthenticationFailed):
		status = http.StatusServiceUnavailable
		code = "SERVICE_UNAVAILABLE"
		msg = "Storage provider authentication failed"

	default:
		if err != nil {
			msg = err.Error()
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Response{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: msg,
		},
	})
}
