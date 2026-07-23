package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type TaskArtifactStreamRequest struct {
	RelativePath string
	ContentType  string
	Size         int64
	SHA256       string
	Body         io.Reader
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
	if req.Size > maxTaskArtifactUploadBytes {
		return nil, taskArtifactInvalidf("file size exceeds the %d MB limit", maxTaskArtifactUploadBytes/(1024*1024))
	}
	wantSHA256 := strings.ToLower(strings.TrimSpace(req.SHA256))
	if !validTaskArtifactSHA256(wantSHA256) {
		return nil, taskArtifactInvalidf("sha256 must be a 64-character hex string")
	}

	task, err := s.ValidateAgentTaskAccess(ctx, taskID, authenticatedUserID)
	if err != nil {
		return nil, err
	}
	executionID, err := s.validateTaskArtifactExecution(ctx, task, authenticatedUserID, authenticatedExecutionID, authenticatedExecutionID)
	if err != nil {
		return nil, err
	}
	relativePath, err := cleanTaskArtifactRelativePath(task, req.RelativePath)
	if err != nil {
		return nil, taskArtifactInvalidf("%v", err)
	}
	contentType := normalizeTaskArtifactContentType(req.ContentType, relativePath)
	objectKey := buildTaskArtifactStorageKey(task, executionID, relativePath)

	hash := sha256.New()
	counter := &artifactByteCounter{}
	bounded := io.LimitReader(req.Body, req.Size+1)
	stream := io.TeeReader(bounded, io.MultiWriter(hash, counter))
	upload, err := s.store.Upload(ctx, objectKey, stream, contentType)
	if err != nil {
		cleanupErr := s.store.Delete(ctx, objectKey)
		if cleanupErr != nil {
			return nil, fmt.Errorf("%w: upload task artifact: %v; cleanup: %v", ErrTaskArtifactUnavailable, err, cleanupErr)
		}
		return nil, fmt.Errorf("%w: upload task artifact: %v", ErrTaskArtifactUnavailable, err)
	}

	storedKey := objectKey
	if upload != nil && strings.TrimSpace(upload.Key) != "" {
		storedKey = strings.TrimSpace(upload.Key)
	}
	actualSHA256 := hex.EncodeToString(hash.Sum(nil))
	var validationErr error
	switch {
	case storedKey != objectKey:
		validationErr = taskArtifactInvalidf("storage returned an unexpected object key")
	case counter.n < req.Size:
		validationErr = taskArtifactInvalidf("artifact body is shorter than declared size")
	case counter.n > req.Size:
		validationErr = taskArtifactInvalidf("artifact body exceeds declared size")
	case actualSHA256 != wantSHA256:
		validationErr = taskArtifactInvalidf("artifact sha256 does not match request")
	}
	if validationErr != nil {
		cleanupErr := s.store.Delete(ctx, storedKey)
		if storedKey != objectKey {
			if err := s.store.Delete(ctx, objectKey); cleanupErr == nil {
				cleanupErr = err
			}
		}
		if cleanupErr != nil {
			return nil, fmt.Errorf("%w: %v; cleanup failed: %v", ErrTaskArtifactUnavailable, validationErr, cleanupErr)
		}
		return nil, validationErr
	}

	return &TaskArtifactStreamResult{
		ObjectKey: objectKey, ContentType: contentType, Size: counter.n, SHA256: actualSHA256,
	}, nil
}
