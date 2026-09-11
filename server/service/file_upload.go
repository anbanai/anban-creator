package service

import (
	"context"
	"fmt"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	FileUploadPurposeLiveAudio = "live_audio"
	// Direct-upload credentials are execution-scoped. Keep their lifetime no
	// longer than the maximum execution credential lifetime so a leaked URL
	// cannot outlive the task authorization that created it.
	defaultFileUploadURLTTL = int(auth.MaximumExecutionTokenLifetime / time.Second)
	maxFileUploadURLTTL     = defaultFileUploadURLTTL
)

type fileUploadPurposePolicy struct {
	keyPrefix string
	validate  func(string) bool
}

var fileUploadPurposePolicies = map[string]fileUploadPurposePolicy{
	FileUploadPurposeLiveAudio: {keyPrefix: "uploads/live-audio/", validate: isAllowedAudioContentType},
}

type PreparedFileUpload struct {
	Key           string            `json:"key"`
	UploadURL     string            `json:"upload_url"`
	DownloadURL   string            `json:"download_url"`
	PublicURL     string            `json:"public_url"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	ExpiresSecond int               `json:"expires_seconds"`
	Size          int64             `json:"size"`
	MaxSize       int64             `json:"max_size"`
}

type FileUploadPrepareRequest struct {
	UserID         string `json:"-"`
	ProjectID      string `json:"-"`
	TaskID         string `json:"-"`
	Purpose        string `json:"purpose"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	Size           int64  `json:"size"`
	ExpiresSeconds int    `json:"expires_seconds,omitempty"`
}

const maxExecutionLiveAudioBytes = 100 * 1024 * 1024

type FileUploadService struct {
	store storage.Provider
}

func NewFileUploadService(store storage.Provider) *FileUploadService {
	return &FileUploadService{store: store}
}

func (s *FileUploadService) Prepare(ctx context.Context, req FileUploadPrepareRequest) (*PreparedFileUpload, error) {
	if s == nil {
		return nil, fmt.Errorf("storage provider is not available; configure OSS storage")
	}
	return PrepareFileUpload(ctx, s.store, req)
}

// PrepareForExecution creates an upload target bound to one authenticated
// execution. The legacy Prepare method remains available to direct service
// callers, while MCP execution requests use this bounded form.
func (s *FileUploadService) PrepareForExecution(ctx context.Context, userID, projectID, taskID string, req FileUploadPrepareRequest) (*PreparedFileUpload, error) {
	req.UserID = strings.TrimSpace(userID)
	req.ProjectID = strings.TrimSpace(projectID)
	req.TaskID = strings.TrimSpace(taskID)
	return s.Prepare(ctx, req)
}

func PrepareFileUpload(ctx context.Context, store storage.Provider, req FileUploadPrepareRequest) (*PreparedFileUpload, error) {
	if store == nil {
		return nil, fmt.Errorf("storage provider is not available; configure OSS storage")
	}
	if store.Name() != "oss" {
		return nil, fmt.Errorf("prepare_file_upload requires OSS storage")
	}

	purpose := strings.TrimSpace(req.Purpose)
	policy, ok := fileUploadPurposePolicies[purpose]
	if !ok {
		return nil, fmt.Errorf("unsupported upload purpose %q", purpose)
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		return nil, fmt.Errorf("filename is required")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if !safeUploadExtension(ext) {
		ext = ""
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = audioContentType(ext)
	}
	if !policy.validate(contentType) {
		return nil, fmt.Errorf("audio content_type is required for %s uploads", purpose)
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	if req.UserID == "" || req.ProjectID == "" || req.TaskID == "" {
		return nil, fmt.Errorf("upload identity is incomplete")
	}
	for name, value := range map[string]string{"user_id": req.UserID, "project_id": req.ProjectID, "task_id": req.TaskID} {
		if !safeUploadScopeSegment(value) {
			return nil, fmt.Errorf("invalid upload %s", name)
		}
	}
	if req.Size <= 0 {
		return nil, fmt.Errorf("file size is required")
	}
	if req.Size > maxExecutionLiveAudioBytes {
		return nil, fmt.Errorf("file size exceeds the %d MB limit", maxExecutionLiveAudioBytes/(1024*1024))
	}
	if ext == "" {
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			ext = strings.ToLower(exts[0])
		}
	}
	if ext == "" {
		ext = ".bin"
	}

	expires := req.ExpiresSeconds
	if expires <= 0 {
		expires = defaultFileUploadURLTTL
	}
	if expires > maxFileUploadURLTTL {
		return nil, fmt.Errorf("expires_seconds exceeds the %d second execution limit", maxFileUploadURLTTL)
	}
	keyPrefix := policy.keyPrefix + req.UserID + "/" + req.ProjectID + "/" + req.TaskID + "/"
	key := fmt.Sprintf("%s%s%s", keyPrefix, uuid.NewString(), ext)
	sized, ok := store.(storage.ContentLengthUploadURLProvider)
	if !ok {
		return nil, fmt.Errorf("storage provider cannot bind upload size")
	}
	uploadURL, err := sized.UploadURLWithContentLength(ctx, key, contentType, req.Size, expires)
	if err != nil {
		return nil, fmt.Errorf("create signed upload URL: %w", err)
	}
	downloadURL, err := store.DownloadURL(ctx, key, expires)
	if err != nil {
		return nil, fmt.Errorf("create signed download URL: %w", err)
	}
	headers := map[string]string{"Content-Type": contentType}
	// The signed URL binds both content type and exact byte length.
	headers["Content-Length"] = strconv.FormatInt(req.Size, 10)
	return &PreparedFileUpload{
		Key:           key,
		UploadURL:     uploadURL,
		DownloadURL:   downloadURL,
		PublicURL:     store.GetURL(key),
		Method:        "PUT",
		Headers:       headers,
		ExpiresSecond: expires,
		Size:          req.Size,
		MaxSize:       maxExecutionLiveAudioBytes,
	}, nil
}

func safeUploadScopeSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || len(value) > 128 || strings.ContainsAny(value, "/\\") {
		return false
	}
	return unsafeFilenameRunes.ReplaceAllString(value, "") == value
}

func safeUploadExtension(ext string) bool {
	if len(ext) < 2 || len(ext) > 9 || ext[0] != '.' {
		return false
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func isAllowedAudioContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "audio/aac", "audio/flac", "audio/mpeg", "audio/mp4", "audio/ogg", "audio/wav", "audio/wave", "audio/x-wav":
		return true
	default:
		return false
	}
}
