package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

var ErrStaleTaskExecution = errors.New("task execution is no longer current")
var ErrTaskCompletionConflict = errors.New("task completion result conflicts with the terminal execution outcome")
var ErrFinalizationLeaseLost = errors.New("task execution finalization lease lost")
var ErrCloudPublishingAmbiguous = errors.New("cloud draft publication outcome is ambiguous and requires reconciliation")

// CompleteCloudExecution records one attempt's immutable terminal outcome, then
// resumes the durable business finalizer. Only the task's current attempt may
// cross the terminal CAS; repeats of that attempt resume incomplete stages.
func (s *TaskService) CompleteCloudExecution(ctx context.Context, executionID string, result *agent.ExecutionResult) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if !isTerminalExecution(execution.Status) && task.Status != model.TaskStatusRunning {
		return ErrTaskCompletionConflict
	}
	terminal, reason, normalized, err := s.cloudTerminalOutcome(ctx, task, execution, result)
	if err != nil {
		return err
	}

	if !isTerminalExecution(execution.Status) {
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return fmt.Errorf("marshal cloud execution result: %w", err)
		}
		won, err := s.repo.TaskExecutions().Transition(ctx, execution.ID,
			[]string{model.TaskExecutionStarting, model.TaskExecutionRunning}, terminal,
			model.ExecutionTransition{
				TerminalReason:     reason,
				Result:             encoded,
				FinalizationStatus: model.TaskExecutionFinalizationTerminal,
			})
		if err != nil {
			return fmt.Errorf("terminalize cloud execution: %w", err)
		}
		if !won {
			execution, task, err = s.currentExecution(ctx, executionID)
			if err != nil {
				return err
			}
			if !isTerminalExecution(execution.Status) {
				return fmt.Errorf("cloud execution terminal transition lost from status %s", execution.Status)
			}
			if err := ensureCompletionOutcomeMatches(execution, terminal, reason, normalized); err != nil {
				return err
			}
		} else {
			execution.Status = terminal
			execution.TerminalReason = reason
			execution.Result = encoded
			execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
		}
	} else if err := ensureCompletionOutcomeMatches(execution, terminal, reason, normalized); err != nil {
		return err
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func ensureCompletionOutcomeMatches(execution *model.TaskExecution, terminal, reason string, result *agent.ExecutionResult) error {
	if execution == nil || execution.Status != terminal || execution.TerminalReason != reason {
		return ErrTaskCompletionConflict
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal completion retry result: %w", err)
	}
	equal, err := semanticJSONEqual(execution.Result, encoded)
	if err != nil {
		return ErrTaskCompletionConflict
	}
	if !equal {
		return ErrTaskCompletionConflict
	}
	return nil
}

func semanticJSONEqual(left, right []byte) (bool, error) {
	decode := func(data []byte) (any, error) {
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("multiple JSON values")
			}
			return nil, err
		}
		return value, nil
	}
	leftValue, err := decode(left)
	if err != nil {
		return false, err
	}
	rightValue, err := decode(right)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(leftValue, rightValue), nil
}

func (s *TaskService) currentExecution(ctx context.Context, executionID string) (*model.TaskExecution, *model.Task, error) {
	execution, err := s.repo.TaskExecutions().FindByID(ctx, strings.TrimSpace(executionID))
	if err != nil {
		return nil, nil, fmt.Errorf("find task execution: %w", err)
	}
	task, err := s.repo.Tasks().FindByID(ctx, execution.TaskID)
	if err != nil {
		return nil, nil, fmt.Errorf("find execution task: %w", err)
	}
	if task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return nil, nil, ErrStaleTaskExecution
	}
	return execution, task, nil
}

func (s *TaskService) cloudTerminalOutcome(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) (string, string, *agent.ExecutionResult, error) {
	failureReason := "execution_failed"
	missingResult := result == nil
	result, err := cloneTerminalExecutionResult(result)
	if err != nil {
		return "", "", nil, err
	}
	if missingResult {
		result.RemoteArtifacts = true
	}
	if result.Success && agent.IsNestedAgentDelegationOnly(result.ToolUseSummary) {
		result.Success, result.Error = false, agent.NestedAgentDelegationError
		result.TerminalReason = model.TaskBillingTerminalPlatformError
		failureReason = "nested_agent_delegation"
	}
	wasSuccessful := result.Success
	deliveryAccepted := false
	if !execution.ManifestSealed {
		if wasSuccessful {
			result.Success, result.Error = false, "artifact manifest is not sealed"
			result.TerminalReason = model.TaskBillingTerminalPlatformError
			failureReason = "deliverable_validation_failed"
		}
	} else if !agent.IsNestedAgentDelegationOnly(result.ToolUseSummary) {
		files, err := s.repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
		if err != nil {
			return "", "", nil, fmt.Errorf("list pending execution artifacts: %w", err)
		}
		var validation agent.ArtifactValidation
		switch {
		case model.IsMontagePlatform(task.Type):
			validation = validateMontageCompletionArtifacts(files)
		default:
			validation = agent.ValidateTaskArtifactsFromTaskFiles(task, files)
		}
		if err != nil {
			return "", "", nil, err
		}
		var deliveryErr error
		if !validation.Valid {
			deliveryErr = errors.New(validation.Error())
		} else {
			deliveryErr = s.validateExecutionDelivery(ctx, task.ID, execution, files)
		}
		if deliveryErr != nil {
			if !errors.Is(deliveryErr, ErrTaskDeliveryObjectInvalid) && !errors.Is(deliveryErr, ErrTaskDeliveryContractUnavailable) {
				if validation.Valid {
					return "", "", nil, deliveryErr
				}
			}
			result.Success = false
			if wasSuccessful {
				result.Error = "delivery validation failed: " + deliveryErr.Error()
				result.TerminalReason = model.TaskBillingTerminalPlatformError
			}
			failureReason = "deliverable_validation_failed"
		} else {
			deliveryAccepted = true
		}
	}
	if result.Success || deliveryAccepted {
		return model.TaskExecutionSucceeded, "completed", result, nil
	}
	if strings.TrimSpace(result.Error) == "" {
		result.Error = "execution returned unsuccessful result"
	}
	if !approvedTaskBillingTerminalReason(result.TerminalReason) {
		result.TerminalReason = model.TaskBillingTerminalProviderError
	}
	return model.TaskExecutionFailed, failureReason, result, nil
}

