//go:build unix

package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func materializeReferenceAssetBytes(ctx context.Context, workDir string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rootFD, err := unix.Open(workDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open reference workspace: %w", err)
	}
	defer unix.Close(rootFD)
	mkdirErr := unix.Mkdirat(rootFD, referenceImageDirName, 0o700)
	if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
		return fmt.Errorf("create reference directory: %w", mkdirErr)
	}
	if mkdirErr == nil {
		if err := unix.Fsync(rootFD); err != nil {
			return fmt.Errorf("sync reference directory entry: %w", err)
		}
	}
	dirFD, err := unix.Openat(rootFD, referenceImageDirName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open reference directory: %w", err)
	}
	defer unix.Close(dirFD)
	if err := unix.Fchmod(dirFD, 0o700); err != nil {
		return fmt.Errorf("secure reference directory: %w", err)
	}
	tmpName, tmpFD, err := createReferenceTempAt(dirFD)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(dirFD, tmpName, 0)
	tmp := os.NewFile(uintptr(tmpFD), tmpName)
	if tmp == nil {
		unix.Close(tmpFD)
		return errors.New("open reference temp file")
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write reference temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync reference temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
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
	if err := ensureReferenceWorkspaceLinked(workDir, rootFD); err != nil {
		return err
	}
	if err := ensureReferenceDirectoryLinked(rootFD, dirFD); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Renameat(dirFD, tmpName, dirFD, taskReferenceImageFileName); err != nil {
		return fmt.Errorf("replace reference asset: %w", err)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("sync reference directory: %w", err)
	}
	return nil
}

func ensureReferenceWorkspaceLinked(workDir string, rootFD int) error {
	currentFD, err := unix.Open(workDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen reference workspace: %w", err)
	}
	defer unix.Close(currentFD)
	var opened, current unix.Stat_t
	if err := unix.Fstat(rootFD, &opened); err != nil {
		return fmt.Errorf("inspect opened reference workspace: %w", err)
	}
	if err := unix.Fstat(currentFD, &current); err != nil {
		return fmt.Errorf("inspect current reference workspace: %w", err)
	}
	if opened.Dev != current.Dev || opened.Ino != current.Ino {
		return errors.New("reference workspace changed before commit")
	}
	return nil
}

func createReferenceTempAt(dirFD int) (string, int, error) {
	for i := 0; i < 16; i++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", -1, err
		}
		name := ".reference-" + hex.EncodeToString(random[:]) + ".tmp"
		fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if err == nil {
			return name, fd, nil
		}
		if !errors.Is(err, unix.EEXIST) {
			return "", -1, fmt.Errorf("create reference temp file: %w", err)
		}
	}
	return "", -1, errors.New("allocate reference temp file")
}

func ensureReferenceDirectoryLinked(rootFD, dirFD int) error {
	currentFD, err := unix.Openat(rootFD, referenceImageDirName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen reference directory: %w", err)
	}
	defer unix.Close(currentFD)
	var opened, current unix.Stat_t
	if err := unix.Fstat(dirFD, &opened); err != nil {
		return fmt.Errorf("inspect opened reference directory: %w", err)
	}
	if err := unix.Fstat(currentFD, &current); err != nil {
		return fmt.Errorf("inspect current reference directory: %w", err)
	}
	if opened.Dev != current.Dev || opened.Ino != current.Ino {
		return errors.New("reference directory changed before commit")
	}
	return nil
}
