package memory

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
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	StatusEmpty    = "empty"
	StatusReady    = "ready"
	maxScanEntries = 4096
)

var (
	ErrInvalidProjectID           = errors.New("project ID must be a canonical UUID")
	ErrProjectMemoryMissing       = errors.New("project memory directory is missing")
	ErrProjectMemoryQuotaExceeded = errors.New("project memory quota exceeded")
	errFileChanged                = errors.New("project memory file changed while reading")
)

type Limits struct {
	MaxProjectBytes int64
	MaxFiles        int
	MaxDepth        int
	MaxFileBytes    int64
	MaxPreviewBytes int64
}

type FileView struct {
	Path       string    `json:"path"`
	Content    string    `json:"content"`
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at"`
	Truncated  bool      `json:"truncated"`
}

type ProjectView struct {
	Status    string     `json:"status"`
	UpdatedAt *time.Time `json:"updated_at"`
	Partial   bool       `json:"partial"`
	Files     []FileView `json:"files"`
}

type FilesystemStore struct {
	root     string
	projects string
	limits   Limits
}

func NewFilesystemStore(root string, limits Limits) (*FilesystemStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("project memory root must be an absolute path")
	}
	if limits.MaxProjectBytes <= 0 || limits.MaxFiles <= 0 || limits.MaxDepth <= 0 || limits.MaxFileBytes <= 0 || limits.MaxPreviewBytes <= 0 {
		return nil, fmt.Errorf("project memory limits must be positive")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create project memory root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("project memory root must be a real directory")
	}
	projects := filepath.Join(root, "projects")
	if err := os.MkdirAll(projects, 0o750); err != nil {
		return nil, fmt.Errorf("create project memory projects directory: %w", err)
	}
	projectsInfo, err := os.Lstat(projects)
	if err != nil || !projectsInfo.IsDir() || projectsInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("project memory projects path must be a real directory")
	}
	probe, err := os.CreateTemp(projects, ".write-probe-")
	if err != nil {
		return nil, fmt.Errorf("project memory root is not writable: %w", err)
	}
	probeName := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probeName)
	if closeErr != nil || removeErr != nil {
		return nil, fmt.Errorf("verify project memory root: %w", errors.Join(closeErr, removeErr))
	}
	return &FilesystemStore{root: root, projects: projects, limits: limits}, nil
}