func (s *TaskService) finalizeTaskFromExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) (err error) {
	if execution.FinalizationStatus == model.TaskExecutionFinalizationDone {
		return s.ensureTerminalExecutionAuthority(ctx, task.ID, execution.ID)
	}
	if err := s.ensureExecutionAuthority(ctx, task.ID, execution.ID); err != nil {
		return err
	}
	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimFinalization(ctx, execution.ID, token, s.cloudFinalizationLease())
	if err != nil {
		return fmt.Errorf("claim execution finalization: %w", err)
	}
	if !won {
		return nil
	}
	leaseCtx, stopLease, leaseLost := s.renewFinalizationLease(ctx, execution.ID, token)
	defer func() {
		stopLease()
		if releaseErr := s.repo.TaskExecutions().ReleaseFinalization(context.WithoutCancel(ctx), execution.ID, token); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()

	var result *agent.ExecutionResult
	if len(execution.Result) > 0 {
		if err := json.Unmarshal(execution.Result, &result); err != nil {
			return fmt.Errorf("decode stored execution result: %w", err)
		}
	}
	stage := execution.FinalizationStatus
	if stage == "" {
		stage = model.TaskExecutionFinalizationTerminal
	}
	if stage == model.TaskExecutionFinalizationTerminal && s.finalizationAfterStage != nil {
		if err := s.finalizationAfterStage(stage); err != nil {
			return err
		}
	}
	for stage != model.TaskExecutionFinalizationDone {
		if err := s.ensureFinalizationAuthority(leaseCtx, task.ID, execution.ID, leaseLost); err != nil {
			return err
		}
		next, step, err := s.cloudFinalizationStep(task, execution, result, stage)
		if err != nil {
			return err
		}
		stepErr := step(leaseCtx)
		if leaseErr := finalizationLeaseError(leaseLost); leaseErr != nil {
			return leaseErr
		}
		if stepErr != nil {
			return stepErr
		}
		if s.finalizationAfterStage != nil {
			if err := s.finalizationAfterStage(next); err != nil {
				return err
			}
		}
		if err := s.ensureFinalizationAuthority(leaseCtx, task.ID, execution.ID, leaseLost); err != nil {
			return err
		}
		if err := finalizationLeaseError(leaseLost); err != nil {
			return err
		}
		renewed, err := s.renewFinalizationClaim(leaseCtx, execution.ID, token)
		if err != nil {
			if leaseErr := finalizationLeaseError(leaseLost); leaseErr != nil {
				return leaseErr
			}
			return fmt.Errorf("renew finalization before advancing to %s: %w", next, err)
		}
		if !renewed {
			return ErrFinalizationLeaseLost
		}
		if err := s.advanceExecutionFinalization(leaseCtx, execution.ID, token, stage, next); err != nil {
			return err
		}
		stage = next
		if s.finalizationAfterAdvance != nil {
			if err := s.finalizationAfterAdvance(stage); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *TaskService) ensureTerminalExecutionAuthority(ctx context.Context, taskID, executionID string) error {
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		execution, err := tx.TaskExecutions().FindByIDForUpdate(ctx, executionID)
		if err != nil {
			return fmt.Errorf("verify terminal execution authority: %w", err)
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if err != nil {
			return fmt.Errorf("verify terminal task authority: %w", err)
		}
		if execution.TaskID != taskID || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID ||
			(task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled) {
			return ErrStaleTaskExecution
		}
		return nil
	})
}

func (s *TaskService) ensureFinalizationAuthority(ctx context.Context, taskID, executionID string, leaseLost <-chan error) error {
	if err := finalizationLeaseError(leaseLost); err != nil {
		return err
	}
	if err := s.ensureExecutionAuthority(ctx, taskID, executionID); err != nil {
		if leaseErr := finalizationLeaseError(leaseLost); leaseErr != nil {
			return leaseErr
		}
		return err
	}
	return nil
}

func (s *TaskService) ensureExecutionAuthority(ctx context.Context, taskID, executionID string) error {
	latest, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("verify current task execution: %w", err)
	}
	if latest.CurrentExecutionID == nil || *latest.CurrentExecutionID != executionID {
		return ErrStaleTaskExecution
	}
	return nil
}

type cloudFinalizationStep func(context.Context) error

func (s *TaskService) cloudFinalizationStep(task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult, stage string) (string, cloudFinalizationStep, error) {
	switch stage {
	case model.TaskExecutionFinalizationTerminal:
		return model.TaskExecutionFinalizationArtifacts, func(ctx context.Context) error {
			return s.finalizeCloudExecutionCore(ctx, task, execution, result)
		}, nil
	case model.TaskExecutionFinalizationArtifacts:
		return model.TaskExecutionFinalizationResult, func(ctx context.Context) error {
			return s.recordTerminalProviderCost(ctx, task, result)
		}, nil
	case model.TaskExecutionFinalizationResult:
		return model.TaskExecutionFinalizationWorkflow, func(ctx context.Context) error {
			return s.RebuildWorkflowStatus(ctx, task.ID)
		}, nil
	case model.TaskExecutionFinalizationWorkflow:
		return model.TaskExecutionFinalizationDraftDelivery, func(ctx context.Context) error {
			return s.finalizeCloudDraftDelivery(ctx, task, execution, result)
		}, nil
	case model.TaskExecutionFinalizationDraftDelivery:
		return model.TaskExecutionFinalizationTask, func(ctx context.Context) error {
			return s.finalizeCloudTaskOutcome(ctx, task, execution, result)
		}, nil
	case model.TaskExecutionFinalizationTask:
		return model.TaskExecutionFinalizationSlot, func(ctx context.Context) error {
			return s.syncCloudSlot(ctx, task)
		}, nil
	case model.TaskExecutionFinalizationSlot:
		return model.TaskExecutionFinalizationDispatch, func(ctx context.Context) error {
			if task.ProjectID == "" {
				return nil
			}
			return s.DispatchPendingTasks(ctx, task.ProjectID)
		}, nil
	case model.TaskExecutionFinalizationDispatch:
		return model.TaskExecutionFinalizationNotification, func(ctx context.Context) error {
			status, errMsg := taskTerminalFromExecution(execution, result)
			return s.notifyTerminalDurable(ctx, task, status, errMsg)
		}, nil
	case model.TaskExecutionFinalizationNotification:
		return model.TaskExecutionFinalizationDone, func(context.Context) error { return nil }, nil
	default:
		return "", nil, fmt.Errorf("unknown execution finalization stage %q", stage)
	}
}

func (s *TaskService) finalizeCloudExecutionCore(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	if task == nil || execution == nil || result == nil {
		return ErrTaskCompletionConflict
	}
	resultJSON, err := marshalExecutionEvidence(result)
	if err != nil {
		return err
	}
	target, errMsg := taskTerminalFromExecution(execution, result)
	reason := terminalBillingReason(execution, result)
	durableDelivery, err := s.taskHasDurableDelivery(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("inspect durable task delivery: %w", err)
	}
	artifactAction := repository.CloudTaskArtifactsRetain
	if execution.Status == model.TaskExecutionSucceeded {
		artifactAction = repository.CloudTaskArtifactsDeliver
	}

	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		_, finalizeErr := tx.Tasks().FinalizeCloudTaskWithArtifactsInTx(
			ctx, task.ID, execution.ID, target, errMsg, resultJSON,
			result.ModelUsage, result.CostStatus, artifactAction,
		)
		if errors.Is(finalizeErr, repository.ErrCloudTaskExecutionCASLost) {
			return ErrStaleTaskExecution
		}
		if finalizeErr != nil {
			return fmt.Errorf("finalize cloud task core: %w", finalizeErr)
		}
		return s.persistTerminalBillingInTx(ctx, tx, task, execution, reason, durableDelivery)
	})
	if err != nil {
		return err
	}
	task.Status = target
	task.ErrorMessage = errMsg
	task.Result = &resultJSON
	task.TerminalModelUsage = datatypes.NewJSONType(result.ModelUsage)
	task.CostStatus = result.CostStatus
	task.BillingTerminalReason = reason
	return nil
}

