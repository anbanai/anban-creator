package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
)

// LocalProvider implements Provider using the local filesystem.
type LocalProvider struct {
	dataDir string
	logger  *zerolog.Logger
}

// NewLocalProvider creates a new LocalProvider and ensures dataDir exists.
func NewLocalProvider(dataDir string) (*LocalProvider, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create local storage directory %s: %w", dataDir, err)
	}
	logger := zerolog.Nop()
	return &LocalProvider{
		dataDir: dataDir,
		logger:  &logger,
	}, nil
}

// NewLocalProviderWithLogger creates a new LocalProvider with a custom logger.
func NewLocalProviderWithLogger(dataDir string, logger *zerolog.Logger) (*LocalProvider, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create local storage directory %s: %w", dataDir, err)
	}
	return &LocalProvider{
		dataDir: dataDir,
		logger:  logger,
	}, nil
}

// Name returns the provider name.
func (p *LocalProvider) Name() string {
	return "local"
}

// safePath validates that the resolved path is within dataDir and returns the absolute path.
func (p *LocalProvider) safePath(key string) (string, error) {
	cleanKey := filepath.Clean(key)
	if strings.Contains(cleanKey, "..") {
		return "", fmt.Errorf("invalid key: path traversal detected")
	}
	destPath := filepath.Join(p.dataDir, cleanKey)
	absPath, err := filepath.Abs(destPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absDataDir, err := filepath.Abs(p.dataDir)
	if err != nil {
		return "", fmt.Errorf("invalid data dir: %w", err)
	}
	if !strings.HasPrefix(absPath, absDataDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes data directory")
	}
	return absPath, nil
}

// Upload writes data from reader to {dataDir}/{key}, creating parent directories as needed.
func (p *LocalProvider) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*UploadResult, error) {
	destPath, err := p.safePath(key)
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}
	destDir := filepath.Dir(destPath)

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", destDir, err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer f.Close()

	size, err := io.Copy(f, reader)
	if err != nil {
		return nil, fmt.Errorf("write file %s: %w", destPath, err)
	}

	p.logger.Debug().
		Str("key", key).
		Str("path", destPath).
		Int64("size", size).
		Msg("file uploaded to local storage")

	return &UploadResult{
		URL:      p.GetURL(key),
		Key:      key,
		Size:     size,
		MimeType: contentType,
	}, nil
}

// UploadFile copies a local file into the storage data directory.
func (p *LocalProvider) UploadFile(_ context.Context, key string, filePath string, contentType string) (*UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open source file %s: %w", filePath, err)
	}
	defer f.Close()

	return p.Upload(context.Background(), key, f, contentType)
}

// GetURL returns the relative URL path for serving the file via the API.
func (p *LocalProvider) GetURL(key string) string {
	return "/api/v1/files/" + key
}

// Read reads a file from local storage by key and returns its content.
func (p *LocalProvider) Read(_ context.Context, key string) ([]byte, error) {
	destPath, err := p.safePath(key)
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}
	data, err := os.ReadFile(destPath)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", destPath, err)
	}
	return data, nil
}

// Delete removes the file from local storage.
// If the file does not exist, a warning is logged but no error is returned.
func (p *LocalProvider) Delete(_ context.Context, key string) error {
	destPath, err := p.safePath(key)
	if err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	if err := os.Remove(destPath); err != nil {
		if os.IsNotExist(err) {
			p.logger.Warn().
				Str("key", key).
				Msg("attempted to delete non-existent file")
			return nil
		}
		return fmt.Errorf("delete file %s: %w", destPath, err)
	}

	p.logger.Debug().
		Str("key", key).
		Msg("file deleted from local storage")

	return nil
}

// DownloadURL returns the same URL as GetURL since local files don't need signed URLs.
func (p *LocalProvider) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return p.GetURL(key), nil
}

// HasCustomDomain returns false for local storage (no CDN domain).
func (p *LocalProvider) HasCustomDomain() bool {
	return false
}

// IsOwnedURL reports whether the given URL is a local-storage file URL served
// by this backend. Local URLs are relative paths under /api/v1/files/. Absolute
// URLs are rejected to prevent SSRF.
func (p *LocalProvider) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "/api/v1/files/")
}
