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

// detectMimeType returns the MIME type for a file based on its extension.
// Falls back to net/http.DetectContentType by reading the first 512 bytes.
func detectMimeType(filePath string) string {
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

// determineFileRole returns the role for a file based on its name and MIME type.
func determineFileRole(filename, mimeType string) string {
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

// UploadTaskFiles uploads all files from a task's work directory to storage.
// It recursively walks the directory tree to find files in nested subdirectories
// (e.g. output/articles/staging/). Individual upload failures are logged but do
// not abort the remaining uploads. Returns an error if no storage provider is
// configured or if the work directory cannot be read at all.
func (s *TaskService) UploadTaskFiles(ctx context.Context, taskID, userID, workDir string) error {
	if s.store == nil {
		return fmt.Errorf("no storage provider configured: cannot upload files for task %s", taskID)
	}

	var batch []*model.TaskFile
	providerName := s.store.Name()

	err := filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip config directories entirely to avoid uploading settings.json etc.
		if d.IsDir() {
			name := d.Name()
			if name == ".anbanwriter" || name == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}

		filename := filepath.Base(path)

		// Skip dotfiles (sensitive config files like .mcp.json, .gitignore, etc.).
		if strings.HasPrefix(filename, ".") {
			return nil
		}

		mimeType := detectMimeType(path)
		role := determineFileRole(filename, mimeType)

		// Preserve subdirectory structure in the OSS key.
		relPath, err := filepath.Rel(workDir, path)
		if err != nil {
			relPath = filename
		}
		ossKey := fmt.Sprintf("%s/%s/%s", userID, taskID, relPath)

		info, err := d.Info()
		if err != nil {
			s.logger.Warn().Err(err).Str("file", filename).Msg("failed to get file info, skipping")
			return nil
		}

		uploadResult, err := s.store.UploadFile(ctx, ossKey, path, mimeType)
		if err != nil {
			s.logger.Error().Err(err).
				Str("file", filename).
				Str("key", ossKey).
				Msg("failed to upload file, skipping")
			return nil
		}

		fileSize := info.Size()
		if uploadResult.Size > 0 {
			fileSize = uploadResult.Size
		}

		batch = append(batch, &model.TaskFile{
			TaskID:          taskID,
			Role:            role,
			FileName:        filename,
			MimeType:        mimeType,
			FileSize:        fileSize,
			OSSKey:          ossKey,
			OSSURL:          uploadResult.URL,
			StorageProvider: providerName,
			FilePath:        path,
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk work directory %s: %w", workDir, err)
	}

	if len(batch) > 0 {
		if err := s.repo.TaskFiles().BatchCreate(ctx, batch); err != nil {
			s.logger.Error().Err(err).
				Str("task_id", taskID).
				Int("count", len(batch)).
				Msg("failed to persist task file records")
		} else {
			s.logger.Info().
				Str("task_id", taskID).
				Int("count", len(batch)).
				Msg("task files uploaded to storage")
		}
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