func (s *TaskService) syncCloudSlot(ctx context.Context, task *model.Task) error {
	if task == nil || task.ProjectID == "" {
		return nil
	}
	if s.pubsub != nil {
		if running, err := s.repo.Tasks().CountRunningByProject(ctx, task.ProjectID); err != nil {
			return fmt.Errorf("count running tasks for cloud slot reconciliation: %w", err)
		} else if err := s.pubsub.SyncProjectCount(ctx, task.ProjectID, running); err != nil {
			return fmt.Errorf("reconcile cloud concurrency slot: %w", err)
		}
	}
	return nil
}

func (s *TaskService) finalizeCloudDraftDelivery(ctx context.Context, task *model.Task, execution *model.TaskExecution, _ *agent.ExecutionResult) error {
	latestExecution, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return err
	}
	switch latestExecution.DraftDeliveryStatus {
	case model.TaskExecutionDraftDeliverySucceeded, model.TaskExecutionDraftDeliverySkipped,
		model.TaskExecutionDraftDeliveryFailed, model.TaskExecutionDraftDeliveryAmbiguous,
		model.TaskExecutionDraftDeliveryNotRequested:
		execution.DraftDeliveryStatus = latestExecution.DraftDeliveryStatus
		return nil
	case model.TaskExecutionDraftDeliveryInFlight:
		won, transitionErr := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID,
			model.TaskExecutionDraftDeliveryInFlight, model.TaskExecutionDraftDeliveryAmbiguous,
			[]byte(`{"source":"durable_publication","reason":"completion_observed_in_flight"}`))
		if transitionErr != nil {
			return transitionErr
		}
		if !won {
			return fmt.Errorf("record ambiguous draft delivery: state changed concurrently")
		}
		execution.DraftDeliveryStatus = model.TaskExecutionDraftDeliveryAmbiguous
		return nil
	}
	status, evidence, err := s.resolveCloudDraftDelivery(ctx, task, execution)
	if err != nil {
		return err
	}
	won, err := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", status, evidence)
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("record draft delivery: state changed concurrently")
	}
	execution.DraftDeliveryStatus = status
	return nil
}

