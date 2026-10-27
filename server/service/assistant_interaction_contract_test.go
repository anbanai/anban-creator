package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInteractionContractUsesStableLifecycleValues(t *testing.T) {
	if got := []string{
		InteractionLifecycleActive,
		InteractionLifecycleReconnecting,
		InteractionLifecycleNeedsConfirmation,
		InteractionLifecycleCompleted,
		InteractionLifecycleAbandoned,
	}; strings.Join(got, ",") != "active,reconnecting,needs_confirmation,completed,abandoned" {
		t.Fatalf("lifecycle values = %v", got)
	}
	if got := []string{
		InteractionRuntimeStarting,
		InteractionRuntimeRunning,
		InteractionRuntimeClosed,
		InteractionRuntimeFailed,
	}; strings.Join(got, ",") != "starting,running,closed,failed" {
		t.Fatalf("runtime values = %v", got)
	}
	if got := []string{
		InteractionDraftPending,
		InteractionDraftConfirmed,
		InteractionDraftConsumed,
		InteractionDraftSuperseded,
	}; strings.Join(got, ",") != "pending,confirmed,consumed,superseded" {
		t.Fatalf("draft confirmation values = %v", got)
	}
}

func TestInteractionErrorContractExposesRecoveryActions(t *testing.T) {
	cases := []struct {
		code    string
		actions []string
	}{
		{InteractionErrorGenerationChanged, []string{"reload_snapshot"}},
		{InteractionErrorReconnecting, []string{"wait_for_recovery"}},
		{InteractionErrorRestartRequired, []string{"restore_draft", "restart"}},
		{InteractionErrorMessageNotAccepted, []string{"retry_message"}},
		{InteractionErrorCommandOutcomeUnknown, []string{"retry_with_new_attempt", "restart"}},
		{InteractionErrorDraftSavedRetryable, []string{"retry_stage", "confirm_draft"}},
		{InteractionErrorDraftRevisionConflict, []string{"reload_draft", "confirm_draft"}},
		{InteractionErrorCommitAlreadyApplied, []string{"view_task"}},
		{InteractionErrorCommitReconciliationRequired, []string{"reconcile_commit"}},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			err := NewInteractionError(tc.code, 409, "safe message", tc.actions...)
			if err.Code != tc.code || strings.Join(err.AvailableActions, ",") != strings.Join(tc.actions, ",") {
				t.Fatalf("error = %#v", err)
			}
			encoded, marshalErr := json.Marshal(err)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if strings.Contains(string(encoded), "runtime_token") || strings.Contains(string(encoded), "payload") {
				t.Fatalf("error leaked internal data: %s", encoded)
			}
		})
	}
}

func TestInteractionAttemptIDDoesNotEmbedInput(t *testing.T) {
	attemptID := NewInteractionAttemptID()
	for _, secret := range []string{"bearer-token", "user body", "https://example.test/material"} {
		if strings.Contains(attemptID, secret) {
			t.Fatalf("attempt id %q contains sensitive input %q", attemptID, secret)
		}
	}
	if attemptID == "" {
		t.Fatal("attempt id is empty")
	}
}

func TestNormalizeInteractionErrorHidesRawSessionFailure(t *testing.T) {
	err := NormalizeInteractionError(ErrInteractiveSessionNotFound)
	if err.Code != InteractionErrorRestartRequired {
		t.Fatalf("code = %q", err.Code)
	}
	if strings.Contains(strings.ToLower(err.Message), "session") || strings.Contains(strings.ToLower(err.Message), "mcp") {
		t.Fatalf("user-facing message exposed implementation detail: %q", err.Message)
	}
}
