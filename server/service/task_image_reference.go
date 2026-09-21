package service

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"path"
	"path/filepath"
	"strings"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

const maxTaskImageReferenceBytes int64 = 10 << 20

type taskImageReferenceUse uint8

const (
	taskImageReferenceGeneration taskImageReferenceUse = iota
	taskImageReferenceAnalysis
	taskImageReferenceTransform
)

func (s *TaskService) materializeAuthorizedTaskImageReference(
	ctx context.Context,
	task *model.Task,
	userID, projectID, executionID, logicalPath string,
	use taskImageReferenceUse,
	maxBytes int64,
) (string, func(), error) {
	if s == nil || s.repo == nil || s.store == nil {
		return "", nil, errors.New("task image storage is not available")
	}
	if task == nil || task.ID == "" {
		return "", nil, errors.New("task is required to resolve an image reference")
	}
	if err := s.ValidateAgentExecutionAccess(ctx, userID, projectID, task.ID, executionID); err != nil {
		return "", nil, fmt.Errorf("authorize task image execution: %w", err)
	}

	cleanPath, err := cleanAuthorizedTaskImagePath(logicalPath)
	if err != nil {
		return "", nil, err
	}
	if cleanPath == serveragent.TaskReferenceImagePath {
		return s.materializeTaskReferenceAsset(ctx, task, maxBytes)
	}
	if cleanPath == serveragent.ProjectPortraitReferenceImagePath {
		asset, err := resolveProjectPortraitReferenceAsset(ctx, s.repo, task)
		if err != nil {
			return "", nil, err
		}
		if asset == nil {
			return "", nil, errors.New("task has no project portrait reference image")
		}
		return s.materializeAuthorizedImageObject(ctx, asset.StorageKey, asset.ContentType, asset.Size, cleanPath, maxBytes)
	}
	if cleanPath == serveragent.ProjectStyleReferenceImagePath {
		if use != taskImageReferenceAnalysis {
			return "", nil, errors.New("project style reference is prompt-only and cannot be passed to image generation or transforms")
		}
		return s.materializeProjectStyleReferenceAsset(ctx, task, maxBytes)
	}
	if attachment, ok := frozenImageAttachmentForPath(task.InputAttachments.Data(), cleanPath); ok {
		return s.materializeFrozenImageAttachment(ctx, task, attachment, cleanPath, maxBytes)
	}
	if attachment, ok := resumeImageAttachmentForPath(task.InputAttachments.Data(), cleanPath); ok {
		return s.materializeResumeImageAttachment(ctx, task, attachment, cleanPath, maxBytes)
	}
	if file, err := s.currentExecutionImageFile(ctx, task.ID, executionID, cleanPath); err != nil {
		return "", nil, err
	} else if file != nil {
		return s.materializeTaskImageFile(ctx, file, cleanPath, maxBytes)
	}
	file, err := s.repo.TaskFiles().FindExisting(ctx, task.ID, cleanPath)
	if err != nil {
		return "", nil, fmt.Errorf("find delivered task image %q: %w", cleanPath, err)
	}
	if file != nil {
		return s.materializeTaskImageFile(ctx, file, cleanPath, maxBytes)
	}
	return "", nil, fmt.Errorf("image reference %q is not an authorized task-relative image", cleanPath)
}

func resumeImageAttachmentForPath(attachments []model.EntryAttachment, logicalPath string) (model.EntryAttachment, bool) {
	for _, attachment := range attachments {
		if attachment.Role != model.EntryAttachmentRoleResumeFile || attachment.Type != "image" || !strings.HasPrefix(normalizedImageContentType(attachment.ContentType), "image/") {
			continue
		}
		workspacePath, err := serveragent.ResumeAttachmentWorkspacePath(attachment.FileName)
		if err == nil && workspacePath == logicalPath {
			return attachment, true
		}
	}
	return model.EntryAttachment{}, false
}

