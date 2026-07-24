package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	maxTaskArtifactUploadBytes = 512 * 1024 * 1024
	taskArtifactSHA256Header   = "X-Oss-Meta-Sha256"
)

var (
	ErrTaskArtifactInvalid           = errors.New("invalid task artifact request")
	ErrTaskArtifactUnavailable       = errors.New("task artifact storage unavailable")
	ErrTaskArtifactPersistence       = errors.New("task artifact persistence failed")
	ErrTaskArtifactExecutionConflict = errors.New("task artifact execution conflict")
)

type taskArtifactPromotionStorage interface {
	storage.ObjectStatProvider
	storage.ConditionalObjectPromoter
}

type taskArtifactFinalizationClaim struct {
	sessionID string
	token     string
}

type TaskArtifactPrepareRequest struct {
	TaskID       string `json:"task_id"`
	ExecutionID  string `json:"execution_id,omitempty"`
	RelativePath string `json:"relative_path"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type TaskArtifactManifestRequest struct {
	TaskID      string                     `json:"task_id"`
	ExecutionID string                     `json:"execution_id,omitempty"`
	Files       []TaskArtifactManifestFile `json:"files"`
}

type TaskArtifactManifestFile struct {
	RelativePath string `json:"relative_path"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	Role         string `json:"role"`
}

