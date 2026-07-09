package memory

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	currentArchiveName = "current.tar.gz"
	manifestName       = "manifest.json"
	memoryContentType  = "application/gzip"
	manifestType       = "application/json"
)

// Locker serializes project memory writeback across replicas.
type Locker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (release func(), ok bool, err error)
}

// ProjectMemoryManager stages project memory into task workspaces and merges it
// back to object storage after successful execution.
type ProjectMemoryManager struct {
	store  storage.Provider
	cfg    config.MemoryConfig
	locker Locker
	log    zerolog.Logger
}

type Manifest struct {
	ProjectID   string `json:"project_id"`
	Version     int64  `json:"version"`
	LastTaskID  string `json:"last_task_id"`
	UpdatedAt   string `json:"updated_at"`
	ContentHash string `json:"content_hash"`
	Source      string `json:"source"`
}

func NewProjectMemoryManager(store storage.Provider, cfg config.MemoryConfig, locker Locker, log zerolog.Logger) *ProjectMemoryManager {
	return &ProjectMemoryManager{store: store, cfg: cfg, locker: locker, log: log}
}

func (m *ProjectMemoryManager) Enabled() bool {
	return m != nil && m.cfg.Enabled && m.store != nil
}

func (m *ProjectMemoryManager) RuntimeDir(workDir string) string {
	if m == nil {
		return ""
	}
	return filepath.Join(workDir, filepath.FromSlash(m.cfg.RuntimeDir))
}

func (m *ProjectMemoryManager) Stage(ctx context.Context, projectID, taskID, workDir string) (string, error) {
	if !m.Enabled() || strings.TrimSpace(projectID) == "" || strings.TrimSpace(workDir) == "" {
		return "", nil
	}
	runtimeDir := m.RuntimeDir(workDir)
	if err := os.RemoveAll(runtimeDir); err != nil {
		return "", fmt.Errorf("clear runtime memory dir: %w", err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", fmt.Errorf("create runtime memory dir: %w", err)
	}

	data, err := m.store.Read(ctx, m.currentKey(projectID))
	if err != nil {
		if isMissingObject(err) {
			if err := writeDefaultMemory(runtimeDir, projectID); err != nil {
				return "", err
			}
			return runtimeDir, nil
		}
		return "", fmt.Errorf("read project memory archive: %w", err)
	}
	if m.cfg.MaxArchiveBytes > 0 && int64(len(data)) > m.cfg.MaxArchiveBytes {
		return "", fmt.Errorf("project memory archive exceeds max_archive_bytes (%d > %d)", len(data), m.cfg.MaxArchiveBytes)
	}
	if err := extractTarGz(bytes.NewReader(data), runtimeDir, m.cfg.MaxArchiveBytes); err != nil {
		return "", err
	}
	return runtimeDir, nil
}

func (m *ProjectMemoryManager) Merge(ctx context.Context, projectID, taskID, workDir string) (bool, error) {
	if !m.Enabled() || strings.TrimSpace(projectID) == "" || strings.TrimSpace(workDir) == "" {
		return false, nil
	}
	runtimeDir := m.RuntimeDir(workDir)
	info, err := os.Stat(runtimeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat runtime memory dir: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("runtime memory path is not a directory: %s", runtimeDir)
	}
	archive, err := createTarGz(runtimeDir, m.cfg.MaxArchiveBytes)
	if err != nil {
		return false, err
	}
	if m.cfg.MaxArchiveBytes > 0 && int64(len(archive)) > m.cfg.MaxArchiveBytes {
		return false, fmt.Errorf("runtime memory archive exceeds max_archive_bytes (%d > %d)", len(archive), m.cfg.MaxArchiveBytes)
	}
	return m.storeArchive(ctx, projectID, taskID, archive)
}

// MergeArchive validates and persists a project memory archive captured from a
// remote agent workspace. It lets remote executors keep the same memory behavior
// as local/docker without mounting the workspace filesystem on the server.
func (m *ProjectMemoryManager) MergeArchive(ctx context.Context, projectID, taskID string, archive []byte) (bool, error) {
	if !m.Enabled() || strings.TrimSpace(projectID) == "" || len(archive) == 0 {
		return false, nil
	}
	if m.cfg.MaxArchiveBytes > 0 && int64(len(archive)) > m.cfg.MaxArchiveBytes {
		return false, fmt.Errorf("remote memory archive exceeds max_archive_bytes (%d > %d)", len(archive), m.cfg.MaxArchiveBytes)
	}
	normalized, err := m.normalizeArchive(archive)
	if err != nil {
		return false, err
	}
	if m.cfg.MaxArchiveBytes > 0 && int64(len(normalized)) > m.cfg.MaxArchiveBytes {
		return false, fmt.Errorf("remote memory archive exceeds max_archive_bytes after normalization (%d > %d)", len(normalized), m.cfg.MaxArchiveBytes)
	}
	return m.storeArchive(ctx, projectID, taskID, normalized)
}

func (m *ProjectMemoryManager) normalizeArchive(archive []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "anban-project-memory-*")
	if err != nil {
		return nil, fmt.Errorf("create project memory normalize dir: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := extractTarGz(bytes.NewReader(archive), dir, m.cfg.MaxArchiveBytes); err != nil {
		return nil, err
	}
	return createTarGz(dir, m.cfg.MaxArchiveBytes)
}

func (m *ProjectMemoryManager) storeArchive(ctx context.Context, projectID, taskID string, archive []byte) (bool, error) {
	release := func() {}
	if m.locker != nil {
		var ok bool
		var err error
		release, ok, err = m.locker.TryLock(ctx, "memory:project:"+projectID, m.cfg.LockTTL)
		if err != nil {
			return false, fmt.Errorf("project memory lock: %w", err)
		}
		if !ok {
			m.log.Warn().Str("project_id", projectID).Str("task_id", taskID).Msg("project memory merge skipped because lock is busy")
			return false, nil
		}
	}
	defer release()

	now := time.Now().UTC()
	versionKey := m.versionKey(projectID, taskID, now)
	if _, err := m.store.Upload(ctx, versionKey, bytes.NewReader(archive), memoryContentType); err != nil {
		return false, fmt.Errorf("upload project memory version: %w", err)
	}
	if _, err := m.store.Upload(ctx, m.currentKey(projectID), bytes.NewReader(archive), memoryContentType); err != nil {
		return false, fmt.Errorf("upload project memory current archive: %w", err)
	}
	sum := sha256.Sum256(archive)
	manifest := Manifest{
		ProjectID:   projectID,
		Version:     now.Unix(),
		LastTaskID:  taskID,
		UpdatedAt:   now.Format(time.RFC3339),
		ContentHash: hex.EncodeToString(sum[:]),
		Source:      "claude-auto-memory",
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return false, fmt.Errorf("marshal project memory manifest: %w", err)
	}
	if _, err := m.store.Upload(ctx, m.manifestKey(projectID), bytes.NewReader(manifestData), manifestType); err != nil {
		return false, fmt.Errorf("upload project memory manifest: %w", err)
	}
	return true, nil
}

func (m *ProjectMemoryManager) currentKey(projectID string) string {
	return filepath.ToSlash(filepath.Join(strings.Trim(m.cfg.OSSPrefix, "/"), projectID, currentArchiveName))
}

func (m *ProjectMemoryManager) manifestKey(projectID string) string {
	return filepath.ToSlash(filepath.Join(strings.Trim(m.cfg.OSSPrefix, "/"), projectID, manifestName))
}

func (m *ProjectMemoryManager) versionKey(projectID, taskID string, now time.Time) string {
	stamp := now.Format("20060102T150405Z")
	return filepath.ToSlash(filepath.Join(strings.Trim(m.cfg.OSSPrefix, "/"), projectID, "versions", stamp+"-"+safeKeyPart(taskID)+".tar.gz"))
}

func safeKeyPart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(s)
}

