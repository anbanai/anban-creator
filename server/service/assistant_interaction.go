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
	ModeID              string `json:"mode_id"`
	ProjectID           string `json:"project_id,omitempty"`
	ContentID           string `json:"content_id,omitempty"`
	ResumeInteractionID string `json:"resume_interaction_id,omitempty"`
	AgentID             string `json:"agent_id,omitempty"`
	Intent              string `json:"intent,omitempty"`
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
	modeSessions map[string]map[interactionSessionScope]string
	capabilities map[string]interactionRuntimeCapability
	liveEvents   map[string][]interactionLiveEvent
	metrics      interactionMetrics
}

type interactionSessionScope struct {
	ModeID    string
	ProjectID string
	ContentID string
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
		runtimes: map[string]Runtime{}, modeSessions: map[string]map[interactionSessionScope]string{}, capabilities: map[string]interactionRuntimeCapability{}, liveEvents: map[string][]interactionLiveEvent{},
	}
}

// Metrics returns a process-local diagnostic snapshot. Durable interaction
// state remains the recovery authority; these counters are observability only.
func (s *AssistantInteractionService) Metrics() InteractionMetricsSnapshot {
	if s == nil {
		return InteractionMetricsSnapshot{}
	}
	return s.metrics.snapshot()
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
	switch in.ModeID {
	case AssistantModeProfile, AssistantModeCreation, AssistantModeReview:
	default:
		return InteractionSnapshot{}, NewInteractionError(InteractionErrorMessageNotAccepted, 400, "当前模式不受支持，请重新选择", "restart")
	}
	normalizedIdempotencyKey := strings.TrimSpace(idempotencyKey)
	if normalizedIdempotencyKey != "" && len(normalizedIdempotencyKey) > 128 {
		return InteractionSnapshot{}, NewInteractionError(InteractionErrorMessageNotAccepted, 400, "请求标识过长，请重试", "retry_message")
	}
	createAttemptID := ""
	createPayloadDigest := ""
	if normalizedIdempotencyKey != "" && strings.TrimSpace(in.ResumeInteractionID) == "" {
		createAttemptID = interactionCreateAttemptID(normalizedIdempotencyKey)
		createPayloadDigest = interactionCreatePayloadDigest(in)
		if previous, findErr := s.repo.AssistantInteractions().FindCreateAttempt(ctx, userID, createAttemptID); findErr == nil {
			if previous.PayloadDigest != createPayloadDigest {
				return InteractionSnapshot{}, ErrInteractiveIdempotency
			}
			var receipt struct {
				InteractionID string `json:"interaction_id"`
			}
			if err := json.Unmarshal(previous.Result, &receipt); err != nil || strings.TrimSpace(receipt.InteractionID) == "" {
				return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, err)
			}
			// Replaying a create request must still repair a missing runtime
			// binding, while completed interactions remain terminal and simply
			// return their durable snapshot.
			return s.Snapshot(ctx, userID, receipt.InteractionID)
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, findErr)
		}
	}
	lockKey := userID + ":" + in.ProjectID + ":" + in.ContentID
	if strings.TrimSpace(in.ResumeInteractionID) != "" {
		lockKey = in.ResumeInteractionID
	}
	lock := s.lockFor(lockKey)
	lock.Lock()
	defer lock.Unlock()
	if _, _, contextErr := s.interactionContextRecords(ctx, userID, in.ProjectID, in.ContentID); contextErr != nil {
		return InteractionSnapshot{}, contextErr
	}
	now := s.clock()
	var interaction *model.AssistantInteraction
	var err error
	if strings.TrimSpace(in.ResumeInteractionID) != "" {
		interaction, err = s.repo.AssistantInteractions().FindByIDForUser(ctx, userID, in.ResumeInteractionID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return InteractionSnapshot{}, NewInteractionError(InteractionErrorRestartRequired, 404, "当前对话已结束，已保留可恢复内容", "restore_draft", "restart")
		}
		if err != nil {
			return InteractionSnapshot{}, err
		}
		if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
			return s.snapshotWithDrafts(ctx, userID, interaction)
		}
		if err := s.repo.AssistantInteractions().UpdateContext(ctx, interaction.ID, in.ModeID, in.ProjectID, in.ContentID, now); err != nil {
			return InteractionSnapshot{}, err
		}
		interaction.ModeID = in.ModeID
		interaction.ProjectID = in.ProjectID
		interaction.ContentID = in.ContentID
		interaction.UpdatedAt = now
	} else {
		interaction, _, err = s.repo.AssistantInteractions().FindOrCreateActive(ctx, &model.AssistantInteraction{ID: uuid.NewString(), UserID: userID, ModeID: in.ModeID, ProjectID: in.ProjectID, ContentID: in.ContentID, Lifecycle: model.AssistantInteractionActive, Generation: 1, ExpiresAt: now.Add(24 * time.Hour), LastActivityAt: now, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return InteractionSnapshot{}, err
		}
		interaction.ModeID = in.ModeID
		interaction.UpdatedAt = now
		if err := s.repo.AssistantInteractions().UpdateMode(ctx, interaction.ID, in.ModeID, now); err != nil {
			return InteractionSnapshot{}, err
		}
	}
	if createAttemptID != "" {
		receipt, marshalErr := json.Marshal(struct {
			InteractionID string `json:"interaction_id"`
		}{InteractionID: interaction.ID})
		if marshalErr != nil {
			return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, marshalErr)
		}
		attempt := &model.AssistantInteractionAttempt{
			ID:            uuid.NewString(),
			InteractionID: interaction.ID,
			UserID:        userID,
			AttemptID:     createAttemptID,
			Operation:     string(InteractionOperationCreate),
			Generation:    interaction.Generation,
			PayloadDigest: createPayloadDigest,
			Status:        model.AssistantInteractionAttemptCompleted,
			Result:        receipt,
			CreatedAt:     now,
			ExpiresAt:     now.Add(InteractionDraftRetention),
		}
		if createErr := s.repo.AssistantInteractions().CreateAttempt(ctx, attempt); createErr != nil {
			if previous, findErr := s.repo.AssistantInteractions().FindCreateAttempt(ctx, userID, createAttemptID); findErr == nil {
				if previous.PayloadDigest != createPayloadDigest {
					_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionAbandoned, now)
					return InteractionSnapshot{}, ErrInteractiveIdempotency
				}
				var replay struct {
					InteractionID string `json:"interaction_id"`
				}
				if unmarshalErr := json.Unmarshal(previous.Result, &replay); unmarshalErr != nil || replay.InteractionID == "" {
					return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, unmarshalErr)
				}
				return s.Snapshot(ctx, userID, replay.InteractionID)
			}
			return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, createErr)
		}
	}
	if _, err := s.ensureRuntimeLocked(ctx, userID, interaction); err != nil {
		return InteractionSnapshot{}, err
	}
	return s.snapshotWithDrafts(ctx, userID, interaction)
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
	// A ready draft is a durable confirmation boundary. It must remain
	// readable even when the runtime binding is stale or cannot be replaced;
	// confirmation is handled entirely by CommitInteraction. This also repairs
	// the small window where the draft transaction committed but the ready event
	// or lifecycle update did not.
	if drafts, draftErr := s.repo.AssistantInteractions().ListDrafts(ctx, userID, interaction.ID); draftErr == nil {
		for _, draft := range drafts {
			if draft != nil && draft.Ready && draft.ConfirmationState != model.AssistantInteractionDraftConsumed {
				if interaction.Lifecycle != model.AssistantInteractionNeedsConfirmation {
					// Derive this terminal-for-runtime state from the durable draft
					// for this response. Avoid an unconditional lifecycle write here:
					// a concurrent commit may have completed the interaction after the
					// draft query, and must not be overwritten by snapshot recovery.
					interaction.Lifecycle = model.AssistantInteractionNeedsConfirmation
				}
				return s.snapshotWithDrafts(ctx, userID, interaction)
			}
		}
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
	if current, runtimeErr := s.repo.AssistantInteractions().FindCurrentRuntime(ctx, interaction.ID, interaction.Generation); runtimeErr == nil {
		snapshot.RuntimeStatus = current.Status
		if current.Status != model.AssistantInteractionRuntimeClosed && s.runtimeFor(interaction.ID, interaction.Generation) == nil {
			// A durable binding without a process-local handle is the state left
			// by a Server restart. Keep the draft readable, but expose that the
			// runtime still needs replacement instead of claiming it is running.
			snapshot.RuntimeStatus = model.AssistantInteractionRuntimeFailed
		}
	} else if interaction.Lifecycle == model.AssistantInteractionCompleted || interaction.Lifecycle == model.AssistantInteractionAbandoned {
		snapshot.RuntimeStatus = model.AssistantInteractionRuntimeClosed
	} else if interaction.Lifecycle == model.AssistantInteractionReconnecting {
		snapshot.RuntimeStatus = model.AssistantInteractionRuntimeFailed
	} else {
		snapshot.RuntimeStatus = model.AssistantInteractionRuntimeStarting
	}
	if drafts, draftErr := s.repo.AssistantInteractions().ListDrafts(ctx, userID, interaction.ID); draftErr == nil {
		snapshot.Drafts = make([]InteractionDraftView, 0, len(drafts))
		for _, draft := range drafts {
			snapshot.Drafts = append(snapshot.Drafts, InteractionDraftView{ID: draft.ID, ModeID: draft.ModeID, Revision: draft.Revision, Payload: draft.Payload, PayloadDigest: draft.PayloadDigest, Ready: draft.Ready, ConfirmationState: draft.ConfirmationState})
		}
	}
	if receipt, receiptErr := s.repo.AssistantInteractions().FindLatestCommitAttempt(ctx, interaction.ID); receiptErr == nil {
		if receipt.ErrorCode == InteractionErrorCommitReconciliationRequired {
			// An ambiguous external side effect is durable evidence in its own
			// right. Surface the reconciliation action after a restart instead of
			// presenting a ready draft that invites an unsafe blind retry.
			snapshot.RecoveryErrorCode = receipt.ErrorCode
			snapshot.AvailableActions = []string{"view_task", "reconcile_commit"}
		} else if len(receipt.Result) > 0 {
			var result InteractionCommitResult
			if err := json.Unmarshal(receipt.Result, &result); err != nil {
				return InteractionSnapshot{}, errors.Join(ErrInteractivePersistence, err)
			}
			snapshot.CommitResult = &result
			// A successful receipt is the durable authority even if the process
			// stopped after writing it and before the lifecycle update committed.
			// Surface the terminal state immediately so refresh cannot offer a
			// misleading second confirmation.
			snapshot.Lifecycle = model.AssistantInteractionCompleted
			snapshot.AvailableActions = []string{"view_task", "restart"}
		}
	}
	if snapshot.CommitResult == nil && snapshot.RecoveryErrorCode == "" {
		if _, attemptErr := s.repo.AssistantInteractions().FindLatestCommandOutcomeUnknown(ctx, interaction.ID); attemptErr == nil {
			snapshot.RecoveryErrorCode = InteractionErrorCommandOutcomeUnknown
			snapshot.AvailableActions = []string{"retry_with_new_attempt", "restart"}
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
	events, err := s.repo.AssistantInteractions().ListEventsAfter(ctx, interactionID, current.Generation, after, 200)
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
	if terminalErr := interactionRuntimeTerminalError(interaction.Lifecycle); terminalErr != nil {
		return terminalErr
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
	if terminalErr := interactionRuntimeTerminalError(interaction.Lifecycle); terminalErr != nil {
		return RuntimeBindingView{}, terminalErr
	}
	return s.ensureRuntimeLocked(ctx, userID, interaction)
}

func (s *AssistantInteractionService) ensureRuntimeLocked(ctx context.Context, userID string, interaction *model.AssistantInteraction) (RuntimeBindingView, error) {
	if terminalErr := interactionRuntimeTerminalError(interaction.Lifecycle); terminalErr != nil {
		return RuntimeBindingView{}, terminalErr
	}
	current, err := s.repo.AssistantInteractions().FindCurrentRuntime(ctx, interaction.ID, interaction.Generation)
	if err == nil {
		if runtime := s.runtimeFor(interaction.ID, interaction.Generation); runtime != nil {
			capability, bound := s.runtimeCapability(current.TokenHash)
			// Project context may have committed before a failed replacement.
			// The bound credential retains the runtime's original mount identity.
			if !bound || capability.ProjectID != interaction.ProjectID {
				return s.replaceRuntimeLocked(ctx, userID, interaction, interaction.Generation, "project_binding_changed")
			}
			s.refreshCapabilityMode(interaction)
			logicalSessionID, sessionErr := s.ensureModeSessionLocked(ctx, interaction, runtime)
			if sessionErr != nil {
				return RuntimeBindingView{}, sessionErr
			}
			return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: current.Status, RuntimeID: current.RuntimeID, LogicalSessionID: logicalSessionID, runtime: runtime}, nil
		}
		// A binding loaded from the database without an in-process runtime is a
		// stale binding left by a Server restart. Replace it before accepting a
		// command so an old logical session can never be used as authority.
		now := s.clock()
		_ = s.repo.AssistantInteractions().CloseRuntime(ctx, current.ID, now)
		s.clearModeSessions(interaction.ID, interaction.Generation)
		if ok, casErr := s.repo.AssistantInteractions().CompareAndSwapGeneration(ctx, interaction.ID, interaction.Generation, interaction.Generation+1, now); casErr != nil || !ok {
			if casErr != nil {
				return RuntimeBindingView{}, casErr
			}
			return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
		}
		s.metrics.observeGenerationReplacement()
		s.metrics.observeRecovery("server_restart", "replaced")
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
	initialContext, contextErr := s.interactionInitialContext(ctx, interaction)
	if contextErr != nil {
		return RuntimeBindingView{}, contextErr
	}
	logicalSessionID := uuid.NewString()
	generation := interaction.Generation
	runtimeToken, tokenErr := randomToken()
	if tokenErr != nil {
		return RuntimeBindingView{}, tokenErr
	}
	tokenHash := confirmationHash(runtimeToken)
	s.runtimeMu.Lock()
	if s.capabilities == nil {
		s.capabilities = map[string]interactionRuntimeCapability{}
	}
	s.capabilities[tokenHash] = interactionRuntimeCapability{UserID: interaction.UserID, InteractionID: interaction.ID, ModeID: interaction.ModeID, ProjectID: interaction.ProjectID, Generation: generation, ExpiresAt: interaction.ExpiresAt}
	s.runtimeMu.Unlock()
	removeCapability := func() {
		s.runtimeMu.Lock()
		delete(s.capabilities, tokenHash)
		s.runtimeMu.Unlock()
	}
	runtime, startErr := s.startRuntime(ctx, interaction, logicalSessionID, runtimeToken, func(eventType string, data any) {
		s.acceptRuntimeEventForToken(interaction.ID, generation, tokenHash, eventType, data)
	})
	if startErr != nil {
		removeCapability()
		s.metrics.observeRecovery("runtime_start_failed", "restart_required")
		_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionReconnecting, s.clock())
		return RuntimeBindingView{}, &InteractionError{Code: InteractionErrorRestartRequired, Status: 410, Message: "当前连接已结束，已保留可恢复内容", AvailableActions: []string{"restore_draft", "restart"}, cause: startErr}
	}
	// A freshly started Bridge has no logical session until the Server admits
	// this interaction generation. Keep the handshake interaction-scoped so a
	// stale runtime cannot initialize a session under the new generation.
	s.runtimeMu.Lock()
	if s.modeSessions == nil {
		s.modeSessions = map[string]map[interactionSessionScope]string{}
	}
	scope := interactionSessionScope{interaction.ModeID, interaction.ProjectID, interaction.ContentID}
	s.modeSessions[runtimeKey(interaction.ID, interaction.Generation)] = map[interactionSessionScope]string{scope: logicalSessionID}
	s.runtimeMu.Unlock()
	if err := runtime.Command(ctx, "session/start", map[string]any{
		"sessionId":      logicalSessionID,
		"presetId":       agentPackIDForInteraction(interaction.ModeID),
		"initialContext": initialContext,
		"interaction_id": interaction.ID,
		"generation":     interaction.Generation,
	}); err != nil {
		removeCapability()
		s.metrics.observeRecovery("runtime_handshake_failed", "restart_required")
		s.clearModeSessions(interaction.ID, interaction.Generation)
		_ = runtime.Close()
		_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionReconnecting, s.clock())
		return RuntimeBindingView{}, &InteractionError{Code: InteractionErrorRestartRequired, Status: 410, Message: "当前连接已结束，已保留可恢复内容", AvailableActions: []string{"restore_draft", "restart"}, cause: err}
	}
	now := s.clock()
	binding := &model.AssistantInteractionRuntime{ID: uuid.NewString(), InteractionID: interaction.ID, Generation: interaction.Generation, ServerInstance: "server", RuntimeID: uuid.NewString(), LogicalSession: logicalSessionID, TokenHash: confirmationHash(runtimeToken), Status: InteractionRuntimeRunning, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.AssistantInteractions().CreateRuntimeBinding(ctx, binding); err != nil {
		removeCapability()
		_ = runtime.Close()
		return RuntimeBindingView{}, err
	}
	s.runtimeMu.Lock()
	s.runtimes[runtimeKey(interaction.ID, interaction.Generation)] = runtime
	if s.modeSessions == nil {
		s.modeSessions = map[string]map[interactionSessionScope]string{}
	}
	s.modeSessions[runtimeKey(interaction.ID, interaction.Generation)] = map[interactionSessionScope]string{scope: logicalSessionID}
	s.capabilities[binding.TokenHash] = interactionRuntimeCapability{UserID: interaction.UserID, InteractionID: interaction.ID, ModeID: interaction.ModeID, ProjectID: interaction.ProjectID, Generation: interaction.Generation, ExpiresAt: interaction.ExpiresAt}
	s.runtimeMu.Unlock()
	return RuntimeBindingView{InteractionID: interaction.ID, Generation: interaction.Generation, RuntimeStatus: binding.Status, RuntimeID: binding.RuntimeID, LogicalSessionID: binding.LogicalSession, runtime: runtime}, nil
}

func agentPackIDForInteraction(modeID string) string {
	switch modeID {
	case AssistantModeCreation:
		return "creation-planner"
	case AssistantModeReview:
		return "postmortem-review"
	default:
		return "project-creator"
	}
}

func runtimeKey(interactionID string, generation int64) string {
	return interactionID + ":" + fmt.Sprint(generation)
}

func (s *AssistantInteractionService) runtimeFor(interactionID string, generation int64) Runtime {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	return s.runtimes[runtimeKey(interactionID, generation)]
}

func (s *AssistantInteractionService) runtimeCapability(tokenHash string) (interactionRuntimeCapability, bool) {
	s.runtimeMu.RLock()
	defer s.runtimeMu.RUnlock()
	capability, bound := s.capabilities[tokenHash]
	return capability, bound
}

func (s *AssistantInteractionService) clearModeSessions(interactionID string, generation int64) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	delete(s.modeSessions, runtimeKey(interactionID, generation))
}

// ensureModeSessionLocked gives each Assistant mode and selected work its own
// logical DSH session while sharing the interaction's project runtime.
// This mapping is process-local; a Server restart replaces the generation
// before any command can reuse a stale logical session.
func (s *AssistantInteractionService) ensureModeSessionLocked(ctx context.Context, interaction *model.AssistantInteraction, runtime Runtime) (string, error) {
	key := runtimeKey(interaction.ID, interaction.Generation)
	scope := interactionSessionScope{interaction.ModeID, interaction.ProjectID, interaction.ContentID}
	s.runtimeMu.RLock()
	modeSessions := s.modeSessions[key]
	logicalSessionID := ""
	if modeSessions != nil {
		logicalSessionID = modeSessions[scope]
	}
	s.runtimeMu.RUnlock()
	if logicalSessionID != "" {
		return logicalSessionID, nil
	}
	initialContext, err := s.interactionInitialContext(ctx, interaction)
	if err != nil {
		return "", err
	}
	logicalSessionID = uuid.NewString()
	if err := runtime.Command(ctx, "session/start", map[string]any{
		"sessionId":      logicalSessionID,
		"presetId":       agentPackIDForInteraction(interaction.ModeID),
		"initialContext": initialContext,
		"interaction_id": interaction.ID,
		"generation":     interaction.Generation,
	}); err != nil {
		s.metrics.observeRecovery("mode_session_start_failed", "restart_required")
		return "", &InteractionError{Code: InteractionErrorRestartRequired, Status: 410, Message: "当前连接已结束，已保留可恢复内容", AvailableActions: []string{"restore_draft", "restart"}, cause: err}
	}
	s.runtimeMu.Lock()
	if s.modeSessions == nil {
		s.modeSessions = map[string]map[interactionSessionScope]string{}
	}
	if s.modeSessions[key] == nil {
		s.modeSessions[key] = map[interactionSessionScope]string{}
	}
	if existing := s.modeSessions[key][scope]; existing != "" {
		logicalSessionID = existing
	} else {
		s.modeSessions[key][scope] = logicalSessionID
	}
	s.runtimeMu.Unlock()
	return logicalSessionID, nil
}

func (s *AssistantInteractionService) refreshCapabilityMode(interaction *model.AssistantInteraction) {
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	for tokenHash, capability := range s.capabilities {
		if capability.InteractionID == interaction.ID && capability.Generation == interaction.Generation {
			capability.ModeID = interaction.ModeID
			s.capabilities[tokenHash] = capability
		}
	}
}

func interactionRuntimeTerminalError(lifecycle string) *InteractionError {
	switch lifecycle {
	case model.AssistantInteractionCompleted:
		return NewInteractionError(InteractionErrorRestartRequired, 409, "当前对话已完成，请查看已创建的任务", "view_task", "restart")
	case model.AssistantInteractionAbandoned:
		return NewInteractionError(InteractionErrorRestartRequired, 410, "当前对话已结束，已保留可恢复内容", "restore_draft", "restart")
	default:
		return nil
	}
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
	if lookupErr != nil || interaction.UserID != capability.UserID || interaction.ProjectID != capability.ProjectID || interaction.Generation != capability.Generation || interaction.Lifecycle == model.AssistantInteractionAbandoned || interaction.Lifecycle == model.AssistantInteractionCompleted {
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
		return factory.StartSession(ctx, InteractiveRuntimeSpec{SessionID: sessionID, WorkspaceID: interaction.ID, AgentPackID: agentPackID, PresetIDs: []string{"project-creator", "creation-planner", "postmortem-review"}, RuntimeToken: token, InteractionID: interaction.ID, Generation: interaction.Generation, ProjectID: interaction.ProjectID}, emit)
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
	return s.replaceRuntimeLocked(ctx, userID, interaction, failedGeneration, cause)
}

func (s *AssistantInteractionService) replaceRuntimeLocked(ctx context.Context, userID string, interaction *model.AssistantInteraction, failedGeneration int64, cause string) (RuntimeBindingView, error) {
	if interaction == nil {
		return RuntimeBindingView{}, ErrInteractivePersistence
	}
	if terminalErr := interactionRuntimeTerminalError(interaction.Lifecycle); terminalErr != nil {
		return RuntimeBindingView{}, terminalErr
	}
	if interaction.Generation != failedGeneration {
		return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	now := s.clock()
	_ = s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionReconnecting, now)
	if current, findErr := s.repo.AssistantInteractions().FindCurrentRuntime(ctx, interaction.ID, failedGeneration); findErr == nil {
		_ = s.repo.AssistantInteractions().CloseRuntime(ctx, current.ID, now)
		runtime := s.runtimeFor(interaction.ID, failedGeneration)
		s.clearModeSessions(interaction.ID, failedGeneration)
		s.runtimeMu.Lock()
		delete(s.runtimes, runtimeKey(interaction.ID, failedGeneration))
		delete(s.capabilities, current.TokenHash)
		s.runtimeMu.Unlock()
		if runtime != nil {
			_ = runtime.Close()
		}
	}
	if ok, casErr := s.repo.AssistantInteractions().CompareAndSwapGeneration(ctx, interaction.ID, failedGeneration, failedGeneration+1, now); casErr != nil || !ok {
		if casErr != nil {
			return RuntimeBindingView{}, casErr
		}
		return RuntimeBindingView{}, NewInteractionError(InteractionErrorGenerationChanged, 409, "对话状态已更新，请加载最新内容", "reload_snapshot")
	}
	s.metrics.observeGenerationReplacement()
	s.metrics.observeRecovery(cause, "replaced")
	interaction.Generation = failedGeneration + 1
	interaction.Lifecycle = model.AssistantInteractionReconnecting
	_ = cause
	binding, startErr := s.ensureRuntimeLocked(ctx, userID, interaction)
	if startErr != nil {
		return RuntimeBindingView{}, startErr
	}
	if err := s.repo.AssistantInteractions().UpdateLifecycle(ctx, interaction.ID, model.AssistantInteractionActive, s.clock()); err != nil {
		return RuntimeBindingView{}, err
	}
	interaction.Lifecycle = model.AssistantInteractionActive
	return binding, nil
}

func (s *AssistantInteractionService) AcceptRuntimeEvent(interactionID string, generation int64, eventType string, data any) bool {
	binding, err := s.repo.AssistantInteractions().FindCurrentRuntime(context.Background(), interactionID, generation)
	if err != nil {
		return false
	}
	return s.acceptRuntimeEventForToken(interactionID, generation, binding.TokenHash, eventType, data)
}

func (s *AssistantInteractionService) acceptRuntimeEventForToken(interactionID string, generation int64, tokenHash, eventType string, data any) bool {
	capability, bound := s.runtimeCapability(tokenHash)
	if !bound || capability.InteractionID != interactionID || capability.Generation != generation {
		return false
	}
	data = s.annotateRuntimeEventMode(interactionID, generation, data)
	durable, err := durableInteractionEventData(eventType, data)
	if err != nil {
		return false
	}
	now := s.clock()
	durableEvent := &model.AssistantInteractionEvent{ID: uuid.NewString(), InteractionID: interactionID, Type: eventType, Data: durable, CreatedAt: now}
	accepted, appendErr := s.repo.AssistantInteractions().AppendEvent(context.Background(), interactionID, generation, capability.ProjectID, durableEvent)
	if appendErr != nil || !accepted {
		return false
	}
	s.cacheLiveEvent(interactionID, generation, durableEvent, data, now)
	return true
}

func (s *AssistantInteractionService) annotateRuntimeEventMode(interactionID string, generation int64, data any) any {
	encoded, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil || raw == nil {
		return data
	}
	if _, exists := raw["mode_id"]; exists {
		return raw
	}
	sessionID, _ := raw["session_id"].(string)
	if sessionID == "" {
		sessionID, _ = raw["sessionId"].(string)
	}
	if sessionID == "" {
		return raw
	}
	s.runtimeMu.RLock()
	for scope, logicalSessionID := range s.modeSessions[runtimeKey(interactionID, generation)] {
		if logicalSessionID == sessionID {
			raw["mode_id"] = scope.ModeID
			break
		}
	}
	s.runtimeMu.RUnlock()
	return raw
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
	s.liveEvents[interactionID] = append(s.liveEvents[interactionID], interactionLiveEvent{generation: generation, event: &model.AssistantInteractionEvent{ID: durableEvent.ID, InteractionID: interactionID, Generation: generation, Sequence: durableEvent.Sequence, Type: durableEvent.Type, Data: live, CreatedAt: now}, expiresAt: now.Add(InteractionEventRetention)})
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
	if value, ok := raw["mode_id"]; ok {
		metadata["mode_id"] = value
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
