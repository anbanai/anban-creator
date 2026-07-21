package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

// mimeTypes maps file extensions to MIME types.
var mimeTypes = map[string]string{
	".html":     "text/html",
	".htm":      "text/html",
	".css":      "text/css",
	".js":       "application/javascript",
	".json":     "application/json",
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".png":      "image/png",
	".jpg":      "image/jpeg",
	".jpeg":     "image/jpeg",
	".gif":      "image/gif",
	".webp":     "image/webp",
	".svg":      "image/svg+xml",
	".pdf":      "application/pdf",
	".zip":      "application/zip",
}

const (
	maxBulkZipTasks = 100
	maxBulkZipFiles = 500
	maxBulkZipBytes = 100 * 1024 * 1024
)

// DetectTaskFileMIME returns the MIME type for a file based on its extension.
// Falls back to net/http.DetectContentType by reading the first 512 bytes.
func DetectTaskFileMIME(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	f, err := os.Open(filePath)
	if err != nil {
		if mime, ok := mimeTypes[ext]; ok {
			return mime
		}
		return "application/octet-stream"
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	detected := http.DetectContentType(buf[:n])
	if strings.HasPrefix(detected, "image/") {
		return detected
	}
	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}
	return detected
}

// DetermineTaskFileRole returns the role for a file based on its name and MIME type.
func DetermineTaskFileRole(filename, mimeType string) string {
	if workflowRole := DetermineWorkflowArtifactRole(filename, mimeType); workflowRole != model.FileRoleOther {
		return workflowRole
	}

	base := strings.ToLower(filepath.Base(filename))
	if strings.HasPrefix(base, "cover") {
		return model.FileRoleCover
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".html" || ext == ".htm" {
		return model.FileRoleHTML
	}
	if ext == ".md" || ext == ".markdown" {
		return model.FileRoleMarkdown
	}
	if strings.HasPrefix(mimeType, "image/") {
		return model.FileRoleImage
	}
	return model.FileRoleOther
}

// ShouldSkipTaskFileDir reports whether a directory should be excluded from task uploads.
func ShouldSkipTaskFileDir(name string) bool {
	switch name {
	case ".anban-creator", ".anban-runtime-home", ".claude", ".git", "node_modules", "dist", "build", ".cache", ".vite":
		return true
	default:
		return false
	}
}

// ShouldSkipTaskFile reports whether a file should be excluded from task uploads.
func ShouldSkipTaskFile(name string) bool {
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".") {
		return true
	}
	switch base {
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb", "tsconfig.json", "vite.config.ts", "vite.config.js", "eslint.config.js", "eslint.config.mjs":
		return true
	default:
		return false
	}
}

// CleanTaskFileRelativePath normalizes a user-provided relative task file path.
func CleanTaskFileRelativePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	relPath = strings.TrimPrefix(relPath, "./")
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" {
		return "", fmt.Errorf("relative path is required")
	}

	cleaned := filepath.Clean(relPath)
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("relative path is required")
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path")
	}
	return cleaned, nil
}

func buildTaskStorageKey(userID, taskID, relPath string) string {
	return fmt.Sprintf("%s/%s/%s", userID, taskID, filepath.ToSlash(relPath))
}

// UploadTaskFileFromReader uploads one task output file and persists its metadata.
// If the same path already exists with identical content, the existing record is
// returned. If the path exists with new content, the storage object and DB row
// are overwritten so resumed tasks can refresh their deliverables.
func (s *TaskService) UploadTaskFileFromReader(ctx context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	return s.uploadTaskFileFromReader(ctx, nil, taskID, userID, "", relPath, reader, mimeType, fileSize, nil)
}

