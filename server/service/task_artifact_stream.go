package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

const (
	taskArtifactCleanupTimeout = 30 * time.Second
	taskArtifactStreamTTL      = time.Hour
)

type TaskArtifactStreamRequest struct {
	RelativePath string
	ContentType  string
	Size         int64
	SHA256       string
	Body         io.Reader
	// SetReadDeadline is a transport hook invoked only after task/execution/path/size validation.
	SetReadDeadline func(time.Time) error
}

type TaskArtifactStreamResult struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type artifactByteCounter struct{ n int64 }

func (c *artifactByteCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// StreamTaskArtifact writes one execution-scoped artifact through the
// configured storage provider while bounding and hashing the request stream.
func (s *TaskService) StreamTaskArtifact(ctx context.Context, taskID, authenticatedUserID, authenticatedExecutionID string, req TaskArtifactStreamRequest) (*TaskArtifactStreamResult, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("%w: storage provider is not available", ErrTaskArtifactUnavailable)
	}
	taskID = strings.TrimSpace(taskID)
	authenticatedUserID = strings.TrimSpace(authenticatedUserID)
	authenticatedExecutionID = strings.TrimSpace(authenticatedExecutionID)
	if taskID == "" || authenticatedUserID == "" || authenticatedExecutionID == "" {
		return nil, fmt.Errorf("%w: task, user, and execution identity are required", ErrTaskArtifactExecutionConflict)
	}
	if req.Body == nil {
		return nil, taskArtifactInvalidf("artifact body is required")
	}
	if req.Size <= 0 {
		return nil, taskArtifactInvalidf("file size is required")
	}
	wantSHA256 := strings.ToLower(strings.TrimSpace(req.SHA256))
	if !validTaskArtifactSHA256(wantSHA256) {
		return nil, taskArtifactInvalidf("sha256 must be a 64-character hex string")
	}

	task, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID)
	if err != nil {
		return nil, err
	}
	executionID, err := s.validateTaskArtifactExecution(ctx, task, authenticatedUserID, authenticatedExecutionID)
	if err != nil {
		return nil, err
	}
	relativePath, err := cleanTaskArtifactRelativePath(task, req.RelativePath)
	if err != nil {
		return nil, taskArtifactInvalidf("%v", err)
	}
	if req.Size > taskArtifactByteLimit(task, relativePath) {
		return nil, taskArtifactInvalidf("file size exceeds scoped artifact limit")
	}
	if model.IsHypitPlatform(task.Type) && (relativePath == "output/project.zip" || relativePath == "output/final.mp4") {
		boundedCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		ctx = boundedCtx
		deadline, _ := ctx.Deadline()
		if req.SetReadDeadline != nil {
			if err := req.SetReadDeadline(deadline); err != nil {
				return nil, fmt.Errorf("%w: set scoped artifact stream deadline", ErrTaskArtifactUnavailable)
			}
		}
	}
	contentType := normalizeTaskArtifactContentType(req.ContentType, relativePath)
	attemptID := uuid.NewString()
	objectKey := buildTaskArtifactStagingStorageKey(task, executionID, wantSHA256, attemptID, relativePath)
	now := time.Now()
	if err := s.repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: attemptID, UserID: authenticatedUserID, Purpose: DirectUploadPurposeTaskArtifact,
		StagingKey: objectKey, FileName: filepath.Base(relativePath), ContentType: contentType,
		Size: req.Size, Status: model.UploadSessionPending, ExpiresAt: now.Add(taskArtifactStreamTTL),
	}); err != nil {
		return nil, fmt.Errorf("%w: record task artifact stream attempt: %w", ErrTaskArtifactUnavailable, err)
	}

	hash := sha256.New()
	counter := &artifactByteCounter{}
	bounded := io.LimitReader(req.Body, req.Size+1)
	stream := io.TeeReader(bounded, io.MultiWriter(hash, counter))
	upload, err := s.store.Upload(ctx, objectKey, stream, contentType)
	if err != nil {
		uploadErr := fmt.Errorf("%w: upload task artifact: %w", ErrTaskArtifactUnavailable, err)
		return nil, s.failTaskArtifactAttempt(ctx, attemptID, objectKey, uploadErr)
	}
	uploadedBytes := counter.n
	probe := []byte{0}
	probeBytes, probeErr := io.ReadFull(stream, probe)

	storedKey := objectKey
	if upload != nil && strings.TrimSpace(upload.Key) != "" {
		storedKey = strings.TrimSpace(upload.Key)
	}
	actualSHA256 := hex.EncodeToString(hash.Sum(nil))
	var validationErr error
	switch {
	case storedKey != objectKey:
		validationErr = taskArtifactInvalidf("storage returned an unexpected object key")
	case probeBytes > 0 && uploadedBytes < req.Size:
		validationErr = taskArtifactInvalidf("storage provider did not consume the declared artifact body")
	case probeBytes > 0:
		validationErr = taskArtifactInvalidf("artifact body exceeds declared size")
	case probeErr != nil && !errors.Is(probeErr, io.EOF):
		validationErr = fmt.Errorf("%w: verify artifact body completion: %w", ErrTaskArtifactUnavailable, probeErr)
	case uploadedBytes < req.Size:
		validationErr = taskArtifactInvalidf("artifact body is shorter than declared size")
	case uploadedBytes > req.Size:
		validationErr = taskArtifactInvalidf("artifact body exceeds declared size")
	case actualSHA256 != wantSHA256:
		validationErr = taskArtifactInvalidf("artifact sha256 does not match request")
	}
	if validationErr != nil {
		return nil, s.failTaskArtifactAttempt(ctx, attemptID, objectKey, validationErr)
	}
	if _, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID); err != nil {
		return nil, s.failTaskArtifactAttempt(ctx, attemptID, objectKey, err)
	}

	return &TaskArtifactStreamResult{
		ObjectKey: objectKey, ContentType: contentType, Size: uploadedBytes, SHA256: actualSHA256,
	}, nil
}

func (s *TaskService) failTaskArtifactAttempt(ctx context.Context, attemptID, objectKey string, original error) error {
	expireCtx, expireCancel := context.WithTimeout(context.WithoutCancel(ctx), taskArtifactCleanupTimeout)
	expireErr := s.repo.UploadSessions().ScheduleTaskArtifactExpiration(expireCtx, attemptID, time.Now())
	expireCancel()
	if expireErr != nil {
		expireErr = fmt.Errorf("%w: schedule task artifact cleanup: %w", ErrTaskArtifactUnavailable, expireErr)
	}
	return errors.Join(original, expireErr, taskArtifactCleanupError(s.cleanupTaskArtifactObject(ctx, objectKey)))
}

func (s *TaskService) cleanupTaskArtifactObject(ctx context.Context, objectKey string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), taskArtifactCleanupTimeout)
	defer cancel()
	return s.store.Delete(cleanupCtx, objectKey)
}

func taskArtifactCleanupError(cleanup error) error {
	if cleanup == nil {
		return nil
	}
	return fmt.Errorf("%w: cleanup task artifact: %w", ErrTaskArtifactUnavailable, cleanup)
}
