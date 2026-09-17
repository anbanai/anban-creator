package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

var (
	ErrInvalidTaskLifecyclePlan     = errors.New("invalid task lifecycle plan")
	ErrTaskLifecycleImmutablePrefix = errors.New("task lifecycle immutable prefix changed")
	ErrTaskLifecycleOutOfOrder      = errors.New("task lifecycle stage is out of order")
	ErrTaskLifecycleUnknownStage    = errors.New("task lifecycle stage is unknown")
	ErrTaskLifecycleInvalidState    = errors.New("task lifecycle state is invalid")
)

var taskLifecycleStageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

// SetTaskProgressPlan declares or replans the agent-owned work stages for the
// current execution. Completed work is durable across resumed executions;
// active work is immutable within one execution; only the untouched tail can
// change.
func (s *TaskService) SetTaskProgressPlan(ctx context.Context, taskID, executionID string, stages []model.TaskLifecyclePlanStage) (*model.TaskLifecycle, error) {
	normalized, err := normalizeTaskLifecyclePlan(stages)
	if err != nil {
		return nil, err
	}
	var result model.TaskLifecycle
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := lockCurrentLifecycleExecution(ctx, tx, taskID, executionID, true); err != nil {
			return err
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStaleTaskExecution
			}
			return fmt.Errorf("lock lifecycle task: %w", err)
		}
		if task.Status != model.TaskStatusRunning || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
			return ErrStaleTaskExecution
		}

		current := cloneTaskLifecycle(task.Lifecycle.Data())
		serverStages, err := taskServerLifecycleStages(ctx, tx, task)
		if err != nil {
			return err
		}
		next, changed, err := replanTaskLifecycle(current, executionID, normalized, serverStages)
		if err != nil {
			return err
		}
		if !changed {
			result = current
			return nil
		}
		now := time.Now().UTC()
		next.Version = model.TaskLifecycleVersion
		next.Revision = current.Revision + 1
		next.ExecutionID = executionID
		next.UpdatedAt = now
		updated, err := tx.Tasks().UpdateLifecycle(ctx, taskID, executionID, next)
		if err != nil {
			return fmt.Errorf("persist task lifecycle plan: %w", err)
		}
		if !updated {
			return ErrStaleTaskExecution
		}
		result = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publishLifecycle(ctx, taskID, result)
	return &result, nil
}

// UpdateTaskProgress advances one declared work stage. The Server owns titles,
// ordering, timestamps, and revision; the client may only select a stage ID,
// active/complete state, and a concise latest update.
func (s *TaskService) UpdateTaskProgress(ctx context.Context, taskID, executionID, stageID, state, description string) (*model.TaskLifecycle, error) {
	stageID = strings.TrimSpace(stageID)
	state = strings.TrimSpace(state)
	description = strings.TrimSpace(description)
	if state != model.TaskLifecycleStateActive && state != model.TaskLifecycleStateComplete {
		return nil, ErrTaskLifecycleInvalidState
	}
	var result model.TaskLifecycle
	var changed bool
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := lockCurrentLifecycleExecution(ctx, tx, taskID, executionID, true); err != nil {
			return err
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStaleTaskExecution
			}
			return fmt.Errorf("lock lifecycle task: %w", err)
		}
		if task.Status != model.TaskStatusRunning || task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
			return ErrStaleTaskExecution
		}
		current := cloneTaskLifecycle(task.Lifecycle.Data())
		if current.Version != model.TaskLifecycleVersion || current.ExecutionID != executionID {
			return ErrTaskLifecycleUnknownStage
		}
		next, didChange, err := advanceTaskLifecycle(current, stageID, state, description, time.Now().UTC())
		if err != nil {
			return err
		}
		if !didChange {
			result = current
			return nil
		}
		next.Revision++
		next.UpdatedAt = time.Now().UTC()
		updated, err := tx.Tasks().UpdateLifecycle(ctx, taskID, executionID, next)
		if err != nil {
			return fmt.Errorf("persist task lifecycle update: %w", err)
		}
		if !updated {
			return ErrStaleTaskExecution
		}
		changed = true
		result = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.publishLifecycle(ctx, taskID, result)
	}
	return &result, nil
}