// UploadExecutionTaskFileFromReader persists an MCP-produced artifact as part
// of the current cloud attempt. It remains pending until the durable execution
// finalizer publishes the complete attempt artifact set.
func (s *TaskService) UploadExecutionTaskFileFromReader(ctx context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	task, err := s.ValidateAgentTaskAccess(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.ValidateAgentExecutionAccess(ctx, userID, task.ProjectID, taskID, executionID); err != nil {
		return nil, err
	}
	return s.uploadTaskFileFromReader(ctx, task, taskID, userID, executionID, relPath, reader, mimeType, fileSize, nil)
}

type ImageOperationVerificationSnapshot struct {
	Passed          bool     `json:"passed"`
	Score           string   `json:"score"`
	MissingEntities []string `json:"missing_entities,omitempty"`
}

type ImageOperationBillingSnapshot struct {
	OperationID  string `json:"operation_id"`
	CatalogID    string `json:"catalog_id"`
	SKUID        string `json:"sku_id"`
	PriceCredits int64  `json:"price_credits"`
}

// ImageOperationResultSnapshot is the complete, sanitized generate_image
// response persisted beside the settlement intent for exact retry replay.
// Provider evidence, prompts, local paths, raw model output, and usage never
// enter this customer-facing idempotency record.
type ImageOperationResultSnapshot struct {
	FilePath           string                              `json:"file_path"`
	DownloadURL        string                              `json:"download_url"`
	Size               string                              `json:"size,omitempty"`
	Width              int                                 `json:"width,omitempty"`
	Height             int                                 `json:"height,omitempty"`
	ImageType          string                              `json:"image_type,omitempty"`
	Provider           string                              `json:"provider"`
	Model              string                              `json:"model"`
	SelectionReason    string                              `json:"selection_reason,omitempty"`
	SupportsReference  bool                                `json:"supports_reference"`
	MaxReferenceImages int                                 `json:"max_reference_images"`
	ResponseType       string                              `json:"response_type,omitempty"`
	OutputMIME         string                              `json:"output_mime,omitempty"`
	WeChatURL          string                              `json:"wechat_url,omitempty"`
	MediaID            string                              `json:"media_id,omitempty"`
	UploadError        string                              `json:"upload_error,omitempty"`
	Verification       *ImageOperationVerificationSnapshot `json:"verification,omitempty"`
	Billing            ImageOperationBillingSnapshot       `json:"billing"`
}

type TaskFileOperationSettlement struct {
	CatalogID, SKUID, ToolCallID, RequestFingerprint string
	PriceCredits                                     int64
	MediaID, WeChatURL                               string
	ResultSnapshot                                   ImageOperationResultSnapshot
}

type GenericTaskFileOperationSettlement struct {
	ResourceType, IdempotencyScope                    string
	CatalogID, SKUID, OperationID, RequestFingerprint string
	PriceCredits                                      int64
	ResultSnapshot                                    []byte
}

func (s *TaskService) FindTaskFileOperationSettlement(ctx context.Context, taskID, executionID, operationID, requestFingerprint string) (*model.TaskFile, []byte, error) {
	return s.FindExecutionTaskFileSettlement(ctx, taskID, executionID, operationID, requestFingerprint, "image", "mcp-image-settlement")
}

func (s *TaskService) FindExecutionTaskFileSettlement(ctx context.Context, taskID, executionID, operationID, requestFingerprint, resourceType, idempotencyScope string) (*model.TaskFile, []byte, error) {
	if s == nil || s.repo == nil {
		return nil, nil, fmt.Errorf("task repository is not available")
	}
	key := billingFingerprint(taskID, executionID, operationID)
	settlement, err := s.repo.Billing().FindSettlementByKey(ctx, idempotencyScope, key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if settlement.TaskID == nil || settlement.AttemptID == nil || settlement.ToolCallID == nil ||
		*settlement.TaskID != taskID || *settlement.AttemptID != executionID || *settlement.ToolCallID != operationID || settlement.ResourceType != resourceType || settlement.RequestFingerprint != requestFingerprint {
		return nil, nil, ErrBillingConflict
	}
	file, err := s.repo.TaskFiles().FindByIDForExecution(ctx, settlement.ResourceID, taskID, executionID)
	if err != nil {
		return nil, nil, err
	}
	if file == nil || file.TaskID != taskID || file.ExecutionID != executionID {
		return nil, nil, ErrBillingConflict
	}
	if len(settlement.ResultSnapshot) == 0 || !json.Valid(settlement.ResultSnapshot) {
		return nil, nil, ErrBillingConflict
	}
	return file, append([]byte(nil), settlement.ResultSnapshot...), nil
}

func (s *TaskService) UploadExecutionTaskFileWithSettlementFromReader(ctx context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64, settlement TaskFileOperationSettlement) (*model.TaskFile, error) {
	settlement.ResultSnapshot.Billing = ImageOperationBillingSnapshot{
		OperationID: settlement.ToolCallID, CatalogID: settlement.CatalogID,
		SKUID: settlement.SKUID, PriceCredits: settlement.PriceCredits,
	}
	snapshot, err := json.Marshal(settlement.ResultSnapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal image operation result snapshot: %w", err)
	}
	return s.UploadExecutionTaskFileWithOperationSettlementFromReader(ctx, taskID, userID, executionID, relPath, reader, mimeType, fileSize, GenericTaskFileOperationSettlement{
		ResourceType: "image", IdempotencyScope: "mcp-image-settlement",
		CatalogID: settlement.CatalogID, SKUID: settlement.SKUID, PriceCredits: settlement.PriceCredits,
		OperationID: settlement.ToolCallID, RequestFingerprint: settlement.RequestFingerprint, ResultSnapshot: snapshot,
	})
}

func (s *TaskService) UploadExecutionTaskFileWithOperationSettlementFromReader(ctx context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64, settlement GenericTaskFileOperationSettlement) (*model.TaskFile, error) {
	task, err := s.ValidateAgentTaskAccess(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.ValidateAgentExecutionAccess(ctx, userID, task.ProjectID, taskID, executionID); err != nil {
		return nil, err
	}
	intent := &SettlementIntent{
		Action: model.BillingSettlementActionChargeOperation, UserID: userID, TaskID: taskID, AttemptID: executionID,
		ToolCallID: settlement.OperationID, CatalogID: settlement.CatalogID, SKUID: settlement.SKUID,
		ResourceType: settlement.ResourceType, IdempotencyScope: settlement.IdempotencyScope, IdempotencyKey: billingFingerprint(taskID, executionID, settlement.OperationID),
		RequestFingerprint: settlement.RequestFingerprint,
	}
	if strings.TrimSpace(settlement.ResourceType) == "" || strings.TrimSpace(settlement.IdempotencyScope) == "" ||
		strings.TrimSpace(settlement.OperationID) == "" || len(settlement.ResultSnapshot) == 0 || !json.Valid(settlement.ResultSnapshot) {
		return nil, fmt.Errorf("invalid fixed-SKU task-file settlement")
	}
	var snapshot map[string]any
	if err := json.Unmarshal(settlement.ResultSnapshot, &snapshot); err != nil {
		return nil, fmt.Errorf("decode operation result snapshot: %w", err)
	}
	if snapshot == nil {
		return nil, fmt.Errorf("decode operation result snapshot: object is required")
	}
	snapshot["billing"] = ImageOperationBillingSnapshot{
		OperationID: settlement.OperationID, CatalogID: settlement.CatalogID,
		SKUID: settlement.SKUID, PriceCredits: settlement.PriceCredits,
	}
	intent.ResultSnapshot, err = json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal operation result snapshot: %w", err)
	}
	return s.uploadTaskFileFromReader(ctx, task, taskID, userID, executionID, relPath, reader, mimeType, fileSize, intent)
}

func (s *TaskService) uploadTaskFileFromReader(ctx context.Context, task *model.Task, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64, settlement *SettlementIntent) (*model.TaskFile, error) {
	if s.store == nil {
		return nil, fmt.Errorf("no storage provider configured: cannot upload files for task %s", taskID)
	}

	cleanRelPath, err := CleanTaskFileRelativePath(relPath)
	if err != nil {
		return nil, err
	}

	filename := filepath.Base(cleanRelPath)
	if ShouldSkipTaskFile(filename) {
		return nil, fmt.Errorf("refusing to upload dotfile %q", filename)
	}

	if mimeType == "" {
		mimeType = mimeTypes[strings.ToLower(filepath.Ext(filename))]
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Compute content hash before dedup so same-path retries stay idempotent
	// while changed files from resume executions can replace old metadata.
	var buf bytes.Buffer
	tee := io.TeeReader(reader, &buf)
	h := sha256.New()
	if _, err := io.Copy(h, tee); err != nil {
		return nil, fmt.Errorf("compute content hash: %w", err)
	}
	contentHash := hex.EncodeToString(h.Sum(nil))
	reader = io.MultiReader(&buf, reader)

	if executionID == "" {
		existing, err := s.repo.TaskFiles().FindExisting(ctx, taskID, cleanRelPath)
		if err != nil {
			return nil, fmt.Errorf("check existing task file: %w", err)
		}
		if existing != nil && existing.ContentHash == contentHash && settlement == nil {
			return existing, nil
		}
	}

	ossKey := buildTaskStorageKey(userID, taskID, cleanRelPath)
	if executionID != "" {
		if task == nil {
			return nil, fmt.Errorf("task is required for execution artifact upload")
		}
		ossKey = buildTaskMCPArtifactStoragePrefix(task, executionID) + filepath.ToSlash(cleanRelPath)
	}
	uploadResult, err := s.store.Upload(ctx, ossKey, reader, mimeType)
	if err != nil {
		return nil, fmt.Errorf("upload file %s: %w", cleanRelPath, err)
	}

	if uploadResult.Size > 0 {
		fileSize = uploadResult.Size
	}

	taskFile := &model.TaskFile{
		TaskID:          taskID,
		ExecutionID:     executionID,
		Role:            DetermineTaskFileRole(filename, mimeType),
		FileName:        filename,
		MimeType:        mimeType,
		FileSize:        fileSize,
		ContentHash:     contentHash,
		OSSKey:          ossKey,
		OSSURL:          uploadResult.URL,
		StorageProvider: s.store.Name(),
		FilePath:        cleanRelPath,
	}
	var settlementSnapshot map[string]any
	if settlement != nil {
		var snapshot map[string]any
		if err := json.Unmarshal(settlement.ResultSnapshot, &snapshot); err != nil {
			return nil, fmt.Errorf("decode operation result snapshot: %w", err)
		}
		snapshot["file_path"] = cleanRelPath
		snapshot["output_mime"] = mimeType
		// Private OSS URLs expire and therefore are not immutable replay data.
		// Store only stable public/local identities; the MCP replay path mints a
		// fresh signed URL from the linked TaskFile when necessary.
		snapshot["download_url"] = ""
		if s.store.HasCustomDomain() || s.store.Name() == "local" {
			snapshot["download_url"] = s.store.GetURL(ossKey)
		}
		settlement.ResultSnapshot, err = json.Marshal(snapshot)
		if err != nil {
			return nil, fmt.Errorf("marshal durable operation result snapshot: %w", err)
		}
		if mediaID, ok := snapshot["media_id"].(string); ok {
			taskFile.MediaID = mediaID
		}
		if wechatURL, ok := snapshot["wechat_url"].(string); ok {
			taskFile.WechatURL = wechatURL
		}
		settlementSnapshot = snapshot
	}
	var persisted *model.TaskFile
	persist := func(repo repository.Repository) error {
		if executionID != "" {
			persisted, err = repo.TaskFiles().UpsertPendingCurrentExecution(ctx, taskID, executionID, taskFile)
		} else {
			persisted, err = repo.TaskFiles().Upsert(ctx, taskFile)
		}
		if err != nil {
			return err
		}
		if settlement != nil {
			if s.billingWalletSvc == nil {
				return fmt.Errorf("fixed-SKU billing wallet is required for operation settlement")
			}
			settlement.ResourceID = persisted.ID
			settlementSnapshot["task_file_id"] = persisted.ID
			settlement.ResultSnapshot, err = json.Marshal(settlementSnapshot)
			if err != nil {
				return fmt.Errorf("marshal linked operation result snapshot: %w", err)
			}
			_, err = s.billingWalletSvc.EnqueueSettlementInTx(ctx, repo, *settlement)
		}
		return err
	}
	if settlement != nil {
		err = s.repo.WithTx(ctx, persist)
	} else {
		err = persist(s.repo)
	}
	if err != nil {
		return nil, fmt.Errorf("persist task file %s: %w", cleanRelPath, err)
	}

	return persisted, nil
}

// UpdateTaskFileMetadata updates publication-facing metadata on a task file
// after a downstream upload step (for example WeChat CDN registration).
func (s *TaskService) UpdateTaskFileMetadata(ctx context.Context, file *model.TaskFile, role, mediaID, wechatURL string) (*model.TaskFile, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("task service repository is not available")
	}
	if file == nil {
		return nil, fmt.Errorf("task file is required")
	}
	updated := *file
	if role != "" {
		updated.Role = role
	}
	updated.MediaID = mediaID
	updated.WechatURL = wechatURL
	persisted, err := s.repo.TaskFiles().Upsert(ctx, &updated)
	if err != nil {
		return nil, fmt.Errorf("update task file metadata: %w", err)
	}
	return persisted, nil
}

func (s *TaskService) uploadTaskFileFromPath(ctx context.Context, taskID, userID, workDir, path string, info os.FileInfo) (*model.TaskFile, error) {
	relPath, err := filepath.Rel(workDir, path)
	if err != nil {
		relPath = filepath.Base(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open task file %s: %w", path, err)
	}
	defer f.Close()

	var fileSize int64
	if info != nil {
		fileSize = info.Size()
	}

	return s.UploadTaskFileFromReader(ctx, taskID, userID, relPath, f, DetectTaskFileMIME(path), fileSize)
}

// uploadMissingTaskFiles uploads files from the workspace that aren't already
// recorded as task files. This handles text files written by the agent directly,
// while MCP tool-generated files already have TaskFile records.
func (s *TaskService) uploadMissingTaskFiles(ctx context.Context, taskID, userID, workDir string) error {
	if s.store == nil {
		return nil
	}

	// Workspace may not exist if the executor failed before creating it.
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		return nil
	}

	// Prefer the output/ subdirectory (created by the agent via mkdir -p) to isolate
	// content files from agent runtime artifacts (node_modules, .claude, etc.).
	scanDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		scanDir = filepath.Join(workDir, "output")
	}

	task, _ := s.repo.Tasks().FindByID(ctx, taskID)

	var uploadedCount int
	err := filepath.WalkDir(scanDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if ShouldSkipTaskFileDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipTaskFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			s.logger.Warn().Err(err).Str("file", d.Name()).Msg("failed to get file info, skipping")
			return nil
		}

		relPath, err := filepath.Rel(workDir, path)
		if err != nil {
			relPath = filepath.Base(path)
		}
		relPath = filepath.ToSlash(relPath)

		if !ShouldCollectTaskFile(task, relPath) {
			return nil
		}

		// Content-hash dedup: skip files with identical content already recorded
		// under a different path (e.g. Downloader saves generated-0.png while
		// the agent model also saves the same image as images/image-1.png).
		if f, openErr := os.Open(path); openErr == nil {
			h := sha256.New()
			if _, copyErr := io.Copy(h, f); copyErr == nil {
				contentHash := hex.EncodeToString(h.Sum(nil))
				if dup, dupErr := s.repo.TaskFiles().FindByTaskIDAndContentHash(ctx, taskID, contentHash); dupErr == nil && dup != nil {
					_ = f.Close()
					s.logger.Debug().
						Str("task_id", taskID).
						Str("file", relPath).
						Str("duplicate_of", dup.FilePath).
						Msg("skipping workspace file with identical content hash")
					return nil
				}
			}
			if closeErr := f.Close(); closeErr != nil {
				s.logger.Warn().Err(closeErr).Str("file", relPath).Msg("failed to close task file after hashing")
			}
		}

		if _, err := s.uploadTaskFileFromPath(ctx, taskID, userID, workDir, path, info); err != nil {
			s.logger.Error().Err(err).Str("file", path).Msg("failed to upload file, skipping")
			return nil
		}
		uploadedCount++
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk work directory %s: %w", workDir, err)
	}

	if uploadedCount > 0 {
		s.logger.Info().
			Str("task_id", taskID).
			Int("count", uploadedCount).
			Msg("uploaded missing workspace files to storage")
	}
	return nil
}

