package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/runner"
)

// RunnerHandler provides endpoints for inspecting and controlling the cloud execution runner.
type RunnerHandler struct {
	instanceMgr runner.InstanceManager
	wakeup      RunnerWakeup
	scaleToZero bool
}

func NewRunnerHandler(instanceMgr runner.InstanceManager, wakeup RunnerWakeup, scaleToZero bool) *RunnerHandler {
	return &RunnerHandler{
		instanceMgr: instanceMgr,
		wakeup:      wakeup,
		scaleToZero: scaleToZero,
	}
}

func (h *RunnerHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	state := runner.RunnerStateUnknown
	if h.instanceMgr != nil {
		if s, err := h.instanceMgr.GetRunnerState(ctx); err == nil {
			state = s
		}
	} else {
		state = runner.RunnerStateRunning
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"scale_to_zero_enabled": h.scaleToZero,
		"state":                 state,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *RunnerHandler) WakeRunner(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if h.wakeup != nil {
		if err := h.wakeup.TriggerWakeup(ctx); err != nil && err != runner.ErrWakeupCooldownActive {
			httputil.ErrorJSON(w, err)
			return
		}
	} else if h.instanceMgr != nil {
		if err := h.instanceMgr.StartRunner(ctx); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"message": "Runner wake-up signal dispatched",
		"status":  "waking",
	})
}

func (h *RunnerHandler) SleepRunner(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if h.instanceMgr != nil {
		if err := h.instanceMgr.StopRunner(ctx); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"message": "Runner stop signal dispatched",
		"status":  "stopping",
	})
}
