package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	ossv2 "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/rs/zerolog"
)

var ErrSemanticIndexPending = errors.New("OSS semantic index is pending")

type MediaSemanticSearch interface {
	Search(ctx context.Context, query, userPrefix, mediaType string, limit int) ([]SemanticMediaResult, error)
	Lookup(ctx context.Context, objectKey, mediaType string) (*SemanticMediaResult, error)
}

type SemanticMediaResult struct {
	ObjectKey        string
	Summary          string
	Description      string
	Size             int64
	ETag             string
	FileModifiedTime time.Time
}

type OSSMetaQueryProvider struct {
	client       *ossv2.Client
	bucket       string
	timeout      time.Duration
	maxResults   int
	videoEnabled bool
	logger       *zerolog.Logger
}

func NewOSSMetaQueryProvider(cfg config.StorageConfig, logger *zerolog.Logger) (*OSSMetaQueryProvider, error) {
	if cfg.Provider != "" && cfg.Provider != "oss" {
		return nil, fmt.Errorf("OSS semantic search requires storage.provider=oss")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" || strings.TrimSpace(cfg.BucketName) == "" {
		return nil, errors.New("OSS semantic search requires endpoint, credentials, and bucket")
	}
	cfg.AIContentAwareness.ApplyDefaults()
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	client := ossv2.NewClient(ossv2.LoadDefaultConfig().WithEndpoint(endpoint).WithRegion(cfg.Region).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)).WithUsePathStyle(true))
	return &OSSMetaQueryProvider{client: client, bucket: cfg.BucketName, timeout: cfg.AIContentAwareness.QueryTimeout, maxResults: cfg.AIContentAwareness.MaxResults, videoEnabled: cfg.AIContentAwareness.VideoEnabled, logger: logger}, nil
}

func (p *OSSMetaQueryProvider) Search(ctx context.Context, query, userPrefix, mediaType string, limit int) ([]SemanticMediaResult, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("OSS semantic search is not configured")
	}
	if mediaType == "video" && !p.videoEnabled {
		return nil, errors.New("OSS video content awareness is not enabled")
	}
	query, userPrefix = strings.TrimSpace(query), strings.TrimSpace(userPrefix)
	if query == "" {
		return nil, nil
	}
	if userPrefix == "" {
		return nil, errors.New("user prefix is required")
	}
	if limit <= 0 || limit > p.maxResults {
		limit = p.maxResults
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	simple, err := metaQueryJSON(map[string]any{"Operation": "prefix", "Field": "Filename", "Value": userPrefix})
	if err != nil {
		return nil, err
	}
	mode := "semantic"
	result, err := p.client.DoMetaQuery(ctx, &ossv2.DoMetaQueryRequest{Bucket: ossv2.Ptr(p.bucket), Mode: &mode, MetaQuery: &ossv2.MetaQuery{MaxResults: ossv2.Ptr(int64(limit)), Query: &query, MediaTypes: &ossv2.MetaQueryMediaTypes{MediaTypes: []string{mediaType}}, SimpleQuery: &simple}})
	if err != nil {
		return nil, fmt.Errorf("OSS semantic query: %w", err)
	}
	return mapMetaQueryFiles(result.Files), nil
}

func (p *OSSMetaQueryProvider) Lookup(ctx context.Context, objectKey, mediaType string) (*SemanticMediaResult, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("OSS semantic search is not configured")
	}
	if mediaType == "video" && !p.videoEnabled {
		return nil, errors.New("OSS video content awareness is not enabled")
	}
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" {
		return nil, errors.New("object key is required")
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	simple, err := metaQueryJSON(map[string]any{"Operation": "eq", "Field": "Filename", "Value": objectKey})
	if err != nil {
		return nil, err
	}
	mode := "semantic"
	result, err := p.client.DoMetaQuery(ctx, &ossv2.DoMetaQueryRequest{Bucket: ossv2.Ptr(p.bucket), Mode: &mode, MetaQuery: &ossv2.MetaQuery{MaxResults: ossv2.Ptr(int64(1)), MediaTypes: &ossv2.MetaQueryMediaTypes{MediaTypes: []string{mediaType}}, SimpleQuery: &simple}})
	if err != nil {
		return nil, fmt.Errorf("OSS semantic lookup: %w", err)
	}
	mapped := mapMetaQueryFiles(result.Files)
	if len(mapped) == 0 {
		return nil, ErrSemanticIndexPending
	}
	return &mapped[0], nil
}

func metaQueryJSON(value map[string]any) (string, error) {
	raw, err := json.Marshal(value)
	return string(raw), err
}

func mapMetaQueryFiles(files []ossv2.MetaQueryFile) []SemanticMediaResult {
	results := make([]SemanticMediaResult, 0, len(files))
	for _, file := range files {
		if file.Filename == nil || strings.TrimSpace(*file.Filename) == "" {
			continue
		}
		result := SemanticMediaResult{ObjectKey: *file.Filename}
		if file.Size != nil {
			result.Size = *file.Size
		}
		if file.ETag != nil {
			result.ETag = strings.Trim(*file.ETag, `"`)
		}
		if file.FileModifiedTime != nil {
			result.FileModifiedTime, _ = time.Parse(time.RFC3339Nano, *file.FileModifiedTime)
		}
		if file.Insights != nil && file.Insights.Video != nil {
			if file.Insights.Video.Caption != nil {
				result.Summary = strings.TrimSpace(*file.Insights.Video.Caption)
			}
			if file.Insights.Video.Description != nil {
				result.Description = strings.TrimSpace(*file.Insights.Video.Description)
			}
			if result.Description == "" {
				result.Description = result.Summary
			}
		}
		results = append(results, result)
	}
	return results
}