// ShouldCollectTaskFile reports whether a workspace file should become a
// user-facing task file. Videocreator/videoeditor use explicit delivery
// allowlists so runtime project files never leak into task deliverables.
func ShouldCollectTaskFile(task *model.Task, relPath string) bool {
	if task == nil || !model.IsVideoPlatform(task.Type) {
		return true
	}
	normalized := filepath.ToSlash(strings.TrimPrefix(relPath, "./"))
	noOutput := strings.TrimPrefix(normalized, "output/")
	ext := strings.ToLower(filepath.Ext(noOutput))

	if model.IsVideoEditorPlatform(task.Type) {
		if isVideoFileExtension(ext) {
			return isVideoEditingDeliveryVideo(noOutput)
		}
		return isVideoEditingDeliveryFile(noOutput)
	}
	if isVideoFileExtension(ext) {
		return true
	}
	return isVideoGenerationDeliveryFile(noOutput)
}

func isVideoFileExtension(ext string) bool {
	switch ext {
	case ".mp4", ".mov", ".webm", ".m4v":
		return true
	default:
		return false
	}
}

func isVideoEditingDeliveryVideo(path string) bool {
	switch path {
	case "final.mp4", "preview.mp4":
		return true
	default:
		return false
	}
}

func isVideoGenerationDeliveryFile(path string) bool {
	switch path {
	case "input-manifest.md",
		"reference-anchors.md",
		"script.md",
		"shot-plan.md",
		"generation-plan.json",
		"video-generation-plan.md",
		"video-task-submit.json",
		"video-task-result.json",
		"delivery-manifest.json",
		"quality-review.md",
		"iteration-log.md":
		return true
	default:
		return false
	}
}

