package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AssistantInteractionInput struct {
	ModeID    string `json:"mode_id"`
	ProjectID string `json:"project_id,omitempty"`
	ContentID string `json:"content_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Intent    string `json:"intent,omitempty"`
}

type RuntimeBindingView struct {
	InteractionID    string
	Generation       int64
	RuntimeStatus    string
	RuntimeID        string
	LogicalSessionID string
	runtime          Runtime
}

type AssistantInteractionService struct {
	repo    repository.Repository
	factory RuntimeFactory
	taskSvc *TaskService
	clock   func() time.Time
	mu      sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewAssistantInteractionService(repo repository.Repository, runtime RuntimeFactory, clock func() time.Time) *AssistantInteractionService {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &AssistantInteractionService{repo: repo, factory: runtime, clock: clock, locks: map[string]*sync.Mutex{}}
}

func (s *AssistantInteractionService) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock := s.locks[id]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	s.locks[id] = lock
	return lock
}

func (s *AssistantInteractionService) CreateOrResume(ctx context.Context, userID string, in AssistantInteractionInput, idempotencyKey string) (InteractionSnapshot, error) {
	if s.repo == nil || s.repo.AssistantInteractions() == nil {
		return InteractionSnapshot{}, ErrInteractivePersistence
	}
	if userID == "" || in.ModeID == "" {
		return InteractionSnapshot{}, NewInteractionError(InteractionErrorMessageNotAccepted, 400, "当前对话信息不完整", "retry_message")
	}
	lock := s.lockFor(userID + ":" + in.ModeID + ":" + in.ProjectID + ":" + in.ContentID)
	lock.Lock()
	defer lock.Unlock()
	now := s.clock()
	interaction, err := s.repo.AssistantInteractions().FindByScopeForUpdate(ctx, userID, in.ModeID, in.ProjectID, in.ContentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		interaction = &model.AssistantInteraction{ID: uuid.NewString(), UserID: userID, ModeID: in.ModeID, ProjectID: in.ProjectID, ContentID: in.ContentID, Lifecycle: model.AssistantInteractionActive, Generation: 1, ExpiresAt: now.Add(24 * time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now}
		if createErr := s.repo.AssistantInteractions().Create(ctx, interaction); createErr != nil {
			return InteractionSnapshot{}, createErr
		}
	} else if err != nil {
		return InteractionSnapshot{}, err
	}
	_ = idempotencyKey
	if _, ensureErr := s.ensureRuntimeLocked(ctx, userID, interaction); ensureErr != nil {
		return InteractionSnapshot{}, ensureErr
	}
	return s.snapshotFromInteraction(interaction), nil
}

func (s *AssistantInteractionService) Snapshot(ctx context.Context, userID, interactionID string) (InteractionSnapshot, error) {
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InteractionSnapshot{}, NewInteractionError(InteractionErrorRestartRequired, 404, "当前对话已结束，已保留可恢复内容", "restore_draft", "restart")
		}
		return InteractionSnapshot{}, err
	}
	snapshot := s.snapshotFromInteraction(interaction)
	if drafts, draftErr := s.repo.AssistantInteractions().ListDrafts(ctx, userID, interactionID); draftErr == nil {
		snapshot.Drafts = make([]InteractionDraftView, 0, len(drafts))
		for _, draft := range drafts {
			snapshot.Drafts = append(snapshot.Drafts, InteractionDraftView{ID: draft.ID, ModeID: draft.ModeID, Revision: draft.Revision, Payload: draft.Payload, PayloadDigest: draft.PayloadDigest, Ready: draft.Ready, ConfirmationState: draft.ConfirmationState})
		}
	}
	return snapshot, nil
}

func (s *AssistantInteractionService) Events(ctx context.Context, userID, interactionID string, after uint64) ([]*model.AssistantInteractionEvent, error) {
	if _, err := s.Snapshot(ctx, userID, interactionID); err != nil {
		return nil, err
	}
	return s.repo.AssistantInteractions().ListEventsAfter(ctx, interactionID, after, 200)
}

func (s *AssistantInteractionService) EnsureRuntime(ctx context.Context, userID, interactionID string, expectedGeneration int64) (RuntimeBindingView, error) {
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return RuntimeBindingView{}, err
	}
	if expectedGeneration > 0 && interaction.Generation != expectedGeneration {
		return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	return s.ensureRuntimeLocked(ctx, userID, interaction)
}

func (s *AssistantInteractionService) ensureRuntimeLocked(ctx context.Context, _ string, interaction *model.AssistantInteraction) (RuntimeBindingView, error) {
	current, err := s.repo.AssistantInteractions().FindCurrentRuntime(ctx, interaction.ID, interaction.Generation)
	if err == nil {
		return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: current.Status, RuntimeID: current.RuntimeID, LogicalSessionID: current.LogicalSession}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return RuntimeBindingView{}, err
	}
	if s.factory == nil {
		return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: InteractionRuntimeStarting}, nil
	}
	logicalSessionID := uuid.NewString()
	generation := interaction.Generation
	runtime, startErr := s.factory.Start(ctx, logicalSessionID, "project-creator", "assistant-interaction", func(eventType string, data any) {
		s.AcceptRuntimeEvent(interaction.ID, generation, eventType, data)
	})
	if startErr != nil {
		_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionReconnecting, s.clock())
		return RuntimeBindingView{}, &InteractionError{Code: InteractionErrorRestartRequired, Status: 410, Message: "当前连接已结束，已保留可恢复内容", AvailableActions: []string{"restore_draft", "restart"}, cause: startErr}
	}
	now := s.clock()
	binding := &model.AssistantInteractionRuntime{ID: uuid.NewString(), InteractionID: interaction.ID, Generation: interaction.Generation, ServerInstance: "server", RuntimeID: uuid.NewString(), LogicalSession: logicalSessionID, Status: InteractionRuntimeRunning, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.AssistantInteractions().CreateRuntimeBinding(ctx, binding); err != nil {
		_ = runtime.Close()
		return RuntimeBindingView{}, err
	}
	return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: binding.Status, RuntimeID: binding.RuntimeID, LogicalSessionID: binding.LogicalSession, runtime: runtime}, nil
}

func (s *AssistantInteractionService) RunCommand(ctx context.Context, userID, interactionID string, expectedGeneration int64, op InteractionOperation, attemptID string, fn func(RuntimeBindingView) error) error {
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return err
	}
	if interaction.Generation != expectedGeneration {
		return NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	binding, err := s.ensureRuntimeLocked(ctx, userID, interaction)
	if err != nil {
		return err
	}
	if attemptID == "" {
		attemptID = NewInteractionAttemptID()
	}
	if fn == nil {
		return fmt.Errorf("interaction %s command %s has no operation", interactionID, op)
	}
	return fn(binding)
}

func (s *AssistantInteractionService) ReplaceRuntime(ctx context.Context, userID, interactionID string, failedGeneration int64, cause string) (RuntimeBindingView, error) {
	lock := s.lockFor(interactionID)
	lock.Lock()
	defer lock.Unlock()
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return RuntimeBindingView{}, err
	}
	if interaction.Generation != failedGeneration {
		return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	now := s.clock()
	_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interactionID, model.AssistantInteractionReconnecting, now)
	if current, findErr := s.repo.AssistantInteractions().FindCurrentRuntime(ctx, interactionID, failedGeneration); findErr == nil {
		_ = s.repo.AssistantInteractions().CloseRuntime(ctx, current.ID, now)
	}
	if ok, casErr := s.repo.AssistantInteractions().CompareAndSwapGeneration(ctx, interactionID, failedGeneration, failedGeneration+1, now); casErr != nil || !ok {
		if casErr != nil {
			return RuntimeBindingView{}, casErr
		}
		return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	interaction.Generation = failedGeneration + 1
	interaction.Lifecycle = model.AssistantInteractionReconnecting
	_ = cause
	binding, startErr := s.ensureRuntimeLocked(ctx, userID, interaction)
	if startErr != nil {
		return RuntimeBindingView{}, startErr
	}
	_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interactionID, model.AssistantInteractionActive, s.clock())
	return binding, nil
}

func (s *AssistantInteractionService) AcceptRuntimeEvent(interactionID string, generation int64, eventType string, data any) bool {
	interaction, err := s.repo.AssistantInteractions().FindByID(context.Background(), interactionID)
	if err != nil || interaction.Generation != generation {
		return false
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return false
	}
	sequence, sequenceErr := s.repo.AssistantInteractions().NextEventSequence(context.Background(), interactionID)
	if sequenceErr != nil {
		return false
	}
	if err := s.repo.AssistantInteractions().CreateEvent(context.Background(), &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: interactionID, Sequence: sequence, Type: eventType, Data: encoded, CreatedAt: s.clock()}); err != nil {
		return false
	}
	return true
}

func (s *AssistantInteractionService) RuntimeBindingForTest(ctx context.Context, userID, interactionID string, generation int64) (RuntimeBindingView, error) {
	return s.EnsureRuntime(ctx, userID, interactionID, generation)
}

func (s *AssistantInteractionService) snapshotFromInteraction(value *model.AssistantInteraction) InteractionSnapshot {
	actions := []string{"send_message", "restart"}
	if value.Lifecycle == model.AssistantInteractionNeedsConfirmation {
		actions = []string{"confirm_draft", "continue_editing", "restart"}
	}
	if value.Lifecycle == model.AssistantInteractionCompleted {
		actions = []string{"view_task", "restart"}
	}
	return InteractionSnapshot{ID: value.ID, ModeID: value.ModeID, ProjectID: value.ProjectID, ContentID: value.ContentID, Lifecycle: value.Lifecycle, Generation: value.Generation, RuntimeStatus: InteractionRuntimeRunning, ExpiresAt: value.ExpiresAt.UTC().Format(time.RFC3339Nano), LastActivityAt: value.LastActivityAt.UTC().Format(time.RFC3339Nano), AvailableActions: actions}
}
