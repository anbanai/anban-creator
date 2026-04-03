package storage

import (
	"context"
	"io"
)

// UploadResult holds the result of a file upload.
type UploadResult struct {
	URL      string // Public URL or local path
	Key      string // Storage key (OSS object key or relative local path)
	Size     int64
	MimeType string
}

// Provider is the interface for file storage backends.
type Provider interface {
	Name() string
	Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*UploadResult, error)
	UploadFile(ctx context.Context, key string, filePath string, contentType string) (*UploadResult, error)
	GetURL(key string) string
	Delete(ctx context.Context, key string) error
	DownloadURL(ctx context.Context, key string, expirySeconds int) (string, error)
}
