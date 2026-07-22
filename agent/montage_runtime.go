package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

var montageTaskInputNames = []string{
	".anban-creator",
	".task-context",
	"CLAUDE.md",
	"montage-input.json",
	"montage-tool-policy.json",
	"montage-pipeline-defaults.json",
}

func materializeMontageRuntime(workspace string) (string, error) {
	runtimePath := montageRuntimePath(workspace)
	if info, err := os.Lstat(runtimePath); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("Montage workspace %s must be a real directory", runtimePath)
		}
		return runtimePath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect Montage workspace: %w", err)
	}

	templatePath, err := resolveMontageTemplatePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(workspace, 0o770); err != nil {
		return "", fmt.Errorf("create task workspace: %w", err)
	}
	stagingPath, err := os.MkdirTemp(workspace, ".montage-init-")
	if err != nil {
		return "", fmt.Errorf("create Montage staging workspace: %w", err)
	}
	defer os.RemoveAll(stagingPath)
	if err := copyWritableTree(templatePath, stagingPath); err != nil {
		return "", fmt.Errorf("copy Montage template into workspace: %w", err)
	}
	if err := os.Rename(stagingPath, runtimePath); err != nil {
		if info, statErr := os.Lstat(runtimePath); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return runtimePath, nil
		}
		return "", fmt.Errorf("activate Montage workspace: %w", err)
	}
	return runtimePath, nil
}

func syncMontageTaskInputs(workspace, runtimePath string) error {
	for _, name := range montageTaskInputNames {
		source := filepath.Join(workspace, name)
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect Montage task input %s: %w", name, err)
		}
		destination := filepath.Join(runtimePath, name)
		var err error
		if name == "CLAUDE.md" {
			err = mergeMontageClaudeInstructions(source, destination)
		} else {
			err = mergeWritableEntry(source, destination)
		}
		if err != nil {
			return fmt.Errorf("copy Montage task input %s: %w", name, err)
		}
		if err := os.RemoveAll(source); err != nil {
			return fmt.Errorf("remove migrated Montage task input %s: %w", name, err)
		}
	}
	return nil
}

func mergeMontageClaudeInstructions(source, destination string) error {
	sourceBody, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("destination %s must be a regular file", destination)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	destinationBody, err := os.ReadFile(destination)
	if os.IsNotExist(err) {
		return copyWritableFile(source, destination, 0o644)
	}
	if err != nil {
		return err
	}
	section := append([]byte("\n\n# Anban Project Instructions\n\n"), sourceBody...)
	if bytes.Contains(destinationBody, section) {
		return nil
	}
	file, err := os.OpenFile(destination, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(section)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func resolveMontageTemplatePath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(serveragent.MontageTemplateEnvName)); configured != "" {
		if info, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("inspect Montage template %s: %w", configured, err)
		} else if !info.IsDir() {
			return "", fmt.Errorf("Montage template %s must be a directory", configured)
		}
		return configured, nil
	}

	for _, candidate := range []string{
		serveragent.ContainerMontageTemplatePath,
		filepath.Join("third_party", "OpenMontage"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Montage template is unavailable; set %s", serveragent.MontageTemplateEnvName)
}

func copyWritableTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			mode := info.Mode().Perm() | 0o700
			if err := os.MkdirAll(target, mode); err != nil {
				return err
			}
			return os.Chmod(target, mode)
		case info.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		case info.Mode().IsRegular():
			return copyWritableFile(path, target, info.Mode().Perm()|0o600)
		default:
			return fmt.Errorf("unsupported file type %s", path)
		}
	})
}

func copyWritableFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		_ = input.Close()
		return err
	}
	_, copyErr := io.Copy(output, input)
	inputCloseErr := input.Close()
	outputCloseErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if inputCloseErr != nil {
		return inputCloseErr
	}
	if outputCloseErr != nil {
		return outputCloseErr
	}
	return os.Chmod(destination, mode)
}

func mergeWritableEntry(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if targetInfo, err := os.Lstat(destination); err == nil {
			if !targetInfo.IsDir() || targetInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("destination %s is not a real directory", destination)
			}
		} else if os.IsNotExist(err) {
			if err := os.MkdirAll(destination, info.Mode().Perm()|0o700); err != nil {
				return err
			}
		} else {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := mergeWritableEntry(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	if targetInfo, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 && targetInfo.Mode()&os.ModeSymlink != 0 {
			sourceTarget, sourceErr := os.Readlink(source)
			destinationTarget, destinationErr := os.Readlink(destination)
			if sourceErr == nil && destinationErr == nil && sourceTarget == destinationTarget {
				return nil
			}
		}
		if info.Mode().IsRegular() && targetInfo.Mode().IsRegular() {
			same, compareErr := sameFileContents(source, destination)
			if compareErr != nil {
				return compareErr
			}
			if same {
				return nil
			}
		}
		return fmt.Errorf("destination %s conflicts with workspace input", destination)
	} else if !os.IsNotExist(err) {
		return err
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	case info.Mode().IsRegular():
		return copyWritableFile(source, destination, info.Mode().Perm()|0o600)
	default:
		return fmt.Errorf("unsupported file type %s", source)
	}
}

func sameFileContents(first, second string) (bool, error) {
	firstBody, err := os.ReadFile(first)
	if err != nil {
		return false, err
	}
	secondBody, err := os.ReadFile(second)
	if err != nil {
		return false, err
	}
	return bytes.Equal(firstBody, secondBody), nil
}