func (s *TaskService) materializeResumeImageAttachment(ctx context.Context, task *model.Task, attachment model.EntryAttachment, logicalPath string, maxBytes int64) (string, func(), error) {
	parsed, err := storage.ParseRuntimeStorageKey(attachment.Key)
	if err != nil {
		return "", nil, fmt.Errorf("authorize resume image %q: %w", logicalPath, err)
	}
	prefix := path.Join("uploads/users", task.UserID, "projects", task.ProjectID, "tasks", task.ID, "resume") + "/"
	relative := strings.TrimPrefix(parsed.Key, prefix)
	parts := strings.Split(relative, "/")
	if relative == parsed.Key || len(parts) != 3 || parts[0] == "" || parts[1] != "attachments" || parts[2] != attachment.FileName {
		return "", nil, fmt.Errorf("authorize resume image %q: storage key is outside the task resume namespace", logicalPath)
	}
	return s.materializeAuthorizedImageObject(ctx, parsed.Key, attachment.ContentType, attachment.Size, logicalPath, maxBytes)
}

func cleanAuthorizedTaskImagePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	normalized := strings.ReplaceAll(raw, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "//") ||
		(len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/') || filepath.IsAbs(raw) {
		return "", errors.New("image reference must be an authorized task-relative path")
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", errors.New("image reference task-relative path must not contain directory traversal")
		}
	}
	clean, err := CleanTaskFileRelativePath(normalized)
	if err != nil {
		return "", fmt.Errorf("image reference must be an authorized task-relative path: %w", err)
	}
	return filepath.ToSlash(clean), nil
}

func frozenImageAttachmentForPath(attachments []model.EntryAttachment, logicalPath string) (model.EntryAttachment, bool) {
	for index, attachment := range attachments {
		if model.IsResumeEntryAttachment(attachment) {
			continue
		}
		if serveragent.InputAttachmentReferencePath(index+1, attachment) == logicalPath {
			return attachment, true
		}
	}
	return model.EntryAttachment{}, false
}

func (s *TaskService) materializeFrozenImageAttachment(ctx context.Context, task *model.Task, attachment model.EntryAttachment, logicalPath string, maxBytes int64) (string, func(), error) {
	assetID := strings.TrimSpace(attachment.AssetID)
	if assetID == "" || strings.TrimSpace(attachment.URL) != "" || strings.TrimSpace(attachment.Key) != "" || strings.TrimSpace(attachment.UploadID) != "" {
		return "", nil, fmt.Errorf("image attachment %q must have one unique immutable asset identity", logicalPath)
	}
	asset, err := NewReferenceAssetService(s.repo, nil, nil).RequireOwned(ctx, task.UserID, assetID, []string{
		DirectUploadPurposeTaskReference,
		DirectUploadPurposeAIEntryAttachment,
		DirectUploadPurposeEcommercePhoto,
	})
	if err != nil {
		return "", nil, fmt.Errorf("authorize image attachment %q: %w", logicalPath, err)
	}
	return s.materializeAuthorizedImageObject(ctx, asset.StorageKey, asset.ContentType, asset.Size, logicalPath, maxBytes)
}

func (s *TaskService) materializeTaskReferenceAsset(ctx context.Context, task *model.Task, maxBytes int64) (string, func(), error) {
	assetID := strings.TrimSpace(task.ReferenceImageAssetID)
	allowed := []string{DirectUploadPurposeTaskReference, DirectUploadPurposeAIEntryAttachment, DirectUploadPurposeProjectPortraitReference}
	if assetID == "" {
		return "", nil, errors.New("task has no direct task reference image")
	}
	asset, err := NewReferenceAssetService(s.repo, nil, nil).RequireOwned(ctx, task.UserID, assetID, allowed)
	if err != nil {
		return "", nil, fmt.Errorf("authorize task reference image: %w", err)
	}
	return s.materializeAuthorizedImageObject(ctx, asset.StorageKey, asset.ContentType, asset.Size, serveragent.TaskReferenceImagePath, maxBytes)
}

