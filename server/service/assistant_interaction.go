package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	repo         repository.Repository
	factory      RuntimeFactory
	taskSvc      *TaskService
	clock        func() time.Time
	mu           sync.Mutex
	locks        map[string]*sync.Mutex
	runtimeMu    sync.RWMutex
	runtimes     map[string]Runtime
	capabilities map[string]interactionRuntimeCapability
	liveEvents   map[string][]interactionLiveEvent
}

type interactionLiveEvent struct {
	generation int64
	event      *model.AssistantInteractionEvent
	expiresAt  time.Time
}

type interactionRuntimeCapability struct {
	UserID        string
	InteractionID string
	ModeID        string
	ProjectID     string
	Generation    int64
	ExpiresAt     time.Time
}

type InteractionCleanupResult struct {
	Drafts   int64
	Events   int64
	Attempts int64
}

func NewAssistantInteractionService(repo repository.Repository, runtime RuntimeFactory, clock func() time.Time) *AssistantInteractionService {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &AssistantInteractionService{
		repo: repo, factory: runtime, clock: clock, locks: map[string]*sync.Mutex{},
		runtimes: map[string]Runtime{}, capabilities: map[string]interactionRuntimeCapability{}, liveEvents: map[string][]interactionLiveEvent{},
	}
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
	lock := s.lockFor(userID + ":" + in.ProjectID + ":" + in.ContentID)
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
	interaction.ModeID = in.ModeID
	interaction.UpdatedAt = now
	if err := s.repo.AssistantInteractions().UpdateMode(ctx, interaction.ID, in.ModeID, now); err != nil {
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
	// A completed interaction is already backed by its durable commit receipt;
	// reopening a runtime here could create a new execution after confirmation.
	// Abandoned interactions are terminal for the same reason.
	if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
		return s.snapshotWithDrafts(ctx, userID, interaction)
	}
	// A GET is the first request after a browser or Server restart. Ensure it
	// fences any durable binding whose in-process runtime handle disappeared so
	// the client never resumes an old logical session with the old generation.
	if _, ensureErr := s.EnsureRuntime(ctx, userID, interactionID, interaction.Generation); ensureErr != nil {
		current, lookupErr := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
		if lookupErr == nil {
			if structured := NormalizeInteractionError(ensureErr); structured != nil {
				fallback, snapshotErr := s.snapshotWithDrafts(ctx, userID, current)
				if snapshotErr == nil {
					return InteractionSnapshot{}, structured.WithSnapshot(fallback)
				}
			}
		}
		return InteractionSnapshot{}, ensureErr
	}
	interaction, err = s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return InteractionSnapshot{}, err
	}
	return s.snapshotWithDrafts(ctx, userID, interaction)
}

func (s *AssistantInteractionService) snapshotWithDrafts(ctx context.Context, userID string, interaction *model.AssistantInteraction) (InteractionSnapshot, error) {
	snapshot := s.snapshotFromInteraction(interaction)
	if drafts, draftErr := s.repo.AssistantInteractions().ListDrafts(ctx, userID, interaction.ID); draftErr == nil {
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
	current, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return nil, err
	}
	events, err := s.repo.AssistantInteractions().ListEventsAfter(ctx, interactionID, after, 200)
	if err != nil {
		return nil, err
	}
	// Runtime messages are retained in memory for the active process so the
	// live stream remains useful, while the durable rows contain metadata only.
	s.runtimeMu.Lock()
	now := s.clock()
	live := s.liveEvents[interactionID]
	kept := live[:0]
	for _, item := range live {
		if item.expiresAt.After(now) {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		delete(s.liveEvents, interactionID)
	} else {
		s.liveEvents[interactionID] = kept
	}
	live = kept
	s.runtimeMu.Unlock()
	if len(live) == 0 || len(events) == 0 {
		return events, nil
	}
	bySequence := make(map[uint64]*model.AssistantInteractionEvent, len(live))
	for _, item := range live {
		if item.generation == current.Generation && item.event != nil {
			bySequence[item.event.Sequence] = item.event
		}
	}
	for i, event := range events {
		if replacement := bySequence[event.Sequence]; replacement != nil {
			events[i] = replacement
		}
	}
	return events, nil
}

func (s *AssistantInteractionService) Abandon(ctx context.Context, userID, interactionID string) error {
	interaction, err := s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, interactionID)
	if err != nil {
		return err
	}
	return s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionAbandoned, s.clock())
}