// FinalizeTaskLifecycle normalizes work stages when an execution reaches a
// terminal outcome. Publication stages remain pending after successful content
// delivery and are driven independently by the Server publication lifecycle.
func (s *TaskService) FinalizeTaskLifecycle(ctx context.Context, taskID, executionID, taskStatus, description string) (*model.TaskLifecycle, error) {
	description = strings.TrimSpace(description)
	var result model.TaskLifecycle
	var changed bool
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := lockCurrentLifecycleExecution(ctx, tx, taskID, executionID, false); err != nil {
			return err
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrStaleTaskExecution
			}
			return fmt.Errorf("lock lifecycle task for finalization: %w", err)
		}
		if task.CurrentExecutionID == nil || *task.CurrentExecutionID != executionID {
			return ErrStaleTaskExecution
		}
		current := cloneTaskLifecycle(task.Lifecycle.Data())
		if current.Version != model.TaskLifecycleVersion || len(current.Stages) == 0 {
			result = current
			return nil
		}
		now := time.Now().UTC()
		next, normalized := model.NormalizeTaskLifecycleTerminal(current, taskStatus, description, now)
		changed = normalized
		if !changed {
			result = current
			return nil
		}
		updated, err := tx.Tasks().UpdateLifecycle(ctx, taskID, executionID, next)
		if err != nil {
			return fmt.Errorf("persist finalized task lifecycle: %w", err)
		}
		if !updated {
			return ErrStaleTaskExecution
		}
		result = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.publishLifecycle(ctx, taskID, result)
	}
	return &result, nil
}

type taskLifecycleStageProjection struct {
	State        string
	LatestUpdate string
}

type wechatPublicationLifecycleProjection struct {
	Draft       taskLifecycleStageProjection
	Publication taskLifecycleStageProjection
}

// SyncWechatPublicationLifecycle projects durable draft/publication evidence
// onto the two Server-owned stages. Provider rows remain the source of truth;
// the lifecycle is the compact user-facing read model.
func (s *TaskService) SyncWechatPublicationLifecycle(ctx context.Context, taskID string) (*model.TaskLifecycle, error) {
	var result model.TaskLifecycle
	var changed bool
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		// Resolve the execution identity without a lock, then follow the global
		// execution -> task lock order and revalidate the relationship.
		snapshot, err := tx.Tasks().FindByID(ctx, taskID)
		if err != nil {
			return err
		}
		if snapshot.CurrentExecutionID == nil || strings.TrimSpace(*snapshot.CurrentExecutionID) == "" {
			result = cloneTaskLifecycle(snapshot.Lifecycle.Data())
			return nil
		}
		executionID := strings.TrimSpace(*snapshot.CurrentExecutionID)
		execution, err := tx.TaskExecutions().FindByIDForUpdate(ctx, executionID)
		if err != nil {
			return err
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, taskID)
		if err != nil {
			return err
		}
		if task.CurrentExecutionID == nil || strings.TrimSpace(*task.CurrentExecutionID) != executionID || execution.TaskID != task.ID {
			return ErrStaleTaskExecution
		}
		current := cloneTaskLifecycle(task.Lifecycle.Data())
		result = current
		if current.Version != model.TaskLifecycleVersion {
			return nil
		}
		publication, err := tx.WechatPublications().FindByTaskID(ctx, task.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			publication = nil
		} else if err != nil {
			return err
		}

		now := time.Now().UTC()
		next := cloneTaskLifecycle(current)
		projection := deriveWechatPublicationLifecycle(execution.DraftDeliveryStatus, publication)
		stageChanged := applyTaskLifecycleStageProjection(&next, "system_draft", projection.Draft, now)
		stageChanged = applyTaskLifecycleStageProjection(&next, "system_publication", projection.Publication, now) || stageChanged
		if !stageChanged {
			return nil
		}
		next.Revision++
		next.UpdatedAt = now
		updated, err := tx.Tasks().UpdateLifecycle(ctx, task.ID, *task.CurrentExecutionID, next)
		if err != nil {
			return fmt.Errorf("persist WeChat publication lifecycle: %w", err)
		}
		if !updated {
			return ErrStaleTaskExecution
		}
		changed = true
		result = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.publishLifecycle(ctx, taskID, result)
	}
	return &result, nil
}

