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

const maxTaskArtifactUploadBytes = 512 * 1024 * 1024

var (
	ErrTaskArtifactInvalid           = errors.New("invalid task artifact request")
	ErrTaskArtifactUnavailable       = errors.New("task artifact storage unavailable")
	ErrTaskArtifactPersistence       = errors.New("task artifact persistence failed")
	ErrTaskArtifactExecutionConflict = errors.New("task artifact execution conflict")
)

type taskArtifactObjectStatProvider interface {
	StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error)
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
	ETag         string `json:"etag"`
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
	if req.SHA256 != "" && !validTaskArtifactSHA256(req.SHA256) {
		return nil, taskArtifactInvalidf("sha256 must be a 64-character hex string")
	}

	contentType := normalizeTaskArtifactContentType(req.ContentType, relPath)
	key := buildTaskArtifactStorageKey(task, executionID, relPath)
	now := time.Now
	if cfg.Now != nil {
		now = cfg.Now
	}
	expiresSeconds := cfg.Storage.DirectUploadExpiresSeconds
	if expiresSeconds <= 0 {
		expiresSeconds = defaultDirectUploadTTLSeconds
	}
	expiresAt := now().Add(time.Duration(expiresSeconds) * time.Second)
	uploadURL, err := s.store.UploadURL(ctx, key, contentType, expiresSeconds)
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
		Policy:      directUploadPolicyJSON(cfg.Storage.BucketName, key),
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

	return &DirectUploadPrepareResult{
		UploadID:           uuid.NewString(),
		Key:                key,
		PublicURL:          s.store.GetURL(key),
		UploadURL:          uploadURL,
		Method:             "PUT",
		Headers:            map[string]string{"Content-Type": contentType},
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
	prefix := buildTaskArtifactStoragePrefix(task, executionID)
	files := make([]*model.TaskFile, 0, len(req.Files))
	for _, file := range req.Files {
		relPath, err := cleanTaskArtifactRelativePath(task, file.RelativePath)
		if err != nil {
			return taskArtifactInvalidf("%v", err)
		}
		objectKey := strings.TrimPrefix(strings.TrimSpace(file.ObjectKey), "/")
		if objectKey == "" {
			return taskArtifactInvalidf("object_key is required for %s", relPath)
		}
		if !strings.HasPrefix(objectKey, prefix) {
			return taskArtifactInvalidf("object key %q is outside task artifact prefix %q", objectKey, prefix)
		}
		expectedKey := buildTaskArtifactStorageKey(task, executionID, relPath)
		if objectKey != expectedKey {
			return taskArtifactInvalidf("object key %q does not match relative path %q", objectKey, relPath)
		}
		if !validTaskArtifactSHA256(file.SHA256) {
			return taskArtifactInvalidf("sha256 must be a 64-character hex string for %s", relPath)
		}
		contentType := normalizeTaskArtifactContentType(file.ContentType, relPath)
		size := file.Size
		if statProvider, ok := s.store.(taskArtifactObjectStatProvider); ok {
			stat, err := statProvider.StatObject(ctx, objectKey)
			if err != nil {
				return fmt.Errorf("%w: stat task artifact %s: %v", ErrTaskArtifactUnavailable, objectKey, err)
			}
			if stat.Size > 0 {
				if size > 0 && stat.Size != size {
					return taskArtifactInvalidf("task artifact %s size mismatch: manifest=%d storage=%d", relPath, size, stat.Size)
				}
				size = stat.Size
			}
			if statContentType := firstNonEmptyString(stat.ContentType, stat.MimeType); statContentType != "" {
				contentType = statContentType
			}
		}
		if size <= 0 {
			return taskArtifactInvalidf("file size is required for %s", relPath)
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
			ContentHash:     strings.ToLower(file.SHA256),
			OSSKey:          objectKey,
			OSSURL:          s.store.GetURL(objectKey),
			StorageProvider: s.store.Name(),
			FilePath:        relPath,
		}
		if executionID != "" {
			taskFile.State = model.TaskFileStatePending
		}
		files = append(files, taskFile)
	}
	if executionID != "" {
		if err := s.repo.TaskFiles().ReplacePendingCurrentExecution(ctx, task.ID, executionID, files); err != nil {
			if errors.Is(err, repository.ErrTaskFileExecutionNotCurrent) || errors.Is(err, repository.ErrTaskFileTaskNotRunning) || errors.Is(err, repository.ErrTaskFileManifestState) {
				return fmt.Errorf("%w: %v", ErrTaskArtifactExecutionConflict, err)
			}
			return fmt.Errorf("%w: %v", ErrTaskArtifactPersistence, err)
		}
		return nil
	}
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		for _, file := range files {
			if _, err := tx.TaskFiles().Upsert(ctx, file); err != nil {
				return fmt.Errorf("%w: %v", ErrTaskArtifactPersistence, err)
			}
		}
		return nil
	})
}

func buildTaskArtifactStoragePrefix(task *model.Task, executionID string) string {
	segments := []string{"uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID}
	if executionID != "" {
		segments = append(segments, "executions", executionID)
	}
	return path.Join(append(segments, "artifacts")...) + "/"
}

func buildTaskArtifactStorageKey(task *model.Task, executionID, relPath string) string {
	return buildTaskArtifactStoragePrefix(task, executionID) + filepath.ToSlash(relPath)
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