func isVideoEditingDeliveryFile(path string) bool {
	switch path {
	case "input-manifest.md",
		"clip_results.json",
		"render-report.md",
		"quality-review.md",
		"edit/edl.json",
		"edit/media-manifest.json",
		"edit/takes_packed.md",
		"edit/edit-candidates.json":
		return true
	}
	if strings.HasPrefix(path, "edit/transcripts/") && strings.HasSuffix(path, ".json") {
		return true
	}
	if strings.HasPrefix(path, "capcut/") && strings.HasSuffix(path, ".json") {
		return true
	}
	if strings.HasPrefix(path, "capcut-draft/") && strings.HasSuffix(path, ".json") {
		return true
	}
	return false
}

// VerifyFileBelongsToTask checks that a file belongs to the specified task.
// Returns an error if the file does not exist or does not belong to the task.
func (s *TaskService) VerifyFileBelongsToTask(ctx context.Context, taskID, fileID string) error {
	exists, err := s.repo.TaskFiles().ExistsByTaskIDAndID(ctx, taskID, fileID)
	if err != nil {
		return fmt.Errorf("check file ownership: %w", err)
	}
	if !exists {
		return fmt.Errorf("file %s does not belong to task %s", fileID, taskID)
	}
	return nil
}

// GetFileStream returns a ReadCloser for a task file's content and the TaskFile metadata.
func (s *TaskService) GetFileStream(ctx context.Context, fileID string) (io.ReadCloser, *model.TaskFile, error) {
	file, err := s.repo.TaskFiles().FindByID(ctx, fileID)
	if err != nil {
		return nil, nil, fmt.Errorf("find task file: %w", err)
	}

	data, err := s.getFileContent(ctx, file)
	if err != nil {
		return nil, file, fmt.Errorf("read file content: %w", err)
	}

	return io.NopCloser(bytes.NewReader(data)), file, nil
}

