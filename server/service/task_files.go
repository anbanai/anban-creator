package service

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	".mp4":      "video/mp4",
	".pdf":      "application/pdf",
	".zip":      "application/zip",
}

// DetectTaskFileMIME returns the MIME type for a file based on its extension.
// Falls back to net/http.DetectContentType by reading the first 512 bytes.
func DetectTaskFileMIME(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if mime, ok := mimeTypes[ext]; ok {
		return mime
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n])
}

// DetermineTaskFileRole returns the role for a file based on its name and MIME type.
func DetermineTaskFileRole(filename, mimeType string) string {
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
		OSSKey:          ossKey,
		OSSURL:          uploadResult.URL,
		StorageProvider: s.store.Name(),
		FilePath:        cleanRelPath,
	}
	if err := s.repo.TaskFiles().Create(ctx, taskFile); err != nil {
		return nil, fmt.Errorf("persist task file %s: %w", cleanRelPath, err)
	}

	return taskFile, nil
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
	err = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
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

// UploadTaskFiles uploads all files from a task's work directory to storage.
// It recursively walks the directory tree to find files in nested subdirectories
// (e.g. output/articles/staging/). Individual upload failures are logged but do
// not abort the remaining uploads. Returns an error if no storage provider is
// configured or if the work directory cannot be read at all.
func (s *TaskService) UploadTaskFiles(ctx context.Context, taskID, userID, workDir string) error {
	if s.store == nil {
		return fmt.Errorf("no storage provider configured: cannot upload files for task %s", taskID)
	}

	var uploadedCount int

	err := filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip config directories entirely to avoid uploading settings.json etc.
		if d.IsDir() {
			if ShouldSkipTaskFileDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip dotfiles (sensitive config files like .mcp.json, .gitignore, etc.).
		if ShouldSkipTaskFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			s.logger.Warn().Err(err).Str("file", d.Name()).Msg("failed to get file info, skipping")
			return nil
		}

		if _, err := s.uploadTaskFileFromPath(ctx, taskID, userID, workDir, path, info); err != nil {
			s.logger.Error().Err(err).
				Str("file", path).
				Msg("failed to upload file, skipping")
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
			Msg("task files uploaded to storage")
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
