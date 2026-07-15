package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// LocalProvider implements Provider using the local filesystem.
type LocalProvider struct {
	dataDir string
	logger  *zerolog.Logger
	mu      sync.RWMutex
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
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.upload(key, reader, contentType)
}

func (p *LocalProvider) upload(key string, reader io.Reader, contentType string) (*UploadResult, error) {
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

// UploadURL is not supported by local storage because direct uploads need OSS.
func (p *LocalProvider) UploadURL(context.Context, string, string, int) (string, error) {
	return "", fmt.Errorf("signed upload URLs require OSS storage")
}

// GetURL returns the relative URL path for serving the file via the API.
func (p *LocalProvider) GetURL(key string) string {
	return "/api/v1/files/" + key
}

// Read reads a file from local storage by key and returns its content.
func (p *LocalProvider) Read(_ context.Context, key string) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
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

// StatObject returns local file metadata without reading its contents.
func (p *LocalProvider) StatObject(_ context.Context, key string) (*ObjectInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	destPath, err := p.safePath(key)
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}
	info, err := os.Stat(destPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrObjectNotFound, key)
		}
		return nil, fmt.Errorf("stat file %s: %w", destPath, err)
	}
	file, err := os.Open(destPath)
	if err != nil {
		return nil, fmt.Errorf("open file %s for ETag: %w", destPath, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, fmt.Errorf("hash file %s: %w", destPath, err)
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(destPath)))
	return &ObjectInfo{Key: key, Size: info.Size(), MimeType: contentType, ContentType: contentType, ETag: fmt.Sprintf("\"%x\"", hash.Sum(nil))}, nil
}

// PromoteObject conditionally copies a local object to an immutable final path.
func (p *LocalProvider) PromoteObject(_ context.Context, sourceKey, finalKey, expectedETag string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	sourcePath, err := p.safePath(sourceKey)
	if err != nil {
		return fmt.Errorf("invalid source key: %w", err)
	}
	finalPath, err := p.safePath(finalKey)
	if err != nil {
		return fmt.Errorf("invalid final key: %w", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrObjectNotFound, sourceKey)
		}
		return fmt.Errorf("open promotion source %s: %w", sourcePath, err)
	}
	defer source.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return fmt.Errorf("hash promotion source %s: %w", sourcePath, err)
	}
	actualETag := fmt.Sprintf("\"%x\"", hash.Sum(nil))
	if actualETag != expectedETag {
		return fmt.Errorf("%w: %s", ErrPromotionPreconditionFailed, sourceKey)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind promotion source %s: %w", sourcePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return fmt.Errorf("create final object directory: %w", err)
	}
	dest, err := os.OpenFile(finalPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%w: %s", ErrObjectAlreadyExists, finalKey)
		}
		return fmt.Errorf("create final object %s: %w", finalPath, err)
	}
	if _, err := io.Copy(dest, source); err != nil {
		_ = dest.Close()
		_ = os.Remove(finalPath)
		return fmt.Errorf("copy final object %s: %w", finalPath, err)
	}
	if err := dest.Close(); err != nil {
		_ = os.Remove(finalPath)
		return fmt.Errorf("close final object %s: %w", finalPath, err)
	}
	return nil
}

// ReadObject reads a local object with a hard memory bound.
func (p *LocalProvider) ReadObject(_ context.Context, key string, maxBytes int64) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	destPath, err := p.safePath(key)
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}
	file, err := os.Open(destPath)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", destPath, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", destPath, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: key=%s size>%d", ErrObjectExceedsMaxSize, key, maxBytes)
	}
	return data, nil
}

// Delete removes the file from local storage.
// If the file does not exist, a warning is logged but no error is returned.
func (p *LocalProvider) Delete(_ context.Context, key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
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