type articleStatusArtifact struct {
	Status      string `json:"status"`
	ContentHash string `json:"content_hash"`
}

func (s *TaskService) resolveCloudDraftDelivery(ctx context.Context, task *model.Task, execution *model.TaskExecution) (string, []byte, error) {
	publication, err := s.repo.WechatPublications().FindByTaskID(ctx, task.ID)
	if err == nil && publication.ExecutionID == execution.ID {
		finalHTML, found, readErr := s.readExecutionArtifact(ctx, execution.ID, "output/05-article.html", maxTaskDeliveryHTMLBytes)
		if readErr != nil {
			return "", nil, fmt.Errorf("read final article for durable draft validation: %w", readErr)
		}
		if !found || publication.DraftContentFingerprint == "" ||
			WechatContentFingerprint(string(finalHTML)) != publication.DraftContentFingerprint {
			reason := "final_html_missing"
			if found {
				reason = "content_fingerprint_mismatch"
			}
			evidence, _ := json.Marshal(map[string]string{
				"source": "durable_publication", "status": publication.Status, "reason": reason,
			})
			return model.TaskExecutionDraftDeliveryAmbiguous, evidence, nil
		}
		status := draftDeliveryStatusFromPublication(publication)
		evidence, _ := json.Marshal(map[string]string{"source": "durable_publication", "status": publication.Status})
		return status, evidence, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, fmt.Errorf("find durable draft publication: %w", err)
	}

	var draftResult articleStatusArtifact
	if found, readErr := s.readCurrentArticleStatusArtifact(ctx, execution.ID, "output/draft-result.json", &draftResult); readErr != nil {
		return model.TaskExecutionDraftDeliveryAmbiguous, []byte(`{"source":"draft_result","status":"invalid"}`), nil
	} else if found {
		status := model.TaskExecutionDraftDeliveryAmbiguous
		switch draftResult.Status {
		case string(model.TaskPublicationSkipped):
			status = model.TaskExecutionDraftDeliverySkipped
		case string(model.TaskPublicationFailed):
			status = model.TaskExecutionDraftDeliveryFailed
		case string(model.TaskPublicationAmbiguous), string(model.TaskPublicationSucceeded):
			// Success without the atomic capability's durable record is not trustworthy.
			status = model.TaskExecutionDraftDeliveryAmbiguous
		case string(model.TaskPublicationNotRequested):
			status = model.TaskExecutionDraftDeliveryNotRequested
		}
		evidence, _ := json.Marshal(map[string]string{"source": "draft_result", "status": draftResult.Status})
		return status, evidence, nil
	}

	var scan articleStatusArtifact
	if found, readErr := s.readCurrentArticleStatusArtifact(ctx, execution.ID, "output/marketing-scan.json", &scan); readErr == nil && found && scan.Status == "block_publish" {
		return model.TaskExecutionDraftDeliverySkipped, []byte(`{"source":"marketing_scan","status":"block_publish"}`), nil
	}
	return model.TaskExecutionDraftDeliveryNotRequested, []byte(`{"source":"finalizer","status":"not_requested"}`), nil
}

