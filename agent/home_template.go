package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const homeTemplateEnv = "ANBAN_HOME_TEMPLATE"

func materializeHomeTemplateFromEnvironment() error {
	templateRoot := strings.TrimSpace(os.Getenv(homeTemplateEnv))
	if templateRoot == "" {
		return nil
	}
	homeRoot := strings.TrimSpace(os.Getenv("HOME"))
	if homeRoot == "" {
		return fmt.Errorf("HOME is required when %s is set", homeTemplateEnv)
	}
	return materializeHomeTemplate(templateRoot, homeRoot)
}

// materializeHomeTemplate seeds only missing runtime-home entries. The template
// is immutable image content and the destination is a private Job emptyDir, so
// rejecting every observed symlink plus using exclusive file creation closes
// the relevant path-redirection boundary without overwriting runtime state.
func materializeHomeTemplate(templateRoot, homeRoot string) error {
	templateRoot, err := filepath.Abs(filepath.Clean(templateRoot))
	if err != nil {
		return fmt.Errorf("resolve home template: %w", err)
	}
	homeRoot, err = filepath.Abs(filepath.Clean(homeRoot))
	if err != nil {
		return fmt.Errorf("resolve runtime home: %w", err)
	}
	if pathsOverlap(templateRoot, homeRoot) {
		return fmt.Errorf("home template %q and runtime home %q must be disjoint", templateRoot, homeRoot)
	}
	if err := rejectSymlinkPathComponents(templateRoot, "home template"); err != nil {
		return err
	}
	if err := requireRealDirectory(templateRoot, "home template"); err != nil {
		return err
	}
	if err := rejectSymlinkPathComponents(homeRoot, "runtime home"); err != nil {
		return err
	}
	if err := os.MkdirAll(homeRoot, 0o700); err != nil {
		return fmt.Errorf("create runtime home %q: %w", homeRoot, err)
	}
	if err := rejectSymlinkPathComponents(homeRoot, "runtime home"); err != nil {
		return err
	}
	if err := requireRealDirectory(homeRoot, "runtime home"); err != nil {
		return err
	}

	return filepath.WalkDir(templateRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(templateRoot, sourcePath)
		if err != nil {
			return fmt.Errorf("resolve template path %q: %w", sourcePath, err)
		}
		if relative == "." {
			return nil
		}
		if !safeRelativePath(relative) {
			return fmt.Errorf("home template path escapes root: %q", relative)
		}
		if strings.EqualFold(filepath.Base(relative), ".credentials.json") {
			return fmt.Errorf("home template contains credential file %q", relative)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("home template contains symlink %q", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect home template entry %q: %w", relative, err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("home template contains unsupported file type %q", relative)
		}

		targetPath := filepath.Join(homeRoot, relative)
		exists, err := validateExistingTarget(targetPath, info.IsDir())
		if err != nil {
			return fmt.Errorf("validate runtime home entry %q: %w", relative, err)
		}
		if exists {
			return nil
		}
		if info.IsDir() {
			mode := info.Mode().Perm() | 0o700
			if err := os.Mkdir(targetPath, mode); err != nil {
				return fmt.Errorf("create runtime home directory %q: %w", relative, err)
			}
			return nil
		}
		if err := copyMissingHomeFile(sourcePath, targetPath, info.Mode().Perm()|0o600); err != nil {
			return fmt.Errorf("copy runtime home file %q: %w", relative, err)
		}
		return nil
	})
}

func requireRealDirectory(path, label string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s %q: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %q must not be a symlink", label, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %q must be a directory", label, path)
	}
	return nil
}

func rejectSymlinkPathComponents(path, label string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(clean, root)
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect %s path component %q: %w", label, current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s path component %q must not be a symlink", label, current)
		}
	}
	return nil
}

func validateExistingTarget(path string, wantDirectory bool) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("destination is a symlink")
	}
	if wantDirectory && !info.IsDir() {
		return false, fmt.Errorf("destination is not a directory")
	}
	if !wantDirectory && !info.Mode().IsRegular() {
		return false, fmt.Errorf("destination is not a regular file")
	}
	return true, nil
}

func copyMissingHomeFile(sourcePath, targetPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(targetPath)
		return errors.Join(copyErr, closeErr)
	}
	if err := os.Chmod(targetPath, mode); err != nil {
		_ = os.Remove(targetPath)
		return err
	}
	return nil
}

func safeRelativePath(path string) bool {
	return path != ".." && !filepath.IsAbs(path) && !strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && safeRelativePath(relative)
}