// CleanupExpired removes only durable interaction records whose explicit
// retention deadline has passed. Commit receipts use their own longer expiry
// in the attempts table, so a single cleanup pass preserves replay evidence.
func (s *AssistantInteractionService) CleanupExpired(ctx context.Context, before time.Time) (InteractionCleanupResult, error) {
	if s.repo == nil || s.repo.AssistantInteractions() == nil {
		return InteractionCleanupResult{}, ErrInteractivePersistence
	}
	retention := s.repo.AssistantInteractions()
	drafts, err := retention.ExpireDrafts(ctx, before)
	if err != nil {
		return InteractionCleanupResult{}, err
	}
	events, err := retention.ExpireEvents(ctx, before)
	if err != nil {
		return InteractionCleanupResult{}, err
	}
	attempts, err := retention.ExpireAttempts(ctx, before)
	if err != nil {
		return InteractionCleanupResult{}, err
	}
	return InteractionCleanupResult{Drafts: drafts, Events: events, Attempts: attempts}, nil
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
		if runtime := s.runtimeFor(interaction.ID, interaction.Generation); runtime != nil {
			return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: current.Status, RuntimeID: current.RuntimeID, LogicalSessionID: current.LogicalSession, runtime: runtime}, nil
		}
		// A binding loaded from the database without an in-process runtime is a
		// stale binding left by a Server restart. Replace it before accepting a
		// command so an old logical session can never be used as authority.
		now := s.clock()
		_ = s.repo.AssistantInteractions().CloseRuntime(ctx, current.ID, now)
		if ok, casErr := s.repo.AssistantInteractions().CompareAndSwapGeneration(ctx, interaction.ID, interaction.Generation, interaction.Generation+1, now); casErr != nil || !ok {
			if casErr != nil {
				return RuntimeBindingView{}, casErr
			}
			return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
		}
		interaction.Generation++
		_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionReconnecting, now)
		// The old binding has been fenced and the next generation must follow
		// the normal runtime creation path below.
		err = gorm.ErrRecordNotFound
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return RuntimeBindingView{}, err
	}
	if s.factory == nil {
		return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: InteractionRuntimeStarting}, nil
	}
	logicalSessionID := uuid.NewString()
	generation := interaction.Generation
	runtimeToken, tokenErr := randomToken()
	if tokenErr != nil {
		return RuntimeBindingView{}, tokenErr
	}
	runtime, startErr := s.startRuntime(ctx, interaction, logicalSessionID, runtimeToken, func(eventType string, data any) {
		s.AcceptRuntimeEvent(interaction.ID, generation, eventType, data)
	})
	if startErr != nil {
		_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionReconnecting, s.clock())
		return RuntimeBindingView{}, &InteractionError{Code: InteractionErrorRestartRequired, Status: 410, Message: "当前连接已结束，已保留可恢复内容", AvailableActions: []string{"restore_draft", "restart"}, cause: startErr}
	}
	now := s.clock()
	binding := &model.AssistantInteractionRuntime{ID: uuid.NewString(), InteractionID: interaction.ID, Generation: interaction.Generation, ServerInstance: "server", RuntimeID: uuid.NewString(), LogicalSession: logicalSessionID, TokenHash: confirmationHash(runtimeToken), Status: InteractionRuntimeRunning, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.AssistantInteractions().CreateRuntimeBinding(ctx, binding); err != nil {
		_ = runtime.Close()
		return RuntimeBindingView{}, err
	}
	s.runtimeMu.Lock()
	s.runtimes[runtimeKey(interaction.ID, interaction.Generation)] = runtime
	s.capabilities[binding.TokenHash] = interactionRuntimeCapability{UserID: interaction.UserID, InteractionID: interaction.ID, ModeID: interaction.ModeID, ProjectID: interaction.ProjectID, Generation: interaction.Generation, ExpiresAt: interaction.ExpiresAt}
	s.runtimeMu.Unlock()
	return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: binding.Status, RuntimeID: binding.RuntimeID, LogicalSessionID: binding.LogicalSession, runtime: runtime}, nil
}

func runtimeKey(interactionID string, generation int64) string {
	return interactionID + ":" + fmt.Sprint(generation)
}

func (s *AssistantInteractionService) runtimeFor(interactionID string, generation int64) Runtime {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.runtimes[runtimeKey(interactionID, generation)]
}

// ResolveRuntimeCapability authenticates the short-lived runtime bearer without
// exposing the bearer or any runtime identifier to MCP tools.
func (s *AssistantInteractionService) ResolveRuntimeCapability(token string) (userID, interactionID, modeID, projectID string, generation int64, expiresAt time.Time, err error) {
	hash := confirmationHash(strings.TrimSpace(token))
	if strings.TrimSpace(token) == "" {
		return "", "", "", "", 0, time.Time{}, ErrInteractiveToken
	}
	s.runtimeMu.RLock()
	capability, ok := s.capabilities[hash]
	s.runtimeMu.RUnlock()
	if !ok || s.clock().After(capability.ExpiresAt) {
		return "", "", "", "", 0, time.Time{}, ErrInteractiveToken
	}
	if s.repo == nil || s.repo.AssistantInteractions() == nil {
		return "", "", "", "", 0, time.Time{}, ErrInteractiveToken
	}
	interaction, lookupErr := s.repo.AssistantInteractions().FindByID(context.Background(), capability.InteractionID)
	if lookupErr != nil || interaction.UserID != capability.UserID || interaction.Generation != capability.Generation || interaction.Lifecycle == model.AssistantInteractionAbandoned {
		return "", "", "", "", 0, time.Time{}, ErrInteractiveToken
	}
	return capability.UserID, capability.InteractionID, capability.ModeID, capability.ProjectID, capability.Generation, capability.ExpiresAt, nil
}