func draftDeliveryStatusFromPublication(publication *model.WechatPublication) string {
	if publication == nil {
		return model.TaskExecutionDraftDeliveryNotRequested
	}
	if publication.DraftMediaID != "" {
		return model.TaskExecutionDraftDeliverySucceeded
	}
	switch publication.Status {
	case model.WechatPublicationStatusDrafted, model.WechatPublicationStatusPublishing,
		model.WechatPublicationStatusPublished, model.WechatPublicationStatusNeedsSelection:
		return model.TaskExecutionDraftDeliverySucceeded
	case model.WechatPublicationStatusUnsupported, model.WechatPublicationStatusPublishFailed:
		return model.TaskExecutionDraftDeliveryFailed
	default:
		return model.TaskExecutionDraftDeliveryAmbiguous
	}
}

func (s *TaskService) readExecutionJSONArtifact(ctx context.Context, executionID, path string, destination any) (bool, error) {
	body, found, err := s.readExecutionArtifact(ctx, executionID, path, maxTaskDeliveryJSONBytes)
	if err != nil || !found {
		return found, err
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return false, fmt.Errorf("decode %s: %w", path, err)
	}
	return true, nil
}

func (s *TaskService) readExecutionArtifact(ctx context.Context, executionID, path string, maxBytes int64) ([]byte, bool, error) {
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		return nil, false, err
	}
	for _, file := range files {
		if file.FilePath != path {
			continue
		}
		if file.StorageProvider != "" && (s.store == nil || file.StorageProvider != s.store.Name()) {
			return nil, false, fmt.Errorf("artifact storage provider is unavailable")
		}
		body, err := storage.ReadObject(ctx, s.store, file.OSSKey, maxBytes)
		if err != nil {
			return nil, false, err
		}
		return body, true, nil
	}
	return nil, false, nil
}

func (s *TaskService) readCurrentArticleStatusArtifact(ctx context.Context, executionID, path string, destination *articleStatusArtifact) (bool, error) {
	found, err := s.readExecutionJSONArtifact(ctx, executionID, path, destination)
	if err != nil || !found {
		return found, err
	}
	reportedHash := strings.TrimSpace(destination.ContentHash)
	if len(reportedHash) != sha256.Size*2 || reportedHash != strings.ToLower(reportedHash) {
		return false, fmt.Errorf("%s content_hash is not a lowercase SHA-256 digest", path)
	}
	if _, err := hex.DecodeString(reportedHash); err != nil {
		return false, fmt.Errorf("%s content_hash is invalid: %w", path, err)
	}
	article, articleFound, err := s.readExecutionArtifact(ctx, executionID, "output/04-article-final.md", maxTaskDeliveryMarkdownBytes)
	if err != nil {
		return false, fmt.Errorf("read final article for %s validation: %w", path, err)
	}
	if !articleFound {
		return false, fmt.Errorf("final article is unavailable for %s validation", path)
	}
	actualHash := fmt.Sprintf("%x", sha256.Sum256(article))
	if reportedHash != actualHash {
		return false, fmt.Errorf("%s content_hash does not match the final article", path)
	}
	return true, nil
}

func (s *TaskService) finalizeCloudTaskOutcome(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) error {
	outcome, err := s.buildCloudTaskOutcome(ctx, task, execution, result)
	if err != nil {
		return err
	}
	won, err := s.repo.Tasks().UpdateOutcomeForExecution(ctx, task.ID, execution.ID, outcome)
	if err != nil {
		return fmt.Errorf("persist task outcome: %w", err)
	}
	if !won {
		return ErrStaleTaskExecution
	}
	task.Outcome = &outcome
	return nil
}

