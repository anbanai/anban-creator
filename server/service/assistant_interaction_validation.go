package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

// The Agent requests readiness; only immutable structured fields determine
// creation completeness. Prose is never classified as a missing input here.
func validateInteractionCreation(payload json.RawMessage) ([]InteractionBlockingIssue, error) {
	plan, err := decodeAssistantCreationPlanFields(payload)
	if err != nil {
		return nil, creationDraftError(InteractionErrorDraftInvalid, nil)
	}
	issues := make([]InteractionBlockingIssue, 0)
	for _, field := range []struct{ name, value string }{
		{"goal", plan.Goal}, {"content_type", plan.ContentType}, {"topic", plan.Topic},
		{"angle", plan.Angle}, {"expression_form", plan.ExpressionForm},
	} {
		if strings.TrimSpace(field.value) == "" {
			issues = append(issues, InteractionBlockingIssue{Code: "required", Field: "plan." + field.name, Message: "请补充创作计划的必要信息"})
		}
	}
	if plan.ProfileContext == nil {
		issues = append(issues, InteractionBlockingIssue{Code: "required", Field: "plan.profile_context", Message: "请明确本次使用的画像上下文"})
	}
	if plan.TemporaryOverrides == nil {
		issues = append(issues, InteractionBlockingIssue{Code: "required", Field: "plan.temporary_overrides", Message: "请明确本次临时要求"})
	}
	for _, input := range plan.MissingInputs {
		issues = append(issues, InteractionBlockingIssue{Code: "missing_input", Field: "plan.missing_inputs", Message: input})
	}
	if len(issues) == 0 {
		if _, _, err := decodeAssistantCreationPlan(payload); err != nil {
			issues = append(issues, InteractionBlockingIssue{Code: "invalid", Field: "plan", Message: "创作计划内容不符合要求，请修改后重新确认"})
		}
	}
	return issues, nil
}

func creationDraftError(code string, issues []InteractionBlockingIssue) *InteractionError {
	if code == InteractionErrorDraftInvalid && len(issues) == 0 {
		issues = []InteractionBlockingIssue{{Code: "invalid", Field: "plan", Message: "请检查创作计划的格式和必要信息"}}
	}
	status, message := 422, "创作计划内容不完整，请继续修改"
	if code == InteractionErrorCreationInputsMissing {
		message = "仍有创作所需信息待补充，请继续讨论"
	}
	if code == InteractionErrorDraftNotReady {
		status, message = 409, "当前计划尚未准备好确认，请继续讨论"
	}
	return &InteractionError{Code: code, Status: status, Message: message, AvailableActions: []string{"continue_editing"}, BlockingIssues: issues}
}

func creationIssuesError(issues []InteractionBlockingIssue) *InteractionError {
	code := InteractionErrorDraftInvalid
	for _, issue := range issues {
		if issue.Code == "missing_input" {
			code = InteractionErrorCreationInputsMissing
			break
		}
	}
	return creationDraftError(code, issues)
}

func interactionDraftView(draft *model.AssistantInteractionDraft, eligible bool) InteractionDraftView {
	view := InteractionDraftView{ID: draft.ID, ModeID: draft.ModeID, Revision: draft.Revision, Payload: draft.Payload, PayloadDigest: draft.PayloadDigest, Ready: draft.Ready, ConfirmationState: draft.ConfirmationState}
	if draft.ModeID == AssistantModeCreation {
		issues, err := validateInteractionCreation(draft.Payload)
		if err != nil {
			issues = []InteractionBlockingIssue{{Code: "invalid", Field: "plan", Message: "创作计划格式不正确，请重新整理"}}
		}
		view.BlockingIssues = issues
		view.Ready = view.Ready && len(issues) == 0 && eligible && draft.ConfirmationState == model.AssistantInteractionDraftPending
	}
	return view
}

func creationDraftActions(view InteractionDraftView) []string {
	if view.Ready {
		return []string{"confirm_draft", "continue_editing"}
	}
	return []string{"continue_editing", "send_message"}
}

func (s *AssistantInteractionService) creationErrorWithSnapshot(ctx context.Context, userID string, interaction *model.AssistantInteraction, structured *InteractionError) error {
	if snapshot, err := s.snapshotWithDrafts(ctx, userID, interaction); err == nil {
		structured.WithSnapshot(snapshot)
	}
	return structured
}