func (s *FilesystemStore) EnsureProject(ctx context.Context, projectID string) error {
	projectDir, err := s.projectDir(projectID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Mkdir(projectDir, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create project memory directory: %w", err)
	}
	if err := s.requireDirectory(projectDir); err != nil {
		return err
	}
	if err := os.Chmod(projectDir, 0o700); err != nil {
		return fmt.Errorf("secure project memory directory: %w", err)
	}
	return s.checkQuota(ctx, projectDir)
}

func (s *FilesystemStore) RequireProject(ctx context.Context, projectID string) error {
	projectDir, err := s.projectDir(projectID)
	if err != nil {
		return err
	}
	if err := s.requireDirectory(projectDir); err != nil {
		return err
	}
	return s.checkQuota(ctx, projectDir)
}

func (s *FilesystemStore) ReadProject(ctx context.Context, projectID string) (ProjectView, error) {
	empty := ProjectView{Status: StatusEmpty, Files: []FileView{}}
	projectDir, err := s.projectDir(projectID)
	if err != nil {
		return empty, err
	}
	if err := s.requireDirectory(projectDir); err != nil {
		if errors.Is(err, ErrProjectMemoryMissing) {
			return empty, nil
		}
		return empty, err
	}

	type candidate struct {
		path string
		rel  string
	}
	var candidates []candidate
	partial := false
	err = filepath.WalkDir(projectDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == projectDir {
			return nil
		}
		rel, err := filepath.Rel(projectDir, path)
		if err != nil {
			return err
		}
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1
		if entry.IsDir() && depth >= s.limits.MaxDepth {
			partial = true
			return filepath.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				partial = true
			}
			return nil
		}
		if depth > s.limits.MaxDepth || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			if depth > s.limits.MaxDepth {
				partial = true
			}
			return nil
		}
		if len(candidates) >= maxScanEntries {
			partial = true
			return nil
		}
		candidates = append(candidates, candidate{path: path, rel: filepath.ToSlash(rel)})
		return nil
	})
	if err != nil {
		return empty, fmt.Errorf("scan project memory: %w", err)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].rel == "MEMORY.md" {
			return candidates[j].rel != "MEMORY.md"
		}
		if candidates[j].rel == "MEMORY.md" {
			return false
		}
		return candidates[i].rel < candidates[j].rel
	})

	view := empty
	view.Partial = partial
	remaining := s.limits.MaxPreviewBytes
	for index, item := range candidates {
		if len(view.Files) >= s.limits.MaxFiles {
			if index < len(candidates) {
				view.Partial = true
			}
			break
		}
		if remaining <= 0 {
			view.Partial = true
			break
		}
		maxBytes := min(s.limits.MaxFileBytes, remaining)
		file, ok, err := readMarkdownFile(item.path, maxBytes)
		if err != nil {
			return empty, fmt.Errorf("read project memory file %q: %w", item.rel, err)
		}
		if !ok {
			view.Partial = true
			continue
		}
		file.Path = item.rel
		view.Files = append(view.Files, file)
		remaining -= int64(len(file.Content))
		if file.Truncated {
			view.Partial = true
		}
		if view.UpdatedAt == nil || file.ModifiedAt.After(*view.UpdatedAt) {
			updated := file.ModifiedAt
			view.UpdatedAt = &updated
		}
	}
	if len(view.Files) > 0 {
		view.Status = StatusReady
	}
	return view, nil
}

func (s *FilesystemStore) DeleteProject(ctx context.Context, projectID string) error {
	projectDir, err := s.projectDir(projectID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("delete project memory directory: %w", err)
	}
	return nil
}

func (s *FilesystemStore) projectDir(projectID string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil || parsed.String() != projectID {
		return "", ErrInvalidProjectID
	}
	return filepath.Join(s.projects, projectID), nil
}

func (s *FilesystemStore) requireDirectory(projectDir string) error {
	info, err := os.Lstat(projectDir)
	if os.IsNotExist(err) {
		return ErrProjectMemoryMissing
	}
	if err != nil {
		return fmt.Errorf("stat project memory directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("project memory path must be a real directory")
	}
	return nil
}

func (s *FilesystemStore) checkQuota(ctx context.Context, projectDir string) error {
	var total int64
	err := filepath.WalkDir(projectDir, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		if total > s.limits.MaxProjectBytes {
			return ErrProjectMemoryQuotaExceeded
		}
		return nil
	})
	return err
}

func readMarkdownFile(path string, maxBytes int64) (FileView, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		view, ok, err := readMarkdownFileOnce(path, maxBytes)
		if !errors.Is(err, errFileChanged) {
			return view, ok, err
		}
	}
	return FileView{}, false, errFileChanged
}

func readMarkdownFileOnce(path string, maxBytes int64) (FileView, bool, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return FileView{}, false, err
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return FileView{}, false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return FileView{}, false, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return FileView{}, false, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return FileView{}, false, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return FileView{}, false, err
	}
	truncated := int64(len(data)) > maxBytes || before.Size() > maxBytes
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	for removed := 0; removed < utf8.UTFMax-1 && len(data) > 0 && !utf8.Valid(data) && truncated; removed++ {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return FileView{}, false, nil
	}
	after, err := os.Lstat(path)
	if err != nil {
		return FileView{}, false, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return FileView{}, false, errFileChanged
	}
	return FileView{Content: string(data), SizeBytes: before.Size(), ModifiedAt: before.ModTime().UTC(), Truncated: truncated}, true, nil
}
