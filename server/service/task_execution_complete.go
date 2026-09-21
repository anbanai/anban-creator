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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
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
	inputDigest, err := completionInputDigest(result)
	if err != nil {
		return err
	}
	if isTerminalExecution(execution.Status) {
		if err := ensureCompletionInputMatches(execution, inputDigest); err != nil {
			return err
		}
		return s.finalizeTaskFromExecution(ctx, task, execution)
	}
	terminal, reason, normalized, err := s.cloudTerminalOutcome(ctx, task, execution, result)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(cloudCompletionRecord{ExecutionResult: normalized, CompletionInputDigest: inputDigest})
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
		if err := ensureCompletionInputMatches(execution, inputDigest); err != nil {
			return err
		}
	} else {
		execution.Status = terminal
		execution.TerminalReason = reason
		execution.Result = encoded
		execution.FinalizationStatus = model.TaskExecutionFinalizationTerminal
	}
	return s.finalizeTaskFromExecution(ctx, task, execution)
}

// This server-owned receipt is stored atomically with the normalized outcome.
// Replays compare the original request, never validation against mutable file state.
// Embedding preserves the result shape consumed by the durable finalizer.
type cloudCompletionRecord struct {
	*agent.ExecutionResult
	CompletionInputDigest string `json:"completion_input_digest"`
}

func completionInputDigest(result *agent.ExecutionResult) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal completion input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func ensureCompletionInputMatches(execution *model.TaskExecution, inputDigest string) error {
	var stored cloudCompletionRecord
	if execution == nil || json.Unmarshal(execution.Result, &stored) != nil || stored.ExecutionResult == nil || stored.CompletionInputDigest != inputDigest {
		return ErrTaskCompletionConflict
	}
	return nil
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
			if failure := missingRequiredUploadFailure(execution, files, validation.Missing, result.ArtifactUploadFailures); failure != nil {
				result.RootErrorCode = "artifact_upload_failed"
				result.FailureStage = "artifact_upload"
				result.Error = "产物上传失败，已成功上传的文件已保留。"
				result.TerminalReason = model.TaskBillingTerminalPlatformError
				result.HTTPStatus = safePublicHTTPStatus(failure.HTTPStatus)
				result.RequestID = failure.RequestID
				result.Recoverable = failure.Retryable
				failureReason = "artifact_upload_failed"
			}
		} else {
			deliveryAccepted = true
		}
	}
	if !execution.ManifestSealed {
		if code := artifactFailureCode(result); code != "" {
			result.Error = "产物清单提交失败，交付尚未确认。"
			if code == "artifact_upload_failed" {
				result.Error = "产物上传失败，交付尚未确认。"
			}
			result.FailureStage = "artifact_upload"
			result.TerminalReason = model.TaskBillingTerminalPlatformError
			failureReason = code
			if failure := result.ArtifactFinalizationFailure; validArtifactTransferFailure(failure) && validArtifactOperation(failure.Operation) {
				result.HTTPStatus = safePublicHTTPStatus(failure.HTTPStatus)
				result.RequestID = failure.RequestID
				result.Recoverable = failure.Retryable
			}
		}
	}
	if result.Success || deliveryAccepted {
		if artifactFailureCode(result) != "" {
			result.Success, result.Error, result.RootErrorCode = true, "", ""
			result.FailureStage, result.HTTPStatus, result.RequestID = "", 0, ""
			result.Recoverable = false
		}
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
			result.ModelUsage, result.CostStatus, artifactAction, executionTerminalScope(result),
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

func (s *TaskService) finalizeCloudDraftDelivery(ctx context.Context, task *model.Task, execution *model.TaskExecution, _ *agent.ExecutionResult) (err error) {
	// Preflight failures do not reach WechatPublicationService, so its lifecycle
	// callback cannot project them. Sync after recording every delivery result,
	// including replays after an interrupted finalization.
	defer func() {
		if err == nil && task.Type == model.PlatformArticle {
			_, err = s.SyncWechatPublicationLifecycle(ctx, task.ID)
		}
	}()
	latestExecution, err := s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return err
	}
	switch latestExecution.DraftDeliveryStatus {
	case model.TaskExecutionDraftDeliverySucceeded, model.TaskExecutionDraftDeliverySkipped,
		model.TaskExecutionDraftDeliveryBlocked, model.TaskExecutionDraftDeliveryFailed, model.TaskExecutionDraftDeliveryAmbiguous,
		model.TaskExecutionDraftDeliveryNotRequested:
		execution.DraftDeliveryStatus = latestExecution.DraftDeliveryStatus
		execution.DraftDeliveryResult = latestExecution.DraftDeliveryResult
		return nil
	case model.TaskExecutionDraftDeliveryInFlight:
		// The caller that moved the execution to in_flight still owns the
		// external call. A concurrent finalizer must not steal that ownership;
		// its completion callback or durable reconciliation will resolve it.
		execution.DraftDeliveryStatus = latestExecution.DraftDeliveryStatus
		execution.DraftDeliveryResult = latestExecution.DraftDeliveryResult
		return nil
	}
	status, evidence, err := s.resolveCloudDraftDelivery(ctx, task, execution)
	if err != nil {
		return err
	}
	latestExecution, err = s.repo.TaskExecutions().FindByID(ctx, execution.ID)
	if err != nil {
		return err
	}
	if latestExecution.DraftDeliveryStatus != "" {
		execution.DraftDeliveryStatus = latestExecution.DraftDeliveryStatus
		execution.DraftDeliveryResult = latestExecution.DraftDeliveryResult
		return nil
	}
	won, err := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, "", status, evidence)
	if err != nil {
		return err
	}
	if !won {
		return fmt.Errorf("record draft delivery: state changed concurrently")
	}
	execution.DraftDeliveryStatus = status
	execution.DraftDeliveryResult = evidence
	return nil
}