// EnrichFilesWithURLs populates the computed URL field for each task file.
// For OSS with custom domain, uses permanent public URLs.
// For OSS without custom domain, generates time-limited signed URLs (1 hour expiry).
// For local storage, uses the authenticated download API path.
func (s *TaskService) EnrichFilesWithURLs(ctx context.Context, files []*model.TaskFile) {
	if s.store == nil {
		return
	}
	for _, f := range files {
		if f.OSSKey == "" {
			continue
		}
		if s.store.HasCustomDomain() {
			f.URL = s.store.GetURL(f.OSSKey)
			continue
		}
		signedURL, err := s.store.DownloadURL(ctx, f.OSSKey, 3600)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("file_id", f.ID).
				Str("oss_key", f.OSSKey).
				Msg("failed to generate signed URL, falling back to OSSURL")
			f.URL = f.OSSURL
			continue
		}
		f.URL = signedURL
	}
}

// DownloadZip creates a ZIP archive of all files belonging to a task.
// Returns the ZIP buffer and the suggested download filename.
func (s *TaskService) DownloadZip(ctx context.Context, taskID string) (*bytes.Buffer, string, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, "", fmt.Errorf("find task files: %w", err)
	}
	if len(files) == 0 {
		return nil, "", fmt.Errorf("no files found for task %s", taskID)
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for _, file := range files {
		data, err := s.getFileContent(ctx, file)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("file_name", file.FileName).
				Str("oss_key", file.OSSKey).
				Msg("failed to read file for zip, skipping")
			continue
		}

		w, err := zipWriter.Create(file.FileName)
		if err != nil {
			s.logger.Warn().Err(err).
				Str("file_name", file.FileName).
				Msg("failed to create zip entry, skipping")
			continue
		}

		if _, err := w.Write(data); err != nil {
			s.logger.Warn().Err(err).
				Str("file_name", file.FileName).
				Msg("failed to write file to zip, skipping")
			continue
		}
	}

	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("close zip writer: %w", err)
	}

	zipName := fmt.Sprintf("task_%s_files.zip", taskID)
	return &buf, zipName, nil
}

