package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/royalrick/anbanwriter/server/config"
	"github.com/rs/zerolog"
)

// OSSProvider implements Provider using Alibaba Cloud OSS.
type OSSProvider struct {
	client       *oss.Client
	bucket       *oss.Bucket
	endpoint     string
	bucketName   string
	customDomain string
	logger       *zerolog.Logger
}

// NewOSSProvider creates a new OSSProvider by validating the config and
// initialising the Alibaba Cloud OSS client.
func NewOSSProvider(cfg config.StorageConfig, logger *zerolog.Logger) (*OSSProvider, error) {
	required := map[string]string{
		"endpoint":          cfg.Endpoint,
		"access_key_id":     cfg.AccessKeyID,
		"access_key_secret": cfg.AccessKeySecret,
		"bucket_name":       cfg.BucketName,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("storage.%s is required for OSS provider", field)
		}
	}

	// Ensure endpoint uses HTTPS so signed URLs use https:// scheme.
	// The OSS SDK defaults to http:// when no scheme prefix is provided,
	// which causes mixed-content errors on HTTPS pages.
	endpoint := cfg.Endpoint
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		endpoint = "https://" + strings.TrimPrefix(endpoint, "http://")
	case !strings.HasPrefix(endpoint, "https://"):
		endpoint = "https://" + endpoint
	}

	client, err := oss.New(endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("create OSS client: %w", err)
	}

	bucket, err := client.Bucket(cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("get OSS bucket %s: %w", cfg.BucketName, err)
	}

	// Store bare hostname (strip scheme) so GetURL() constructs correct URLs.
	bareEndpoint := strings.TrimPrefix(endpoint, "https://")

	return &OSSProvider{
		client:       client,
		bucket:       bucket,
		endpoint:     bareEndpoint,
		bucketName:   cfg.BucketName,
		customDomain: cfg.CustomDomain,
		logger:       logger,
	}, nil
}

// Name returns the provider name.
func (p *OSSProvider) Name() string {
	return "oss"
}

// Upload streams data from reader into the OSS bucket under the given key.
func (p *OSSProvider) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*UploadResult, error) {
	options := []oss.Option{}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}

	if err := p.bucket.PutObject(key, reader, options...); err != nil {
		return nil, fmt.Errorf("oss put object %s: %w", key, err)
	}

	p.logger.Debug().
		Str("key", key).
		Str("content_type", contentType).
		Msg("file uploaded to OSS")

	return &UploadResult{
		URL:      p.GetURL(key),
		Key:      key,
		MimeType: contentType,
	}, nil
}

// UploadFile uploads a local file to the OSS bucket.
func (p *OSSProvider) UploadFile(_ context.Context, key string, filePath string, contentType string) (*UploadResult, error) {
	options := []oss.Option{}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}

	if err := p.bucket.PutObjectFromFile(key, filePath, options...); err != nil {
		return nil, fmt.Errorf("oss put object from file %s: %w", key, err)
	}

	p.logger.Debug().
		Str("key", key).
		Str("file_path", filePath).
		Str("content_type", contentType).
		Msg("file uploaded to OSS from disk")

	return &UploadResult{
		URL:      p.GetURL(key),
		Key:      key,
		MimeType: contentType,
	}, nil
}

// UploadURL generates a time-limited signed PUT URL for direct client uploads.
func (p *OSSProvider) UploadURL(_ context.Context, key string, contentType string, expirySeconds int) (string, error) {
	options := []oss.Option{}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}
	signedURL, err := p.bucket.SignURL(key, oss.HTTPPut, int64(expirySeconds), options...)
	if err != nil {
		return "", fmt.Errorf("oss sign upload url for %s: %w", key, err)
	}
	return signedURL, nil
}

// GetURL returns the public URL for the given key.
// If a custom domain is configured, it is used; otherwise the default
// OSS bucket endpoint is used.
func (p *OSSProvider) GetURL(key string) string {
	if p.customDomain != "" {
		domain := strings.TrimRight(p.customDomain, "/")
		return fmt.Sprintf("https://%s/%s", domain, key)
	}
	return fmt.Sprintf("https://%s.%s/%s", p.bucketName, p.endpoint, key)
}

// Read downloads an object from OSS by key and returns its content.
// Transient server errors (5xx) are retried up to 3 times with exponential backoff.
func (p *OSSProvider) Read(ctx context.Context, key string) ([]byte, error) {
	signedURL, err := p.DownloadURL(ctx, key, 3600)
	if err != nil {
		return nil, fmt.Errorf("get signed URL for %s: %w", key, err)
	}

	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<uint(attempt-1)) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request for %s: %w", key, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("download %s from OSS: %w", key, err)
			continue
		}

		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("download %s from OSS: unexpected status %d", key, resp.StatusCode)
			continue
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s from OSS: %w", key, err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download %s from OSS: unexpected status %d", key, resp.StatusCode)
		}
		return data, nil
	}

	return nil, lastErr
}

// Delete removes an object from the OSS bucket.
func (p *OSSProvider) Delete(_ context.Context, key string) error {
	if err := p.bucket.DeleteObject(key); err != nil {
		return fmt.Errorf("oss delete object %s: %w", key, err)
	}

	p.logger.Debug().
		Str("key", key).
		Msg("file deleted from OSS")

	return nil
}

// DownloadURL generates a time-limited signed URL for the given key.
func (p *OSSProvider) DownloadURL(_ context.Context, key string, expirySeconds int) (string, error) {
	signedURL, err := p.bucket.SignURL(key, http.MethodGet, int64(expirySeconds))
	if err != nil {
		return "", fmt.Errorf("oss sign url for %s: %w", key, err)
	}
	return signedURL, nil
}

// HasCustomDomain reports whether a custom CDN domain is configured for public access.
func (p *OSSProvider) HasCustomDomain() bool {
	return p.customDomain != ""
}

// IsOwnedURL reports whether the given URL points at this OSS bucket.
// Used as an SSRF guard before server-side fetches of user-supplied URLs.
// The scheme must be http/https, the host (case-insensitive, port-insensitive)
// must match either the configured custom domain or the default
// "{bucket}.{endpoint}" form.
func (p *OSSProvider) IsOwnedURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if p.customDomain != "" {
		domain := strings.TrimPrefix(strings.TrimPrefix(p.customDomain, "https://"), "http://")
		if i := strings.Index(domain, "/"); i >= 0 {
			domain = domain[:i]
		}
		domain = strings.ToLower(strings.TrimRight(domain, "/"))
		return host == domain
	}
	expected := strings.ToLower(fmt.Sprintf("%s.%s", p.bucketName, p.endpoint))
	return host == expected
}