type articleStatusArtifact struct {
	Status      string `json:"status"`
	ContentHash string `json:"content_hash"`
	Code        string `json:"code,omitempty"`
}

type articlePublicationPackage struct {
	SchemaVersion string `json:"schema_version"`
	Article       struct {
		Title         string `json:"title"`
		Digest        string `json:"digest"`
		ContentPath   string `json:"content_path"`
		ContentSHA256 string `json:"content_sha256"`
	} `json:"article"`
	Readiness struct {
		Status        string   `json:"status"`
		Code          string   `json:"code"`
		EvidencePaths []string `json:"evidence_paths"`
	} `json:"readiness"`
}

type publicationDeliveryResult struct {
	Source     string `json:"source"`
	Status     string `json:"status"`
	Code       string `json:"code,omitempty"`
	Attempted  bool   `json:"attempted"`
	Action     string `json:"action,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

func publicationDeliveryEvidence(source, status, code string, attempted bool, action string) publicationDeliveryResult {
	return publicationDeliveryResult{
		Source: source, Status: status, Code: code, Attempted: attempted, Action: action,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func encodedPublicationDeliveryEvidence(source, status, code string, attempted bool, action string) []byte {
	evidence, _ := json.Marshal(publicationDeliveryEvidence(source, status, code, attempted, action))
	return evidence
}

func (s *TaskService) resolveCloudDraftDelivery(ctx context.Context, task *model.Task, execution *model.TaskExecution) (string, []byte, error) {
	if execution == nil || execution.Status != model.TaskExecutionSucceeded {
		return model.TaskExecutionDraftDeliveryNotRequested,
			encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryNotRequested, "execution_not_succeeded", false, ""), nil
	}
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
			status := model.TaskExecutionDraftDeliveryFailed
			if publication.DraftAddAttemptedAt != nil {
				status = model.TaskExecutionDraftDeliveryAmbiguous
			}
			return status, evidence, nil
		}
		if !draftRetryEligible(publication) {
			status := draftDeliveryStatusFromPublication(publication)
			evidence, _ := json.Marshal(map[string]string{"source": "durable_publication", "status": publication.Status})
			return status, evidence, nil
		}
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, fmt.Errorf("find durable draft publication: %w", err)
	}

	if task.Type != model.PlatformArticle {
		return model.TaskExecutionDraftDeliveryNotRequested, encodedPublicationDeliveryEvidence("finalizer", model.TaskExecutionDraftDeliveryNotRequested, "", false, ""), nil
	}
	return s.finalizeArticlePublication(ctx, task, execution)
}

func draftRetryEligible(publication *model.WechatPublication) bool {
	if publication == nil || publication.Status != model.WechatPublicationStatusUnsupported || publication.DraftMediaID != "" ||
		publication.WechatStatusCode == 0 || publication.DraftAddAttempts >= 2 {
		return false
	}
	return publication.DraftAddAttemptedAt == nil || publication.DraftRetryAuthorizedAt != nil
}

func (s *TaskService) finalizeArticlePublication(ctx context.Context, task *model.Task, execution *model.TaskExecution) (string, []byte, error) {
	if _, err := s.repo.Projects().FindByID(ctx, task.ProjectID); err != nil {
		return "", nil, fmt.Errorf("load article publication project: %w", err)
	}
	block := func(code, action string) (string, []byte, error) {
		return model.TaskExecutionDraftDeliveryBlocked, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryBlocked, code, false, action), nil
	}
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, execution.ID)
	if err != nil {
		return "", nil, err
	}
	byPath := make(map[string]*model.TaskFile, len(files))
	allowedImageURLs := make(map[string]struct{})
	coverImageURLs := make(map[string]struct{})
	contentImageURLs := make(map[string]struct{})
	coverMediaIDs := make(map[string]struct{})
	coverMediaIDsByURL := make(map[string]map[string]struct{})
	for _, file := range files {
		if file == nil || file.ExecutionID != execution.ID || file.TaskID != task.ID {
			continue
		}
		byPath[file.FilePath] = file
		if file.State != model.TaskFileStateDelivered {
			continue
		}
		isCover := file.Role == model.FileRoleCover || strings.HasPrefix(strings.ToLower(file.FileName), "cover")
		isImage := isCover || file.Role == model.FileRoleImage || strings.HasPrefix(file.MimeType, "image/")
		wechatURL := strings.TrimSpace(file.WechatURL)
		if isCover {
			if mediaID := strings.TrimSpace(file.MediaID); mediaID != "" {
				coverMediaIDs[mediaID] = struct{}{}
				if wechatURL != "" {
					if coverMediaIDsByURL[wechatURL] == nil {
						coverMediaIDsByURL[wechatURL] = make(map[string]struct{})
					}
					coverMediaIDsByURL[wechatURL][mediaID] = struct{}{}
				}
			}
		}
		if isImage && wechatURL != "" {
			allowedImageURLs[wechatURL] = struct{}{}
			if isCover {
				coverImageURLs[wechatURL] = struct{}{}
			} else {
				contentImageURLs[wechatURL] = struct{}{}
			}
		}
	}
	for coverURL := range coverImageURLs {
		delete(contentImageURLs, coverURL)
	}
	coverRequested := task.ArticleWithCover == nil || *task.ArticleWithCover
	contentImagesRequested := task.ArticleWithContentImages == nil || *task.ArticleWithContentImages
	if coverRequested && len(coverMediaIDs) == 0 {
		return block("cover_media_missing", "retry_visuals")
	}
	if contentImagesRequested && len(contentImageURLs) == 0 {
		return block("content_images_missing", "retry_visuals")
	}

	packageBody, found, err := s.readExecutionArtifact(ctx, execution.ID, "output/draft.json", maxTaskDeliveryJSONBytes)
	if err != nil {
		return "", nil, fmt.Errorf("read article publication package: %w", err)
	}
	if !found {
		return block("publication_package_missing", "review_content")
	}
	var pkg articlePublicationPackage
	decoder := json.NewDecoder(strings.NewReader(string(packageBody)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pkg); err != nil {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content"), nil
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content"), nil
	}
	if pkg.SchemaVersion != "1.0" || pkg.Article.ContentPath != "output/05-article.html" ||
		strings.TrimSpace(pkg.Article.Title) == "" || utf8.RuneCountInString(pkg.Article.Title) > 64 ||
		utf8.RuneCountInString(pkg.Article.Digest) > 120 {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content"), nil
	}
	if pkg.Readiness.Status != "ready" {
		return block("semantic_review_blocked", "review_content")
	}
	if pkg.Readiness.Code != "" || len(pkg.Readiness.EvidencePaths) != 3 {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content"), nil
	}
	requiredEvidence := map[string]bool{
		"output/marketing-scan.json": false, "output/final-review.md": false, "output/viral-audit.md": false,
	}
	for _, path := range pkg.Readiness.EvidencePaths {
		present, required := requiredEvidence[path]
		if !required || present {
			return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "publication_package_invalid", false, "review_content"), nil
		}
		file := byPath[path]
		requiredEvidence[path] = file != nil && file.State == model.TaskFileStateDelivered
	}
	for _, present := range requiredEvidence {
		if !present {
			return block("review_evidence_missing", "review_content")
		}
	}
	htmlBody, found, err := s.readExecutionArtifact(ctx, execution.ID, "output/05-article.html", maxTaskDeliveryHTMLBytes)
	if err != nil {
		return "", nil, fmt.Errorf("read final article HTML: %w", err)
	}
	if !found {
		return block("final_html_missing", "review_content")
	}
	actualHash := fmt.Sprintf("%x", sha256.Sum256(htmlBody))
	if strings.TrimSpace(pkg.Article.ContentSHA256) != actualHash {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "content_hash_mismatch", false, "review_content"), nil
	}
	if err := validateWechatHTMLFragment("output/05-article.html", htmlBody); err != nil {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "unsafe_html", false, "review_content"), nil
	}
	imageSources, err := wechatDraftImageSources(string(htmlBody))
	if err != nil {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "unsafe_html", false, "review_content"), nil
	}
	if contentImagesRequested && len(imageSources) == 0 {
		return block("content_images_missing", "retry_visuals")
	}
	hasRenderedContentImage := false
	for index, source := range imageSources {
		if _, ok := allowedImageURLs[source]; !ok {
			return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "content_image_source_invalid", false, "review_content"), nil
		}
		if _, isCover := coverImageURLs[source]; isCover && index != 0 {
			return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, "content_image_source_invalid", false, "review_content"), nil
		}
		if _, ok := contentImageURLs[source]; ok {
			hasRenderedContentImage = true
		}
	}
	if contentImagesRequested && !hasRenderedContentImage {
		return block("content_images_missing", "retry_visuals")
	}
	selectedCoverMediaIDs := coverMediaIDs
	if len(imageSources) > 0 {
		if _, heroIsCover := coverImageURLs[imageSources[0]]; heroIsCover {
			selectedCoverMediaIDs = coverMediaIDsByURL[imageSources[0]]
		}
	}
	coverMediaID := ""
	if len(selectedCoverMediaIDs) == 1 {
		for mediaID := range selectedCoverMediaIDs {
			coverMediaID = mediaID
		}
	}
	if coverRequested && coverMediaID == "" {
		return block("cover_media_missing", "retry_visuals")
	}
	var scan articleStatusArtifact
	found, err = s.readCurrentArticleStatusArtifact(ctx, execution.ID, "output/marketing-scan.json", &scan)
	if err != nil || !found {
		return block("marketing_scan_invalid", "review_content")
	}
	if scan.Status == "block_publish" {
		return block("marketing_scan_blocked", "review_content")
	}
	if scan.Status != "passed" && scan.Status != "warning" {
		return block("marketing_scan_invalid", "review_content")
	}
	if s.wechatPublicationSvc == nil {
		return block("publication_service_unavailable", "retry_draft")
	}
	request := appwechat.DraftAddRequest{Articles: []appwechat.DraftArticle{{
		Title: strings.TrimSpace(pkg.Article.Title), Author: strings.TrimSpace(task.ProjectSnapshot.Data().Author),
		Digest: strings.TrimSpace(pkg.Article.Digest), Content: string(htmlBody), ThumbMediaID: coverMediaID,
	}}}
	publication, createErr := s.wechatPublicationSvc.CreateDraft(ctx, task.UserID, task.ID, task.ProjectID, execution.ID, request)
	attempted := publication != nil && publication.DraftAddAttemptedAt != nil
	if createErr == nil && publication != nil && publication.DraftMediaID != "" {
		return model.TaskExecutionDraftDeliverySucceeded, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliverySucceeded, "", true, ""), nil
	}
	if errors.Is(createErr, ErrWechatPublicationPending) && attempted {
		return model.TaskExecutionDraftDeliveryAmbiguous, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryAmbiguous, "create_draft_pending_reconciliation", true, "check_wechat"), nil
	}
	if attempted && (errors.Is(createErr, ErrWechatPublicationDraftRejected) || errors.Is(createErr, ErrWechatPublicationDraftUnsupported)) {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, wechatDraftDeliveryFailureCode(createErr), true, "retry_draft"), nil
	}
	if attempted {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, wechatDraftDeliveryFailureCode(createErr), true, "check_wechat"), nil
	}
	if errors.Is(createErr, ErrWechatPublicationInvalidPayload) || errors.Is(createErr, ErrWechatPublicationMarketingBlocked) {
		return model.TaskExecutionDraftDeliveryFailed, encodedPublicationDeliveryEvidence("server_finalizer", model.TaskExecutionDraftDeliveryFailed, wechatDraftDeliveryFailureCode(createErr), false, "review_content"), nil
	}
	return block(wechatDraftDeliveryFailureCode(createErr), "retry_draft")
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
		if publication.DraftAddAttemptedAt == nil {
			return model.TaskExecutionDraftDeliveryBlocked
		}
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
		Publication:  publicationTaskOutcome(execution),
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
			if strings.TrimSpace(failure.Path) == "" || strings.TrimSpace(failure.Code) == "" {
				continue
			}
			outcome.Warnings = append(outcome.Warnings, model.TaskOutcomeWarning{
				Code: "artifact_upload_failed", Stage: "artifact_upload",
				Message: fmt.Sprintf("文件 %s 上传失败：%s", safePublicArtifactPath(failure.Path), safePublicArtifactUploadReason(failure.Code)),
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
	case model.TaskExecutionDraftDeliveryBlocked:
		return model.TaskPublicationBlocked
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

func publicationTaskOutcome(execution *model.TaskExecution) model.TaskPublicationOutcome {
	if execution == nil {
		return model.TaskPublicationOutcome{Status: model.TaskPublicationNotRequested}
	}
	outcome := model.TaskPublicationOutcome{Status: publicationOutcomeStatus(execution.DraftDeliveryStatus)}
	var evidence struct {
		Code       string `json:"code"`
		Attempted  bool   `json:"attempted"`
		Action     string `json:"action"`
		OccurredAt string `json:"occurred_at"`
	}
	if len(execution.DraftDeliveryResult) > 0 && json.Unmarshal(execution.DraftDeliveryResult, &evidence) == nil {
		outcome.Code = safePublicationOutcomeCode(evidence.Code)
		outcome.Attempted = evidence.Attempted
		outcome.Action = safePublicationOutcomeAction(evidence.Action)
		if _, err := time.Parse(time.RFC3339, evidence.OccurredAt); err == nil {
			outcome.OccurredAt = evidence.OccurredAt
		}
	}
	if outcome.Status == model.TaskPublicationSucceeded || outcome.Status == model.TaskPublicationAmbiguous {
		outcome.Attempted = true
	}
	outcome.Message = publicationOutcomeMessage(outcome.Status, outcome.Code)
	return outcome
}

func safePublicationOutcomeAction(action string) string {
	switch strings.TrimSpace(action) {
	case "retry_visuals", "retry_draft", "fix_project_config", "review_content", "check_wechat":
		return strings.TrimSpace(action)
	default:
		return ""
	}
}

func safePublicationOutcomeCode(code string) string {
	switch strings.TrimSpace(code) {
	case "create_draft_invalid_payload",
		"create_draft_marketing_blocked",
		"create_draft_not_found",
		"create_draft_project_mismatch",
		"create_draft_execution_mismatch",
		"create_draft_execution_required",
		"create_draft_forbidden",
		"create_draft_disabled",
		"create_draft_unsupported",
		"create_draft_rejected",
		"create_draft_reconciliation_failed",
		"create_draft_conflict",
		"create_draft_pending_reconciliation",
		"create_draft_provider_failure",
		"publication_disabled",
		"execution_not_succeeded",
		"publication_not_attempted",
		"publication_package_missing",
		"publication_package_invalid",
		"semantic_review_blocked",
		"review_evidence_missing",
		"final_html_missing",
		"content_hash_mismatch",
		"unsafe_html",
		"content_image_source_invalid",
		"marketing_scan_invalid",
		"marketing_scan_blocked",
		"cover_media_missing",
		"content_images_missing",
		"publication_service_unavailable":
		return strings.TrimSpace(code)
	default:
		return ""
	}
}

func publicationOutcomeMessage(status model.TaskPublicationStatus, code string) string {
	switch code {
	case "create_draft_invalid_payload":
		return "草稿内容或图片不符合公众号投递要求。"
	case "create_draft_marketing_blocked":
		return "发布前检查发现不适合自动投递的内容，本次没有提交到微信。"
	case "create_draft_execution_mismatch", "create_draft_execution_required":
		return "任务执行状态不匹配，本次没有提交到微信。"
	case "create_draft_disabled":
		return "该项目未启用公众号投递，本次没有提交到微信。"
	case "create_draft_unsupported":
		return "当前公众号没有草稿接口权限，本次没有创建草稿。"
	case "create_draft_rejected":
		return "微信拒绝创建草稿，请检查公众号凭据或 IP 白名单。"
	case "create_draft_conflict":
		return "该任务已有另一份草稿请求，本次没有重复提交。"
	case "create_draft_pending_reconciliation":
		return "微信是否收到草稿请求暂时无法确认；为避免重复投稿，系统不会再次提交。"
	case "create_draft_provider_failure":
		return "创建公众号草稿时服务异常，本次自动发布没有启动。"
	case "create_draft_reconciliation_failed":
		return "微信是否收到草稿请求仍无法确认，请到公众号后台核对草稿箱；系统不会重复提交。"
	case "publication_package_missing":
		return "发布包缺失，微信尚未收到请求。"
	case "publication_package_invalid", "content_hash_mismatch", "unsafe_html":
		return "发布包校验失败，微信尚未收到请求。"
	case "content_image_source_invalid":
		return "正文图片不属于当前任务执行，微信尚未收到请求。"
	case "semantic_review_blocked", "review_evidence_missing", "marketing_scan_invalid", "marketing_scan_blocked":
		return "发布前审核未通过，微信尚未收到请求。"
	case "cover_media_missing":
		return "封面尚未生成或上传完成，微信尚未收到请求。"
	case "content_images_missing":
		return "正文配图尚未生成，微信尚未收到请求。"
	case "publication_service_unavailable":
		return "公众号发布服务暂时不可用，微信尚未收到请求。"
	case "execution_not_succeeded":
		return "内容生成未成功，本次未请求公众号发布。"
	}
	if status == model.TaskPublicationAmbiguous {
		return "微信是否收到草稿请求暂时无法确认；为避免重复投稿，系统不会再次提交。"
	}
	return ""
}

func publicExecutionDiagnostic(execution *model.TaskExecution, result *agent.ExecutionResult) *model.ExecutionDiagnostic {
	if execution == nil || result == nil {
		return nil
	}
	if code := artifactFailureCode(result); code != "" {
		summary := "产物上传失败，已成功上传的文件已保留。"
		if code == "artifact_manifest_failed" {
			summary = "产物清单提交失败，交付尚未确认。"
		}
		return &model.ExecutionDiagnostic{Code: code, Stage: "artifact_upload", Summary: summary,
			HTTPStatus: safePublicHTTPStatus(result.HTTPStatus), Recoverable: result.Recoverable,
			RequestID: safePublicRequestID(result.RequestID)}
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

func safePublicArtifactUploadReason(code string) string {
	switch code {
	case "network_timeout", "deadline_exceeded":
		return "上传超时。"
	case "integrity_mismatch":
		return "文件完整性校验失败。"
	case "unauthorized", "signature_expired":
		return "上传授权失败。"
	case "connection_reset", "network_unavailable", "service_unavailable", "rate_limited":
		return "上传服务暂时不可用。"
	case "cancelled":
		return "上传已取消。"
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
