package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

const maxTaskArtifactUploadBytes = 512 * 1024 * 1024

type taskArtifactObjectStatProvider interface {
	StatObject(ctx context.Context, key string) (*storage.ObjectInfo, error)
}

type TaskArtifactPrepareRequest struct {
	TaskID       string `json:"task_id"`
	RelativePath string `json:"relative_path"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type TaskArtifactManifestRequest struct {
	TaskID string                     `json:"task_id"`
	Files  []TaskArtifactManifestFile `json:"files"`
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

func (s *TaskService) PrepareTaskArtifactUpload(ctx context.Context, taskID, authenticatedUserID string, cfg DirectUploadConfig, req TaskArtifactPrepareRequest) (*DirectUploadPrepareResult, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("storage provider is not available")
	}
	if s.store.Name() != "oss" {
		return nil, fmt.Errorf("task artifact direct uploads require OSS storage")
	}
	taskID = firstNonEmptyString(strings.TrimSpace(taskID), strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	task, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID)
	if err != nil {
		return nil, err
	}
	relPath, err := cleanTaskArtifactRelativePath(task, req.RelativePath)
	if err != nil {
		return nil, err
	}
	if req.Size <= 0 {
		return nil, fmt.Errorf("file size is required")
	}
	if req.Size > maxTaskArtifactUploadBytes {
		return nil, fmt.Errorf("file size exceeds the %d MB limit", maxTaskArtifactUploadBytes/(1024*1024))
	}
	if req.SHA256 != "" && !validTaskArtifactSHA256(req.SHA256) {
		return nil, fmt.Errorf("sha256 must be a 64-character hex string")
	}

	contentType := normalizeTaskArtifactContentType(req.ContentType, relPath)
	key := buildTaskArtifactStorageKey(task, relPath)
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
		return nil, fmt.Errorf("create signed upload URL: %w", err)
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
		return nil, fmt.Errorf("issue upload credential: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("upload credential issuer returned no credential")
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

func (s *TaskService) FinalizeTaskArtifactManifest(ctx context.Context, taskID, authenticatedUserID string, req TaskArtifactManifestRequest) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("task service repository is not available")
	}
	if s.store == nil {
		return fmt.Errorf("storage provider is not available")
	}
	taskID = firstNonEmptyString(strings.TrimSpace(taskID), strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if strings.TrimSpace(req.TaskID) != "" && req.TaskID != taskID {
		return fmt.Errorf("manifest task_id does not match request task_id")
	}
	task, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID)
	if err != nil {
		return err
	}
	prefix := buildTaskArtifactStoragePrefix(task)
	for _, file := range req.Files {
		relPath, err := cleanTaskArtifactRelativePath(task, file.RelativePath)
		if err != nil {
			return err
		}
		objectKey := strings.TrimPrefix(strings.TrimSpace(file.ObjectKey), "/")
		if objectKey == "" {
			return fmt.Errorf("object_key is required for %s", relPath)
		}
		if !strings.HasPrefix(objectKey, prefix) {
			return fmt.Errorf("object key %q is outside task artifact prefix %q", objectKey, prefix)
		}
		expectedKey := buildTaskArtifactStorageKey(task, relPath)
		if objectKey != expectedKey {
			return fmt.Errorf("object key %q does not match relative path %q", objectKey, relPath)
		}
		if !validTaskArtifactSHA256(file.SHA256) {
			return fmt.Errorf("sha256 must be a 64-character hex string for %s", relPath)
		}
		contentType := normalizeTaskArtifactContentType(file.ContentType, relPath)
		size := file.Size
		if statProvider, ok := s.store.(taskArtifactObjectStatProvider); ok {
			stat, err := statProvider.StatObject(ctx, objectKey)
			if err != nil {
				return fmt.Errorf("stat task artifact %s: %w", objectKey, err)
			}
			if stat.Size > 0 {
				if size > 0 && stat.Size != size {
					return fmt.Errorf("task artifact %s size mismatch: manifest=%d storage=%d", relPath, size, stat.Size)
				}
				size = stat.Size
			}
			if statContentType := firstNonEmptyString(stat.ContentType, stat.MimeType); statContentType != "" {
				contentType = statContentType
			}
		}
		if size <= 0 {
			return fmt.Errorf("file size is required for %s", relPath)
		}

		filename := filepath.Base(relPath)
		role := DetermineTaskFileRole(filename, contentType)
		if normalizedRole := strings.TrimSpace(file.Role); normalizedRole != "" {
			role = normalizedRole
		}
		taskFile := &model.TaskFile{
			TaskID:          task.ID,
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
		if _, err := s.repo.TaskFiles().Upsert(ctx, taskFile); err != nil {
			return fmt.Errorf("persist task artifact %s: %w", relPath, err)
		}
	}
	return nil
}

func buildTaskArtifactStoragePrefix(task *model.Task) string {
	return path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID, "artifacts") + "/"
}

func buildTaskArtifactStorageKey(task *model.Task, relPath string) string {
	return buildTaskArtifactStoragePrefix(task) + filepath.ToSlash(relPath)
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
