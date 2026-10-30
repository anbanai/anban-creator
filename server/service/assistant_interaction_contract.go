package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

const (
	InteractionDraftRetention         = 30 * 24 * time.Hour
	InteractionCommitReceiptRetention = 30 * 24 * time.Hour
	InteractionEventRetention         = 24 * time.Hour
	InteractionAttemptRetention       = 24 * time.Hour
)

const (
	InteractionLifecycleActive            = model.AssistantInteractionActive
	InteractionLifecycleReconnecting      = model.AssistantInteractionReconnecting
	InteractionLifecycleNeedsConfirmation = model.AssistantInteractionNeedsConfirmation
	InteractionLifecycleCompleted         = model.AssistantInteractionCompleted
	InteractionLifecycleAbandoned         = model.AssistantInteractionAbandoned
	InteractionRuntimeStarting            = model.AssistantInteractionRuntimeStarting
	InteractionRuntimeRunning             = model.AssistantInteractionRuntimeRunning
	InteractionRuntimeClosed              = model.AssistantInteractionRuntimeClosed
	InteractionRuntimeFailed              = model.AssistantInteractionRuntimeFailed
	InteractionDraftPending               = model.AssistantInteractionDraftPending
	InteractionDraftConfirmed             = model.AssistantInteractionDraftConfirmed
	InteractionDraftConsumed              = model.AssistantInteractionDraftConsumed
	InteractionDraftSuperseded            = model.AssistantInteractionDraftSuperseded
)

const (
	InteractionErrorGenerationChanged            = "assistant_interaction_generation_changed"
	InteractionErrorReconnecting                 = "assistant_interaction_reconnecting"
	InteractionErrorRestartRequired              = "assistant_interaction_restart_required"
	InteractionErrorMessageNotAccepted           = "assistant_message_not_accepted"
	InteractionErrorCommandOutcomeUnknown        = "assistant_command_outcome_unknown"
	InteractionErrorDraftSavedRetryable          = "assistant_draft_saved_retryable"
	InteractionErrorDraftRevisionConflict        = "assistant_draft_revision_conflict"
	InteractionErrorCommitAlreadyApplied         = "assistant_commit_already_applied"
	InteractionErrorCommitReconciliationRequired = "assistant_commit_reconciliation_required"
	InteractionErrorCreationInputsMissing        = "assistant_creation_inputs_missing"
	InteractionErrorDraftInvalid                 = "assistant_draft_invalid"
	InteractionErrorDraftNotReady                = "assistant_draft_not_ready"
)

type InteractionOperation string

const (
	InteractionOperationCreate  InteractionOperation = "create"
	InteractionOperationMessage InteractionOperation = "message"
	InteractionOperationAnswer  InteractionOperation = "answer"
	InteractionOperationStage   InteractionOperation = "stage"
	InteractionOperationEvents  InteractionOperation = "events"
	InteractionOperationCommit  InteractionOperation = "commit"
)

func interactionCreateAttemptID(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(idempotencyKey)))
	return "create:" + hex.EncodeToString(digest[:])
}

func interactionCreatePayloadDigest(input AssistantInteractionInput) string {
	return interactionCommandDigest("assistant_interaction_create", "", map[string]any{
		"mode_id":               input.ModeID,
		"project_id":            input.ProjectID,
		"content_id":            input.ContentID,
		"resume_interaction_id": input.ResumeInteractionID,
		"agent_id":              input.AgentID,
		"intent":                input.Intent,
	})
}

// InteractionError is safe to serialize to the client. Internal causes are
// retained through Unwrap but are never included in JSON.
type InteractionError struct {
	Code             string                     `json:"error_code"`
	Status           int                        `json:"status"`
	Message          string                     `json:"message"`
	AvailableActions []string                   `json:"available_actions,omitempty"`
	Snapshot         *InteractionSnapshot       `json:"snapshot,omitempty"`
	BlockingIssues   []InteractionBlockingIssue `json:"blocking_issues,omitempty"`
	cause            error
}

func (e *InteractionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func (e *InteractionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func NewInteractionError(code string, status int, message string, actions ...string) *InteractionError {
	return &InteractionError{Code: code, Status: status, Message: message, AvailableActions: append([]string(nil), actions...)}
}

func NewInteractionAttemptID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "attempt-unavailable"
	}
	return hex.EncodeToString(raw[:])
}