func (s *AssistantInteractionService) startRuntime(ctx context.Context, interaction *model.AssistantInteraction, sessionID, token string, emit func(string, any)) (Runtime, error) {
	agentPackID := "project-creator"
	switch interaction.ModeID {
	case AssistantModeCreation:
		agentPackID = "creation-planner"
	case AssistantModeReview:
		agentPackID = "postmortem-review"
	}
	if factory, ok := s.factory.(RuntimeFactoryWithSession); ok {
		return factory.StartSession(ctx, InteractiveRuntimeSpec{SessionID: sessionID, AgentPackID: agentPackID, PresetIDs: []string{"project-creator", "creation-planner", "postmortem-review"}, RuntimeToken: token, InteractionID: interaction.ID, Generation: interaction.Generation, ProjectID: interaction.ProjectID}, emit)
	}
	return s.factory.Start(ctx, sessionID, agentPackID, token, emit)
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
		if runtime := s.runtimeFor(interactionID, failedGeneration); runtime != nil {
			_ = runtime.Close()
		}
		s.runtimeMu.Lock()
		delete(s.runtimes, runtimeKey(interactionID, failedGeneration))
		delete(s.capabilities, current.TokenHash)
		s.runtimeMu.Unlock()
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
	durable, err := durableInteractionEventData(eventType, data)
	if err != nil {
		return false
	}
	sequence, sequenceErr := s.repo.AssistantInteractions().NextEventSequence(context.Background(), interactionID)
	if sequenceErr != nil {
		return false
	}
	now := s.clock()
	durableEvent := &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: interactionID, Sequence: sequence, Type: eventType, Data: durable, CreatedAt: now}
	if err := s.repo.AssistantInteractions().CreateEvent(context.Background(), durableEvent); err != nil {
		return false
	}
	s.cacheLiveEvent(interactionID, generation, durableEvent, data, now)
	return true
}

// cacheLiveEvent keeps the process-local event rich enough for an active
// Studio stream while the durable row remains metadata-only. The sequence and
// event ID stay identical so Events can replace the redacted row in-place.
func (s *AssistantInteractionService) cacheLiveEvent(interactionID string, generation int64, durableEvent *model.AssistantInteractionEvent, data any, now time.Time) {
	if durableEvent == nil {
		return
	}
	live, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		return
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.liveEvents == nil {
		s.liveEvents = map[string][]interactionLiveEvent{}
	}
	s.liveEvents[interactionID] = append(s.liveEvents[interactionID], interactionLiveEvent{generation: generation, event: &model.AssistantInteractionEvent{ID: durableEvent.ID, InteractionID: interactionID, Sequence: durableEvent.Sequence, Type: durableEvent.Type, Data: live, CreatedAt: now}, expiresAt: now.Add(InteractionEventRetention)})
}

// durableInteractionEventData keeps the event chain useful for recovery and
// diagnosis without persisting runtime tokens, user prose, full drafts, or
// material URLs. The live process may still stream the original event.
func durableInteractionEventData(eventType string, data any) (json.RawMessage, error) {
	var raw map[string]any
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		raw = map[string]any{}
	}
	metadata := map[string]any{"event_type": eventType}
	if value, ok := raw["generation"]; ok {
		metadata["generation"] = value
	}
	switch eventType {
	case "assistant.message", "user.message":
		if text, ok := raw["text"].(string); ok {
			metadata["text_length"] = len([]rune(text))
		}
	case "question.opened", "question.created":
		for _, key := range []string{"call_id", "callId", "question_id", "questionId", "multi_select", "multiSelect", "option_count"} {
			if value, ok := raw[key]; ok {
				metadata[key] = value
			}
		}
	case "draft.updated", "draft.ready":
		for _, key := range []string{"draft_id", "revision", "mode_id", "ready", "durable", "payload_digest"} {
			if value, ok := raw[key]; ok {
				metadata[key] = value
			}
		}
	case "runtime.error", "tool.error", "session.error":
		for _, key := range []string{"error_code", "status", "retryable"} {
			if value, ok := raw[key]; ok {
				metadata[key] = value
			}
		}
	}
	return json.Marshal(metadata)
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
