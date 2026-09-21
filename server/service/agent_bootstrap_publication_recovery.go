package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

// Copy verified image bytes into the new execution's immutable namespace. The
// finalizer still consumes only current-execution artifacts and Server-owned
// WeChat metadata; no agent manifest can import another execution's identity.
func (s *AgentBootstrapService) buildPublicationRecoveryImages(ctx context.Context, execution *model.TaskExecution, task *model.Task, deadline time.Time) ([]BootstrapFile, []*model.TaskFile, error) {
	if execution.Purpose != model.TaskExecutionPurposePublicationRecovery {
		return nil, nil, nil
	}
	source, err := s.repo.TaskExecutions().FindByID(ctx, execution.ParentExecutionID)
	if err != nil {
		return nil, nil, err
	}
	if source.TaskID != task.ID || source.ID == execution.ID || !isTerminalExecution(source.Status) {
		return nil, nil, fmt.Errorf("%w: invalid publication recovery image source", ErrAgentBootstrapConflict)
	}
	artifacts, err := s.repo.TaskFiles().FindByExecutionID(ctx, source.ID)
	if err != nil {
		return nil, nil, err
	}
	var files []BootstrapFile
	var images []*model.TaskFile
	for _, original := range artifacts {
		if original.TaskID != task.ID || original.ExecutionID != source.ID || original.State != model.TaskFileStateDelivered || !strings.HasPrefix(original.MimeType, "image/") {
			continue
		}
		cleanPath, pathErr := cleanAuthorizedTaskImagePath(original.FilePath)
		validKey := original.OSSKey == buildTaskMCPArtifactStoragePrefix(task, source.ID)+original.FilePath ||
			original.OSSKey == buildTaskArtifactFinalStorageKey(task, source.ID, original.ContentHash, original.FilePath)
		if pathErr != nil || cleanPath != original.FilePath || !strings.HasPrefix(cleanPath, "output/") || !lowercaseSHA256.MatchString(original.ContentHash) ||
			original.FileSize <= 0 || original.FileSize > maxTaskDeliveryImageBytes || s.cfg.Store == nil ||
			original.StorageProvider != s.cfg.Store.Name() || !validKey {
			return nil, nil, fmt.Errorf("%w: invalid publication recovery image %q", ErrAgentBootstrapConflict, original.FilePath)
		}
		body, err := storage.ReadObject(ctx, s.cfg.Store, original.OSSKey, maxTaskDeliveryImageBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("read publication recovery image: %w", err)
		}
		mimeType, _, imageErr := validateRasterImageSafety(body, maxTaskDeliveryImageBytes)
		if int64(len(body)) != original.FileSize || fmt.Sprintf("%x", sha256.Sum256(body)) != original.ContentHash || imageErr != nil || mimeType != original.MimeType {
			return nil, nil, fmt.Errorf("%w: publication recovery image %q failed integrity validation", ErrAgentBootstrapConflict, original.FilePath)
		}
		key := buildTaskArtifactFinalStorageKey(task, execution.ID, original.ContentHash, original.FilePath)
		if _, err := s.cfg.Store.Upload(ctx, key, bytes.NewReader(body), mimeType); err != nil {
			return nil, nil, fmt.Errorf("copy publication recovery image: %w", err)
		}
		signed, err := s.signedBootstrapObjectKey(ctx, key, deadline)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, BootstrapFile{
			Path: original.FilePath, DownloadURL: signed, ContentSHA256: original.ContentHash,
			Mode: 0644, ExpectedSize: original.FileSize, MaxBytes: maxTaskDeliveryImageBytes,
		})
		images = append(images, &model.TaskFile{
			TaskID: task.ID, ExecutionID: execution.ID, State: model.TaskFileStatePending,
			Role: original.Role, FilePath: original.FilePath, FileName: path.Base(original.FilePath),
			MimeType: mimeType, FileSize: original.FileSize, ContentHash: original.ContentHash,
			MediaID: original.MediaID, WechatURL: original.WechatURL,
			OSSKey: key, OSSURL: s.cfg.Store.GetURL(key), StorageProvider: s.cfg.Store.Name(),
		})
	}
	return files, images, nil
}
