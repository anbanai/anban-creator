package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// PrepareResult is the response for workspace preparation.
type PrepareResult struct {
	Path string `json:"path"`
}

// ArchiveResult is the response for workspace archiving.
type ArchiveResult struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// WorkspaceService manages working directory paths for content creation.
// It does NOT create or modify the filesystem — the agent is responsible
// for running mkdir -p and mv commands locally.
type WorkspaceService struct {
	baseDir      string
	workspaceDir string
}

// NewWorkspaceService creates a new WorkspaceService.
// baseDir is the root output directory (defaults to "output" in cwd if empty).
// workspaceDir is the Docker workspace base directory for task-based staging.
func NewWorkspaceService(baseDir, workspaceDir string) *WorkspaceService {
	if baseDir == "" {
		baseDir = filepath.Join(".", "output")
	}
	return &WorkspaceService{baseDir: baseDir, workspaceDir: workspaceDir}
}

// Prepare returns the canonical working directory path without touching the filesystem.
// When taskID is provided, returns "output" (relative to the task workspace root).
// Otherwise, returns baseDir/<contentType> (e.g. "output/rednote").
func (s *WorkspaceService) Prepare(contentType, taskID string) (*PrepareResult, error) {
	if contentType == "" {
		return nil, fmt.Errorf("content_type is required")
	}
	var workDir string
	if taskID != "" {
		workDir = "output"
	} else {
		workDir = filepath.Join(s.baseDir, contentType)
	}
	return &PrepareResult{Path: workDir}, nil
}

// Archive returns the computed archive directory path without moving files.
// The agent is responsible for running mkdir -p and mv commands locally.
func (s *WorkspaceService) Archive(contentType, name string) (*ArchiveResult, error) {
	if contentType == "" {
		return nil, fmt.Errorf("content_type is required")
	}
	contentDir := filepath.Join(s.baseDir, contentType)

	var archiveDir string
	if name != "" {
		archiveDir = filepath.Join(contentDir, sanitizeDirName(name))
	} else {
		archiveDir = filepath.Join(contentDir, time.Now().Format("20060102-150405"))
	}

	return &ArchiveResult{
		From: contentDir,
		To:   archiveDir,
	}, nil
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