func (s *TaskService) buildCloudTaskOutcome(ctx context.Context, task *model.Task, execution *model.TaskExecution, result *agent.ExecutionResult) (model.TaskOutcome, error) {
	outcome := model.TaskOutcome{
		CoreDelivery: model.TaskCoreDeliveryOutcome{Status: model.TaskCoreDeliveryNone},
		Visual:       model.TaskVisualOutcome{Status: model.TaskVisualNotRequested},
		Review:       model.TaskReviewOutcome{Status: model.TaskReviewUnavailable},
		Publication:  model.TaskPublicationOutcome{Status: publicationOutcomeStatus(execution.DraftDeliveryStatus)},
		Warnings:     []model.TaskOutcomeWarning{},
	}
	if task.Status == model.TaskStatusCompleted {
		outcome.CoreDelivery.Status = model.TaskCoreDeliveryComplete
	}

	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
	if err != nil {
		return model.TaskOutcome{}, fmt.Errorf("list execution files for outcome: %w", err)
	}
	if task.Type == model.PlatformArticle {
		outcome.Visual.Status = articleVisualOutcome(task, files)
		if outcome.Visual.Status == model.TaskVisualPartial {
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{
				Code: "visual_partial", Stage: "visual", Message: "请求的封面或正文配图未全部生成，现有图片已保留。",
			})
		}
		var scan articleStatusArtifact
		found, readErr := s.readCurrentArticleStatusArtifact(ctx, execution.ID, "output/marketing-scan.json", &scan)
		switch {
		case readErr != nil || !found:
			outcome.Review.Status = model.TaskReviewUnavailable
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{
				Code: "review_unavailable", Stage: "review", Message: "营销扫描报告不可用或与最终文章不匹配，文章交付不受影响。",
			})
		case scan.Status == "passed":
			outcome.Review.Status = model.TaskReviewPassed
		case scan.Status == "warning", scan.Status == "block_publish":
			outcome.Review.Status = model.TaskReviewWarning
			code, message := "review_warning", "营销扫描发现需要人工确认的表达，文章仍可交付。"
			if scan.Status == "block_publish" {
				code, message = "review_block_publish", "营销扫描阻止自动创建公众号草稿，文章仍已交付。"
			}
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{Code: code, Stage: "review", Message: message})
		default:
			outcome.Review.Status = model.TaskReviewUnavailable
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{
				Code: "review_unavailable", Stage: "review", Message: "营销扫描报告状态无效，文章交付不受影响。",
			})
		}
	}

	switch outcome.Publication.Status {
	case model.TaskPublicationSkipped:
		outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{Code: "publication_skipped", Stage: "publication", Message: "公众号草稿创建已跳过。"})
	case model.TaskPublicationFailed:
		outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{Code: "publication_failed", Stage: "publication", Message: "公众号草稿创建失败，文章交付不受影响。"})
	case model.TaskPublicationAmbiguous:
		outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{Code: "publication_ambiguous", Stage: "publication", Message: "公众号草稿结果暂时无法确认，需要对账。"})
	}

	if diagnostic := publicExecutionDiagnostic(execution, result); diagnostic != nil {
		outcome.Diagnostic = diagnostic
		if result != nil && result.ErrorCode == "provider_policy_rejection" {
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{
				Code: "provider_policy_rejection", Stage: safePublicDiagnosticStage(result.FailureStage),
				Message: "供应商内容安全策略拒绝了一次请求；已按服务端文件契约判定最终交付。",
			})
		}
	}
	if result != nil {
		for _, failure := range result.ArtifactUploadFailures {
			if strings.TrimSpace(failure.Path) == "" || strings.TrimSpace(failure.Reason) == "" {
				continue
			}
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{
				Code: "artifact_upload_failed", Stage: "artifact_upload",
				Message: fmt.Sprintf("文件 %s 上传失败：%s", safePublicArtifactPath(failure.Path), safePublicArtifactUploadReason(failure.Reason)),
			})
		}
	}
	return outcome, nil
}

func articleVisualOutcome(task *model.Task, files []*model.TaskFile) model.TaskVisualStatus {
	coverRequested := task.ArticleWithCover == nil || *task.ArticleWithCover
	contentRequested := task.ArticleWithContentImages == nil || *task.ArticleWithContentImages
	if !coverRequested && !contentRequested {
		return model.TaskVisualNotRequested
	}
	hasCover, hasContent := false, false
	for _, file := range files {
		if file.State != model.TaskFileStateDelivered && file.State != model.TaskFileStateRetained {
			continue
		}
		isImage := file.Role == model.FileRoleCover || file.Role == model.FileRoleImage || strings.HasPrefix(file.MimeType, "image/")
		if !isImage {
			continue
		}
		if file.Role == model.FileRoleCover || strings.HasPrefix(strings.ToLower(file.FileName), "cover") {
			hasCover = true
		} else {
			hasContent = true
		}
	}
	if (!coverRequested || hasCover) && (!contentRequested || hasContent) {
		return model.TaskVisualComplete
	}
	return model.TaskVisualPartial
}

func publicationOutcomeStatus(status string) model.TaskPublicationStatus {
	switch status {
	case model.TaskExecutionDraftDeliverySucceeded:
		return model.TaskPublicationSucceeded
	case model.TaskExecutionDraftDeliverySkipped:
		return model.TaskPublicationSkipped
	case model.TaskExecutionDraftDeliveryFailed:
		return model.TaskPublicationFailed
	case model.TaskExecutionDraftDeliveryAmbiguous, model.TaskExecutionDraftDeliveryInFlight:
		return model.TaskPublicationAmbiguous
	default:
		return model.TaskPublicationNotRequested
	}
}

func publicExecutionDiagnostic(execution *model.TaskExecution, result *agent.ExecutionResult) *model.ExecutionDiagnostic {
	if execution == nil || result == nil {
		return nil
	}
	providerFailure := result.ErrorCode != "" || result.ProviderCode != "" || result.HTTPStatus != 0 || result.TerminalReason == model.TaskBillingTerminalProviderError
	if !providerFailure {
		return nil
	}
	direction := safePublicContentDirection(result.ContentDirection)
	summary := "供应商请求失败，原始执行上下文未对外披露。"
	if result.ErrorCode == "provider_policy_rejection" {
		summary = "供应商内容安全策略拒绝了请求。"
		if direction == "unknown" {
			summary += "供应商未披露具体片段，也未说明发生在输入还是输出。"
		}
	}
	return &model.ExecutionDiagnostic{
		Provider: execution.Provider, ProviderCode: safePublicProviderCode(result.ProviderCode), HTTPStatus: safePublicHTTPStatus(result.HTTPStatus),
		Stage: safePublicDiagnosticStage(result.FailureStage), ContentDirection: direction, Recoverable: result.Recoverable,
		ResumePoint: safePublicDiagnosticStage(result.ResumeFrom), RequestID: safePublicRequestID(result.RequestID), Summary: summary,
	}
}

