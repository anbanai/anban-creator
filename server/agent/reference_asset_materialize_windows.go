//go:build windows

package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func materializeReferenceAssetBytes(ctx context.Context, workDir string, data []byte) error {
	if err := validateWindowsReferenceDirectory(workDir, true); err != nil {
		return err
	}
	dir := filepath.Join(workDir, referenceImageDirName)
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create reference directory: %w", err)
	}
	if err := validateWindowsReferenceDirectory(dir, true); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure reference directory: %w", err)
	}
	tmpPath, tmp, err := createWindowsReferenceTemp(dir)
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write reference temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync reference temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure reference temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close reference temp file: %w", err)
	}
	if referenceMaterializeBeforeCommitHook != nil {
		if err := referenceMaterializeBeforeCommitHook(); err != nil {
			return err
		}
	}
	if err := validateWindowsReferenceDirectory(workDir, true); err != nil {
		return err
	}
	if err := validateWindowsReferenceDirectory(dir, true); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(tmpPath)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(dir, referenceImageFileName))
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("replace reference asset: %w", err)
	}
	return nil
}

func createWindowsReferenceTemp(dir string) (string, *os.File, error) {
	for i := 0; i < 16; i++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		path := filepath.Join(dir, ".reference-"+hex.EncodeToString(random[:])+".tmp")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return path, file, nil
		}
		if !os.IsExist(err) {
			return "", nil, fmt.Errorf("create reference temp file: %w", err)
		}
	}
	return "", nil, errors.New("allocate reference temp file")
}

func validateWindowsReferenceDirectory(path string, wantDir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect reference path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || (wantDir && !info.IsDir()) {
		return errors.New("reference path is unsafe")
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(ptr)
	if err != nil {
		return fmt.Errorf("inspect reference path attributes: %w", err)
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("reference path is a reparse point")
	}
	return nil
}