func deriveWechatPublicationLifecycle(deliveryStatus string, publication *model.WechatPublication) wechatPublicationLifecycleProjection {
	projection := wechatPublicationLifecycleProjection{
		Draft:       taskLifecycleStageProjection{State: model.TaskLifecycleStatePending},
		Publication: taskLifecycleStageProjection{State: model.TaskLifecycleStatePending},
	}
	draftReady := publication != nil && strings.TrimSpace(publication.DraftMediaID) != "" || deliveryStatus == model.TaskExecutionDraftDeliverySucceeded
	if draftReady {
		projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateComplete, LatestUpdate: "公众号草稿已创建"}
	} else {
		switch deliveryStatus {
		case model.TaskExecutionDraftDeliveryInFlight:
			projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateActive, LatestUpdate: "正在创建公众号草稿"}
		case model.TaskExecutionDraftDeliveryAmbiguous:
			projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "创建结果待确认，请先检测公众号状态"}
		case model.TaskExecutionDraftDeliveryBlocked, model.TaskExecutionDraftDeliveryFailed:
			projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "创建草稿暂时受阻，可在修复后重试"}
		}
		if publication != nil {
			switch publication.Status {
			case model.WechatPublicationStatusDrafting:
				if publication.DraftAddAttemptedAt != nil {
					projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "创建结果待确认，请先检测公众号状态"}
				} else if projection.Draft.State == model.TaskLifecycleStatePending {
					projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateActive, LatestUpdate: "正在创建公众号草稿"}
				}
			case model.WechatPublicationStatusUnsupported:
				if publication.DraftAddAttemptedAt != nil && publication.DraftRetryAuthorizedAt == nil {
					projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "创建结果待确认，请先检测公众号状态"}
				} else {
					projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "创建草稿暂时受阻，可在修复后重试"}
				}
			case model.WechatPublicationStatusPublishFailed:
				projection.Draft = taskLifecycleStageProjection{State: model.TaskLifecycleStateFailed, LatestUpdate: "公众号草稿创建失败"}
			}
		}
	}

	if !draftReady || publication == nil {
		return projection
	}
	switch publication.Status {
	case model.WechatPublicationStatusPublished:
		projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateComplete, LatestUpdate: "文章已正式发布"}
	case model.WechatPublicationStatusPublishFailed:
		projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateFailed, LatestUpdate: "正式发布失败，请前往公众号后台处理"}
	case model.WechatPublicationStatusNeedsSelection:
		projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "请选择要关联的公众号文章"}
	case model.WechatPublicationStatusPublishSubmitting, model.WechatPublicationStatusPublishing:
		projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateActive, LatestUpdate: "正在等待微信返回正式发布结果"}
	case model.WechatPublicationStatusUnsupported:
		if hasWechatSubmissionEvidence(publication) {
			projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "正式发布可能已提交，只能检测结果"}
		} else {
			projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateBlocked, LatestUpdate: "当前发布能力不可用，可重试发布"}
		}
	case model.WechatPublicationStatusDrafted:
		if hasWechatSubmissionEvidence(publication) {
			projection.Publication = taskLifecycleStageProjection{State: model.TaskLifecycleStateActive, LatestUpdate: "正式发布可能已提交，正在核对结果"}
		} else {
			projection.Publication.LatestUpdate = "草稿已就绪，等待正式发布"
		}
	}
	return projection
}

func applyTaskLifecycleStageProjection(lifecycle *model.TaskLifecycle, stageID string, projection taskLifecycleStageProjection, now time.Time) bool {
	for i := range lifecycle.Stages {
		stage := &lifecycle.Stages[i]
		if stage.ID != stageID || stage.Source != model.TaskLifecycleSourceServer {
			continue
		}
		if stage.State == projection.State && stage.LatestUpdate == projection.LatestUpdate {
			return false
		}
		stage.State = projection.State
		stage.LatestUpdate = projection.LatestUpdate
		switch projection.State {
		case model.TaskLifecycleStatePending:
			stage.StartedAt = nil
			stage.CompletedAt = nil
		case model.TaskLifecycleStateActive, model.TaskLifecycleStateBlocked:
			if stage.StartedAt == nil {
				startedAt := now
				stage.StartedAt = &startedAt
			}
			stage.CompletedAt = nil
		case model.TaskLifecycleStateComplete, model.TaskLifecycleStateFailed, model.TaskLifecycleStateCancelled, model.TaskLifecycleStateSkipped:
			if stage.StartedAt == nil {
				startedAt := now
				stage.StartedAt = &startedAt
			}
			completedAt := now
			stage.CompletedAt = &completedAt
		}
		return true
	}
	return false
}

