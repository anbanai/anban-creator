package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

func TestCreationInvalidFinalImageRatioAllowsCorrection(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "invalid-ratio", Options: json.RawMessage(`{"plan":{"image_ratio":"invalid"}}`)}
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	structured := NormalizeInteractionError(err)
	if structured == nil || structured.Code != InteractionErrorDraftInvalid || structured.Status != 422 || structured.Snapshot == nil || len(structured.BlockingIssues) == 0 || structured.BlockingIssues[0].Field != "plan.image_ratio" {
		t.Fatalf("invalid ratio = %#v, %v", structured, err)
	}
	if len(structured.AvailableActions) != 1 || structured.AvailableActions[0] != "continue_editing" {
		t.Fatalf("invalid ratio actions = %v", structured.AvailableActions)
	}
	if _, err := svc.repo.Tasks().FindByAssistantCheckpointID(context.Background(), "user-1", stage.DraftID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("invalid ratio task = %v", err)
	}
	if _, err := svc.repo.AssistantInteractions().FindAttempt(context.Background(), snapshot.ID, "commit:"+req.IdempotencyKey); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("invalid ratio receipt = %v", err)
	}
	draft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", stage.DraftID)
	if err != nil || draft.ConfirmationState != model.AssistantInteractionDraftPending {
		t.Fatalf("invalid ratio draft = %#v, %v", draft, err)
	}
	if _, err := svc.ContinueInteraction(context.Background(), "user-1", snapshot.ID, InteractionContinueRequest{DraftID: stage.DraftID, Revision: stage.Revision}); err != nil {
		t.Fatalf("continue after invalid ratio = %v", err)
	}
	corrected := stageReadinessPlan(t, svc, snapshot, "corrected", completeInteractionPlan)
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, InteractionCommitRequest{DraftID: corrected.DraftID, Revision: corrected.Revision, IdempotencyKey: "corrected-ratio", Options: json.RawMessage(`{"plan":{"image_ratio":"1:1"}}`)})
	if err != nil || result.TaskID == "" {
		t.Fatalf("corrected ratio commit = %#v, %v", result, err)
	}
}

func TestCreationUnavailableImageCapabilityPreservesBusinessError(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	resolver := svc.taskSvc.imageCapabilities
	svc.taskSvc.SetImageCapabilityResolver(nil)
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "capability"}
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	if !errors.Is(err, ErrImageCapabilityResolverUnavailable) {
		t.Fatalf("unavailable image capability = %v", err)
	}
	var structured *InteractionError
	if errors.As(err, &structured) {
		t.Fatalf("business error replaced by interaction outcome: %#v", structured)
	}
	draft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", stage.DraftID)
	if err != nil || draft.ConfirmationState != model.AssistantInteractionDraftPending {
		t.Fatalf("unavailable capability claim = %#v, %v", draft, err)
	}
	svc.taskSvc.SetImageCapabilityResolver(resolver)
	result, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	if err != nil || result.TaskID == "" {
		t.Fatalf("restored capability commit = %#v, %v", result, err)
	}
}

type failingConfirmationAdmissionRepository struct{ repository.Repository }

func (r failingConfirmationAdmissionRepository) WithTx(context.Context, func(repository.Repository) error) error {
	return ErrAgentProfileAccessDenied
}

func TestCreationErrorAfterEnteringPersistenceRetainsClaim(t *testing.T) {
	svc, snapshot := newCreationConfirmationTestService(t)
	stage := stageReadinessPlan(t, svc, snapshot, "plan", completeInteractionPlan)
	svc.taskSvc.repo = failingConfirmationAdmissionRepository{svc.taskSvc.repo}
	req := InteractionCommitRequest{DraftID: stage.DraftID, Revision: stage.Revision, IdempotencyKey: "admission-error"}
	_, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req)
	structured := NormalizeInteractionError(err)
	if structured == nil || structured.Code != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("persistence outcome = %#v, %v", structured, err)
	}
	draft, err := svc.repo.AssistantInteractions().FindDraftForUser(context.Background(), "user-1", stage.DraftID)
	if err != nil || draft.ConfirmationState != model.AssistantInteractionDraftConfirmed {
		t.Fatalf("persistence claim = %#v, %v", draft, err)
	}
	if _, err := svc.ContinueInteraction(context.Background(), "user-1", snapshot.ID, InteractionContinueRequest{DraftID: stage.DraftID, Revision: stage.Revision}); err == nil {
		t.Fatal("uncertain persistence claim was withdrawn")
	}
	if _, err := svc.CommitInteraction(context.Background(), "user-1", snapshot.ID, req); NormalizeInteractionError(err).Code != InteractionErrorCommitReconciliationRequired {
		t.Fatalf("persistence replay = %v", err)
	}
}
