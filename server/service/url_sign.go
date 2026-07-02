package service

import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

// DefaultSignedURLTTL is the validity window (seconds) for generated signed URLs.
// It comfortably exceeds the studio's React Query staleTime, so cached responses
// remain usable until a refetch re-signs them.
const DefaultSignedURLTTL = 3600 // 1 hour

// SignURL resolves a stored image URL/key into a directly-fetchable URL for the
// active storage provider:
//   - OSS private bucket (no custom domain): a time-limited signed URL via DownloadURL.
//   - OSS with a custom CDN domain: a permanent public URL via GetURL.
//   - local provider: the "/api/v1/files/<key>" proxy path (GetURL/DownloadURL).
//   - external URLs not owned by the backend (e.g. Unsplash avatars): returned unchanged.
//
// store==nil, empty input, or any signing failure returns rawURL unchanged, so
// handlers exercised in tests without a store are unaffected. This mirrors the
// behaviour of TaskService.EnrichFilesWithURLs and DesignerService.signResultURLs.
//
// rawURL is NOT trusted: external URLs are never signed (IsOwnedURL guard), and
// storage.StorageKeyFromURL performs no ownership validation by design.
func SignURL(ctx context.Context, store storage.Provider, log *zerolog.Logger, rawURL string, ttl int) string {
	if store == nil {
		return rawURL
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return rawURL
	}
	if ttl <= 0 {
		ttl = DefaultSignedURLTTL
	}

	// Custom CDN domain: objects are publicly readable, no signing needed.
	if store.HasCustomDomain() {
		if key, ok := storage.StorageKeyFromURL(rawURL); ok {
			return store.GetURL(key)
		}
		return rawURL
	}

	// Only sign URLs that point at our own backend; external URLs pass through
	// verbatim so we never (a) mis-sign a third-party URL or (b) feed an
	// attacker-controlled host into DownloadURL/Read.
	if !store.IsOwnedURL(rawURL) {
		return rawURL
	}

	key, ok := storage.StorageKeyFromURL(rawURL)
	if !ok {
		return rawURL
	}
	signed, err := store.DownloadURL(ctx, key, ttl)
	if err != nil {
		if log != nil {
			log.Warn().Err(err).Str("url", rawURL).Msg("sign image url failed, returning original")
		}
		return rawURL
	}
	return signed
}

// SignTemplateURLs resolves a *model.Template's runtime image fields to
// directly-fetchable signed URLs in place. No-op when t is nil or no store is
// wired.
func SignTemplateURLs(ctx context.Context, store storage.Provider, log *zerolog.Logger, t *model.Template) {
	if t == nil {
		return
	}
	t.ThumbnailURL = SignURL(ctx, store, log, t.ThumbnailURL, DefaultSignedURLTTL)
}