func (s *TaskService) PrepareTaskArtifactUpload(ctx context.Context, taskID, authenticatedUserID, authenticatedExecutionID string, cfg DirectUploadConfig, req TaskArtifactPrepareRequest) (*DirectUploadPrepareResult, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage provider is not available", ErrTaskArtifactUnavailable)
	}
	if s.store.Name() != "oss" {
		return nil, fmt.Errorf("%w: direct uploads require OSS storage", ErrTaskArtifactUnavailable)
	}
	taskID = firstNonEmptyString(strings.TrimSpace(taskID), strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return nil, taskArtifactInvalidf("task_id is required")
	}
	if strings.TrimSpace(req.TaskID) != "" && req.TaskID != taskID {
		return nil, taskArtifactInvalidf("request task_id does not match task scope")
	}
	task, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID)
	if err != nil {
		return nil, err
	}
	executionID, err := s.validateTaskArtifactExecution(ctx, task, authenticatedUserID, authenticatedExecutionID, req.ExecutionID)
	if err != nil {
		return nil, err
	}
	relPath, err := cleanTaskArtifactRelativePath(task, req.RelativePath)
	if err != nil {
		return nil, taskArtifactInvalidf("%v", err)
	}
	if req.Size <= 0 {
		return nil, taskArtifactInvalidf("file size is required")
	}
	if req.Size > maxTaskArtifactUploadBytes {
		return nil, taskArtifactInvalidf("file size exceeds the %d MB limit", maxTaskArtifactUploadBytes/(1024*1024))
	}
	if !validTaskArtifactSHA256(req.SHA256) {
		return nil, taskArtifactInvalidf("sha256 must be a 64-character hex string")
	}
	req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))

	contentType := normalizeTaskArtifactContentType(req.ContentType, relPath)
	finalKey := buildTaskArtifactFinalStorageKey(task, executionID, req.SHA256, relPath)
	artifactStore, ok := s.store.(taskArtifactPromotionStorage)
	if !ok {
		return nil, fmt.Errorf("%w: storage provider does not support immutable object promotion", ErrTaskArtifactUnavailable)
	}
	stat, statErr := artifactStore.StatObject(ctx, finalKey)
	if statErr == nil && stat == nil {
		return nil, fmt.Errorf("%w: stat task artifact %s returned no metadata", ErrTaskArtifactUnavailable, finalKey)
	}
	switch {
	case statErr == nil && stat != nil && stat.Size == req.Size && strings.EqualFold(strings.TrimSpace(stat.SHA256), req.SHA256):
		return &DirectUploadPrepareResult{
			UploadRequired: false,
			ETag:           stat.ETag,
			StagingKey:     finalKey,
			Key:            finalKey,
			PublicURL:      s.store.GetURL(finalKey),
			Method:         "PUT",
			Headers: map[string]string{
				"Content-Type":           contentType,
				taskArtifactSHA256Header: req.SHA256,
			},
			MaxSize: maxTaskArtifactUploadBytes,
		}, nil
	case statErr == nil:
		return nil, taskArtifactInvalidf("immutable final object metadata mismatch for %s", relPath)
	case errors.Is(statErr, storage.ErrObjectNotFound):
		// Missing immutable content is uploaded through a fresh staging object.
	default:
		return nil, fmt.Errorf("%w: stat task artifact %s: %v", ErrTaskArtifactUnavailable, finalKey, statErr)
	}
	uploadID := uuid.NewString()
	stagingKey := buildTaskArtifactStagingStorageKey(task, executionID, req.SHA256, uploadID, relPath)
	now := time.Now
	if cfg.Now != nil {
		now = cfg.Now
	}
	expiresSeconds := cfg.Storage.DirectUploadExpiresSeconds
	if expiresSeconds <= 0 {
		expiresSeconds = defaultDirectUploadTTLSeconds
	}
	expiresAt := now().Add(time.Duration(expiresSeconds) * time.Second)
	uploadURL, err := s.store.UploadURL(ctx, stagingKey, contentType, expiresSeconds)
	if err != nil {
		return nil, fmt.Errorf("%w: create signed upload URL: %v", ErrTaskArtifactUnavailable, err)
	}

	issuer := cfg.CredentialIssuer
	if issuer == nil {
		issuer = NewAliyunUploadCredentialIssuer(cfg.Storage)
	}
	cred, err := issuer.IssueUploadCredential(ctx, UploadCredentialRequest{
		RoleArn:     cfg.Storage.STSRoleArn,
		SessionName: firstNonEmptyString(cfg.Storage.STSSessionName, "agent-task-artifact-upload"),
		Policy:      directUploadPolicyJSON(cfg.Storage.BucketName, stagingKey),
		Expires:     expiresSeconds,
		STSEndpoint: cfg.Storage.STSEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: issue upload credential: %v", ErrTaskArtifactUnavailable, err)
	}
	if cred == nil {
		return nil, fmt.Errorf("%w: upload credential issuer returned no credential", ErrTaskArtifactUnavailable)
	}
	if cred.ExpiresAt.IsZero() {
		cred.ExpiresAt = expiresAt
	}
	session := &model.UploadSession{
		ID:          uploadID,
		UserID:      task.UserID,
		Purpose:     DirectUploadPurposeTaskArtifact,
		StagingKey:  stagingKey,
		FileName:    filepath.Base(relPath),
		ContentType: contentType,
		Size:        req.Size,
		Status:      model.UploadSessionPending,
		ExpiresAt:   expiresAt,
	}
	if err := s.repo.UploadSessions().Create(ctx, session); err != nil {
		return nil, fmt.Errorf("%w: record task artifact upload session: %v", ErrTaskArtifactPersistence, err)
	}

	return &DirectUploadPrepareResult{
		UploadRequired:  true,
		UploadSessionID: uploadID,
		UploadID:        uploadID,
		StagingKey:      stagingKey,
		Key:             stagingKey,
		PublicURL:       s.store.GetURL(stagingKey),
		UploadURL:       uploadURL,
		Method:          "PUT",
		Headers: map[string]string{
			"Content-Type":           contentType,
			taskArtifactSHA256Header: req.SHA256,
		},
		Region:             ossBrowserRegion(cfg.Storage.Region, cfg.Storage.Endpoint),
		Bucket:             cfg.Storage.BucketName,
		Endpoint:           cfg.Storage.Endpoint,
		STSAccessKeyID:     cred.AccessKeyID,
		STSAccessKeySecret: cred.AccessKeySecret,
		STSSecurityToken:   cred.SecurityToken,
		ExpiresAt:          minTime(expiresAt, cred.ExpiresAt),
		MaxSize:            maxTaskArtifactUploadBytes,
	}, nil
}

