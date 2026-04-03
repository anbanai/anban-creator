package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

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

// Upload writes data from reader to {dataDir}/{key}, creating parent directories as needed.
func (p *LocalProvider) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*UploadResult, error) {
	destPath := filepath.Join(p.dataDir, key)
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

// Delete removes the file from local storage.
// If the file does not exist, a warning is logged but no error is returned.
func (p *LocalProvider) Delete(_ context.Context, key string) error {
	destPath := filepath.Join(p.dataDir, key)

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
