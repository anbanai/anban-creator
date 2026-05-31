package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/royalrick/anbanwriter/server/model"
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
	case ".anbanwriter", ".claude":
		return true
	default:
		return false
	}
}

// ShouldSkipTaskFile reports whether a file should be excluded from task uploads.
func ShouldSkipTaskFile(name string) bool {
	return strings.HasPrefix(filepath.Base(name), ".")
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
// If a record with the same (taskID, filePath) already exists, the existing record is returned
// to prevent duplicates from MCP tool retries or overlapping upload paths.
func (s *TaskService) UploadTaskFileFromReader(ctx context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
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

	// Deduplicate: if a record with the same (taskID, filePath) already exists, skip.
	existing, err := s.repo.TaskFiles().FindExisting(ctx, taskID, cleanRelPath)
	if err != nil {
		return nil, fmt.Errorf("check existing task file: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	// Compute content hash for content-based dedup.
	var buf bytes.Buffer
	tee := io.TeeReader(reader, &buf)
	h := sha256.New()
	if _, err := io.Copy(h, tee); err != nil {
		return nil, fmt.Errorf("compute content hash: %w", err)
	}
	contentHash := hex.EncodeToString(h.Sum(nil))
	reader = io.MultiReader(&buf, reader)

	ossKey := buildTaskStorageKey(userID, taskID, cleanRelPath)
	uploadResult, err := s.store.Upload(ctx, ossKey, reader, mimeType)
	if err != nil {
		return nil, fmt.Errorf("upload file %s: %w", cleanRelPath, err)
	}

	if uploadResult.Size > 0 {
		fileSize = uploadResult.Size
	}

	taskFile := &model.TaskFile{
		TaskID:          taskID,
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
	persisted, err := s.repo.TaskFiles().Upsert(ctx, taskFile)
	if err != nil {
		return nil, fmt.Errorf("persist task file %s: %w", cleanRelPath, err)
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

	// Collect paths already recorded as task files.
	existingFiles, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("check existing task files: %w", err)
	}
	existingPaths := make(map[string]bool, len(existingFiles))
	for _, f := range existingFiles {
		existingPaths[f.FilePath] = true
	}

	var uploadedCount int
	err = filepath.WalkDir(scanDir, func(path string, d os.DirEntry, err error) error {
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

		if existingPaths[relPath] {
			return nil // already recorded, skip
		}

		// Content-hash dedup: skip files with identical content already recorded
		// under a different path (e.g. Downloader saves generated-0.png while
		// the agent model also saves the same image as images/image-1.png).
		if f, openErr := os.Open(path); openErr == nil {
			defer f.Close()
			h := sha256.New()
			if _, copyErr := io.Copy(h, f); copyErr == nil {
				contentHash := hex.EncodeToString(h.Sum(nil))
				if dup, dupErr := s.repo.TaskFiles().FindByTaskIDAndContentHash(ctx, taskID, contentHash); dupErr == nil && dup != nil {
					s.logger.Debug().
						Str("task_id", taskID).
						Str("file", relPath).
						Str("duplicate_of", dup.FilePath).
						Msg("skipping workspace file with identical content hash")
					return nil
				}
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