var publicDiagnosticStages = map[string]struct{}{
	"project_resolution": {}, "topic_research": {}, "research": {}, "writing": {},
	"title_finalization": {}, "image_generation": {}, "rendering": {}, "review": {},
	"compliance": {}, "publication": {}, "delivery": {}, "provider_request": {},
	"artifact_upload": {}, "completion": {},
}

func safePublicDiagnosticStage(value string) string {
	value = strings.TrimSpace(value)
	if _, ok := publicDiagnosticStages[value]; ok {
		return value
	}
	return ""
}

func safePublicProviderCode(value string) string {
	canonical := strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
	if canonical == "content_exists_risk" {
		return canonical
	}
	return ""
}

func safePublicHTTPStatus(value int) int {
	if value >= 100 && value <= 599 {
		return value
	}
	return 0
}

func safePublicContentDirection(value string) string {
	switch strings.TrimSpace(value) {
	case "input", "output":
		return strings.TrimSpace(value)
	default:
		return "unknown"
	}
}

func safePublicRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 3 || len(value) > 128 {
		return ""
	}
	fingerprint := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", fingerprint)
}

func safePublicArtifactUploadReason(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline"), strings.Contains(lower, "aborted"):
		return "上传超时。"
	case strings.Contains(lower, "413"), strings.Contains(lower, "too large"), strings.Contains(lower, "size limit"):
		return "文件超过上传大小限制。"
	case strings.Contains(lower, "checksum"), strings.Contains(lower, "content hash"), strings.Contains(lower, "integrity"):
		return "文件完整性校验失败。"
	case strings.Contains(lower, "401"), strings.Contains(lower, "403"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"), strings.Contains(lower, "authorization"):
		return "上传授权失败。"
	default:
		return "上传失败，底层错误未公开。"
	}
}

func safePublicArtifactPath(value string) string {
	clean, err := CleanTaskFileRelativePath(strings.TrimSpace(value))
	if err != nil || len(clean) > 500 {
		return "未识别产物"
	}
	return filepath.ToSlash(clean)
}

func (s *TaskService) cloudFinalizationLease() time.Duration {
	if s.finalizationLease > 0 {
		return s.finalizationLease
	}
	return time.Minute
}

func (s *TaskService) cloudFinalizationRenewInterval() time.Duration {
	lease := s.cloudFinalizationLease()
	interval := s.finalizationRenewEvery
	if interval <= 0 || interval >= lease/3 {
		interval = lease / 4
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	return interval
}

func (s *TaskService) renewFinalizationLease(parent context.Context, executionID, token string) (context.Context, func(), <-chan error) {
	ctx, cancel := context.WithCancel(parent)
	stop := make(chan struct{})
	stopped := make(chan struct{})
	lost := make(chan error, 1)
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(s.cloudFinalizationRenewInterval())
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				won, err := s.renewFinalizationClaim(ctx, executionID, token)
				if err == nil && won {
					continue
				}
				if err == nil {
					err = ErrFinalizationLeaseLost
				}
				select {
				case lost <- err:
				default:
				}
				cancel()
				return
			}
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			close(stop)
			<-stopped
			cancel()
		})
	}, lost
}

func (s *TaskService) renewFinalizationClaim(ctx context.Context, executionID, token string) (bool, error) {
	if s.finalizationRenewClaim != nil {
		return s.finalizationRenewClaim(ctx, executionID, token)
	}
	return s.repo.TaskExecutions().RenewFinalizationClaim(ctx, executionID, token)
}

func finalizationLeaseError(lost <-chan error) error {
	select {
	case err := <-lost:
		return fmt.Errorf("%w: %v", ErrFinalizationLeaseLost, err)
	default:
		return nil
	}
}

func (s *TaskService) advanceExecutionFinalization(ctx context.Context, id, token, from, to string) error {
	won, err := s.repo.TaskExecutions().AdvanceFinalization(ctx, id, token, from, to)
	if err != nil {
		return fmt.Errorf("advance execution finalization to %s: %w", to, err)
	}
	if !won {
		return fmt.Errorf("execution finalization lease lost while advancing to %s", to)
	}
	return nil
}

func taskTerminalFromExecution(execution *model.TaskExecution, result *agent.ExecutionResult) (string, string) {
	errMsg := execution.TerminalReason
	if result != nil && strings.TrimSpace(result.Error) != "" {
		errMsg = result.Error
	}
	switch execution.Status {
	case model.TaskExecutionSucceeded:
		return model.TaskStatusCompleted, ""
	case model.TaskExecutionCancelled:
		return model.TaskStatusCancelled, errMsg
	default:
		return model.TaskStatusFailed, errMsg
	}
}

func isTerminalExecution(status string) bool {
	switch status {
	case model.TaskExecutionSucceeded, model.TaskExecutionFailed, model.TaskExecutionCancelled, model.TaskExecutionTimedOut:
		return true
	default:
		return false
	}
}