func writeDefaultMemory(dir, projectID string) error {
	body := "# Project Memory\n\n" +
		"This file stores Claude Code auto memory for project `" + projectID + "`.\n"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(body), 0o644); err != nil {
		return fmt.Errorf("write default MEMORY.md: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		return fmt.Errorf("create default memory topics dir: %w", err)
	}
	return nil
}

func extractTarGz(r io.Reader, destDir string, maxBytes int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open project memory gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var extractedBytes int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read project memory tar: %w", err)
		}
		if isArchiveRootEntry(h.Name) {
			continue
		}
		target, err := safeArchiveTarget(destDir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create memory dir: %w", err)
			}
		case tar.TypeReg:
			if h.Size < 0 {
				return fmt.Errorf("invalid project memory archive entry size for %q", h.Name)
			}
			extractedBytes += h.Size
			if maxBytes > 0 && extractedBytes > maxBytes {
				return fmt.Errorf("project memory archive extracted bytes exceed max_archive_bytes (%d > %d)", extractedBytes, maxBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create memory parent dir: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("create memory file: %w", err)
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return fmt.Errorf("write memory file: %w", copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close memory file: %w", closeErr)
			}
		default:
			return fmt.Errorf("unsupported project memory archive entry type %d for %q", h.Typeflag, h.Name)
		}
	}
}

func isArchiveRootEntry(name string) bool {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	return name == "." || name == "./"
}

func safeArchiveTarget(destDir, name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	target := filepath.Join(destDir, cleaned)
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if absTarget != absDest && !strings.HasPrefix(absTarget, absDest+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return target, nil
}

func createTarGz(srcDir string, maxFileBytes int64) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 || !mode.IsRegular() && !mode.IsDir() {
			return nil
		}
		if maxFileBytes > 0 && mode.IsRegular() && info.Size() > maxFileBytes {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: rel + "/", Mode: 0o755, Typeflag: tar.TypeDir})
		}
		h := &tar.Header{Name: rel, Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, fmt.Errorf("create project memory archive: %w", err)
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("close project memory tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close project memory gzip: %w", err)
	}
	return buf.Bytes(), nil
}

func isMissingObject(err error) bool {
	msg := strings.ToLower(err.Error())
	return errors.Is(err, os.ErrNotExist) ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "404")
}
