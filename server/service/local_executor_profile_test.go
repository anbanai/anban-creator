package service

import (
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestManagedAgentProfilesRejectLocalExecution(t *testing.T) {
	err := validateAgentExecutionTarget(model.ExecutionTargetLocal)
	if !errors.Is(err, ErrManagedProfileLocalExecutionUnsupported) {
		t.Fatalf("validate local target = %v, want unsupported", err)
	}
	if err := validateAgentExecutionTarget(model.ExecutionTargetCloud); err != nil {
		t.Fatalf("validate cloud target: %v", err)
	}
}
