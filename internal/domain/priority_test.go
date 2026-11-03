package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePriority(t *testing.T) {
	for _, ok := range []TaskPriority{"", TaskPriorityCriticalP0, TaskPriorityHighP1, TaskPriorityNormalP2, TaskPriorityBackground} {
		if err := ValidatePriority(ok); err != nil {
			t.Errorf("ValidatePriority(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []TaskPriority{"urgent", "high_p1", "HIGH", "1", " NORMAL_P2"} {
		err := ValidatePriority(bad)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("ValidatePriority(%q) = %v, want ErrValidation", bad, err)
			continue
		}
		if !strings.Contains(err.Error(), "CRITICAL_P0, HIGH_P1, NORMAL_P2, BACKGROUND_P3") {
			t.Errorf("error for %q should list the allowed values, got %q", bad, err.Error())
		}
	}
}

func TestTaskValidateDefaultsAndRejectsPriority(t *testing.T) {
	task := &Task{Title: "t", Goal: "g"}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() with omitted priority: %v", err)
	}
	if task.Priority != TaskPriorityNormalP2 {
		t.Errorf("omitted priority defaulted to %q, want %q", task.Priority, TaskPriorityNormalP2)
	}

	task = &Task{Title: "t", Goal: "g", Priority: "urgent"}
	if err := task.Validate(); !errors.Is(err, ErrValidation) {
		t.Errorf("Validate() with invalid priority = %v, want ErrValidation", err)
	}
}
