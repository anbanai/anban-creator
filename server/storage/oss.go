package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

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
