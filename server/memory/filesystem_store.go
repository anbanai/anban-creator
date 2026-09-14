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
	ErrInvalidProjectID            = errors.New("project ID must be a canonical UUID")
	ErrProjectMemoryMissing        = errors.New("project memory directory is missing")
	ErrProjectMemoryQuotaExceeded  = errors.New("project memory quota exceeded")
	ErrProjectMemoryTooManyEntries = errors.New("project memory contains too many entries")
	errFileChanged                 = errors.New("project memory file changed while reading")
	errUnsafePath                  = errors.New("project memory path contains a symbolic link")
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

	type candidate struct{ rel string }
	var candidates []candidate
	partial := false
	limitReached, err := walkProjectEntries(ctx, projectDir, maxScanEntries, func(rel string, entry fs.DirEntry) (bool, error) {
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1
		if entry.IsDir() && depth >= s.limits.MaxDepth {
			partial = true
			return true, nil
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				partial = true
			}
			return false, nil
		}
		if depth > s.limits.MaxDepth || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			if depth > s.limits.MaxDepth {
				partial = true
			}
			return false, nil
		}
		candidates = append(candidates, candidate{rel: filepath.ToSlash(rel)})
		return false, nil
	})
	if err != nil {
		return empty, fmt.Errorf("scan project memory: %w", err)
	}
	partial = partial || limitReached
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
		file, ok, err := readMarkdownFileInProject(projectDir, item.rel, maxBytes)
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
	limitReached, err := walkProjectEntries(ctx, projectDir, maxScanEntries, func(_ string, entry fs.DirEntry) (bool, error) {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return false, nil
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		if total > s.limits.MaxProjectBytes {
			return false, ErrProjectMemoryQuotaExceeded
		}
		return false, nil
	})
	if limitReached {
		return ErrProjectMemoryTooManyEntries
	}
	return err
}

type projectEntryVisitor func(relativePath string, entry fs.DirEntry) (skipDir bool, err error)

// walkProjectEntries reads directories in bounded batches. filepath.WalkDir
// sorts an entire directory before visiting it, which defeats an entry limit
// when a single directory contains a very large number of files.
func walkProjectEntries(ctx context.Context, projectDir string, maxEntries int, visit projectEntryVisitor) (bool, error) {
	const readBatchSize = 128
	type pendingDirectory struct{ relativePath string }
	pending := []pendingDirectory{{}}
	entriesSeen := 0

	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		dir, err := openProjectDirectoryNoSymlinks(projectDir, current.relativePath)
		if err != nil {
			return false, err
		}
		var children []pendingDirectory
		for {
			remaining := maxEntries - entriesSeen
			if remaining < 0 {
				_ = dir.Close()
				return true, nil
			}
			readSize := min(readBatchSize, remaining+1)
			entries, readErr := dir.ReadDir(readSize)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return false, errors.Join(err, dir.Close())
				}
				entriesSeen++
				if entriesSeen > maxEntries {
					_ = dir.Close()
					return true, nil
				}
				relativePath := entry.Name()
				if current.relativePath != "" {
					relativePath = filepath.Join(current.relativePath, entry.Name())
				}
				skipDir, err := visit(filepath.ToSlash(relativePath), entry)
				if err != nil {
					return false, errors.Join(err, dir.Close())
				}
				if entry.IsDir() && !skipDir {
					children = append(children, pendingDirectory{relativePath: relativePath})
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					return false, errors.Join(readErr, dir.Close())
				}
				break
			}
		}
		if err := dir.Close(); err != nil {
			return false, err
		}
		for index := len(children) - 1; index >= 0; index-- {
			pending = append(pending, children[index])
		}
	}
	return false, nil
}

func readMarkdownFileInProject(projectDir, relativePath string, maxBytes int64) (FileView, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		view, ok, err := readMarkdownFileInProjectOnce(projectDir, relativePath, maxBytes)
		if !errors.Is(err, errFileChanged) {
			return view, ok, err
		}
	}
	return FileView{}, false, nil
}

func readMarkdownFileInProjectOnce(projectDir, relativePath string, maxBytes int64) (FileView, bool, error) {
	f, err := openProjectFileNoSymlinks(projectDir, relativePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errUnsafePath) {
			return FileView{}, false, errFileChanged
		}
		return FileView{}, false, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return FileView{}, false, err
	}
	if !opened.Mode().IsRegular() {
		return FileView{}, false, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return FileView{}, false, err
	}
	truncated := int64(len(data)) > maxBytes || opened.Size() > maxBytes
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	for removed := 0; removed < utf8.UTFMax-1 && len(data) > 0 && !utf8.Valid(data) && truncated; removed++ {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) {
		return FileView{}, false, nil
	}
	after, err := f.Stat()
	if err != nil {
		return FileView{}, false, err
	}
	if opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return FileView{}, false, errFileChanged
	}
	current, err := openProjectFileNoSymlinks(projectDir, relativePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errUnsafePath) {
			return FileView{}, false, errFileChanged
		}
		return FileView{}, false, err
	}
	currentInfo, statErr := current.Stat()
	closeErr := current.Close()
	if statErr != nil || closeErr != nil {
		return FileView{}, false, errors.Join(statErr, closeErr)
	}
	if !os.SameFile(opened, currentInfo) {
		return FileView{}, false, errFileChanged
	}
	return FileView{Content: string(data), SizeBytes: opened.Size(), ModifiedAt: opened.ModTime().UTC(), Truncated: truncated}, true, nil
}
