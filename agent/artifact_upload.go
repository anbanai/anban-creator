package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

type ArtifactPrepareRequest struct {
	TaskID       string `json:"task_id"`
	ExecutionID  string `json:"execution_id,omitempty"`
	RelativePath string `json:"relative_path"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type ArtifactPrepareResponse struct {
	UploadID           string            `json:"upload_id"`
	Key                string            `json:"key"`
	PublicURL          string            `json:"public_url"`
	UploadURL          string            `json:"upload_url,omitempty"`
	Method             string            `json:"method"`
	Headers            map[string]string `json:"headers"`
	Region             string            `json:"region"`
	Bucket             string            `json:"bucket"`
	Endpoint           string            `json:"endpoint"`
	STSAccessKeyID     string            `json:"sts_access_key_id"`
	STSAccessKeySecret string            `json:"sts_access_key_secret"`
	STSSecurityToken   string            `json:"sts_security_token"`
	ExpiresAt          string            `json:"expires_at"`
	MaxSize            int64             `json:"max_size"`
}

type ArtifactManifestRequest struct {
	TaskID      string                 `json:"task_id"`
	ExecutionID string                 `json:"execution_id,omitempty"`
	Files       []ArtifactManifestFile `json:"files"`
}

type ArtifactManifestFile struct {
	RelativePath string `json:"relative_path"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	ETag         string `json:"etag,omitempty"`
	Role         string `json:"role,omitempty"`
}

type WorkspaceArtifact struct {
	LocalPath    string
	RelativePath string
	Filename     string
	ContentType  string
	Size         int64
	SHA256       string
}

type ArtifactReporter interface {
	PrepareArtifactUpload(context.Context, ArtifactPrepareRequest) (*ArtifactPrepareResponse, error)
	ReportArtifactManifest(context.Context, ArtifactManifestRequest) error
	ReportProgress(context.Context, string) error
}

type ArtifactUploader struct {
	cfg       *Config
	reporter  ArtifactReporter
	putObject func(context.Context, *ArtifactPrepareResponse, string, string) (string, error)
}

var openArtifactFile = func(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

func NewArtifactUploader(cfg *Config, reporter ArtifactReporter) *ArtifactUploader {
	return &ArtifactUploader{
		cfg:       cfg,
		reporter:  reporter,
		putObject: putOSSObjectFromFile,
	}
}

func ScanWorkspaceArtifacts(ctx context.Context, root string) ([]WorkspaceArtifact, error) {
	return scanWorkspaceArtifacts(ctx, root, "")
}

func (u *ArtifactUploader) UploadWorkspaceArtifacts(ctx context.Context, result *serveragent.ExecutionResult) error {
	if u == nil || u.cfg == nil || u.reporter == nil {
		return fmt.Errorf("artifact uploader is not configured")
	}
	workDir := u.cfg.Workspace
	if result != nil && strings.TrimSpace(result.WorkDir) != "" {
		workDir = result.WorkDir
	}
	if err := artifactContextCause(ctx); err != nil {
		return err
	}
	if u.cfg.ExecutionID != "" {
		outputDir := filepath.Join(workDir, "output")
		info, err := os.Lstat(outputDir)
		if cause := artifactContextCause(ctx); cause != nil {
			return cause
		}
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect job output directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("job output must be a real directory")
		}
	}
	files, err := scanWorkspaceArtifacts(ctx, workDir, u.cfg.TaskType)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	manifest := ArtifactManifestRequest{
		TaskID:      u.cfg.TaskID,
		ExecutionID: u.cfg.ExecutionID,
		Files:       make([]ArtifactManifestFile, 0, len(files)),
	}
	for _, file := range files {
		if err := artifactContextCause(ctx); err != nil {
			return err
		}
		prepared, err := u.reporter.PrepareArtifactUpload(ctx, ArtifactPrepareRequest{
			TaskID:       u.cfg.TaskID,
			ExecutionID:  u.cfg.ExecutionID,
			RelativePath: file.RelativePath,
			Filename:     file.Filename,
			ContentType:  file.ContentType,
			Size:         file.Size,
			SHA256:       file.SHA256,
		})
		if err != nil {
			return fmt.Errorf("prepare artifact upload %s: %w", file.RelativePath, err)
		}
		if err := artifactContextCause(ctx); err != nil {
			return err
		}
		if prepared == nil {
			return fmt.Errorf("prepare artifact upload %s: empty response", file.RelativePath)
		}
		if prepared.MaxSize > 0 && file.Size > prepared.MaxSize {
			return fmt.Errorf("artifact %s exceeds prepared upload limit", file.RelativePath)
		}
		contentType := prepared.Headers["Content-Type"]
		if contentType == "" {
			contentType = file.ContentType
		}
		etag, err := u.putObject(ctx, prepared, file.LocalPath, contentType)
		if err != nil {
			return fmt.Errorf("upload artifact %s: %w", file.RelativePath, err)
		}
		if err := artifactContextCause(ctx); err != nil {
			return err
		}
		objectKey := strings.TrimSpace(prepared.Key)
		if objectKey == "" {
			return fmt.Errorf("prepare artifact upload %s returned empty object key", file.RelativePath)
		}
		manifest.Files = append(manifest.Files, ArtifactManifestFile{
			RelativePath: file.RelativePath,
			ObjectKey:    objectKey,
			ContentType:  contentType,
			Size:         file.Size,
			SHA256:       file.SHA256,
			ETag:         etag,
		})
	}
	if err := artifactContextCause(ctx); err != nil {
		return err
	}
	if err := u.reporter.ReportArtifactManifest(ctx, manifest); err != nil {
		return fmt.Errorf("report artifact manifest: %w", err)
	}
	_ = u.reporter.ReportProgress(ctx, fmt.Sprintf("uploaded %d workspace artifact(s)", len(manifest.Files)))
	return nil
}