func (s *TaskService) FinalizeTaskArtifactManifest(ctx context.Context, taskID, authenticatedUserID, authenticatedExecutionID string, req TaskArtifactManifestRequest) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("%w: repository is not available", ErrTaskArtifactPersistence)
	}
	if s.store == nil {
		return fmt.Errorf("%w: storage provider is not available", ErrTaskArtifactUnavailable)
	}
	taskID = firstNonEmptyString(strings.TrimSpace(taskID), strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return taskArtifactInvalidf("task_id is required")
	}
	if strings.TrimSpace(req.TaskID) != "" && req.TaskID != taskID {
		return taskArtifactInvalidf("manifest task_id does not match request task_id")
	}
	task, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID)
	if err != nil {
		return err
	}
	executionID, err := s.validateTaskArtifactExecution(ctx, task, authenticatedUserID, authenticatedExecutionID, req.ExecutionID)
	if err != nil {
		return err
	}
	for _, file := range req.Files {
		if file.Size > maxTaskArtifactUploadBytes {
			return taskArtifactInvalidf("task artifact %s exceeds the %d MB limit", strings.TrimSpace(file.RelativePath), maxTaskArtifactUploadBytes/(1024*1024))
		}
	}
	artifactStore, ok := s.store.(taskArtifactPromotionStorage)
	if !ok {
		return fmt.Errorf("%w: storage provider does not support immutable object promotion", ErrTaskArtifactUnavailable)
	}
	operationCtx, operationCancel := context.WithTimeout(ctx, uploadFinalizationTimeout)
	defer operationCancel()
	ctx = operationCtx
	prefix := buildTaskArtifactStoragePrefix(task, executionID)
	files := make([]*model.TaskFile, 0, len(req.Files))
	stagingKeys := make([]string, 0, len(req.Files))
	claims := make([]taskArtifactFinalizationClaim, 0, len(req.Files))
	defer func() {
		releaseTaskArtifactFinalizationClaims(ctx, s.repo, claims)
	}()
	now := time.Now()
	for _, file := range req.Files {
		relPath, err := cleanTaskArtifactRelativePath(task, file.RelativePath)
		if err != nil {
			return taskArtifactInvalidf("%v", err)
		}
		objectKey := file.ObjectKey
		if strings.TrimSpace(objectKey) == "" {
			return taskArtifactInvalidf("object_key is required for %s", relPath)
		}
		if objectKey != strings.TrimSpace(objectKey) {
			return taskArtifactInvalidf("object key %q must not contain surrounding whitespace", objectKey)
		}
		if !strings.HasPrefix(objectKey, prefix) {
			return taskArtifactInvalidf("object key %q is outside task artifact prefix %q", objectKey, prefix)
		}
		if !validTaskArtifactSHA256(file.SHA256) {
			return taskArtifactInvalidf("sha256 must be a 64-character hex string for %s", relPath)
		}
		if file.Size <= 0 {
			return taskArtifactInvalidf("file size is required for %s", relPath)
		}
		if file.Size > maxTaskArtifactUploadBytes {
			return taskArtifactInvalidf("task artifact %s exceeds the %d MB limit", relPath, maxTaskArtifactUploadBytes/(1024*1024))
		}
		hash := strings.ToLower(strings.TrimSpace(file.SHA256))
		finalKey := buildTaskArtifactFinalStorageKey(task, executionID, hash, relPath)
		stagingUploadID, stagingKeyOK := taskArtifactStagingUploadID(task, executionID, hash, relPath, objectKey)
		var stat *storage.ObjectInfo
		switch {
		case objectKey == finalKey:
			stat, err = statTaskArtifactObject(ctx, artifactStore, finalKey)
			if err != nil {
				return err
			}
			if err := validateTaskArtifactObject(relPath, file.Size, hash, stat); err != nil {
				return err
			}
		case stagingKeyOK:
			finalStat, finalErr := artifactStore.StatObject(ctx, finalKey)
			switch {
			case finalErr == nil && finalStat == nil:
				return fmt.Errorf("%w: stat task artifact %s returned no metadata", ErrTaskArtifactUnavailable, finalKey)
			case finalErr == nil:
				if err := validateTaskArtifactObject(relPath, file.Size, hash, finalStat); err != nil {
					return err
				}
				stat = finalStat
			case errors.Is(finalErr, storage.ErrObjectNotFound):
				claim, claimErr := s.claimTaskArtifactStagingSession(ctx, task, stagingUploadID, objectKey, relPath, normalizeTaskArtifactContentType(file.ContentType, relPath), file.Size, now)
				if claimErr != nil {
					return claimErr
				}
				claims = append(claims, claim)
				if err := validateTaskArtifactStagingClaim(ctx, s.repo, task, claim, objectKey, relPath, normalizeTaskArtifactContentType(file.ContentType, relPath), file.Size, now); err != nil {
					return err
				}
				stat, err = statTaskArtifactObject(ctx, artifactStore, objectKey)
				if err != nil {
					return err
				}
				if err := validateTaskArtifactObject(relPath, file.Size, hash, stat); err != nil {
					return err
				}
				sourceETag := strings.TrimSpace(stat.ETag)
				if sourceETag == "" {
					return taskArtifactInvalidf("task artifact %s staging ETag is required", relPath)
				}
				if _, promoteErr := artifactStore.PromoteObject(ctx, objectKey, finalKey, sourceETag); promoteErr != nil && !errors.Is(promoteErr, storage.ErrObjectAlreadyExists) {
					if errors.Is(promoteErr, storage.ErrPromotionPreconditionFailed) {
						return fmt.Errorf("%w: task artifact %s promotion source changed: %v", ErrTaskArtifactUnavailable, relPath, promoteErr)
					}
					return fmt.Errorf("%w: promote task artifact %s: %v", ErrTaskArtifactUnavailable, relPath, promoteErr)
				}
				stat, err = statTaskArtifactObject(ctx, artifactStore, finalKey)
				if err != nil {
					return err
				}
				if err := validateTaskArtifactObject(relPath, file.Size, hash, stat); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%w: stat task artifact %s: %v", ErrTaskArtifactUnavailable, finalKey, finalErr)
			}
			stagingKeys = append(stagingKeys, objectKey)
		default:
			return taskArtifactInvalidf("object key %q is not the immutable final key or a valid staging key for %q", objectKey, relPath)
		}
		contentType := normalizeTaskArtifactContentType(file.ContentType, relPath)
		size := stat.Size
		if statContentType := firstNonEmptyString(stat.ContentType, stat.MimeType); statContentType != "" {
			contentType = statContentType
		}

		filename := filepath.Base(relPath)
		role := DetermineTaskFileRole(filename, contentType)
		if normalizedRole := strings.TrimSpace(file.Role); normalizedRole != "" {
			role = normalizedRole
		}
		taskFile := &model.TaskFile{
			TaskID:          task.ID,
			ExecutionID:     executionID,
			State:           model.TaskFileStatePublished,
			Role:            role,
			FileName:        filename,
			MimeType:        contentType,
			FileSize:        size,
			ContentHash:     hash,
			OSSKey:          finalKey,
			OSSURL:          s.store.GetURL(finalKey),
			StorageProvider: s.store.Name(),
			FilePath:        relPath,
		}
		if executionID != "" {
			taskFile.State = model.TaskFileStatePending
		}
		files = append(files, taskFile)
	}
	if executionID != "" {
		if err := s.repo.TaskFiles().ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx, task.ID, executionID, files); err != nil {
			if errors.Is(err, repository.ErrTaskFileExecutionNotCurrent) || errors.Is(err, repository.ErrTaskFileTaskNotRunning) || errors.Is(err, repository.ErrTaskFileManifestState) {
				return fmt.Errorf("%w: %v", ErrTaskArtifactExecutionConflict, err)
			}
			return fmt.Errorf("%w: %v", ErrTaskArtifactPersistence, err)
		}
	} else {
		if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
			authoritative, err := tx.Tasks().FindByIDForUpdate(ctx, task.ID)
			if err != nil {
				return err
			}
			if authoritative.DeletingAt != nil {
				return ErrTaskDeleting
			}
			for _, file := range files {
				if _, err := tx.TaskFiles().Upsert(ctx, file); err != nil {
					return fmt.Errorf("%w: %v", ErrTaskArtifactPersistence, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	releaseTaskArtifactFinalizationClaims(ctx, s.repo, claims)
	claims = nil
	deleteTaskArtifactStagingObjects(ctx, s.store, stagingKeys)
	return nil
}

func buildTaskMCPArtifactStoragePrefix(task *model.Task, executionID string) string {
	return buildTaskArtifactStoragePrefix(task, executionID) + "mcp/"
}

func buildTaskArtifactStoragePrefix(task *model.Task, executionID string) string {
	segments := []string{strings.TrimSuffix(buildTaskArtifactTaskStoragePrefix(task), "/")}
	if executionID != "" {
		segments = append(segments, "executions", executionID)
	}
	return path.Join(append(segments, "artifacts")...) + "/"
}

func buildTaskArtifactTaskStoragePrefix(task *model.Task) string {
	return path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID) + "/"
}

func buildTaskArtifactFinalStorageKey(task *model.Task, executionID, hash, relPath string) string {
	return buildTaskArtifactStoragePrefix(task, executionID) + "workspace/sha256/" + hash + "/" + filepath.ToSlash(relPath)
}

func buildTaskArtifactStagingStorageKey(task *model.Task, executionID, hash, uploadID, relPath string) string {
	return buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + hash + "/" + uploadID + "/" + filepath.ToSlash(relPath)
}

func taskArtifactStagingUploadID(task *model.Task, executionID, hash, relPath, key string) (string, bool) {
	prefix := buildTaskArtifactStoragePrefix(task, executionID) + "staging/sha256/" + hash + "/"
	remainder, ok := strings.CutPrefix(key, prefix)
	if !ok {
		return "", false
	}
	uploadID, suffix, ok := strings.Cut(remainder, "/")
	if !ok || suffix != filepath.ToSlash(relPath) {
		return "", false
	}
	parsed, err := uuid.Parse(uploadID)
	if err != nil || parsed.String() != uploadID {
		return "", false
	}
	return uploadID, true
}

func (s *TaskService) claimTaskArtifactStagingSession(ctx context.Context, task *model.Task, uploadID, stagingKey, relPath, contentType string, size int64, now time.Time) (taskArtifactFinalizationClaim, error) {
	claim := taskArtifactFinalizationClaim{sessionID: uploadID, token: uuid.NewString()}
	claimed, err := s.repo.UploadSessions().ClaimFinalization(ctx, uploadID, claim.token, now, now.Add(-uploadFinalizationLease))
	if err != nil {
		return taskArtifactFinalizationClaim{}, fmt.Errorf("%w: claim task artifact upload session: %v", ErrTaskArtifactPersistence, err)
	}
	if claimed {
		return claim, nil
	}
	session, err := s.repo.UploadSessions().FindByID(ctx, uploadID)
	if err != nil {
		if errors.Is(err, model.ErrUploadSessionNotFound) {
			return taskArtifactFinalizationClaim{}, taskArtifactInvalidf("task artifact %s staging upload session is unavailable", relPath)
		}
		return taskArtifactFinalizationClaim{}, fmt.Errorf("%w: find task artifact upload session: %v", ErrTaskArtifactPersistence, err)
	}
	if err := validateTaskArtifactStagingSessionIdentity(session, task, stagingKey, relPath, contentType, size); err != nil {
		return taskArtifactFinalizationClaim{}, err
	}
	if session.Status == model.UploadSessionFinalizing && session.FinalizationClaimedAt != nil && session.FinalizationClaimedAt.After(now.Add(-uploadFinalizationLease)) {
		return taskArtifactFinalizationClaim{}, fmt.Errorf("%w: task artifact %s is already being finalized", ErrTaskArtifactUnavailable, relPath)
	}
	if !session.ExpiresAt.After(now) {
		return taskArtifactFinalizationClaim{}, taskArtifactInvalidf("task artifact %s staging upload session has expired", relPath)
	}
	if session.Status != model.UploadSessionPending {
		return taskArtifactFinalizationClaim{}, taskArtifactInvalidf("task artifact %s staging upload session is not pending", relPath)
	}
	return taskArtifactFinalizationClaim{}, fmt.Errorf("%w: task artifact %s finalization claim was rejected", ErrTaskArtifactUnavailable, relPath)
}

func validateTaskArtifactStagingClaim(ctx context.Context, repo repository.Repository, task *model.Task, claim taskArtifactFinalizationClaim, stagingKey, relPath, contentType string, size int64, now time.Time) error {
	session, err := repo.UploadSessions().FindByID(ctx, claim.sessionID)
	if err != nil {
		if errors.Is(err, model.ErrUploadSessionNotFound) {
			return taskArtifactInvalidf("task artifact %s staging upload session is unavailable", relPath)
		}
		return fmt.Errorf("%w: reload task artifact upload session: %v", ErrTaskArtifactPersistence, err)
	}
	if err := validateTaskArtifactStagingSessionIdentity(session, task, stagingKey, relPath, contentType, size); err != nil {
		return err
	}
	if session.Status != model.UploadSessionFinalizing || session.FinalizationToken != claim.token || session.FinalizationClaimedAt == nil {
		return fmt.Errorf("%w: task artifact %s finalization claim was lost", ErrTaskArtifactUnavailable, relPath)
	}
	if !session.ExpiresAt.After(now) {
		return taskArtifactInvalidf("task artifact %s staging upload session has expired", relPath)
	}
	return nil
}

func validateTaskArtifactStagingSessionIdentity(session *model.UploadSession, task *model.Task, stagingKey, relPath, contentType string, size int64) error {
	if session == nil || session.UserID != task.UserID || session.Purpose != DirectUploadPurposeTaskArtifact || session.StagingKey != stagingKey ||
		session.FileName != filepath.Base(relPath) || session.ContentType != contentType || session.Size != size {
		return taskArtifactInvalidf("task artifact %s staging upload session does not match manifest", relPath)
	}
	return nil
}

func releaseTaskArtifactFinalizationClaims(ctx context.Context, repo repository.Repository, claims []taskArtifactFinalizationClaim) {
	if repo == nil || len(claims) == 0 {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), uploadFinalizationCleanupTTL)
	defer cancel()
	for i := len(claims) - 1; i >= 0; i-- {
		_, _ = repo.UploadSessions().ReleaseFinalization(releaseCtx, claims[i].sessionID, claims[i].token)
	}
}

func deleteTaskArtifactStagingObjects(ctx context.Context, store storage.Provider, keys []string) {
	if store == nil || len(keys) == 0 {
		return
	}
	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), uploadFinalizationCleanupTTL)
	defer cancel()
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		_ = store.Delete(deleteCtx, key)
	}
}