func normalizeTaskLifecyclePlan(stages []model.TaskLifecyclePlanStage) ([]model.TaskLifecyclePlanStage, error) {
	if len(stages) < 2 || len(stages) > 7 {
		return nil, ErrInvalidTaskLifecyclePlan
	}
	seen := make(map[string]struct{}, len(stages))
	normalized := make([]model.TaskLifecyclePlanStage, 0, len(stages))
	for _, stage := range stages {
		stage.ID = strings.TrimSpace(stage.ID)
		stage.Title = strings.TrimSpace(stage.Title)
		stage.Goal = strings.TrimSpace(stage.Goal)
		if !taskLifecycleStageIDPattern.MatchString(stage.ID) || strings.HasPrefix(stage.ID, "system_") || stage.Title == "" {
			return nil, ErrInvalidTaskLifecyclePlan
		}
		if _, exists := seen[stage.ID]; exists {
			return nil, ErrInvalidTaskLifecyclePlan
		}
		seen[stage.ID] = struct{}{}
		normalized = append(normalized, stage)
	}
	return normalized, nil
}

func replanTaskLifecycle(current model.TaskLifecycle, executionID string, plan []model.TaskLifecyclePlanStage, serverStages []model.TaskLifecycleStage) (model.TaskLifecycle, bool, error) {
	immutableCount := 0
	if current.Version == model.TaskLifecycleVersion {
		for _, stage := range current.Stages {
			if stage.Source != model.TaskLifecycleSourceAgent || stage.Kind != model.TaskLifecycleKindWork {
				break
			}
			immutable := stage.State == model.TaskLifecycleStateComplete
			if current.ExecutionID == executionID && stage.State == model.TaskLifecycleStateActive {
				immutable = true
			}
			if !immutable {
				break
			}
			immutableCount++
		}
	}
	if immutableCount > len(plan) {
		return model.TaskLifecycle{}, false, ErrTaskLifecycleImmutablePrefix
	}
	nextStages := make([]model.TaskLifecycleStage, 0, len(plan))
	for i, declaration := range plan {
		if i < immutableCount {
			old := current.Stages[i]
			if old.ID != declaration.ID || old.Title != declaration.Title || old.Goal != declaration.Goal {
				return model.TaskLifecycle{}, false, ErrTaskLifecycleImmutablePrefix
			}
			nextStages = append(nextStages, old)
			continue
		}
		nextStages = append(nextStages, model.TaskLifecycleStage{
			ID: declaration.ID, Title: declaration.Title, Goal: declaration.Goal,
			Source: model.TaskLifecycleSourceAgent, Kind: model.TaskLifecycleKindWork,
			State: model.TaskLifecycleStatePending,
		})
	}
	for _, serverStage := range serverStages {
		if current.ExecutionID == executionID {
			for _, old := range current.Stages {
				if old.ID == serverStage.ID && old.Source == model.TaskLifecycleSourceServer && old.Kind == serverStage.Kind {
					serverStage = old
					break
				}
			}
		}
		nextStages = append(nextStages, serverStage)
	}
	next := model.TaskLifecycle{Version: model.TaskLifecycleVersion, Revision: current.Revision, ExecutionID: executionID, UpdatedAt: current.UpdatedAt, Stages: nextStages}
	return next, !equalTaskLifecyclePlan(current, next), nil
}

func taskServerLifecycleStages(ctx context.Context, repo repository.Repository, task *model.Task) ([]model.TaskLifecycleStage, error) {
	if task == nil || task.Type != model.PlatformArticle {
		return nil, nil
	}
	project, err := repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("load lifecycle publication project: %w", err)
	}
	if project.GetWechatPublishMode() == model.WechatPublishModeDisabled {
		return nil, nil
	}
	return []model.TaskLifecycleStage{
		{
			ID: "system_draft", Title: "创建公众号草稿", Source: model.TaskLifecycleSourceServer,
			Kind: model.TaskLifecycleKindDraft, State: model.TaskLifecycleStatePending,
		},
		{
			ID: "system_publication", Title: "正式发布", Source: model.TaskLifecycleSourceServer,
			Kind: model.TaskLifecycleKindPublication, State: model.TaskLifecycleStatePending,
		},
	}, nil
}

