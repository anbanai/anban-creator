package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog"
)

// PrepareResult is the response for workspace preparation.
type PrepareResult struct {
	Path      string `json:"path"`
	Archived  string `json:"archived,omitempty"`
	FileCount int    `json:"files_count,omitempty"`
}

// ArchiveResult is the response for workspace archiving.
type ArchiveResult struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Archived  string `json:"archived"`
	FileCount int    `json:"files_count"`
}

// WorkspaceService manages working directories for content creation.
type WorkspaceService struct {
	baseDir      string
	workspaceDir string
	logger       *zerolog.Logger
}

// NewWorkspaceService creates a new WorkspaceService.
// baseDir is the root output directory (defaults to "output" in cwd if empty).
// workspaceDir is the Docker workspace base directory for task-based staging.
func NewWorkspaceService(baseDir, workspaceDir string, logger *zerolog.Logger) *WorkspaceService {
	if baseDir == "" {
		baseDir = filepath.Join(".", "output")
	}
	return &WorkspaceService{baseDir: baseDir, workspaceDir: workspaceDir, logger: logger}
}

// Prepare creates a clean working directory. When taskID is provided,
// the working directory is the task workspace root {workspaceDir}/{taskID}/.
// Otherwise, it uses the server's baseDir/<contentType>/ (for CLI mode).
func (s *WorkspaceService) Prepare(contentType, taskID string) (*PrepareResult, error) {
	var workDir string
	if taskID != "" {
		workDir = filepath.Join(s.workspaceDir, taskID)
	} else {
		workDir = filepath.Join(s.baseDir, contentType)
	}
	result := &PrepareResult{Path: workDir}

	if info, err := os.Stat(workDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(workDir)
		if err != nil {
			return nil, fmt.Errorf("read work dir: %w", err)
		}
		if len(entries) > 0 {
			archiveDir, err := s.nextArchiveDir(contentType)
			if err != nil {
				return nil, fmt.Errorf("compute archive dir: %w", err)
			}
			moved, err := s.moveFilesToArchive(workDir, archiveDir)
			if err != nil {
				return nil, fmt.Errorf("archive files: %w", err)
			}
			result.Archived = filepath.Base(archiveDir)
			result.FileCount = moved
			if s.logger != nil {
				s.logger.Info().
					Str("content_type", contentType).
					Str("archived_to", filepath.Base(archiveDir)).
					Int("files", moved).
					Msg("archived existing files")
			}
		}
	}

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}

	return result, nil
}

// moveFilesToArchive moves regular files (not subdirectories) from srcDir to destDir.
// Subdirectories (e.g. previous archive dirs) are left in place.
func (s *WorkspaceService) moveFilesToArchive(srcDir, destDir string) (int, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, fmt.Errorf("create archive dir: %w", err)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, err
	}
	var moved int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(srcDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return moved, fmt.Errorf("move %s to archive: %w", entry.Name(), err)
		}
		moved++
	}
	return moved, nil
}

// Archive moves regular files from the content type directory to a dated or
// named archive directory. Subdirectories (e.g. previous archives) are left in
// place. If name is empty, uses YYYYMMDD-NNN format.
func (s *WorkspaceService) Archive(contentType, name string) (*ArchiveResult, error) {
	contentDir := filepath.Join(s.baseDir, contentType)

	if _, err := os.Stat(contentDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("content directory does not exist: %s", contentDir)
	}

	var archiveDir string
	var err error
	if name != "" {
		archiveDir, err = s.namedArchiveDir(contentType, name)
	} else {
		archiveDir, err = s.nextArchiveDir(contentType)
	}
	if err != nil {
		return nil, fmt.Errorf("compute archive dir: %w", err)
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return nil, fmt.Errorf("create archive dir: %w", err)
	}

	entries, err := os.ReadDir(contentDir)
	if err != nil {
		return nil, fmt.Errorf("read content dir: %w", err)
	}
	var moved int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		src := filepath.Join(contentDir, entry.Name())
		dst := filepath.Join(archiveDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return nil, fmt.Errorf("archive %s: %w", entry.Name(), err)
		}
		moved++
	}

	if s.logger != nil {
		s.logger.Info().
			Str("content_type", contentType).
			Str("archived_to", filepath.Base(archiveDir)).
			Int("files", moved).
			Msg("archived content files")
	}

	return &ArchiveResult{
		From:      contentDir,
		To:        archiveDir,
		Archived:  filepath.Base(archiveDir),
		FileCount: moved,
	}, nil
}

// nextArchiveDir computes the next available archive directory path,
// formatted as baseDir/<type>/YYYYMMDD-NNN.
func (s *WorkspaceService) nextArchiveDir(contentType string) (string, error) {
	base := filepath.Join(s.baseDir, contentType)
	today := time.Now().Format("20060102")

	for n := 1; n <= 999; n++ {
		dirName := fmt.Sprintf("%s-%03d", today, n)
		path := filepath.Join(base, dirName)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no available archive slot for %s on %s", contentType, today)
}

// namedArchiveDir computes a named archive directory path.
// If the name collides, appends -2, -3, ... suffixes.
// Falls back to nextArchiveDir if the sanitized name is empty.
func (s *WorkspaceService) namedArchiveDir(contentType, name string) (string, error) {
	base := filepath.Join(s.baseDir, contentType)
	clean := sanitizeDirName(name)
	if clean == "" {
		return s.nextArchiveDir(contentType)
	}
	path := filepath.Join(base, clean)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path, nil
	}
	for n := 2; n <= 999; n++ {
		candidate := filepath.Join(base, fmt.Sprintf("%s-%d", clean, n))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no available archive slot for name %q under %s", name, base)
}

// sanitizeDirName cleans a title for use as a directory name:
// replaces illegal characters with '_', trims whitespace, truncates to 50 runes.
func sanitizeDirName(name string) string {
	illegal := strings.ContainsAny
	var b strings.Builder
	for _, r := range name {
		if illegal(string(r), `/\:*?"<>|`) || unicode.IsControl(r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	result := strings.TrimSpace(b.String())
	runes := []rune(result)
	if len(runes) > 50 {
		runes = runes[:50]
	}
	return string(runes)
}
