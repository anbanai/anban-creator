package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/datatypes"
)

var ErrWechatPublicationRecoveryUnavailable = errors.New("WeChat publication recovery is unavailable")

func (s *TaskService) RecoverWechatPublication(ctx context.Context, userID, taskID string) (model.TaskPublicationOutcome, error) {
	task, err := s.repo.Tasks().FindByID(ctx, strings.TrimSpace(taskID))
	if err != nil || task.UserID != userID {
		return model.TaskPublicationOutcome{}, ErrWechatPublicationNotFound
	}
	if task.Type != model.PlatformArticle || task.Outcome == nil {
		return model.TaskPublicationOutcome{}, ErrWechatPublicationRecoveryUnavailable
	}
	switch task.Outcome.Publication.Action {
	case "retry_draft":
		return s.retryServerOwnedArticlePublication(ctx, task)
	case "retry_visuals":
		return s.enqueueArticleVisualRecovery(ctx, task)
	case "check_wechat":
		if s.wechatPublicationSvc == nil {
			return model.TaskPublicationOutcome{}, ErrWechatPublicationRecoveryUnavailable
		}
		if err := s.wechatPublicationSvc.Reconcile(ctx, userID, taskID); err != nil {
			return model.TaskPublicationOutcome{}, err
		}
		latest, err := s.repo.Tasks().FindByID(ctx, taskID)
		if err != nil {
			return model.TaskPublicationOutcome{}, err
		}
		if latest.Outcome == nil {
			return model.TaskPublicationOutcome{}, ErrWechatPublicationRecoveryUnavailable
		}
		return latest.Outcome.Publication, nil
	default:
		return model.TaskPublicationOutcome{}, ErrWechatPublicationRecoveryUnavailable
	}
}

func (s *TaskService) retryServerOwnedArticlePublication(ctx context.Context, task *model.Task) (model.TaskPublicationOutcome, error) {
	if task.Status != model.TaskStatusCompleted {
		return model.TaskPublicationOutcome{}, ErrWechatPublicationRecoveryUnavailable
	}
	if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) == "" {
		return model.TaskPublicationOutcome{}, ErrWechatPublicationRecoveryUnavailable
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, *task.CurrentExecutionID)
	if err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	if execution.DraftDeliveryStatus == model.TaskExecutionDraftDeliveryBlocked || execution.DraftDeliveryStatus == model.TaskExecutionDraftDeliveryFailed {
		won, err := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, execution.DraftDeliveryStatus, "", nil)
		if err != nil {
			return model.TaskPublicationOutcome{}, err
		}
		if won {
			execution.DraftDeliveryStatus = ""
			execution.DraftDeliveryResult = nil
		}
	}
	if err := s.finalizeCloudDraftDelivery(ctx, task, execution, nil); err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	latest, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	if latest.DraftDeliveryStatus == model.TaskExecutionDraftDeliveryInFlight {
		// Another request owns the external call. Return its transient state to
		// this caller, but do not project it onto the task where it could race
		// with and overwrite the owner's terminal result.
		return publicationTaskOutcome(latest), nil
	}
	var result agent.ExecutionResult
	if len(latest.Result) > 0 {
		_ = json.Unmarshal(latest.Result, &result)
	}
	outcome, err := s.buildCloudTaskOutcome(ctx, task, latest, &result)
	if err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	won, err := s.repo.Tasks().UpdateOutcomeForExecution(ctx, task.ID, latest.ID, outcome)
	if err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	if !won {
		return model.TaskPublicationOutcome{}, ErrWechatPublicationConflict
	}
	return outcome.Publication, nil
}

func (s *TaskService) enqueueArticleVisualRecovery(ctx context.Context, task *model.Task) (model.TaskPublicationOutcome, error) {
	queued := false
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		current, err := tx.Tasks().FindByIDForUpdate(ctx, task.ID)
		if err != nil {
			return err
		}
		if current.Status == model.TaskStatusPending || current.Status == model.TaskStatusRunning {
			return nil
		}
		if current.Status != model.TaskStatusCompleted || current.Outcome == nil || current.Outcome.Publication.Action != "retry_visuals" {
			return ErrWechatPublicationRecoveryUnavailable
		}
		if current.CurrentExecutionID == nil || strings.TrimSpace(*current.CurrentExecutionID) == "" {
			return ErrWechatPublicationRecoveryUnavailable
		}
		sourceExecutionID := strings.TrimSpace(*current.CurrentExecutionID)
		source, err := tx.TaskExecutions().FindByID(ctx, sourceExecutionID)
		if err != nil || source.TaskID != current.ID || !isTerminalExecution(source.Status) {
			return ErrWechatPublicationRecoveryUnavailable
		}
		input := current.AgentInput.Data()
		if input == nil {
			input = map[string]any{}
		}
		input["resume_from"] = "image_generation"
		input["source_execution_id"] = sourceExecutionID
		input["publication_recovery"] = true
		current.AgentInput = datatypes.NewJSONType(input)
		current.Status = model.TaskStatusPending
		current.CompletedAt = nil
		current.ErrorMessage = ""
		if err := tx.Tasks().Update(ctx, current); err != nil {
			return err
		}
		queued = true
		return nil
	})
	if err != nil {
		return model.TaskPublicationOutcome{}, err
	}
	if queued {
		latest, err := s.repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			return model.TaskPublicationOutcome{}, err
		}
		if err := s.EnqueueExecution(ctx, latest, nil); err != nil {
			// Roll back the recovery marker only while the task is still pending;
			// a dispatcher that won the race owns the state and must be left alone.
			_ = s.repo.WithTx(context.WithoutCancel(ctx), func(tx repository.Repository) error {
				current, findErr := tx.Tasks().FindByIDForUpdate(context.WithoutCancel(ctx), task.ID)
				if findErr != nil || current.Status != model.TaskStatusPending {
					return findErr
				}
				current.Status = model.TaskStatusCompleted
				current.CompletedAt = task.CompletedAt
				current.ErrorMessage = task.ErrorMessage
				current.ProgressLog = task.ProgressLog
				current.AgentInput = task.AgentInput
				return tx.Tasks().Update(context.WithoutCancel(ctx), current)
			})
			return model.TaskPublicationOutcome{}, fmt.Errorf("enqueue publication visual recovery: %w", err)
		}
	}
	return task.Outcome.Publication, nil
}
