package service

import (
	"context"
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/storage"
)

const (
	FileUploadPurposeVideoAudio = "video_audio"
	defaultFileUploadURLTTL     = 24 * 3600
)

type PreparedFileUpload struct {
	Key           string            `json:"key"`
	UploadURL     string            `json:"upload_url"`
	DownloadURL   string            `json:"download_url"`
	PublicURL     string            `json:"public_url"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers"`
	ExpiresSecond int               `json:"expires_seconds"`
}

type FileUploadPrepareRequest struct {
	Purpose        string `json:"purpose"`
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	ExpiresSeconds int    `json:"expires_seconds,omitempty"`
}

func PrepareFileUpload(ctx context.Context, store storage.Provider, req FileUploadPrepareRequest) (*PreparedFileUpload, error) {
	if store == nil {
		return nil, fmt.Errorf("storage provider is not available; configure OSS storage")
	}
	if store.Name() != "oss" {
		return nil, fmt.Errorf("prepare_file_upload requires OSS storage")
	}

	purpose := strings.TrimSpace(req.Purpose)
	if purpose != FileUploadPurposeVideoAudio {
		return nil, fmt.Errorf("unsupported upload purpose %q", purpose)
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		return nil, fmt.Errorf("filename is required")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = audioContentType(ext)
	}
	if !isAllowedVideoAudioContentType(contentType) {
		return nil, fmt.Errorf("audio content_type is required for video_audio uploads")
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
	key := fmt.Sprintf("uploads/video-audio/%s%s", uuid.NewString(), ext)
	uploadURL, err := store.UploadURL(ctx, key, contentType, expires)
	if err != nil {
		return nil, fmt.Errorf("create signed upload URL: %w", err)
	}
	downloadURL, err := store.DownloadURL(ctx, key, expires)
	if err != nil {
		return nil, fmt.Errorf("create signed download URL: %w", err)
	}
	return &PreparedFileUpload{
		Key:           key,
		UploadURL:     uploadURL,
		DownloadURL:   downloadURL,
		PublicURL:     store.GetURL(key),
		Method:        "PUT",
		Headers:       map[string]string{"Content-Type": contentType},
		ExpiresSecond: expires,
	}, nil
}

func isAllowedVideoAudioContentType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "audio/aac", "audio/flac", "audio/mpeg", "audio/mp4", "audio/ogg", "audio/wav", "audio/wave", "audio/x-wav":
		return true
	default:
		return false
	}
}
