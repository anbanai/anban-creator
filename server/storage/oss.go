package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/rs/zerolog"
	"github.com/royalrick/anbanwriter/server/config"
)

// OSSProvider implements Provider using Alibaba Cloud OSS.
type OSSProvider struct {
	client      *oss.Client
	bucket      *oss.Bucket
	endpoint    string
	bucketName  string
	customDomain string
	logger      *zerolog.Logger
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

	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("create OSS client: %w", err)
	}

	bucket, err := client.Bucket(cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("get OSS bucket %s: %w", cfg.BucketName, err)
	}

	return &OSSProvider{
		client:       client,
		bucket:       bucket,
		endpoint:     cfg.Endpoint,
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
// Since aliyun-oss-go-sdk does not support context cancellation in PutObject,
// the upload is wrapped in a goroutine with ctx.Done() monitoring.
func (p *OSSProvider) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*UploadResult, error) {
	type result struct {
		err error
	}

	options := []oss.Option{}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}

	ch := make(chan result, 1)
	go func() {
		ch <- result{err: p.bucket.PutObject(key, reader, options...)}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("oss put object %s: %w", key, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("oss put object %s: %w", key, r.err)
		}
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
// Since aliyun-oss-go-sdk does not support context cancellation in PutObjectFromFile,
// the upload is wrapped in a goroutine with ctx.Done() monitoring.
func (p *OSSProvider) UploadFile(ctx context.Context, key string, filePath string, contentType string) (*UploadResult, error) {
	type result struct {
		err error
	}

	options := []oss.Option{}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}

	ch := make(chan result, 1)
	go func() {
		ch <- result{err: p.bucket.PutObjectFromFile(key, filePath, options...)}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("oss put object from file %s: %w", key, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("oss put object from file %s: %w", key, r.err)
		}
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
// Since aliyun-oss-go-sdk does not support context cancellation in DeleteObject,
// the delete is wrapped in a goroutine with ctx.Done() monitoring.
func (p *OSSProvider) Delete(ctx context.Context, key string) error {
	type result struct {
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ch <- result{err: p.bucket.DeleteObject(key)}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("oss delete object %s: %w", key, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("oss delete object %s: %w", key, r.err)
		}
	}

	p.logger.Debug().
		Str("key", key).
		Msg("file deleted from OSS")

	return nil
}

// DownloadURL generates a time-limited signed URL for the given key.
// Since aliyun-oss-go-sdk does not support context cancellation in SignURL,
// the call is wrapped in a goroutine with ctx.Done() monitoring.
func (p *OSSProvider) DownloadURL(ctx context.Context, key string, expirySeconds int) (string, error) {
	type result struct {
		url string
		err error
	}

	ch := make(chan result, 1)
	go func() {
		url, err := p.bucket.SignURL(key, http.MethodGet, int64(expirySeconds))
		ch <- result{url: url, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("oss sign url for %s: %w", key, ctx.Err())
	case r := <-ch:
		if r.err != nil {
			return "", fmt.Errorf("oss sign url for %s: %w", key, r.err)
		}
		return r.url, nil
	}
}

// HasCustomDomain reports whether a custom CDN domain is configured for public access.
func (p *OSSProvider) HasCustomDomain() bool {
	return p.customDomain != ""
}