// BulkDownloadZipManifest describes what was included or skipped in a bulk task export.
type BulkDownloadZipManifest struct {
	GeneratedAt string                        `json:"generated_at"`
	Tasks       []BulkDownloadZipManifestTask `json:"tasks"`
}

// BulkDownloadZipManifestTask is one task entry in the bulk export manifest.
type BulkDownloadZipManifestTask struct {
	TaskID   string   `json:"task_id"`
	Title    string   `json:"title,omitempty"`
	Status   string   `json:"status,omitempty"`
	Included bool     `json:"included"`
	Files    []string `json:"files,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// DownloadTasksZip creates a ZIP archive with files from multiple completed tasks owned by userID.
// Each task is written into its own directory and manifest.json records skipped tasks.
func (s *TaskService) DownloadTasksZip(ctx context.Context, userID string, taskIDs []string) (*bytes.Buffer, string, error) {
	if userID == "" {
		return nil, "", fmt.Errorf("user_id is required")
	}
	if len(taskIDs) == 0 {
		return nil, "", fmt.Errorf("task_ids is required")
	}
	if len(taskIDs) > maxBulkZipTasks {
		return nil, "", fmt.Errorf("task_ids must not exceed %d", maxBulkZipTasks)
	}

	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)
	manifest := BulkDownloadZipManifest{
		GeneratedAt: timeNowUTC(),
		Tasks:       make([]BulkDownloadZipManifestTask, 0, len(taskIDs)),
	}
	usedEntries := make(map[string]int)
	includedCount := 0
	includedFiles := 0
	var includedBytes int64

	for _, taskID := range uniqueNonEmptyStrings(taskIDs) {
		entry := BulkDownloadZipManifestTask{TaskID: taskID}

		task, err := s.repo.Tasks().FindByID(ctx, taskID)
		if err != nil {
			entry.Reason = "unavailable"
			manifest.Tasks = append(manifest.Tasks, entry)
			continue
		}

		if task.UserID != userID {
			entry.Reason = "unavailable"
			manifest.Tasks = append(manifest.Tasks, entry)
			continue
		}

		entry.Title = task.Title
		entry.Status = task.Status
		if task.Status != model.TaskStatusCompleted {
			entry.Reason = "task_not_completed"
			manifest.Tasks = append(manifest.Tasks, entry)
			continue
		}

		files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
		if err != nil {
			entry.Reason = "file_lookup_failed"
			manifest.Tasks = append(manifest.Tasks, entry)
			continue
		}
		if len(files) == 0 {
			entry.Reason = "no_files"
			manifest.Tasks = append(manifest.Tasks, entry)
			continue
		}

		taskDir := uniqueZipEntryName(usedEntries, buildTaskZipDir(task))
		for _, file := range files {
			if includedFiles >= maxBulkZipFiles {
				entry.Reason = "export_file_limit_reached"
				break
			}
			if file.FileSize > 0 && includedBytes+file.FileSize > maxBulkZipBytes {
				entry.Reason = "export_size_limit_reached"
				break
			}

			data, err := s.getFileContent(ctx, file)
			if err != nil {
				s.logger.Warn().Err(err).
					Str("task_id", taskID).
					Str("file_name", file.FileName).
					Str("oss_key", file.OSSKey).
					Msg("failed to read file for bulk zip, skipping")
				continue
			}
			if includedBytes+int64(len(data)) > maxBulkZipBytes {
				entry.Reason = "export_size_limit_reached"
				break
			}

			fileName := file.FilePath
			if fileName == "" {
				fileName = file.FileName
			}
			fileName = cleanZipEntryPath(fileName)
			zipPath := uniqueZipEntryName(usedEntries, taskDir+"/"+fileName)
			w, err := zipWriter.Create(zipPath)
			if err != nil {
				s.logger.Warn().Err(err).Str("zip_path", zipPath).Msg("failed to create bulk zip entry, skipping")
				continue
			}
			if _, err := w.Write(data); err != nil {
				s.logger.Warn().Err(err).Str("zip_path", zipPath).Msg("failed to write bulk zip entry, skipping")
				continue
			}
			entry.Files = append(entry.Files, zipPath)
			includedFiles++
			includedBytes += int64(len(data))
		}

		if len(entry.Files) == 0 {
			if entry.Reason == "" {
				entry.Reason = "no_readable_files"
			}
		} else {
			entry.Included = true
			includedCount++
		}
		manifest.Tasks = append(manifest.Tasks, entry)
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("encode manifest: %w", err)
	}
	w, err := zipWriter.Create("manifest.json")
	if err != nil {
		return nil, "", fmt.Errorf("create manifest entry: %w", err)
	}
	if _, err := w.Write(manifestData); err != nil {
		return nil, "", fmt.Errorf("write manifest entry: %w", err)
	}

	if err := zipWriter.Close(); err != nil {
		return nil, "", fmt.Errorf("close zip writer: %w", err)
	}
	if includedCount == 0 {
		return nil, "", fmt.Errorf("no downloadable files found")
	}

	zipName := fmt.Sprintf("tasks_export_%s.zip", time.Now().Format("20060102_150405"))
	return &buf, zipName, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func buildTaskZipDir(task *model.Task) string {
	label := task.Title
	if label == "" {
		label = task.Prompt
	}
	if label == "" {
		label = task.Type + "-task"
	}
	return cleanZipEntryPath(label + "-" + task.ID)
}

func cleanZipEntryPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "/")
	path = filepath.Clean(path)
	path = filepath.ToSlash(path)
	if path == "." || path == "" || path == ".." || strings.HasPrefix(path, "../") {
		return "file"
	}

	replacer := strings.NewReplacer(":", "-", "*", "-", "?", "", "\"", "", "<", "", ">", "", "|", "-")
	parts := strings.Split(path, "/")
	for i, part := range parts {
		part = strings.TrimSpace(replacer.Replace(part))
		if part == "" || part == "." || part == ".." {
			part = "file"
		}
		if len([]rune(part)) > 80 {
			part = string([]rune(part)[:80])
		}
		parts[i] = part
	}
	return strings.Join(parts, "/")
}

func uniqueZipEntryName(used map[string]int, name string) string {
	name = cleanZipEntryPath(name)
	if count, ok := used[name]; ok {
		used[name] = count + 1
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		return fmt.Sprintf("%s-%d%s", base, count+1, ext)
	}
	used[name] = 1
	return name
}

func timeNowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// getFileContent reads the full content of a task file based on its storage provider.
func (s *TaskService) getFileContent(ctx context.Context, file *model.TaskFile) ([]byte, error) {
	if s.store == nil {
		return nil, fmt.Errorf("no storage provider configured")
	}

	return s.store.Read(ctx, file.OSSKey)
}

// RewriteHTMLImageURLs replaces relative image src references in HTML content
// with their actual storage URLs. The fileMap maps lowercase filenames to URLs.
// It handles both direct references (src="cover.png") and subdirectory references
// (src="images/cover.png").
func RewriteHTMLImageURLs(htmlContent []byte, fileMap map[string]string) []byte {
	if len(fileMap) == 0 {
		return htmlContent
	}

	content := string(htmlContent)
	for filename, url := range fileMap {
		// Replace direct references: src="filename" and src='filename'.
		content = strings.ReplaceAll(content, `src="`+filename+`"`, `src="`+url+`"`)
		content = strings.ReplaceAll(content, `src='`+filename+`'`, `src="`+url+`"`)

		// Replace subdirectory references: src="path/filename" → src="url".
		// Match the full src value containing the filename and replace entirely.
		pattern := `src=["'][^"']*` + regexp.QuoteMeta(filename) + `["']`
		re := regexp.MustCompile(pattern)
		content = re.ReplaceAllString(content, `src="`+url+`"`)
	}
	return []byte(content)
}