func interactionCommandDigest(method, attemptID string, params map[string]any) string {
	payload, _ := json.Marshal(struct {
		Method  string         `json:"method"`
		Attempt string         `json:"attempt_id"`
		Params  map[string]any `json:"params"`
	}{Method: method, Attempt: attemptID, Params: params})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type InteractionSnapshot struct {
	StateSequence     uint64                   `json:"state_sequence,omitempty"`
	ID                string                   `json:"interaction_id"`
	ModeID            string                   `json:"mode_id"`
	ProjectID         string                   `json:"project_id,omitempty"`
	ContentID         string                   `json:"content_id,omitempty"`
	Lifecycle         string                   `json:"lifecycle"`
	Generation        int64                    `json:"generation"`
	RuntimeStatus     string                   `json:"runtime_status"`
	ExpiresAt         string                   `json:"expires_at"`
	LastActivityAt    string                   `json:"last_activity_at"`
	AvailableActions  []string                 `json:"available_actions,omitempty"`
	RecoveryErrorCode string                   `json:"recovery_error_code,omitempty"`
	Drafts            []InteractionDraftView   `json:"drafts,omitempty"`
	CommitResult      *InteractionCommitResult `json:"commit_result,omitempty"`
}

type InteractionDraftView struct {
	StateSequence     uint64                     `json:"state_sequence,omitempty"`
	ID                string                     `json:"draft_id"`
	ModeID            string                     `json:"mode_id"`
	Revision          int64                      `json:"revision"`
	Payload           json.RawMessage            `json:"payload"`
	PayloadDigest     string                     `json:"payload_digest"`
	Ready             bool                       `json:"ready"`
	ConfirmationState string                     `json:"confirmation_state"`
	BlockingIssues    []InteractionBlockingIssue `json:"blocking_issues,omitempty"`
}

type InteractionBlockingIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type InteractionStageInput struct {
	InteractionID string          `json:"interaction_id"`
	Generation    int64           `json:"generation"`
	AttemptID     string          `json:"attempt_id"`
	ModeID        string          `json:"mode_id"`
	Payload       json.RawMessage `json:"payload"`
	PayloadDigest string          `json:"payload_digest"`
	Ready         bool            `json:"ready"`
}

type InteractionStageResult struct {
	DraftID              string                `json:"draft_id"`
	Revision             int64                 `json:"revision"`
	ConfirmationRequired bool                  `json:"confirmation_required"`
	AvailableActions     []string              `json:"available_actions,omitempty"`
	Durable              bool                  `json:"durable"`
	Draft                *InteractionDraftView `json:"draft,omitempty"`
}

type InteractionContinueRequest struct {
	DraftID  string `json:"draft_id"`
	Revision int64  `json:"revision"`
}

type InteractionCommitRequest struct {
	DraftID        string          `json:"draft_id"`
	Revision       int64           `json:"revision"`
	IdempotencyKey string          `json:"idempotency_key"`
	Options        json.RawMessage `json:"options,omitempty"`
}

type InteractionCommitResult struct {
	Status string          `json:"status"`
	TaskID string          `json:"task_id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

func NormalizeInteractionError(err error) *InteractionError {
	if err == nil {
		return nil
	}
	var interactionErr *InteractionError
	if errors.As(err, &interactionErr) {
		return interactionErr
	}
	switch {
	case errors.Is(err, ErrInteractiveSessionNotFound), errors.Is(err, ErrInteractiveSessionClosed):
		return &InteractionError{Code: InteractionErrorRestartRequired, Status: 410, Message: "当前连接已结束，已保留可恢复内容", AvailableActions: []string{"restore_draft", "restart"}, cause: err}
	case errors.Is(err, ErrInteractiveBusy):
		return &InteractionError{Code: InteractionErrorReconnecting, Status: 409, Message: "正在恢复连接，请稍候", AvailableActions: []string{"wait_for_recovery"}, cause: err}
	default:
		return &InteractionError{Code: InteractionErrorMessageNotAccepted, Status: 500, Message: "这次操作未完成，当前内容已保留，请重试", AvailableActions: []string{"retry_message"}, cause: err}
	}
}

func (e *InteractionError) WithSnapshot(snapshot InteractionSnapshot) *InteractionError {
	if e == nil {
		return nil
	}
	copy := snapshot
	e.Snapshot = &copy
	return e
}

func (e *InteractionError) String() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *InteractionError) Validate() error {
	if e == nil || strings.TrimSpace(e.Code) == "" || strings.TrimSpace(e.Message) == "" || e.Status < 400 {
		return errors.New("invalid interaction error")
	}
	return nil
}
