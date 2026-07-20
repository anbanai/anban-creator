package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// materializeReferenceAssetWithRoot is the Windows commit path. os.Root keeps
// every operation handle-relative so a junction or reparse-point swap cannot
// redirect a temp write or rename outside workDir. After this call returns,
// workspace ACLs and single-writer ownership govern later same-user mutations.
func materializeReferenceAssetWithRoot(ctx context.Context, workDir string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := openValidatedReferenceWorkspaceRoot(workDir)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(referenceImageDirName)
	if errors.Is(err, os.ErrNotExist) {
		if err := root.Mkdir(referenceImageDirName, 0o700); err != nil {
			return fmt.Errorf("create reference directory: %w", err)
		}
		info, err = root.Lstat(referenceImageDirName)
	}
	if err != nil {
		return fmt.Errorf("inspect reference directory: %w", err)
	}
	if !safeReferenceDirectoryInfo(info) {
		return errors.New("reference directory is a symlink or reparse point")
	}
	dir, err := root.OpenRoot(referenceImageDirName)
	if err != nil {
		return fmt.Errorf("open reference directory root: %w", err)
	}
	defer dir.Close()
	if err := dir.Chmod(".", 0o700); err != nil {
		return fmt.Errorf("secure reference directory: %w", err)
	}
	tmpName, tmp, err := createReferenceRootTemp(dir)
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = dir.Remove(tmpName)
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
	if err := ensureReferenceWorkspaceRootStillLinked(workDir, root); err != nil {
		return err
	}
	if err := ensureReferenceRootStillLinked(root, dir); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := dir.Rename(tmpName, referenceImageFileName); err != nil {
		return fmt.Errorf("replace reference asset: %w", err)
	}
	return nil
}

func openValidatedReferenceWorkspaceRoot(workDir string) (*os.Root, error) {
	info, err := os.Lstat(workDir)
	if err != nil {
		return nil, fmt.Errorf("inspect reference workspace root: %w", err)
	}
	if !safeReferenceDirectoryInfo(info) {
		return nil, errors.New("reference workspace root is a symlink or reparse point")
	}
	root, err := os.OpenRoot(workDir)
	if err != nil {
		return nil, fmt.Errorf("open reference workspace root: %w", err)
	}
	if err := ensureReferenceWorkspaceRootStillLinked(workDir, root); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func ensureReferenceWorkspaceRootStillLinked(workDir string, root *os.Root) error {
	pathInfo, err := os.Lstat(workDir)
	if err != nil {
		return fmt.Errorf("reinspect reference workspace root: %w", err)
	}
	if !safeReferenceDirectoryInfo(pathInfo) {
		return errors.New("reference workspace root changed before commit")
	}
	pathInfo, err = os.Stat(workDir)
	if err != nil {
		return fmt.Errorf("stat reference workspace root: %w", err)
	}
	openedInfo, err := root.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect opened reference workspace root: %w", err)
	}
	if !os.SameFile(pathInfo, openedInfo) {
		return errors.New("reference workspace root changed before commit")
	}
	return nil
}

func safeReferenceDirectoryInfo(info os.FileInfo) bool {
	if info == nil || !info.IsDir() {
		return false
	}
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0
}

func createReferenceRootTemp(dir *os.Root) (string, *os.File, error) {
	for i := 0; i < 16; i++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := ".reference-" + hex.EncodeToString(random[:]) + ".tmp"
		file, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, fmt.Errorf("create reference temp file: %w", err)
		}
	}
	return "", nil, errors.New("allocate reference temp file")
}

func ensureReferenceRootStillLinked(root, opened *os.Root) error {
	current, err := root.OpenRoot(referenceImageDirName)
	if err != nil {
		return fmt.Errorf("reopen reference directory root: %w", err)
	}
	defer current.Close()
	openedInfo, err := opened.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect opened reference root: %w", err)
	}
	currentInfo, err := current.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect current reference root: %w", err)
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return errors.New("reference directory changed before commit")
	}
	return nil
}