func scanWorkspaceArtifacts(ctx context.Context, root, taskType string) ([]WorkspaceArtifact, error) {
	if err := artifactContextCause(ctx); err != nil {
		return nil, err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat workspace: %w", err)
	}
	if err := artifactContextCause(ctx); err != nil {
		return nil, err
	}

	scanDir := root
	outputDir := filepath.Join(root, "output")
	if info, err := os.Stat(outputDir); err == nil && info.IsDir() {
		scanDir = outputDir
	}
	if err := artifactContextCause(ctx); err != nil {
		return nil, err
	}

	task := taskForArtifactFiltering(taskType)
	var files []WorkspaceArtifact
	err := filepath.WalkDir(scanDir, func(path string, d fs.DirEntry, err error) error {
		if cause := artifactContextCause(ctx); cause != nil {
			return cause
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != scanDir && service.ShouldSkipTaskFileDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if service.ShouldSkipTaskFile(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if cause := artifactContextCause(ctx); cause != nil {
			return cause
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		artifact, err := describeWorkspaceArtifact(ctx, root, path, info)
		if err != nil {
			return err
		}
		if !service.ShouldCollectTaskFile(task, artifact.RelativePath) {
			return nil
		}
		files = append(files, artifact)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := artifactContextCause(ctx); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})
	return files, nil
}

func describeWorkspaceArtifact(ctx context.Context, root, path string, info fs.FileInfo) (WorkspaceArtifact, error) {
	if err := artifactContextCause(ctx); err != nil {
		return WorkspaceArtifact{}, err
	}
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = filepath.Base(path)
	}
	relPath = filepath.ToSlash(relPath)
	hash, err := fileSHA256(ctx, path)
	if err != nil {
		return WorkspaceArtifact{}, err
	}
	if err := artifactContextCause(ctx); err != nil {
		return WorkspaceArtifact{}, err
	}
	contentType := service.DetectTaskFileMIME(path)
	if err := artifactContextCause(ctx); err != nil {
		return WorkspaceArtifact{}, err
	}
	return WorkspaceArtifact{
		LocalPath:    path,
		RelativePath: relPath,
		Filename:     filepath.Base(path),
		ContentType:  contentType,
		Size:         info.Size(),
		SHA256:       hash,
	}, nil
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	if err := artifactContextCause(ctx); err != nil {
		return "", err
	}
	f, err := openArtifactFile(path)
	if err != nil {
		return "", fmt.Errorf("open artifact %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	buffer := make([]byte, 32*1024)
	for {
		if err := artifactContextCause(ctx); err != nil {
			return "", err
		}
		// A kernel-blocked Read cannot be canceled portably; context checks
		// between chunks keep the userspace hashing loop bounded.
		n, readErr := f.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
		}
		if err := artifactContextCause(ctx); err != nil {
			return "", err
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("hash artifact %s: %w", path, readErr)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func artifactContextCause(ctx context.Context) error {
	return context.Cause(ctx)
}

func taskForArtifactFiltering(taskType string) *model.Task {
	taskType = strings.TrimSpace(taskType)
	if taskType == "" {
		return nil
	}
	return &model.Task{Type: taskType}
}

func putOSSObjectFromFile(ctx context.Context, prepared *ArtifactPrepareResponse, localPath string, contentType string) (string, error) {
	if prepared == nil {
		return "", fmt.Errorf("empty prepare response")
	}
	endpoint := normalizeOSSEndpoint(prepared.Endpoint)
	if endpoint == "" || strings.TrimSpace(prepared.Bucket) == "" || strings.TrimSpace(prepared.Key) == "" {
		return "", fmt.Errorf("missing oss upload target")
	}
	if prepared.STSAccessKeyID == "" || prepared.STSAccessKeySecret == "" || prepared.STSSecurityToken == "" {
		return "", fmt.Errorf("missing oss temporary credentials")
	}

	client, err := oss.New(endpoint, prepared.STSAccessKeyID, prepared.STSAccessKeySecret, oss.SecurityToken(prepared.STSSecurityToken))
	if err != nil {
		return "", fmt.Errorf("create oss client: %w", err)
	}
	bucket, err := client.Bucket(prepared.Bucket)
	if err != nil {
		return "", fmt.Errorf("open oss bucket: %w", err)
	}
	options := []oss.Option{oss.WithContext(ctx)}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}
	if err := bucket.PutObjectFromFile(prepared.Key, localPath, options...); err != nil {
		return "", fmt.Errorf("put oss object: %w", err)
	}
	return "", nil
}

func normalizeOSSEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	return "https://" + endpoint
}