func statTaskArtifactObject(ctx context.Context, statProvider storage.ObjectStatProvider, key string) (*storage.ObjectInfo, error) {
	stat, err := statProvider.StatObject(ctx, key)
	if err != nil || stat == nil {
		return nil, fmt.Errorf("%w: stat task artifact %s: %v", ErrTaskArtifactUnavailable, key, err)
	}
	return stat, nil
}

func validateTaskArtifactObject(relPath string, size int64, hash string, stat *storage.ObjectInfo) error {
	if stat.Size != size {
		return taskArtifactInvalidf("task artifact %s size mismatch: manifest=%d storage=%d", relPath, size, stat.Size)
	}
	if !strings.EqualFold(strings.TrimSpace(stat.SHA256), hash) {
		return taskArtifactInvalidf("task artifact %s sha256 mismatch: manifest=%s storage=%s", relPath, hash, strings.ToLower(stat.SHA256))
	}
	return nil
}

func (s *TaskService) validateTaskArtifactExecution(ctx context.Context, task *model.Task, userID, authenticatedExecutionID, requestedExecutionID string) (string, error) {
	authenticatedExecutionID = strings.TrimSpace(authenticatedExecutionID)
	requestedExecutionID = strings.TrimSpace(requestedExecutionID)
	if authenticatedExecutionID == "" {
		if task.CurrentExecutionID != nil || requestedExecutionID != "" {
			return "", fmt.Errorf("%w: execution identity requires an execution token", ErrTaskArtifactExecutionConflict)
		}
		return "", nil
	}
	if requestedExecutionID == "" || requestedExecutionID != authenticatedExecutionID {
		return "", fmt.Errorf("%w: execution identity does not match request", ErrTaskArtifactExecutionConflict)
	}
	if err := s.ValidateAgentExecutionAccess(ctx, userID, task.ProjectID, task.ID, authenticatedExecutionID); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTaskArtifactExecutionConflict, err)
	}
	return authenticatedExecutionID, nil
}

