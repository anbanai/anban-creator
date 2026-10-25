package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	AssistantModeProfile             = model.AssistantModeProfile
	AssistantModeCreation            = model.AssistantModeCreation
	AssistantModeReview              = model.AssistantModeReview
	assistantCheckpointSchemaVersion = "1.0"
	assistantCheckpointMaxBytes      = 128 << 10
)

var (
	ErrAssistantModeUnavailable          = errors.New("assistant mode is unavailable")
	ErrAssistantCheckpointInvalid        = errors.New("assistant checkpoint is invalid")
	ErrAssistantCreationInputsMissing    = errors.New("assistant creation request is missing required inputs")
	ErrAssistantCreationAssetUnavailable = errors.New("assistant creation request references an unavailable asset")
	ErrAssistantCheckpointNotFound       = errors.New("assistant checkpoint not found")
	ErrAssistantCheckpointConflict       = errors.New("assistant checkpoint scope does not match session")
	ErrAssistantWorkspaceBusy            = errors.New("assistant_workspace_busy")
	ErrAssistantWorkspaceModeMismatch    = errors.New("assistant workspace mode does not match request")
	ErrAssistantWorkspaceNotFound        = errors.New("assistant workspace not found")
	ErrAssistantWorkspaceExpired         = errors.New("assistant workspace expired")
)

type AssistantWorkspaceInput struct {
	ModeID       string `json:"mode_id"`
	ProjectID    string `json:"project_id,omitempty"`
	ContentID    string `json:"content_id,omitempty"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

type AssistantWorkspaceView struct {
	ID            string            `json:"id"`
	ModeID        string            `json:"mode_id"`
	ProjectID     string            `json:"project_id,omitempty"`
	ContentID     string            `json:"content_id,omitempty"`
	RuntimeID     string            `json:"runtime_id,omitempty"`
	RuntimeStatus string            `json:"runtime_status"`
	SessionID     string            `json:"session_id"`
	Sessions      map[string]string `json:"sessions"`
	ExpiresAt     time.Time         `json:"expires_at"`
}

type AssistantMode struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	Description    string `json:"description"`
	Available      bool   `json:"available"`
	DisabledReason string `json:"disabledReason,omitempty"`
}

type AssistantCheckpointSummary struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId,omitempty"`
	ContentID string    `json:"contentId,omitempty"`
	Summary   string    `json:"summary"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AssistantWork struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"createdAt"`
}

type AssistantProject struct {
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	Platform          string                   `json:"platform"`
	ProfileReady      bool                     `json:"profileReady"`
	ProfileRevision   int64                    `json:"profileRevision"`
	VideoEditDefaults *model.VideoEditDefaults `json:"video_edit_defaults,omitempty"`
	Works             []AssistantWork          `json:"works"`
}

type AssistantWorkPage struct {
	Items      []AssistantWork      `json:"items"`
	NextCursor *AssistantWorkCursor `json:"next_cursor,omitempty"`
	HasMore    bool                 `json:"has_more"`
}

type AssistantWorkCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

const (
	assistantWorksDefaultLimit = 25
	assistantWorksMaxLimit     = 100
)

type AssistantBootstrap struct {
	Modes         []AssistantMode    `json:"modes"`
	DefaultModeID string             `json:"defaultModeId"`
	Projects      []AssistantProject `json:"projects"`
}

func assistantWorkspaceSessions(workspace *model.AssistantWorkspace) map[string]string {
	return map[string]string{
		AssistantModeProfile:  strings.TrimSpace(workspace.ProfileSessionID),
		AssistantModeCreation: strings.TrimSpace(workspace.CreationSessionID),
		AssistantModeReview:   strings.TrimSpace(workspace.ReviewSessionID),
	}
}

func assistantWorkspaceSessionID(workspace *model.AssistantWorkspace, modeID string) string {
	if workspace == nil {
		return ""
	}
	return assistantWorkspaceSessions(workspace)[modeID]
}

func setAssistantWorkspaceSessionID(workspace *model.AssistantWorkspace, modeID, sessionID string) error {
	switch modeID {
	case AssistantModeProfile:
		workspace.ProfileSessionID = sessionID
	case AssistantModeCreation:
		workspace.CreationSessionID = sessionID
	case AssistantModeReview:
		workspace.ReviewSessionID = sessionID
	default:
		return ErrAssistantModeUnavailable
	}
	return nil
}

func (s *InteractiveService) assistantWorkspaceView(workspace *model.AssistantWorkspace) *AssistantWorkspaceView {
	sessions := assistantWorkspaceSessions(workspace)
	return &AssistantWorkspaceView{
		ID: workspace.ID, ModeID: workspace.ModeID, ProjectID: workspace.ProjectID, ContentID: workspace.ContentID,
		RuntimeID: workspace.RuntimeID, RuntimeStatus: workspace.RuntimeStatus,
		SessionID: sessions[workspace.ModeID], Sessions: sessions, ExpiresAt: workspace.ExpiresAt,
	}
}