// terminalizeCurrentExecution is the narrow service callback used by the
// Runtime reconciler. Infrastructure failures have no successful result.
func (s *TaskService) TerminalizeCurrentExecution(ctx context.Context, executionID, status, reason string, diagnostics []byte) error {
	execution, task, err := s.currentExecution(ctx, executionID)
	if err != nil {
		return err
	}
	if !isTerminalExecution(execution.Status) {
		result := &agent.ExecutionResult{Success: false, Error: reason, RootErrorCode: reason, RemoteArtifacts: true}
		encoded, _ := json.Marshal(result)
		won, transitionErr := s.repo.TaskExecutions().Transition(ctx, execution.ID,
			[]string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning}, status,
			model.ExecutionTransition{TerminalReason: reason, Diagnostics: diagnostics, Result: encoded, FinalizationStatus: model.TaskExecutionFinalizationTerminal})
		if transitionErr != nil {
			return transitionErr
		}
		if !won {
			return nil
		}
		execution.Status, execution.TerminalReason, execution.Result = status, reason, encoded
		execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

func (s *TaskService) cancelCloudExecution(ctx context.Context, task *model.Task, userID string) error {
	if task == nil || task.CurrentExecutionID == nil {
		return fmt.Errorf("task has no current cloud execution")
	}
	executionID := *task.CurrentExecutionID
	encoded, _ := json.Marshal(&agent.ExecutionResult{Success: false, Error: "用户取消", RemoteArtifacts: true})
	var execution *model.TaskExecution
	err := s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		var err error
		execution, err = txRepo.TaskExecutions().FindByIDForUpdate(ctx, executionID)
		if err != nil {
			return err
		}
		lockedTask, err := txRepo.Tasks().FindByIDForUpdate(ctx, task.ID)
		if err != nil {
			return err
		}
		if userID != "" && lockedTask.UserID != userID {
			return fmt.Errorf("task not found")
		}
		if lockedTask.CurrentExecutionID == nil || *lockedTask.CurrentExecutionID != executionID {
			return ErrStaleTaskExecution
		}
		if !isTerminalExecution(execution.Status) {
			won, err := txRepo.TaskExecutions().Transition(ctx, execution.ID,
				[]string{model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting, model.TaskExecutionRunning},
				model.TaskExecutionCancelled,
				model.ExecutionTransition{TerminalReason: "user_cancelled", Result: encoded, FinalizationStatus: model.TaskExecutionFinalizationTerminal})
			if err != nil {
				return err
			}
			if !won {
				return ErrStaleTaskExecution
			}
			execution.Status = model.TaskExecutionCancelled
			execution.TerminalReason = "user_cancelled"
			execution.Result = encoded
			execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
		} else if execution.Status != model.TaskExecutionCancelled {
			return fmt.Errorf("task is not in a cancellable state")
		}
		if lockedTask.Status != model.TaskStatusRunning && lockedTask.Status != model.TaskStatusCancelled {
			return fmt.Errorf("task is not in a cancellable state")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("cancel cloud task: %w", err)
	}
	if err := s.finalizeTaskFromExecution(ctx, task, execution); err != nil {
		return err
	}
	return s.cleanupCancelledExecution(ctx, execution)
}

func (s *TaskService) cleanupCancelledExecution(ctx context.Context, execution *model.TaskExecution) (err error) {
	token := uuid.NewString()
	won, err := s.repo.TaskExecutions().ClaimCleanup(ctx, execution.ID, token, s.cloudFinalizationLease())
	if err != nil || !won {
		return err
	}
	defer func() {
		if releaseErr := s.repo.TaskExecutions().ReleaseCleanup(context.WithoutCancel(ctx), execution.ID, token); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()
	deleteCtx, cancel := context.WithTimeout(ctx, s.cloudFinalizationLease()/2)
	authoritative, exists, deleteErr := s.ResolveExecutionCleanupRuntime(deleteCtx, execution.ID, token)
	if deleteErr == nil && exists {
		deleteErr = s.runtimeDispatcher.Delete(deleteCtx, authoritative)
	}
	cancel()
	if deleteErr != nil {
		backoff := s.cleanupRetryBackoff
		if backoff <= 0 {
			backoff = 10 * time.Second
		}
		failed, failErr := s.repo.TaskExecutions().FailCleanup(context.WithoutCancel(ctx), execution.ID, token, backoff)
		if failErr != nil {
			return errors.Join(fmt.Errorf("delete cancelled runtime workload: %w", deleteErr), failErr)
		}
		if !failed {
			return errors.Join(fmt.Errorf("delete cancelled runtime workload: %w", deleteErr), errors.New("cancelled execution cleanup lease lost while recording retry"))
		}
		return fmt.Errorf("delete cancelled runtime workload: %w", deleteErr)
	}
	completed, err := s.repo.TaskExecutions().CompleteCleanup(ctx, execution.ID, token)
	if err != nil {
		return err
	}
	if !completed {
		return errors.New("cancelled execution cleanup lease lost")
	}
	return nil
}