func cleanTaskArtifactRelativePath(task *model.Task, relPath string) (string, error) {
	cleaned, err := CleanTaskFileRelativePath(relPath)
	if err != nil {
		return "", err
	}
	cleaned = filepath.ToSlash(cleaned)
	segments := strings.Split(cleaned, "/")
	for i, segment := range segments {
		if segment == "" {
			return "", fmt.Errorf("invalid relative path")
		}
		if i < len(segments)-1 && ShouldSkipTaskFileDir(segment) {
			return "", fmt.Errorf("refusing to upload runtime directory %q", segment)
		}
	}
	if ShouldSkipTaskFile(filepath.Base(cleaned)) {
		return "", fmt.Errorf("refusing to upload dotfile %q", filepath.Base(cleaned))
	}
	if !ShouldCollectTaskFile(task, cleaned) {
		return "", fmt.Errorf("task artifact path %q is not a collectable deliverable", cleaned)
	}
	return cleaned, nil
}

func normalizeTaskArtifactContentType(contentType, relPath string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		if inferred := contentTypeForUploadExt(strings.ToLower(filepath.Ext(relPath))); inferred != "" {
			return inferred
		}
	}
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func validTaskArtifactSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func taskArtifactInvalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTaskArtifactInvalid, fmt.Sprintf(format, args...))
}
