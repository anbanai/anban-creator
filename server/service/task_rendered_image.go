package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

const maxRenderedTaskImageBytes = 10 * 1024 * 1024

var (
	ErrRenderedImageTaskNotFound    = errors.New("rendered image task not found")
	ErrRenderedImageProjectMismatch = errors.New("rendered image task project mismatch")
)

type RegisterRenderedImageRequest struct {
	UserID      string
	ProjectID   string
	TaskID      string
	ExecutionID string
	Name        string
	Role        string
	ImageBase64 string
	FilePath    string
}

type RegisterRenderedImageResult struct {
	TaskFileID  string `json:"task_file_id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	FilePath    string `json:"file_path"`
	MimeType    string `json:"mime_type"`
	FileSize    int64  `json:"file_size"`
	DownloadURL string `json:"download_url,omitempty"`
}

func (s *TaskService) RegisterRenderedImage(ctx context.Context, req RegisterRenderedImageRequest) (*RegisterRenderedImageResult, error) {
	if s == nil {
		return nil, fmt.Errorf("task service is not available")
	}
	if req.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if req.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	task, err := s.ValidateAgentTaskAccess(ctx, req.TaskID, req.UserID)
	if err != nil {
		return nil, ErrRenderedImageTaskNotFound
	}
	if task.ProjectID != req.ProjectID {
		return nil, ErrRenderedImageProjectMismatch
	}

	name, err := CleanTaskFileRelativePath(req.Name)
	if err != nil {
		return nil, fmt.Errorf("invalid name: %w", err)
	}
	role, err := normalizeRenderedTaskImageRole(req.Role)
	if err != nil {
		return nil, err
	}
	data, mimeType, err := loadRenderedTaskImage(req)
	if err != nil {
		return nil, err
	}
	if err := validateRenderedTaskImage(name, mimeType); err != nil {
		return nil, err
	}
	if role == "" {
		role = DetermineTaskFileRole(name, mimeType)
		if role == "" {
			role = model.FileRoleImage
		}
	}

	var file *model.TaskFile
	if req.ExecutionID != "" {
		if err := s.ValidateAgentExecutionAccess(ctx, req.UserID, task.ProjectID, req.TaskID, req.ExecutionID); err != nil {
			return nil, err
		}
		file, err = s.uploadTaskFileFromReader(ctx, task, req.TaskID, req.UserID, req.ExecutionID, name, bytes.NewReader(data), mimeType, int64(len(data)), nil, taskFileUploadOptions{
			Role: role, ContentAddressedObject: true, CleanupOnPersistFailure: true,
		})
	} else {
		file, err = s.uploadTaskFileFromReader(ctx, nil, req.TaskID, req.UserID, "", name, bytes.NewReader(data), mimeType, int64(len(data)), nil, taskFileUploadOptions{
			Role: role, ContentAddressedObject: true, CleanupOnPersistFailure: true,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("register task file: %w", err)
	}
	s.EnrichFilesWithURLs(ctx, []*model.TaskFile{file})
	return &RegisterRenderedImageResult{
		TaskFileID: file.ID, Name: name, Role: file.Role,
		FilePath: firstRenderedTaskImageValue(file.FilePath, name),
		MimeType: mimeType, FileSize: int64(len(data)),
		DownloadURL: firstRenderedTaskImageValue(file.URL, file.OSSURL),
	}, nil
}

func loadRenderedTaskImage(req RegisterRenderedImageRequest) ([]byte, string, error) {
	hasBase64 := strings.TrimSpace(req.ImageBase64) != ""
	hasPath := strings.TrimSpace(req.FilePath) != ""
	if hasBase64 == hasPath {
		return nil, "", fmt.Errorf("provide exactly one of image_base64 or file_path")
	}
	var data []byte
	var err error
	if hasBase64 {
		body := strings.TrimSpace(req.ImageBase64)
		if strings.HasPrefix(body, "data:") {
			comma := strings.Index(body, ",")
			if comma < 0 {
				return nil, "", fmt.Errorf("invalid data URL")
			}
			body = body[comma+1:]
		}
		data, err = base64.StdEncoding.DecodeString(body)
		if err != nil {
			return nil, "", fmt.Errorf("decode image_base64: %w", err)
		}
	} else {
		path := strings.TrimSpace(req.FilePath)
		if strings.ContainsRune(path, 0) || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "data:") || !filepath.IsAbs(path) {
			return nil, "", fmt.Errorf("file_path must be an absolute server-local path")
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, "", fmt.Errorf("stat file_path: %w", statErr)
		}
		if info.IsDir() {
			return nil, "", fmt.Errorf("file_path must be a file")
		}
		if info.Size() > maxRenderedTaskImageBytes {
			return nil, "", fmt.Errorf("rendered image is too large (max %d bytes)", maxRenderedTaskImageBytes)
		}
		data, err = os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, "", fmt.Errorf("read file_path: %w", err)
		}
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("image data is required")
	}
	if len(data) > maxRenderedTaskImageBytes {
		return nil, "", fmt.Errorf("rendered image is too large (max %d bytes)", maxRenderedTaskImageBytes)
	}
	return data, http.DetectContentType(data), nil
}

func normalizeRenderedTaskImageRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	switch role {
	case "", model.FileRoleCover, model.FileRoleImage, model.FileRoleOther:
		return role, nil
	default:
		return "", fmt.Errorf("unsupported role %q", role)
	}
}

func validateRenderedTaskImage(name, mimeType string) error {
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return fmt.Errorf("unsupported image MIME %q", mimeType)
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return nil
	default:
		return fmt.Errorf("unsupported image extension %q", filepath.Ext(name))
	}
}

func firstRenderedTaskImageValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
