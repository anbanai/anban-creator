package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

const ObjectMetadataSHA256 = "sha256"

// UploadResult holds the result of a file upload.
type UploadResult struct {
	URL      string // Public URL or local path
	Key      string // Storage key (OSS object key or relative local path)
	Size     int64
	MimeType string
}

// ObjectInfo describes a stored object without downloading its body.
type ObjectInfo struct {
	Key         string
	Size        int64
	MimeType    string
	ContentType string
	ETag        string
	SHA256      string
}

var (
	ErrObjectNotFound              = errors.New("storage object not found")
	ErrObjectStatUnsupported       = errors.New("storage object metadata is unavailable")
	ErrBoundedReadUnsupported      = errors.New("bounded storage reads are unavailable")
	ErrObjectExceedsMaxSize        = errors.New("storage object is too large")
	ErrPromotionPreconditionFailed = errors.New("storage promotion source changed")
	ErrObjectAlreadyExists         = errors.New("storage object already exists")
)

// ObjectStatProvider fetches object metadata without reading the object body.
type ObjectStatProvider interface {
	StatObject(ctx context.Context, key string) (*ObjectInfo, error)
}

// BoundedObjectReader reads at most maxBytes of an object and reports oversized
// objects without first buffering the complete body.
type BoundedObjectReader interface {
	ReadObject(ctx context.Context, key string, maxBytes int64) ([]byte, error)
}

// ObjectStreamProvider opens an object body without first buffering it in
// memory. Callers must close the returned stream.
type ObjectStreamProvider interface {
	OpenObject(ctx context.Context, key string) (io.ReadCloser, error)
}

// AttachmentDownloadURLProvider signs a direct download URL whose response is
// forced to an attachment with the user-facing filename.
type AttachmentDownloadURLProvider interface {
	DownloadAttachmentURL(ctx context.Context, key, filename string, expirySeconds int) (string, error)
}

// OpenObject prefers a storage backend's streaming capability. The buffered
// fallback keeps legacy and test providers compatible; production providers
// should implement ObjectStreamProvider for large-object paths.
func OpenObject(ctx context.Context, provider Provider, key string) (io.ReadCloser, error) {
	if provider == nil {
		return nil, fmt.Errorf("storage provider is unavailable")
	}
	if opener, ok := provider.(ObjectStreamProvider); ok {
		return opener.OpenObject(ctx, key)
	}
	data, err := provider.Read(ctx, key)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// ConditionalObjectPromoter copies a verified source object to an immutable
// destination only when the source still has the expected ETag, and returns
// the destination identity reported by the storage backend.
type ConditionalObjectPromoter interface {
	PromoteObject(ctx context.Context, sourceKey, finalKey, expectedETag string) (*ObjectInfo, error)
}

// MetadataUploadURLProvider signs direct uploads whose object metadata must
// match the headers sent by the uploader.
type MetadataUploadURLProvider interface {
	UploadURLWithMetadata(ctx context.Context, key, contentType string, metadata map[string]string, expirySeconds int) (string, error)
}

// ReadObject requires an explicitly bounded reader. Security-sensitive callers
// must not fall back to Provider.Read because some remote implementations buffer
// the entire object before returning.
func ReadObject(ctx context.Context, provider Provider, key string, maxBytes int64) ([]byte, error) {
	if provider == nil {
		return nil, fmt.Errorf("storage provider is unavailable")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maximum object size must be positive")
	}
	reader, ok := provider.(BoundedObjectReader)
	if !ok {
		return nil, ErrBoundedReadUnsupported
	}
	data, err := reader.ReadObject(ctx, key, maxBytes)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: size=%d max=%d", ErrObjectExceedsMaxSize, len(data), maxBytes)
	}
	return data, nil
}

// Provider is the interface for file storage backends.
type Provider interface {
	Name() string
	Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*UploadResult, error)
	UploadFile(ctx context.Context, key string, filePath string, contentType string) (*UploadResult, error)
	UploadURL(ctx context.Context, key string, contentType string, expirySeconds int) (string, error)
	GetURL(key string) string
	Read(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	DownloadURL(ctx context.Context, key string, expirySeconds int) (string, error)
	HasCustomDomain() bool
	// IsOwnedURL reports whether the given URL points at this storage backend.
	// Used as an SSRF guard before server-side fetches of user-supplied URLs.
	IsOwnedURL(rawURL string) bool
}
