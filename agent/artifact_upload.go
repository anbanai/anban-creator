package main

import (
	"context"
	"errors"
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
	UploadRequired     bool              `json:"upload_required"`
	ETag               string            `json:"etag,omitempty"`
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
	Role         string `json:"role,omitempty"`
}

type ArtifactStreamRequest struct {
	TaskID       string
	ExecutionID  string
	RelativePath string
	ContentType  string
	Size         int64
	SHA256       string
}

type ArtifactStreamResponse struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
}

type WorkspaceArtifact struct {
	LocalPath    string
	RelativePath string
	Filename     string
}

type ArtifactReporter interface {
	PrepareArtifactUpload(context.Context, ArtifactPrepareRequest) (*ArtifactPrepareResponse, error)
	StreamArtifactContent(context.Context, ArtifactStreamRequest, io.Reader) (*ArtifactStreamResponse, error)
	ReportArtifactManifest(context.Context, ArtifactManifestRequest) error
	ReportProgress(context.Context, string) error
}

type ArtifactUploader struct {
	cfg       *Config
	reporter  ArtifactReporter
	putObject func(context.Context, *ArtifactPrepareResponse, io.Reader, string) (string, error)
}

const artifactSHA256Header = "X-Oss-Meta-Sha256"

func NewArtifactUploader(cfg *Config, reporter ArtifactReporter) *ArtifactUploader {
	return &ArtifactUploader{
		cfg:       cfg,
		reporter:  reporter,
		putObject: putOSSObject,
	}
}

func ScanWorkspaceArtifacts(ctx context.Context, root string) ([]WorkspaceArtifact, error) {
	return scanWorkspaceArtifacts(ctx, root, "")
}

func (u *ArtifactUploader) UploadWorkspaceArtifacts(ctx context.Context, _ *serveragent.ExecutionResult) error {
	if u == nil || u.cfg == nil || u.reporter == nil {
		return fmt.Errorf("artifact uploader is not configured")
	}
	workDir := u.cfg.Workspace
	if err := artifactContextCause(ctx); err != nil {
		return err
	}
	var (
		files []WorkspaceArtifact
		err   error
	)
	if u.cfg.ExecutionID != "" {
		outputDir := filepath.Join(workDir, "output")
		info, statErr := os.Lstat(outputDir)
		if cause := artifactContextCause(ctx); cause != nil {
			return cause
		}
		switch {
		case os.IsNotExist(statErr):
			files = []WorkspaceArtifact{}
		case statErr != nil:
			return fmt.Errorf("inspect job output directory: %w", statErr)
		case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("job output must be a real directory")
		default:
			files, err = scanWorkspaceArtifacts(ctx, workDir, u.cfg.TaskType)
		}
	} else {
		files, err = scanWorkspaceArtifacts(ctx, workDir, u.cfg.TaskType)
	}
	if err != nil {
		return err
	}
	if len(files) == 0 && strings.TrimSpace(u.cfg.ExecutionID) == "" {
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
		manifestFile, err := u.uploadWorkspaceArtifact(ctx, file)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, manifestFile)
	}
	if err := artifactContextCause(ctx); err != nil {
		return err
	}
	if err := u.reporter.ReportArtifactManifest(ctx, manifest); err != nil {
		return fmt.Errorf("report artifact manifest: %w", err)
	}
	if len(manifest.Files) > 0 {
		_ = u.reporter.ReportProgress(ctx, fmt.Sprintf("collected %d workspace artifact(s)", len(manifest.Files)))
	}
	return nil
}