func (s *InteractiveService) findAssistantWorkspace(ctx context.Context, userID, workspaceID string) (*model.AssistantWorkspace, error) {
	if s.repo == nil || s.repo.AssistantWorkspaces() == nil {
		return nil, ErrInteractivePersistence
	}
	workspace, err := s.repo.AssistantWorkspaces().FindByIDForUser(ctx, strings.TrimSpace(userID), strings.TrimSpace(workspaceID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAssistantWorkspaceNotFound
	}
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	if !workspace.ExpiresAt.After(time.Now().UTC()) {
		return nil, ErrAssistantWorkspaceExpired
	}
	if workspace.ClosedAt != nil {
		return nil, ErrAssistantWorkspaceNotFound
	}
	return workspace, nil
}

func (s *InteractiveService) workspaceSessionBusy(sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	s.mu.RLock()
	sess := s.sessions[sessionID]
	s.mu.RUnlock()
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.busy || sess.committing || sess.pendingCall != "" || sess.activeCommand != 0 || sess.Status == "waiting_answer"
}

func (s *InteractiveService) findLiveWorkspaceSession(workspaceID, modeID string) *InteractiveSession {
	s.mu.RLock()
	sessions := make([]*InteractiveSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()
	for _, sess := range sessions {
		sess.mu.Lock()
		live := !sess.Closed && sess.ExpiresAt.After(time.Now()) && sess.WorkspaceID == workspaceID && sess.ModeID == modeID
		sess.mu.Unlock()
		if live {
			return sess
		}
	}
	return nil
}

func (s *InteractiveService) bindAssistantWorkspaceSession(ctx context.Context, userID string, workspace *model.AssistantWorkspace, sess *InteractiveSession) (*model.AssistantWorkspace, error) {
	if workspace == nil || sess == nil || workspace.ID != sess.WorkspaceID || workspace.ModeID != sess.ModeID {
		return nil, ErrAssistantWorkspaceNotFound
	}
	workspace.ProjectID = sess.ProjectID
	workspace.ContentID = sess.ContentID
	sess.mu.Lock()
	expiresAt := sess.ExpiresAt.UTC()
	sess.mu.Unlock()
	bound, err := s.repo.AssistantWorkspaces().BindSession(ctx, userID, workspace.ID, workspace.ModeID, sess.ID, workspace.ProjectID, workspace.ContentID, expiresAt, time.Now().UTC())
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	if !bound {
		return nil, ErrAssistantWorkspaceBusy
	}
	if err := setAssistantWorkspaceSessionID(workspace, workspace.ModeID, sess.ID); err != nil {
		return nil, err
	}
	if expiresAt.After(workspace.ExpiresAt) {
		workspace.ExpiresAt = expiresAt
	}
	workspace.UpdatedAt = time.Now().UTC()
	s.setWorkspaceRuntimeActive(workspace.ID, sess.ID)
	return workspace, nil
}

func (s *InteractiveService) recoverUnboundAssistantWorkspace(ctx context.Context, userID string, workspace *model.AssistantWorkspace, input AssistantWorkspaceInput) (*model.AssistantWorkspace, error) {
	if workspace == nil || workspace.ClosedAt != nil || !workspace.ExpiresAt.After(time.Now().UTC()) {
		return nil, ErrAssistantWorkspaceNotFound
	}
	if workspace.RequestFingerprint != assistantWorkspaceRequestFingerprint(input) {
		return nil, ErrInteractiveIdempotency
	}
	if assistantWorkspaceSessionID(workspace, workspace.ModeID) != "" {
		return workspace, nil
	}
	sess := s.findLiveWorkspaceSession(workspace.ID, workspace.ModeID)
	if sess == nil {
		previous, err := s.repo.AssistantSessions().FindLatestByWorkspaceMode(ctx, userID, workspace.ID, workspace.ModeID)
		if err == nil {
			sessionID, recreateErr := s.recreateWorkspaceSession(ctx, userID, workspace, workspace.ModeID, workspace.ProjectID, workspace.ContentID, previous.ID)
			if recreateErr != nil {
				return nil, recreateErr
			}
			s.mu.RLock()
			sess = s.sessions[sessionID]
			s.mu.RUnlock()
			if sess == nil {
				return nil, ErrInteractivePersistence
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			sess, err = s.CreateAssistantSessionWithIdempotency(ctx, userID, AssistantSessionInput{
				ModeID: workspace.ModeID, ProjectID: input.ProjectID, ContentID: input.ContentID,
				CheckpointID: input.CheckpointID, WorkspaceID: workspace.ID, WorkspaceNew: true,
			}, workspace.ID+":session")
			if err != nil {
				return nil, err
			}
		} else {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
	}
	return s.bindAssistantWorkspaceSession(ctx, userID, workspace, sess)
}

func (s *InteractiveService) CreateAssistantWorkspace(ctx context.Context, userID string, input AssistantWorkspaceInput, idempotencyKey string) (*AssistantWorkspaceView, error) {
	modeID := strings.TrimSpace(input.ModeID)
	if modeID == "" {
		modeID = AssistantModeProfile
	}
	input.ModeID = modeID
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.ContentID = strings.TrimSpace(input.ContentID)
	input.CheckpointID = strings.TrimSpace(input.CheckpointID)
	key := strings.TrimSpace(idempotencyKey)
	if len([]byte(key)) > 200 {
		return nil, ErrInteractiveIdempotency
	}
	s.workspaceCreateMu.Lock()
	defer s.workspaceCreateMu.Unlock()
	requestFingerprint := assistantWorkspaceRequestFingerprint(input)
	var keyHash *string
	if key != "" {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(userID)+"\x00"+key)))
		keyHash = &hash
		if existing, lookupErr := s.repo.AssistantWorkspaces().FindByIdempotencyKeyHash(ctx, strings.TrimSpace(userID), hash); lookupErr == nil {
			if existing.RequestFingerprint != requestFingerprint || existing.ClosedAt != nil || !existing.ExpiresAt.After(time.Now().UTC()) {
				return nil, ErrInteractiveIdempotency
			}
			if assistantWorkspaceSessionID(existing, existing.ModeID) == "" {
				existing, lookupErr = s.recoverUnboundAssistantWorkspace(ctx, userID, existing, input)
				if lookupErr != nil {
					return nil, lookupErr
				}
			}
			return s.assistantWorkspaceView(existing), nil
		} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return nil, errors.Join(ErrInteractivePersistence, lookupErr)
		}
	}
	workspaceID := uuid.NewString()
	now := time.Now().UTC()
	s.mu.RLock()
	initialTTL := s.ttl
	if s.idleTimeout > 0 && s.idleTimeout < initialTTL {
		initialTTL = s.idleTimeout
	}
	s.mu.RUnlock()
	workspace := &model.AssistantWorkspace{
		ID: workspaceID, UserID: userID, ProjectID: input.ProjectID, ContentID: input.ContentID,
		ModeID: modeID, RuntimeID: workspaceID, RuntimeStatus: "starting", ExpiresAt: now.Add(initialTTL), CreatedAt: now, UpdatedAt: now,
		IdempotencyKeyHash: keyHash, RequestFingerprint: requestFingerprint,
	}
	if err := s.repo.AssistantWorkspaces().Create(ctx, workspace); err != nil {
		if keyHash != nil {
			if existing, lookupErr := s.repo.AssistantWorkspaces().FindByIdempotencyKeyHash(ctx, strings.TrimSpace(userID), *keyHash); lookupErr == nil {
				if existing.RequestFingerprint != requestFingerprint || existing.ClosedAt != nil || !existing.ExpiresAt.After(time.Now().UTC()) {
					return nil, ErrInteractiveIdempotency
				}
				if assistantWorkspaceSessionID(existing, existing.ModeID) == "" {
					existing, lookupErr = s.recoverUnboundAssistantWorkspace(ctx, userID, existing, input)
					if lookupErr != nil {
						return nil, lookupErr
					}
				}
				return s.assistantWorkspaceView(existing), nil
			}
		}
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	session, err := s.CreateAssistantSessionWithIdempotency(ctx, userID, AssistantSessionInput{
		ModeID: modeID, ProjectID: input.ProjectID, ContentID: input.ContentID, CheckpointID: input.CheckpointID, WorkspaceID: workspaceID, WorkspaceNew: true,
	}, workspaceID+":session")
	if err != nil {
		_ = s.repo.AssistantWorkspaces().Delete(ctx, userID, workspaceID, time.Now().UTC())
		return nil, err
	}
	workspace.ProjectID = session.ProjectID
	workspace.ContentID = session.ContentID
	workspace.ExpiresAt = session.ExpiresAt.UTC()
	workspace, err = s.bindAssistantWorkspaceSession(ctx, userID, workspace, session)
	if err != nil {
		return nil, err
	}
	return s.assistantWorkspaceView(workspace), nil
}

func assistantWorkspaceRequestFingerprint(input AssistantWorkspaceInput) string {
	payload, _ := json.Marshal(struct {
		ModeID, ProjectID, ContentID, CheckpointID string
	}{input.ModeID, input.ProjectID, input.ContentID, input.CheckpointID})
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func (s *InteractiveService) updateAssistantWorkspaceProject(ctx context.Context, userID, workspaceID, projectID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if workspaceID == "" || projectID == "" || s.repo == nil || s.repo.AssistantWorkspaces() == nil {
		return nil
	}
	workspace, err := s.repo.AssistantWorkspaces().FindByIDForUser(ctx, strings.TrimSpace(userID), workspaceID)
	if err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	if workspace.ProjectID == projectID {
		return nil
	}
	workspace.ProjectID = projectID
	workspace.UpdatedAt = time.Now().UTC()
	if err := s.repo.AssistantWorkspaces().Update(ctx, workspace); err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	return nil
}

func (s *InteractiveService) SwitchAssistantWorkspaceMode(ctx context.Context, userID, workspaceID, modeID string) (*AssistantWorkspaceView, error) {
	return s.SwitchAssistantWorkspaceModeWithContext(ctx, userID, workspaceID, AssistantWorkspaceInput{ModeID: modeID})
}

func (s *InteractiveService) SwitchAssistantWorkspaceModeWithContext(ctx context.Context, userID, workspaceID string, input AssistantWorkspaceInput) (*AssistantWorkspaceView, error) {
	modeID := strings.TrimSpace(input.ModeID)
	if modeID != AssistantModeProfile && modeID != AssistantModeCreation && modeID != AssistantModeReview {
		return nil, ErrAssistantModeUnavailable
	}
	workspace, err := s.findAssistantWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if s.workspaceSessionBusy(assistantWorkspaceSessionID(workspace, workspace.ModeID)) {
		return nil, ErrAssistantWorkspaceBusy
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(workspace.ProjectID)
	}
	contentID := strings.TrimSpace(input.ContentID)
	if contentID == "" {
		contentID = strings.TrimSpace(workspace.ContentID)
	}
	if modeID != AssistantModeReview {
		contentID = ""
	}
	if modeID == AssistantModeProfile {
		// Profile sessions may create a new project or update the selected one.
		// Keep the workspace's project context when the caller omits it.
	}
	if modeID == AssistantModeReview && (projectID == "" || contentID == "") {
		return nil, ErrAssistantModeUnavailable
	}
	sessionID := assistantWorkspaceSessionID(workspace, modeID)
	if sessionID != "" && s.workspaceSessionBusy(sessionID) {
		return nil, ErrAssistantWorkspaceBusy
	}

	// A committed/expired/interrupted logical session cannot be reused. The
	// replacement inherits its durable checkpoint so a fresh DSH session can
	// resume the last server-owned structured state.
	reusable, reusableErr := s.workspaceSessionReusable(ctx, userID, sessionID, modeID, projectID, contentID)
	if reusableErr != nil {
		return nil, reusableErr
	}
	if !reusable {
		var createErr error
		sessionID, createErr = s.recreateWorkspaceSession(ctx, userID, workspace, modeID, projectID, contentID, sessionID)
		if createErr != nil {
			return nil, createErr
		}
		if err := setAssistantWorkspaceSessionID(workspace, modeID, sessionID); err != nil {
			return nil, err
		}
	}
	workspace.ProjectID = projectID
	if modeID == AssistantModeReview {
		workspace.ContentID = contentID
	}
	workspace.ModeID = modeID
	workspace.UpdatedAt = time.Now().UTC()
	if err := s.repo.AssistantWorkspaces().Update(ctx, workspace); err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	s.setWorkspaceRuntimeActive(workspace.ID, sessionID)
	return s.assistantWorkspaceView(workspace), nil
}

func (s *InteractiveService) workspaceSessionReusable(ctx context.Context, userID, sessionID, modeID, projectID, contentID string) (bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return false, nil
	}
	s.mu.RLock()
	sess := s.sessions[sessionID]
	s.mu.RUnlock()
	if sess == nil {
		return false, nil
	}
	sess.mu.Lock()
	closed := sess.Closed || sess.ModeID != modeID || sess.ProjectID != projectID || sess.ContentID != contentID || !sess.ExpiresAt.After(time.Now())
	sess.mu.Unlock()
	if closed {
		return false, nil
	}
	if s.repo == nil || s.repo.AssistantSessions() == nil {
		return false, ErrInteractivePersistence
	}
	row, err := s.repo.AssistantSessions().FindByIDForUser(ctx, userID, sessionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, errors.Join(ErrInteractivePersistence, err)
	}
	return row.ClosedAt == nil && row.Status != "interrupted" && row.ExpiresAt.After(time.Now().UTC()), nil
}

func (s *InteractiveService) recreateWorkspaceSession(ctx context.Context, userID string, workspace *model.AssistantWorkspace, modeID, projectID, contentID, previousID string) (string, error) {
	checkpointID := ""
	if previousID != "" && s.repo != nil && s.repo.AssistantSessions() != nil {
		if previous, err := s.repo.AssistantSessions().FindByIDForUser(ctx, userID, previousID); err == nil {
			checkpointID = strings.TrimSpace(previous.CheckpointID)
			if previous.ClosedAt == nil {
				s.mu.RLock()
				_, inMemory := s.sessions[previous.ID]
				s.mu.RUnlock()
				if inMemory {
					_ = s.Close(userID, previous.ID)
				} else {
					_ = s.repo.AssistantSessions().Close(ctx, previous.ID, "replaced", time.Now().UTC())
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.Join(ErrInteractivePersistence, err)
		}
	}
	if checkpointID != "" && s.repo != nil && s.repo.AssistantCheckpoints() != nil {
		checkpoint, err := s.repo.AssistantCheckpoints().FindByIDForUser(ctx, userID, checkpointID)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (!assistantCheckpointRequestStatusAllowed(checkpoint.Status) || !checkpoint.ExpiresAt.After(time.Now().UTC())) {
			// Committed checkpoints are receipts for completed work, not resumable
			// session input. A replacement logical session starts with a clean
			// runtime context unless a draft/ready checkpoint is still recoverable.
			checkpointID = ""
		} else if err != nil {
			return "", errors.Join(ErrInteractivePersistence, err)
		}
	}
	if checkpointID == "" && s.repo != nil && s.repo.AssistantCheckpoints() != nil {
		if checkpoint, err := s.repo.AssistantCheckpoints().FindLatest(ctx, userID, modeID, projectID, contentID, time.Now().UTC()); err == nil {
			checkpointID = checkpoint.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.Join(ErrInteractivePersistence, err)
		}
	}
	session, err := s.CreateAssistantSessionWithIdempotency(ctx, userID, AssistantSessionInput{
		ModeID: modeID, ProjectID: projectID, ContentID: contentID, CheckpointID: checkpointID, WorkspaceID: workspace.ID,
	}, workspace.ID+":"+modeID+":"+uuid.NewString())
	if err != nil {
		return "", err
	}
	return session.ID, nil
}

func (s *InteractiveService) AssistantWorkspaceSession(ctx context.Context, userID, workspaceID, modeID string) (string, error) {
	workspace, err := s.findAssistantWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(modeID) != workspace.ModeID {
		return "", ErrAssistantWorkspaceModeMismatch
	}
	sessionID := assistantWorkspaceSessionID(workspace, modeID)
	projectID, contentID := workspace.ProjectID, workspace.ContentID
	if modeID != AssistantModeReview {
		contentID = ""
	}
	reusable, reusableErr := s.workspaceSessionReusable(ctx, userID, sessionID, modeID, projectID, contentID)
	if reusableErr != nil {
		return "", reusableErr
	}
	if !reusable {
		newID, err := s.recreateWorkspaceSession(ctx, userID, workspace, modeID, projectID, contentID, sessionID)
		if err != nil {
			return "", err
		}
		sessionID = newID
		if err := setAssistantWorkspaceSessionID(workspace, modeID, sessionID); err != nil {
			return "", err
		}
		workspace.UpdatedAt = time.Now().UTC()
		if err := s.repo.AssistantWorkspaces().Update(ctx, workspace); err != nil {
			return "", errors.Join(ErrInteractivePersistence, err)
		}
	}
	if sessionID == "" {
		return "", ErrAssistantWorkspaceNotFound
	}
	return sessionID, nil
}

func (s *InteractiveService) WorkspaceState(ctx context.Context, userID, workspaceID string) (*AssistantWorkspaceView, error) {
	workspace, err := s.findAssistantWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	if _, err := s.AssistantWorkspaceSession(ctx, userID, workspaceID, workspace.ModeID); err != nil {
		return nil, err
	}
	workspace, err = s.findAssistantWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.assistantWorkspaceView(workspace), nil
}

func (s *InteractiveService) WorkspaceMessage(ctx context.Context, userID, workspaceID, modeID, text string) error {
	sessionID, err := s.AssistantWorkspaceSession(ctx, userID, workspaceID, modeID)
	if err != nil {
		return err
	}
	return s.Message(ctx, userID, sessionID, text)
}

func (s *InteractiveService) WorkspaceAnswer(ctx context.Context, userID, workspaceID, modeID, callID string, answers []InteractiveAnswer) error {
	sessionID, err := s.AssistantWorkspaceSession(ctx, userID, workspaceID, modeID)
	if err != nil {
		return err
	}
	return s.Answer(ctx, userID, sessionID, callID, answers)
}

func (s *InteractiveService) CloseAssistantWorkspace(ctx context.Context, userID, workspaceID string) error {
	workspace, err := s.findAssistantWorkspace(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	for _, sessionID := range assistantWorkspaceSessions(workspace) {
		if sessionID != "" {
			_ = s.Close(userID, sessionID)
		}
	}
	if err := s.repo.AssistantWorkspaces().Delete(ctx, userID, workspaceID, time.Now().UTC()); err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	return nil
}

type AssistantCheckpointInput struct {
	SchemaVersion string          `json:"schema_version"`
	Payload       json.RawMessage `json:"payload"`
	Summary       string          `json:"summary"`
	Status        string          `json:"status"`
}

type AssistantDraftStageInput struct {
	Name    string          `json:"name,omitempty"`
	Payload json.RawMessage `json:"payload"`
	Ready   bool            `json:"ready"`
}

type assistantSessionOptions struct {
	workspaceID             string
	workspaceNew            bool
	expectedProfileRevision *int64
	modeID                  string
	contentID               string
	checkpointID            string
	checkpointPayload       []byte
	checkpointSummary       string
	baseContentRevision     int64
	baseAnalyticsRevision   int64
}

type assistantReviewAnalyticsSnapshot struct {
	Status           string                     `json:"status"`
	ProjectID        string                     `json:"project_id"`
	ContentID        string                     `json:"content_id"`
	Platform         string                     `json:"platform"`
	From             string                     `json:"from"`
	To               string                     `json:"to"`
	MetricBasis      string                     `json:"metric_basis"`
	AnalyticsRev     int64                      `json:"analytics_revision"`
	ObservationCount int64                      `json:"observation_count"`
	Reason           string                     `json:"reason,omitempty"`
	Observations     []AnalyticsObservationView `json:"observations,omitempty"`
}

func (s *InteractiveService) AssistantBootstrap(ctx context.Context, userID string) (*AssistantBootstrap, error) {
	if s.repo == nil || strings.TrimSpace(userID) == "" {
		return nil, ErrInteractivePersistence
	}
	if checkpoints := s.repo.AssistantCheckpoints(); checkpoints != nil {
		if _, err := checkpoints.DeleteExpired(ctx, time.Now().UTC()); err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
	}
	projects, err := s.repo.Projects().ListByUserID(ctx, userID, repositoryProjectActiveOptions())
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	out := &AssistantBootstrap{
		Modes: []AssistantMode{
			{ID: AssistantModeProfile, DisplayName: "人设模式", Description: "建立或更新一个人设", Available: true},
			{ID: AssistantModeCreation, DisplayName: "创作模式", Description: "围绕本次目标澄清需求，确认后创建作品任务"},
			{ID: AssistantModeReview, DisplayName: "复盘模式", Description: "分析已有作品并形成改进建议"},
		},
		Projects: make([]AssistantProject, 0, len(projects)),
	}
	confirmedProjectIDs := make([]string, 0, len(projects))
	for _, project := range projects {
		profile := projectProfileFor(project)
		profileReady := profile.IsConfirmed()
		if profileReady {
			confirmedProjectIDs = append(confirmedProjectIDs, project.ID)
		}
		projectItem := AssistantProject{ID: project.ID, Name: project.Name, Platform: project.Platform, ProfileReady: profileReady, ProfileRevision: profile.Version, Works: []AssistantWork{}}
		if model.IsVideoEditPlatform(project.Platform) {
			defaults := project.VideoEditDefaults.Data()
			projectItem.VideoEditDefaults = &defaults
		}
		out.Projects = append(out.Projects, projectItem)
	}
	if len(confirmedProjectIDs) > 0 {
		out.DefaultModeID = AssistantModeCreation
		out.Modes[1].Available = true
	} else {
		out.DefaultModeID = AssistantModeProfile
		out.Modes[1].DisabledReason = "先完成人设初始化，之后即可澄清创作需求"
	}
	hasCompletedWork, err := s.repo.Tasks().HasCompletedWorkInProjects(ctx, userID, confirmedProjectIDs)
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	if hasCompletedWork {
		out.Modes[2].Available = true
	} else {
		if len(confirmedProjectIDs) == 0 {
			out.Modes[2].DisabledReason = "先完成人设初始化并完成一件作品，之后即可复盘"
		} else {
			out.Modes[2].DisabledReason = "先通过创作模式创建并完成一件作品"
		}
	}
	if !s.Enabled() {
		for i := range out.Modes {
			out.Modes[i].Available = false
			out.Modes[i].DisabledReason = "助手运行环境暂不可用，请稍后重试"
		}
	}
	return out, nil
}

func (s *InteractiveService) AssistantWorks(ctx context.Context, userID, projectID string, cursor *AssistantWorkCursor, limit int) (*AssistantWorkPage, error) {
	if s.repo == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(projectID) == "" {
		return nil, ErrInteractivePersistence
	}
	if limit < 0 || (cursor != nil && (cursor.ID == "" || cursor.CreatedAt.IsZero())) {
		return nil, ErrAssistantModeUnavailable
	}
	if limit == 0 {
		limit = assistantWorksDefaultLimit
	}
	if limit > assistantWorksMaxLimit {
		limit = assistantWorksMaxLimit
	}
	project, err := s.repo.Projects().FindByID(ctx, strings.TrimSpace(projectID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInteractiveSessionOwner
	}
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	if project.UserID != userID || project.Status != model.ProjectStatusActive {
		return nil, ErrInteractiveSessionOwner
	}
	if !projectProfileFor(project).IsConfirmed() {
		return nil, ErrAssistantModeUnavailable
	}
	var tasks []*model.Task
	if cursor == nil {
		tasks, err = s.repo.Tasks().FindByUserIDAndStatus(ctx, userID, model.TaskStatusCompleted, project.ID, "", 0, limit+1)
	} else {
		tasks, err = s.repo.Tasks().FindByUserIDAndStatusBefore(ctx, userID, model.TaskStatusCompleted, project.ID, "", cursor.CreatedAt, cursor.ID, limit+1)
	}
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	hasMore := len(tasks) > limit
	if hasMore {
		tasks = tasks[:limit]
	}
	page := &AssistantWorkPage{Items: make([]AssistantWork, 0, len(tasks)), HasMore: hasMore}
	for _, task := range tasks {
		page.Items = append(page.Items, AssistantWork{ID: task.ID, Title: assistantWorkTitle(task), Type: firstNonEmptyAssistant(task.TaskKind, task.Type), CreatedAt: task.CreatedAt})
	}
	if hasMore && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		page.NextCursor = &AssistantWorkCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

// repositoryProjectActiveOptions keeps Assistant Bootstrap scoped to active
// projects while leaving archived project history available elsewhere.
func repositoryProjectActiveOptions() repository.ProjectListOptions {
	return repository.ProjectListOptions{Status: model.ProjectStatusActive}
}

func assistantWorkTitle(task *model.Task) string {
	if strings.TrimSpace(task.Title) != "" {
		return task.Title
	}
	if strings.TrimSpace(task.SelectedTopic) != "" {
		return task.SelectedTopic
	}
	return "未命名作品"
}

func (s *InteractiveService) reviewAnalyticsSnapshot(ctx context.Context, userID, projectID, contentID, platform string) ([]byte, int64, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -90).Format("2006-01-02")
	to := now.Format("2006-01-02")
	snapshot := assistantReviewAnalyticsSnapshot{
		Status: "unavailable", ProjectID: projectID, ContentID: contentID, Platform: platform,
		From: from, To: to, MetricBasis: "cumulative", Observations: []AnalyticsObservationView{},
	}
	if platform != model.PlatformWechat && platform != model.PlatformSeednote {
		state, err := s.repo.Analytics().State(ctx, projectID)
		if err != nil {
			return nil, 0, errors.Join(ErrInteractivePersistence, err)
		}
		snapshot.AnalyticsRev = state.Revision
		snapshot.Reason = "当前项目类型没有可用的作品数据指标"
		encoded, err := json.Marshal(snapshot)
		return encoded, snapshot.AnalyticsRev, err
	}
	state, err := s.repo.Analytics().State(ctx, projectID)
	if err != nil {
		return nil, 0, errors.Join(ErrInteractivePersistence, err)
	}
	revision := state.Revision
	page, err := NewAnalyticsService(s.repo).Observations(ctx, userID, projectID, "task:"+contentID, AnalyticsQuery{
		Platform: platform, From: from, To: to, MetricBasis: "cumulative", Limit: 30, ExpectedRevision: &revision,
	})
	if err != nil {
		return nil, 0, errors.Join(ErrInteractivePersistence, err)
	}
	snapshot.AnalyticsRev = page.Revision
	snapshot.ObservationCount = page.Total
	snapshot.Observations = page.Items
	if len(page.Items) > 0 {
		snapshot.Status = "available"
	} else {
		snapshot.Status = "data_insufficient"
		snapshot.Reason = "所选时间范围内没有该作品的分析数据，复盘结论应标注为假设"
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, 0, err
	}
	return encoded, page.Revision, nil
}

func firstNonEmptyAssistant(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func assistantCheckpointStatusAllowed(status string) bool {
	switch status {
	case model.AssistantCheckpointDraft, model.AssistantCheckpointReady, model.AssistantCheckpointCommitted, model.AssistantCheckpointAbandoned:
		return true
	default:
		return false
	}
}

func assistantCheckpointExpiry(now time.Time) time.Time { return now.UTC().Add(30 * 24 * time.Hour) }

func assistantLeaseScope(userID, modeID, projectID, contentID string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{userID, modeID, projectID, contentID}, "\x00")))
	return "assistant:" + hex.EncodeToString(sum[:])
}

func validateAssistantCheckpointPayload(payload []byte) error {
	if len(payload) == 0 || len(payload) > assistantCheckpointMaxBytes || !json.Valid(payload) {
		return ErrAssistantCheckpointInvalid
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return ErrAssistantCheckpointInvalid
	}
	if _, ok := value.(map[string]any); !ok {
		return ErrAssistantCheckpointInvalid
	}
	var rejectTranscript func(any) bool
	rejectTranscript = func(node any) bool {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				switch strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", "")) {
				case "messages", "chat", "chatlog", "chathistory", "conversation", "conversationhistory", "transcript", "turns":
					return true
				}
				if rejectTranscript(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if rejectTranscript(child) {
					return true
				}
			}
		}
		return false
	}
	if rejectTranscript(value) {
		return ErrAssistantCheckpointInvalid
	}
	return nil
}

func (s *InteractiveService) ensureAssistantCheckpointMutable(ctx context.Context, userID, modeID, checkpointID string) error {
	if modeID != AssistantModeCreation {
		return nil
	}
	if task, err := s.repo.Tasks().FindByAssistantCheckpointID(ctx, userID, checkpointID); err == nil {
		return errors.Join(ErrAssistantCheckpointConflict, fmt.Errorf("creation plan already admitted as task %s", task.ID))
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Join(ErrInteractivePersistence, err)
	}
	return nil
}

func (s *InteractiveService) SaveAssistantCheckpoint(ctx context.Context, userID, sessionID string, input AssistantCheckpointInput) (*model.AssistantCheckpoint, error) {
	sess, err := s.getOwned(userID, sessionID)
	if err != nil {
		return nil, err
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.committing {
		return nil, ErrInteractiveBusy
	}
	if sess.Closed {
		return nil, ErrInteractiveSessionClosed
	}
	modeID := sess.ModeID
	if modeID == "" {
		modeID = assistantModeForAgent(sess.AgentID)
	}
	if modeID == "" || input.SchemaVersion != assistantCheckpointSchemaVersion || !assistantCheckpointRequestStatusAllowed(input.Status) || !utf8.ValidString(input.Summary) || len([]byte(input.Summary)) > 2000 {
		return nil, ErrAssistantCheckpointInvalid
	}
	if len(sess.Draft.Payload) > 0 {
		var staged, submitted any
		if json.Unmarshal(sess.Draft.Payload, &staged) != nil || json.Unmarshal(input.Payload, &submitted) != nil {
			return nil, ErrAssistantCheckpointConflict
		}
		a, _ := json.Marshal(staged)
		b, _ := json.Marshal(submitted)
		if string(a) != string(b) {
			return nil, ErrAssistantCheckpointConflict
		}
	}
	if err := validateAssistantCheckpointPayload(input.Payload); err != nil {
		return nil, err
	}
	if s.repo == nil || s.repo.AssistantCheckpoints() == nil {
		return nil, ErrInteractivePersistence
	}
	if err := s.ensureAssistantCheckpointMutable(ctx, userID, modeID, sess.ID); err != nil {
		return nil, err
	}
	if sess.ProjectID != "" {
		project, err := s.repo.Projects().FindByID(ctx, sess.ProjectID)
		if err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		if project.UserID != userID {
			return nil, ErrInteractiveSessionOwner
		}
	}
	if sess.ContentID != "" {
		task, err := s.repo.Tasks().FindByID(ctx, sess.ContentID)
		if err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		if task.UserID != userID || task.ProjectID != sess.ProjectID {
			return nil, ErrInteractiveSessionOwner
		}
	}
	now := time.Now().UTC()
	checkpoint := &model.AssistantCheckpoint{
		ID: sess.ID, UserID: userID, ModeID: modeID, ProjectID: sess.ProjectID, ContentID: sess.ContentID,
		SchemaVersion: input.SchemaVersion, Payload: append([]byte(nil), input.Payload...), Summary: strings.TrimSpace(input.Summary),
		Status: input.Status, BaseProfileRevision: sess.ProfileVersion, BaseContentRevision: sess.BaseContentRevision,
		BaseAnalyticsRevision: sess.BaseAnalyticsRevision, SourceSessionID: sess.ID, ExpiresAt: assistantCheckpointExpiry(now), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.AssistantCheckpoints().UpsertBySourceSession(ctx, checkpoint); err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	if s.repo.AssistantSessions() != nil {
		if err := s.repo.AssistantSessions().SetCheckpoint(ctx, sess.ID, checkpoint.ID, now); err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		sess.CheckpointID = checkpoint.ID
	}
	return checkpoint, nil
}

func (s *InteractiveService) persistAssistantStageCheckpoint(ctx context.Context, sess *InteractiveSession, modeID string, payload []byte, summary, status string) error {
	if sess == nil || s.repo == nil || s.repo.AssistantCheckpoints() == nil || !assistantCheckpointRequestStatusAllowed(status) {
		return ErrInteractivePersistence
	}
	if err := validateAssistantCheckpointPayload(payload); err != nil {
		return err
	}
	if len([]byte(summary)) > 2000 || !utf8.ValidString(summary) {
		return ErrAssistantCheckpointInvalid
	}
	if err := s.ensureAssistantCheckpointMutable(ctx, sess.UserID, modeID, sess.ID); err != nil {
		return err
	}
	if sess.ProjectID != "" {
		project, err := s.repo.Projects().FindByID(ctx, sess.ProjectID)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if project.UserID != sess.UserID {
			return ErrInteractiveSessionOwner
		}
	}
	if sess.ContentID != "" {
		task, err := s.repo.Tasks().FindByID(ctx, sess.ContentID)
		if err != nil {
			return errors.Join(ErrInteractivePersistence, err)
		}
		if task.UserID != sess.UserID || task.ProjectID != sess.ProjectID {
			return ErrInteractiveSessionOwner
		}
	}
	now := time.Now().UTC()
	checkpoint := &model.AssistantCheckpoint{
		ID: sess.ID, UserID: sess.UserID, ModeID: modeID, ProjectID: sess.ProjectID, ContentID: sess.ContentID,
		SchemaVersion: assistantCheckpointSchemaVersion, Payload: append([]byte(nil), payload...), Summary: strings.TrimSpace(summary),
		Status: status, BaseProfileRevision: sess.ProfileVersion, BaseContentRevision: sess.BaseContentRevision,
		BaseAnalyticsRevision: sess.BaseAnalyticsRevision, SourceSessionID: sess.ID, ExpiresAt: assistantCheckpointExpiry(now), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.AssistantCheckpoints().UpsertBySourceSession(ctx, checkpoint); err != nil {
			return err
		}
		if tx.AssistantSessions() != nil {
			return tx.AssistantSessions().SetCheckpoint(ctx, sess.ID, checkpoint.ID, now)
		}
		return nil
	}); err != nil {
		return errors.Join(ErrInteractivePersistence, err)
	}
	sess.CheckpointID = checkpoint.ID
	return nil
}

func assistantCheckpointRequestStatusAllowed(status string) bool {
	return status == model.AssistantCheckpointDraft || status == model.AssistantCheckpointReady
}

func (s *InteractiveService) LatestAssistantCheckpoint(ctx context.Context, userID, modeID, projectID, contentID string) (*model.AssistantCheckpoint, error) {
	if s.repo == nil || s.repo.AssistantCheckpoints() == nil {
		return nil, ErrInteractivePersistence
	}
	switch modeID {
	case AssistantModeProfile:
		if contentID != "" {
			return nil, ErrAssistantModeUnavailable
		}
	case AssistantModeCreation:
		if projectID == "" || contentID != "" {
			return nil, ErrAssistantModeUnavailable
		}
	case AssistantModeReview:
		if projectID == "" || contentID == "" {
			return nil, ErrAssistantModeUnavailable
		}
	default:
		return nil, ErrAssistantModeUnavailable
	}
	if projectID != "" {
		project, err := s.repo.Projects().FindByID(ctx, projectID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInteractiveSessionOwner
		}
		if err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		if project.UserID != userID || project.Status != model.ProjectStatusActive {
			return nil, ErrInteractiveSessionOwner
		}
		if modeID != AssistantModeProfile && !projectProfileFor(project).IsConfirmed() {
			return nil, ErrAssistantModeUnavailable
		}
	}
	if modeID == AssistantModeReview {
		task, err := s.repo.Tasks().FindByID(ctx, contentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInteractiveSessionOwner
		}
		if err != nil {
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		if task.UserID != userID || task.ProjectID != projectID || task.Status != model.TaskStatusCompleted {
			return nil, ErrInteractiveSessionOwner
		}
	}
	checkpoint, err := s.repo.AssistantCheckpoints().FindLatest(ctx, userID, modeID, projectID, contentID, time.Now().UTC())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Join(ErrInteractivePersistence, err)
	}
	return checkpoint, nil
}

func assistantCheckpointSummary(checkpoint *model.AssistantCheckpoint) *AssistantCheckpointSummary {
	if checkpoint == nil {
		return nil
	}
	return &AssistantCheckpointSummary{
		ID: checkpoint.ID, ProjectID: checkpoint.ProjectID, ContentID: checkpoint.ContentID,
		Summary: checkpoint.Summary, Status: checkpoint.Status, UpdatedAt: checkpoint.UpdatedAt,
	}
}

// StageAssistantDraftForSession stages only creation and review drafts. The
// mode comes from the authenticated Server session, never from Runtime input.
func (s *InteractiveService) StageAssistantDraftForSession(sessionID string, input AssistantDraftStageInput) error {
	return s.StageAssistantDraftForSessionContext(context.Background(), sessionID, input)
}

func (s *InteractiveService) StageAssistantDraftForSessionContext(ctx context.Context, sessionID string, input AssistantDraftStageInput) error {
	s.mu.RLock()
	sess := s.sessions[sessionID]
	s.mu.RUnlock()
	if sess == nil {
		return ErrInteractiveSessionNotFound
	}
	sess.mu.Lock()
	if sess.Closed || time.Now().After(sess.ExpiresAt) {
		expired := time.Now().After(sess.ExpiresAt)
		sess.mu.Unlock()
		if expired {
			s.expire(sessionID)
		}
		return ErrInteractiveSessionClosed
	}
	modeID := sess.ModeID
	if modeID == "" {
		modeID = assistantModeForAgent(sess.AgentID)
	}
	if modeID != AssistantModeCreation && modeID != AssistantModeReview {
		sess.mu.Unlock()
		return ErrAssistantModeUnavailable
	}
	if sess.ProjectID == "" {
		sess.mu.Unlock()
		if modeID == AssistantModeCreation {
			return fmt.Errorf("%w: session project_id is required for creation", ErrAssistantCheckpointInvalid)
		}
		return fmt.Errorf("%w: session project_id is required for review", ErrAssistantCheckpointInvalid)
	}
	if modeID == AssistantModeReview && sess.ContentID == "" {
		sess.mu.Unlock()
		return fmt.Errorf("%w: session content_id is required for review", ErrAssistantCheckpointInvalid)
	}
	sess.mu.Unlock()

	if err := validateAssistantCheckpointPayload(input.Payload); err != nil {
		return err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(input.Payload, &envelope); err != nil || len(envelope) == 0 {
		return ErrAssistantCheckpointInvalid
	}
	name := strings.TrimSpace(input.Name)
	if len([]rune(name)) > 200 {
		return ErrAssistantCheckpointInvalid
	}
	canonical := append(json.RawMessage(nil), input.Payload...)
	var fields map[string]any
	if input.Ready {
		switch modeID {
		case AssistantModeCreation:
			plan, planJSON, err := decodeAssistantCreationPlan(input.Payload)
			if err != nil {
				return err
			}
			canonical, err = json.Marshal(map[string]json.RawMessage{"plan": planJSON})
			if err != nil {
				return ErrAssistantCheckpointInvalid
			}
			name = firstNonEmptyAssistant(name, plan.Goal)
			if err := json.Unmarshal(planJSON, &fields); err != nil {
				return ErrAssistantCheckpointInvalid
			}
		case AssistantModeReview:
			review, reviewJSON, err := decodeAssistantReview(input.Payload)
			if err != nil {
				return err
			}
			canonical, err = json.Marshal(map[string]json.RawMessage{"review": reviewJSON})
			if err != nil {
				return ErrAssistantCheckpointInvalid
			}
			name = firstNonEmptyAssistant(name, review.Summary)
			if err := json.Unmarshal(reviewJSON, &fields); err != nil {
				return ErrAssistantCheckpointInvalid
			}
		}
	} else {
		if err := json.Unmarshal(input.Payload, &fields); err != nil {
			return ErrAssistantCheckpointInvalid
		}
		if modePayload, ok := envelope["plan"]; ok && modeID == AssistantModeCreation {
			if err := json.Unmarshal(modePayload, &fields); err != nil {
				return ErrAssistantCheckpointInvalid
			}
		} else if modePayload, ok := envelope["review"]; ok && modeID == AssistantModeReview {
			if err := json.Unmarshal(modePayload, &fields); err != nil {
				return ErrAssistantCheckpointInvalid
			}
		}
	}
	if name == "" {
		name = map[string]string{AssistantModeCreation: "创作需求", AssistantModeReview: "复盘结论"}[modeID]
	}

	sess.mu.Lock()
	if sess.Closed || sess.committing {
		sess.mu.Unlock()
		return ErrInteractiveBusy
	}
	status := model.AssistantCheckpointDraft
	if input.Ready {
		status = model.AssistantCheckpointReady
	}
	if err := s.persistAssistantStageCheckpoint(ctx, sess, modeID, canonical, name, status); err != nil {
		sess.mu.Unlock()
		return err
	}
	sess.Draft.Revision++
	sess.Draft.Name = name
	sess.Draft.Fields = fields
	sess.Draft.Payload = append(json.RawMessage(nil), canonical...)
	sess.Draft.Ready = input.Ready
	sess.Draft.ConfirmationToken = ""
	sess.confirmationTokenHash = ""
	sess.confirmationToken = ""
	draft := sess.Draft
	sess.mu.Unlock()

	s.logRuntimeFields("info", sess.ID, "assistant checkpoint staged", nil, map[string]any{"mode_id": modeID, "ready": input.Ready})
	s.emit(sess, "draft.updated", draft)
	if input.Ready {
		s.emit(sess, "draft.ready", draft)
	}
	return nil
}

type AssistantSessionInput struct {
	ModeID       string `json:"mode_id"`
	ProjectID    string `json:"project_id,omitempty"`
	ContentID    string `json:"content_id,omitempty"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
	WorkspaceID  string `json:"-"`
	WorkspaceNew bool   `json:"-"`
}

func assistantModeForAgent(agentID string) string {
	switch agentID {
	case "project-creator":
		return AssistantModeProfile
	case "creation-planner":
		return AssistantModeCreation
	case "postmortem-review":
		return AssistantModeReview
	default:
		return ""
	}
}

func assistantAgentForMode(modeID string) (agentID, displayIntent string, ok bool) {
	switch modeID {
	case AssistantModeProfile:
		return "project-creator", "create-project", true
	case AssistantModeCreation:
		return "creation-planner", "plan-creation", true
	case AssistantModeReview:
		return "postmortem-review", "review-content", true
	default:
		return "", "", false
	}
}

func (s *InteractiveService) CreateAssistantSessionWithIdempotency(ctx context.Context, userID string, input AssistantSessionInput, idempotencyKey string) (*InteractiveSession, error) {
	bootstrap, err := s.AssistantBootstrap(ctx, userID)
	if err != nil {
		return nil, err
	}
	var mode *AssistantMode
	for i := range bootstrap.Modes {
		if bootstrap.Modes[i].ID == input.ModeID {
			mode = &bootstrap.Modes[i]
			break
		}
	}
	if mode == nil || !mode.Available {
		return nil, ErrAssistantModeUnavailable
	}
	agentID, intent, ok := assistantAgentForMode(input.ModeID)
	if !ok {
		return nil, ErrAssistantModeUnavailable
	}
	projectID := strings.TrimSpace(input.ProjectID)
	contentID := strings.TrimSpace(input.ContentID)
	if input.ModeID != AssistantModeProfile && projectID == "" {
		return nil, ErrAssistantModeUnavailable
	}
	var selectedProject *AssistantProject
	for i := range bootstrap.Projects {
		if bootstrap.Projects[i].ID == projectID {
			selectedProject = &bootstrap.Projects[i]
			break
		}
	}
	if projectID != "" && selectedProject == nil {
		return nil, ErrInteractiveSessionOwner
	}
	if selectedProject != nil && input.ModeID != AssistantModeProfile && !selectedProject.ProfileReady {
		return nil, ErrAssistantModeUnavailable
	}
	var selectedWork *AssistantWork
	if input.ModeID == AssistantModeReview {
		if contentID == "" {
			return nil, ErrAssistantModeUnavailable
		}
		task, taskErr := s.repo.Tasks().FindByID(ctx, contentID)
		if errors.Is(taskErr, gorm.ErrRecordNotFound) {
			return nil, ErrInteractiveSessionOwner
		}
		if taskErr != nil {
			return nil, errors.Join(ErrInteractivePersistence, taskErr)
		}
		if task.UserID != userID || task.ProjectID != projectID || task.Status != model.TaskStatusCompleted {
			return nil, ErrInteractiveSessionOwner
		}
		selectedWork = &AssistantWork{ID: task.ID, Title: assistantWorkTitle(task), Type: firstNonEmptyAssistant(task.TaskKind, task.Type), CreatedAt: task.CreatedAt}
	} else if contentID != "" {
		return nil, ErrAssistantModeUnavailable
	}
	if input.ModeID == AssistantModeProfile && projectID != "" {
		intent = "update-project-profile"
	}
	options := assistantSessionOptions{workspaceID: strings.TrimSpace(input.WorkspaceID), workspaceNew: input.WorkspaceNew, modeID: input.ModeID, contentID: contentID, checkpointID: strings.TrimSpace(input.CheckpointID)}
	if selectedProject != nil && input.ModeID == AssistantModeReview {
		analytics, analyticsErr := s.repo.Analytics().State(ctx, projectID)
		if analyticsErr != nil {
			return nil, errors.Join(ErrInteractivePersistence, analyticsErr)
		}
		options.baseAnalyticsRevision = analytics.Revision
	}
	if selectedWork != nil {
		task, taskErr := s.repo.Tasks().FindByID(ctx, selectedWork.ID)
		if taskErr != nil {
			return nil, errors.Join(ErrInteractivePersistence, taskErr)
		}
		options.baseContentRevision = task.UpdatedAt.UTC().UnixNano()
	}
	if input.CheckpointID != "" {
		checkpoint, err := s.repo.AssistantCheckpoints().FindByIDForUser(ctx, userID, strings.TrimSpace(input.CheckpointID))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrAssistantCheckpointNotFound
			}
			return nil, errors.Join(ErrInteractivePersistence, err)
		}
		if checkpoint.ModeID != input.ModeID || checkpoint.ProjectID != projectID || checkpoint.ContentID != contentID || !checkpoint.ExpiresAt.After(time.Now()) || !assistantCheckpointRequestStatusAllowed(checkpoint.Status) {
			return nil, ErrAssistantCheckpointConflict
		}
		if selectedProject != nil && checkpoint.BaseProfileRevision != selectedProject.ProfileRevision {
			return nil, ErrAssistantCheckpointConflict
		}
		if selectedWork != nil && (checkpoint.BaseContentRevision != options.baseContentRevision || checkpoint.BaseAnalyticsRevision != options.baseAnalyticsRevision) {
			return nil, ErrAssistantCheckpointConflict
		}
		if err := validateAssistantCheckpointPayload(checkpoint.Payload); err != nil {
			return nil, err
		}
		if checkpoint.ModeID == AssistantModeCreation {
			if task, lookupErr := s.repo.Tasks().FindByAssistantCheckpointID(ctx, userID, checkpoint.ID); lookupErr == nil {
				return nil, errors.Join(ErrAssistantCheckpointConflict, fmt.Errorf("creation plan already admitted as task %s; open that task instead", task.ID))
			} else if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
				return nil, errors.Join(ErrInteractivePersistence, lookupErr)
			}
		}
		options.expectedProfileRevision = &checkpoint.BaseProfileRevision
		options.checkpointPayload = append([]byte(nil), checkpoint.Payload...)
		options.checkpointSummary = checkpoint.Summary
	}
	fingerprintData, _ := json.Marshal(struct {
		ModeID, ProjectID, ContentID, CheckpointID string
	}{input.ModeID, projectID, contentID, input.CheckpointID})
	sum := sha256.Sum256(fingerprintData)
	fingerprint := hex.EncodeToString(sum[:])
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return s.createSessionWithOptions(ctx, userID, agentID, intent, options, projectID)
	}
	if len([]byte(key)) > 128 {
		return nil, ErrInteractiveIdempotency
	}
	reservationKey := strings.TrimSpace(userID) + "\x00" + key
	for {
		s.mu.Lock()
		if s.createIdempotency == nil {
			s.createIdempotency = map[string]*interactiveCreateReservation{}
		}
		reservation, exists := s.createIdempotency[reservationKey]
		if exists {
			if reservation.fingerprint != fingerprint {
				s.mu.Unlock()
				return nil, ErrInteractiveIdempotency
			}
			if reservation.session != nil {
				sess := reservation.session
				s.mu.Unlock()
				return sess, nil
			}
			done := reservation.done
			s.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		reservation = &interactiveCreateReservation{fingerprint: fingerprint, done: make(chan struct{})}
		s.createIdempotency[reservationKey] = reservation
		s.mu.Unlock()
		sess, createErr := s.createSessionWithOptions(ctx, userID, agentID, intent, options, projectID)
		s.mu.Lock()
		if createErr == nil {
			reservation.session = sess
		} else {
			delete(s.createIdempotency, reservationKey)
		}
		close(reservation.done)
		s.mu.Unlock()
		return sess, createErr
	}
}

func assistantCheckpointRuntimeContext(payload []byte, summary string) string {
	if len(payload) == 0 {
		return ""
	}
	return fmt.Sprintf("\n\n[Server structured checkpoint]\nsummary: %s\npayload: %s", strings.TrimSpace(summary), string(payload))
}

func assistantCheckpointIDPointer(id string) *string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return &id
}