func equalTaskLifecyclePlan(left, right model.TaskLifecycle) bool {
	if left.Version != right.Version || left.ExecutionID != right.ExecutionID || len(left.Stages) != len(right.Stages) {
		return false
	}
	for i := range left.Stages {
		if left.Stages[i] != right.Stages[i] {
			return false
		}
	}
	return true
}

func advanceTaskLifecycle(current model.TaskLifecycle, stageID, state, description string, now time.Time) (model.TaskLifecycle, bool, error) {
	next := cloneTaskLifecycle(current)
	index := -1
	for i := range next.Stages {
		if next.Stages[i].ID == stageID && next.Stages[i].Source == model.TaskLifecycleSourceAgent && next.Stages[i].Kind == model.TaskLifecycleKindWork {
			index = i
			break
		}
	}
	if index < 0 {
		return model.TaskLifecycle{}, false, ErrTaskLifecycleUnknownStage
	}
	stage := &next.Stages[index]
	if stage.State == state && stage.LatestUpdate == description {
		return current, false, nil
	}
	if state == model.TaskLifecycleStateActive {
		if stage.State != model.TaskLifecycleStatePending && stage.State != model.TaskLifecycleStateActive {
			return model.TaskLifecycle{}, false, ErrTaskLifecycleOutOfOrder
		}
		for i := range next.Stages {
			candidate := next.Stages[i]
			if candidate.Source != model.TaskLifecycleSourceAgent || candidate.Kind != model.TaskLifecycleKindWork {
				continue
			}
			if i < index && candidate.State != model.TaskLifecycleStateComplete {
				return model.TaskLifecycle{}, false, ErrTaskLifecycleOutOfOrder
			}
			if i != index && candidate.State == model.TaskLifecycleStateActive {
				return model.TaskLifecycle{}, false, ErrTaskLifecycleOutOfOrder
			}
		}
		if stage.State == model.TaskLifecycleStatePending {
			stage.State = model.TaskLifecycleStateActive
			stage.StartedAt = &now
		}
		stage.LatestUpdate = description
		return next, true, nil
	}
	if stage.State == model.TaskLifecycleStateComplete && stage.LatestUpdate == description {
		return current, false, nil
	}
	if stage.State != model.TaskLifecycleStateActive {
		return model.TaskLifecycle{}, false, ErrTaskLifecycleOutOfOrder
	}
	stage.State = model.TaskLifecycleStateComplete
	stage.CompletedAt = &now
	if description != "" {
		stage.LatestUpdate = description
	}
	return next, true, nil
}

func lockCurrentLifecycleExecution(ctx context.Context, tx repository.Repository, taskID, executionID string, requireActive bool) error {
	if strings.TrimSpace(executionID) == "" {
		return ErrStaleTaskExecution
	}
	if requireActive {
		matched, err := tx.TaskExecutions().LockActiveForProgress(ctx, executionID, taskID)
		if err != nil {
			return fmt.Errorf("lock lifecycle execution: %w", err)
		}
		if !matched {
			return ErrStaleTaskExecution
		}
		return nil
	}
	execution, err := tx.TaskExecutions().FindByIDForUpdate(ctx, executionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrStaleTaskExecution
		}
		return fmt.Errorf("lock lifecycle execution: %w", err)
	}
	if execution.TaskID != taskID {
		return ErrStaleTaskExecution
	}
	return nil
}

func cloneTaskLifecycle(lifecycle model.TaskLifecycle) model.TaskLifecycle {
	clone := lifecycle
	clone.Stages = append([]model.TaskLifecycleStage(nil), lifecycle.Stages...)
	return clone
}

func (s *TaskService) publishLifecycle(ctx context.Context, taskID string, lifecycle model.TaskLifecycle) {
	if s.pubsub != nil && lifecycle.Revision > 0 {
		s.pubsub.PublishLifecycle(ctx, taskID, lifecycle)
	}
}