func (u *ArtifactUploader) uploadWorkspaceArtifact(ctx context.Context, file WorkspaceArtifact) (ArtifactManifestFile, error) {
	for attempt := 1; attempt <= maxArtifactSnapshotAttempts; attempt++ {
		if err := artifactContextCause(ctx); err != nil {
			return ArtifactManifestFile{}, err
		}
		snapshot, err := openArtifactSnapshot(ctx, file.LocalPath)
		if err != nil {
			if errors.Is(err, errArtifactSnapshotChanged) && !hasArtifactCloseError(err) {
				continue
			}
			return ArtifactManifestFile{}, fmt.Errorf("snapshot artifact %s: %w", file.RelativePath, err)
		}

		request := ArtifactPrepareRequest{
			TaskID:       u.cfg.TaskID,
			ExecutionID:  u.cfg.ExecutionID,
			RelativePath: file.RelativePath,
			Filename:     file.Filename,
			ContentType:  snapshot.contentType,
			Size:         snapshot.size,
			SHA256:       snapshot.hash,
		}
		var objectKey, contentType string
		switch u.cfg.ArtifactUploadMode {
		case ArtifactUploadDirect, "":
			prepared, prepareErr := u.reporter.PrepareArtifactUpload(ctx, request)
			if prepareErr != nil {
				primary := fmt.Errorf("prepare artifact upload %s: %w", file.RelativePath, prepareErr)
				return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
			}
			if cause := artifactContextCause(ctx); cause != nil {
				return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, cause)
			}
			if prepared == nil {
				primary := fmt.Errorf("prepare artifact upload %s: empty response", file.RelativePath)
				return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
			}
			if prepared.MaxSize > 0 && snapshot.size > prepared.MaxSize {
				primary := fmt.Errorf("artifact %s exceeds prepared upload limit", file.RelativePath)
				return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
			}
			objectKey = strings.TrimSpace(prepared.Key)
			if objectKey == "" {
				primary := fmt.Errorf("prepare artifact upload %s returned empty object key", file.RelativePath)
				return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
			}
			contentType = prepared.Headers["Content-Type"]
			if contentType == "" {
				contentType = snapshot.contentType
			}
			if prepared.UploadRequired {
				preparedSHA256 := strings.ToLower(strings.TrimSpace(prepared.Headers[artifactSHA256Header]))
				if preparedSHA256 == "" || preparedSHA256 != snapshot.hash {
					primary := fmt.Errorf("prepare artifact upload %s returned missing or mismatched SHA-256 metadata", file.RelativePath)
					return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
				}
				prepared.Headers[artifactSHA256Header] = preparedSHA256
				if rewindErr := snapshot.rewind(); rewindErr != nil {
					primary := fmt.Errorf("rewind artifact %s: %w", file.RelativePath, rewindErr)
					return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
				}
				_, err = u.putObject(ctx, prepared, artifactUploadSource{ReadSeeker: snapshot.file}, contentType)
			}
		case ArtifactUploadStream:
			if rewindErr := snapshot.rewind(); rewindErr != nil {
				primary := fmt.Errorf("rewind artifact %s: %w", file.RelativePath, rewindErr)
				return ArtifactManifestFile{}, closeArtifactFile(file.RelativePath, snapshot.file, primary)
			}
			streamed, streamErr := u.reporter.StreamArtifactContent(ctx, ArtifactStreamRequest{
				TaskID: request.TaskID, ExecutionID: request.ExecutionID, RelativePath: request.RelativePath,
				ContentType: request.ContentType, Size: request.Size, SHA256: request.SHA256,
			}, artifactUploadSource{ReadSeeker: snapshot.file})
			if streamErr != nil {
				err = streamErr
				break
			}
			if streamed == nil {
				err = fmt.Errorf("stream upload returned empty response")
				break
			}
			if streamed.Size != snapshot.size || !strings.EqualFold(strings.TrimSpace(streamed.SHA256), snapshot.hash) {
				err = fmt.Errorf("stream upload response does not match artifact")
				break
			}
			objectKey = strings.TrimSpace(streamed.ObjectKey)
			contentType = firstNonEmpty(streamed.ContentType, snapshot.contentType)
			if objectKey == "" {
				err = fmt.Errorf("stream upload returned empty object key")
			}
		default:
			err = fmt.Errorf("unsupported artifact upload mode %q", u.cfg.ArtifactUploadMode)
		}

		stable, stabilityErr := snapshot.unchanged()
		var operationErr error
		if err != nil {
			operationErr = fmt.Errorf("upload artifact %s: %w", file.RelativePath, err)
		}
		if stabilityErr != nil {
			stabilityErr = fmt.Errorf("recheck artifact %s: %w", file.RelativePath, stabilityErr)
			operationErr = errors.Join(operationErr, stabilityErr)
		}
		if operationErr == nil {
			if err := artifactContextCause(ctx); err != nil {
				operationErr = err
			}
		}
		if err := closeArtifactFile(file.RelativePath, snapshot.file, operationErr); err != nil {
			return ArtifactManifestFile{}, err
		}
		if stable {
			return ArtifactManifestFile{
				RelativePath: file.RelativePath,
				ObjectKey:    objectKey,
				ContentType:  contentType,
				Size:         snapshot.size,
				SHA256:       snapshot.hash,
			}, nil
		}
	}
	return ArtifactManifestFile{}, fmt.Errorf("artifact %s changed during final collection", file.RelativePath)
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

		artifact, err := describeWorkspaceArtifact(ctx, root, path)
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

func describeWorkspaceArtifact(ctx context.Context, root, path string) (WorkspaceArtifact, error) {
	if err := artifactContextCause(ctx); err != nil {
		return WorkspaceArtifact{}, err
	}
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = filepath.Base(path)
	}
	relPath = filepath.ToSlash(relPath)
	return WorkspaceArtifact{
		LocalPath:    path,
		RelativePath: relPath,
		Filename:     filepath.Base(path),
	}, nil
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

func putOSSObject(ctx context.Context, prepared *ArtifactPrepareResponse, source io.Reader, contentType string) (string, error) {
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
	return putOSSObjectFromBucket(ctx, bucket, prepared.Key, source, contentType, prepared.Headers[artifactSHA256Header])
}

func putOSSObjectFromBucket(ctx context.Context, bucket *oss.Bucket, key string, source io.Reader, contentType, hash string) (string, error) {
	options := []oss.Option{oss.WithContext(ctx)}
	if contentType != "" {
		options = append(options, oss.ContentType(contentType))
	}
	if hash != "" {
		options = append(options, oss.Meta("sha256", strings.ToLower(hash)))
	}
	if err := bucket.PutObject(key, source, options...); err != nil {
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