func (s *TaskService) materializeProjectStyleReferenceAsset(ctx context.Context, task *model.Task, maxBytes int64) (string, func(), error) {
	assetID := projectStyleReferenceAssetID(task)
	if assetID == "" {
		return "", nil, errors.New("task has no project style reference image")
	}
	asset, err := NewReferenceAssetService(s.repo, nil, nil).RequireOwned(ctx, task.UserID, assetID, []string{DirectUploadPurposeProjectReference, DirectUploadPurposeProjectPortraitReference})
	if err != nil {
		return "", nil, fmt.Errorf("authorize project style reference image: %w", err)
	}
	return s.materializeAuthorizedImageObject(ctx, asset.StorageKey, asset.ContentType, asset.Size, serveragent.ProjectStyleReferenceImagePath, maxBytes)
}

func (s *TaskService) currentExecutionImageFile(ctx context.Context, taskID, executionID, logicalPath string) (*model.TaskFile, error) {
	files, err := s.repo.TaskFiles().FindByExecutionID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("find current execution task images: %w", err)
	}
	for _, file := range files {
		if file != nil && file.TaskID == taskID && file.FilePath == logicalPath &&
			(file.State == model.TaskFileStatePending || file.State == model.TaskFileStateDelivered) {
			return file, nil
		}
	}
	return nil, nil
}

func (s *TaskService) materializeTaskImageFile(ctx context.Context, file *model.TaskFile, logicalPath string, maxBytes int64) (string, func(), error) {
	if file == nil || strings.TrimSpace(file.OSSKey) == "" {
		return "", nil, fmt.Errorf("task image %q has no immutable storage object", logicalPath)
	}
	return s.materializeAuthorizedImageObject(ctx, file.OSSKey, file.MimeType, file.FileSize, logicalPath, maxBytes)
}

func (s *TaskService) materializeAuthorizedImageObject(ctx context.Context, key, contentType string, expectedSize int64, logicalPath string, maxBytes int64) (string, func(), error) {
	key = strings.TrimSpace(key)
	contentType = normalizedImageContentType(contentType)
	if maxBytes <= 0 {
		return "", nil, fmt.Errorf("authorized image %q has no positive size limit", logicalPath)
	}
	if expectedSize > maxBytes {
		return "", nil, fmt.Errorf("authorized image %q: %w", logicalPath, storage.ErrObjectExceedsMaxSize)
	}
	if key == "" || expectedSize <= 0 || !strings.HasPrefix(contentType, "image/") {
		return "", nil, fmt.Errorf("authorized image %q has invalid storage metadata", logicalPath)
	}
	data, err := storage.ReadObject(ctx, s.store, key, maxBytes)
	if err != nil {
		return "", nil, fmt.Errorf("read authorized image %q: %w", logicalPath, err)
	}
	if int64(len(data)) != expectedSize {
		return "", nil, fmt.Errorf("authorized image %q size mismatch: read %d bytes, expected %d", logicalPath, len(data), expectedSize)
	}
	detected, _, err := validateRasterImageSafety(data, maxBytes)
	if err != nil {
		return "", nil, fmt.Errorf("authorized image %q content is not an image: %w", logicalPath, err)
	}
	if detected != contentType {
		return "", nil, fmt.Errorf("authorized image %q declared MIME %q does not match raster MIME %q", logicalPath, contentType, detected)
	}
	// Canonical runtime paths intentionally use stable names (for example,
	// task-reference.png) regardless of the user's original upload format.
	// Give downstream providers a physical suffix matching the actual bytes so
	// multipart/MIME inference cannot reinterpret a JPEG as PNG.
	return writeTaskImageTemp(data, logicalImagePathForMIME(logicalPath, detected))
}

func logicalImagePathForMIME(logicalPath, mimeType string) string {
	ext := ""
	switch normalizedImageContentType(mimeType) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}
	if ext == "" {
		return logicalPath
	}
	return strings.TrimSuffix(logicalPath, filepath.Ext(logicalPath)) + ext
}

func normalizedImageContentType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err == nil {
		return strings.ToLower(strings.TrimSpace(mediaType))
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}
