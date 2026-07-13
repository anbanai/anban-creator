//go:build windows

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func materializePreparedBootstrap(ctx context.Context, root string, prepared []preparedBootstrapFile, client *http.Client) error {
	stage, err := os.MkdirTemp(root, ".anban-bootstrap-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	var total int64
	for i, item := range prepared {
		path := filepath.Join(stage, fmt.Sprintf("%06d", i))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, item.mode)
		if err != nil {
			return err
		}
		err = stageBootstrapSource(ctx, item, client, file, &total)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	existing := make([]bool, len(prepared))
	for i, item := range prepared {
		target := filepath.Join(root, item.rel)
		if err := validateWindowsBootstrapParents(root, filepath.Dir(target)); err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || !windowsBootstrapModeCompatible(item.mode, info.Mode()) {
			return fmt.Errorf("bootstrap target %q conflicts with existing file", item.rel)
		}
		equal, err := filesEqual(target, filepath.Join(stage, fmt.Sprintf("%06d", i)))
		if err != nil || !equal {
			return fmt.Errorf("bootstrap target %q conflicts with existing file", item.rel)
		}
		existing[i] = true
	}
	var createdFiles, createdDirs []string
	for i, item := range prepared {
		if existing[i] {
			continue
		}
		target := filepath.Join(root, item.rel)
		if err := createWindowsBootstrapParents(root, filepath.Dir(target), &createdDirs); err != nil {
			rollbackWindowsBootstrap(createdFiles, createdDirs)
			return err
		}
		if bootstrapCommitHook != nil {
			if err := bootstrapCommitHook(item.rel); err != nil {
				rollbackWindowsBootstrap(createdFiles, createdDirs)
				return err
			}
		}
		if err := os.Link(filepath.Join(stage, fmt.Sprintf("%06d", i)), target); err != nil {
			rollbackWindowsBootstrap(createdFiles, createdDirs)
			return err
		}
		createdFiles = append(createdFiles, target)
	}
	return nil
}

func validateWindowsBootstrapParents(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || !pathWithinRoot(rel) {
		return fmt.Errorf("bootstrap parent escapes workspace")
	}
	current := root
	for _, component := range splitPathComponents(rel) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bootstrap parent is unsafe")
		}
	}
	return nil
}

func createWindowsBootstrapParents(root, target string, created *[]string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || !pathWithinRoot(rel) {
		return fmt.Errorf("bootstrap parent escapes workspace")
	}
	current := root
	for _, component := range splitPathComponents(rel) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			*created = append(*created, current)
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("bootstrap parent is unsafe")
		}
	}
	return nil
}

func rollbackWindowsBootstrap(files, dirs []string) {
	for i := len(files) - 1; i >= 0; i-- {
		_ = os.Remove(files[i])
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
}
