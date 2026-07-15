package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/storage"
)

const defaultMediaSourceTTL = 3600

var mediaSourceHTTPClient = http.DefaultClient

// MediaSourceRequest describes a storage key or URL that a provider consumer
// needs as either bytes or a fetchable URL.
type MediaSourceRequest struct {
	Key         string
	RawURL      string
	TTL         int
	MaxBytes    int64
	ContentType string
}

// MediaSource is the normalized view of a user or agent supplied media source.
type MediaSource struct {
	Key         string
	URL         string
	Bytes       []byte
	Filename    string
	ContentType string
	External    bool
}

// ResolveMediaSource resolves a storage key or URL to a provider-fetchable URL.
// Only URLs owned by the configured storage backend are signed. External HTTPS
// URLs pass through unchanged.
func ResolveMediaSource(ctx context.Context, store storage.Provider, log *zerolog.Logger, req MediaSourceRequest) (*MediaSource, error) {
	ttl := req.TTL
	if ttl <= 0 {
		ttl = defaultMediaSourceTTL
	}
	key := strings.TrimSpace(req.Key)
	rawURL := strings.TrimSpace(req.RawURL)

	if key != "" {
		if store == nil {
			return nil, fmt.Errorf("storage provider is not available")
		}
		url, err := mediaSourceURLForKey(ctx, store, key, ttl)
		if err != nil {
			return nil, err
		}
		return &MediaSource{
			Key:         key,
			URL:         url,
			Filename:    filepath.Base(key),
			ContentType: firstNonEmpty(req.ContentType, audioContentType(strings.ToLower(filepath.Ext(key)))),
		}, nil
	}

	if rawURL == "" {
		return nil, fmt.Errorf("media source key or URL is required")
	}

	if store != nil && store.IsOwnedURL(rawURL) {
		key, ok := storage.StorageKeyFromURL(rawURL)
		if !ok {
			return nil, fmt.Errorf("owned media URL has no storage key")
		}
		url, err := mediaSourceURLForKey(ctx, store, key, ttl)
		if err != nil {
			if log != nil {
				log.Warn().Err(err).Str("url", rawURL).Msg("resolve owned media URL failed")
			}
			return nil, err
		}
		return &MediaSource{
			Key:         key,
			URL:         url,
			Filename:    filepath.Base(key),
			ContentType: firstNonEmpty(req.ContentType, audioContentType(strings.ToLower(filepath.Ext(key)))),
		}, nil
	}

	if !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("external media URL must be an HTTPS URL")
	}
	return &MediaSource{
		URL:         rawURL,
		Filename:    mediaSourceFilename(rawURL),
		ContentType: req.ContentType,
		External:    true,
	}, nil
}

// ResolveMediaSourceBytes resolves a media source and returns its content. Owned
// storage objects are read through the storage provider; external HTTPS URLs are
// downloaded directly with a size cap.
func ResolveMediaSourceBytes(ctx context.Context, store storage.Provider, log *zerolog.Logger, req MediaSourceRequest) (*MediaSource, error) {
	resolved, err := ResolveMediaSource(ctx, store, log, req)
	if err != nil {
		return nil, err
	}
	if resolved.Key != "" {
		if store == nil {
			return nil, fmt.Errorf("storage provider is not available")
		}
		if req.MaxBytes <= 0 {
			return nil, fmt.Errorf("owned media source requires a positive max size")
		}
		data, err := storage.ReadObject(ctx, store, resolved.Key, req.MaxBytes)
		if err != nil {
			return nil, fmt.Errorf("read media object: %w", err)
		}
		resolved.Bytes = data
		return resolved, nil
	}

	data, contentType, err := downloadMediaSourceURL(ctx, resolved.URL, req.MaxBytes)
	if err != nil {
		return nil, err
	}
	resolved.Bytes = data
	if resolved.ContentType == "" {
		resolved.ContentType = contentType
	}
	return resolved, nil
}

func mediaSourceURLForKey(ctx context.Context, store storage.Provider, key string, ttl int) (string, error) {
	if store.HasCustomDomain() {
		return store.GetURL(key), nil
	}
	url, err := store.DownloadURL(ctx, key, ttl)
	if err != nil {
		return "", fmt.Errorf("create signed media URL: %w", err)
	}
	return url, nil
}

func downloadMediaSourceURL(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, "", fmt.Errorf("external media URL must be an HTTPS URL")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create media URL request: %w", err)
	}
	resp, err := mediaSourceHTTPClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("download media URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download media URL: unexpected status %d", resp.StatusCode)
	}
	data, err := readLimitedMediaSource(resp.Body, maxBytes)
	if err != nil {
		return nil, "", err
	}
	contentType := "application/octet-stream"
	if ct := strings.TrimSpace(resp.Header.Get("Content-Type")); ct != "" {
		contentType = strings.Split(ct, ";")[0]
	}
	return data, contentType, nil
}

func readLimitedMediaSource(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(reader)
	}
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("media source exceeds max size %d bytes", maxBytes)
	}
	return data, nil
}

func mediaSourceFilename(rawURL string) string {
	path := strings.Split(rawURL, "?")[0]
	name := filepath.Base(path)
	if name == "." || name == "/" || strings.TrimSpace(name) == "" {
		return "media"
	}
	return name
}
