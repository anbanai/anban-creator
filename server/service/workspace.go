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
	baseDir string
	logger  *zerolog.Logger
}

// NewWorkspaceService creates a new WorkspaceService.
// baseDir is the root output directory (defaults to "output" in cwd if empty).
func NewWorkspaceService(baseDir string, logger *zerolog.Logger) *WorkspaceService {
	if baseDir == "" {
		baseDir = filepath.Join(".", "output")
	}
	return &WorkspaceService{baseDir: baseDir, logger: logger}
}

// Prepare creates a clean staging directory. When taskID is provided,
// the staging directory is created inside the task workspace at
// /tmp/abwriter/<taskID>/output/<contentType>/staging/.
// Otherwise, it uses the server's baseDir/<contentType>/staging/.
func (s *WorkspaceService) Prepare(contentType, taskID string) (*PrepareResult, error) {
	var stagingDir string
	if taskID != "" {
		stagingDir = filepath.Join(os.TempDir(), "abwriter", taskID, "output", contentType, "staging")
	} else {
		stagingDir = filepath.Join(s.baseDir, contentType, "staging")
	}
	result := &PrepareResult{Path: stagingDir}

	if info, err := os.Stat(stagingDir); err == nil && info.IsDir() {
		entries, err := os.ReadDir(stagingDir)
		if err != nil {
			return nil, fmt.Errorf("read staging dir: %w", err)
		}
		if len(entries) > 0 {
			archiveDir, err := s.nextArchiveDir(contentType)
			if err != nil {
				return nil, fmt.Errorf("compute archive dir: %w", err)
			}
			entriesBefore, _ := os.ReadDir(stagingDir)
			if err := os.Rename(stagingDir, archiveDir); err != nil {
				return nil, fmt.Errorf("archive staging: %w", err)
			}
			result.Archived = filepath.Base(archiveDir)
			result.FileCount = len(entriesBefore)
			if s.logger != nil {
				s.logger.Info().
					Str("content_type", contentType).
					Str("archived_to", filepath.Base(archiveDir)).
					Int("files", len(entriesBefore)).
					Msg("archived existing staging directory")
			}
		}
	}

	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}

	return result, nil
}

// Archive moves the staging directory to a dated or named archive directory.
// If name is empty, uses YYYYMMDD-NNN format.
func (s *WorkspaceService) Archive(contentType, name string) (*ArchiveResult, error) {
	stagingDir := filepath.Join(s.baseDir, contentType, "staging")

	if _, err := os.Stat(stagingDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("staging directory does not exist: %s", stagingDir)
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

	entriesBefore, _ := os.ReadDir(stagingDir)
	if err := os.Rename(stagingDir, archiveDir); err != nil {
		return nil, fmt.Errorf("archive staging: %w", err)
	}

	if s.logger != nil {
		s.logger.Info().
			Str("content_type", contentType).
			Str("archived_to", filepath.Base(archiveDir)).
			Int("files", len(entriesBefore)).
			Msg("archived staging directory")
	}

	return &ArchiveResult{
		From:      stagingDir,
		To:        archiveDir,
		Archived:  filepath.Base(archiveDir),
		FileCount: len(entriesBefore),
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
