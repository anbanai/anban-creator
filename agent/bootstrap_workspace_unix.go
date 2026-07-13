//go:build unix

package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type stagedBootstrapFile struct {
	prepared preparedBootstrapFile
	name     string
	existing bool
}

type createdBootstrapEntry struct {
	parentFD int
	name     string
}

var errBootstrapParentMissing = errors.New("bootstrap parent missing")
var bootstrapFsync = unix.Fsync
var bootstrapRollbackDup = unix.Dup

func materializePreparedBootstrap(ctx context.Context, root string, prepared []preparedBootstrapFile, client *http.Client) error {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open workspace root: %w", err)
	}
	defer unix.Close(rootFD)
	stageName, err := createStageDirAt(rootFD)
	if err != nil {
		return err
	}
	if err := unix.Fsync(rootFD); err != nil {
		_ = unix.Unlinkat(rootFD, stageName, unix.AT_REMOVEDIR)
		return fmt.Errorf("sync workspace staging entry: %w", err)
	}
	stageFD, err := unix.Openat(rootFD, stageName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = unix.Unlinkat(rootFD, stageName, unix.AT_REMOVEDIR)
		return fmt.Errorf("open bootstrap staging directory: %w", err)
	}
	staged := make([]stagedBootstrapFile, 0, len(prepared))
	defer cleanupStageAt(rootFD, stageFD, stageName, staged)
	var total int64
	for i, item := range prepared {
		name := fmt.Sprintf("%06d", i)
		fd, err := unix.Openat(stageFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(item.mode.Perm()))
		if err != nil {
			return fmt.Errorf("create staged bootstrap file %q: %w", item.rel, err)
		}
		staged = append(staged, stagedBootstrapFile{prepared: item, name: name})
		file := os.NewFile(uintptr(fd), name)
		err = stageBootstrapSource(ctx, item, client, file, &total)
		if err == nil {
			err = file.Chmod(item.mode)
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return fmt.Errorf("close staged bootstrap file %q: %w", item.rel, closeErr)
		}
	}
	if err := unix.Fsync(stageFD); err != nil {
		return fmt.Errorf("sync bootstrap staging directory: %w", err)
	}
	for i := range staged {
		existing, err := precheckBootstrapTarget(rootFD, stageFD, staged[i])
		if err != nil {
			return err
		}
		staged[i].existing = existing
	}
	var createdFiles, createdDirs []createdBootstrapEntry
	rollback := true
	defer func() {
		if rollback {
			rollbackBootstrapAt(rootFD, createdFiles, createdDirs)
		} else {
			closeCreatedBootstrapEntries(createdFiles, createdDirs)
		}
	}()
	for _, item := range staged {
		if item.existing {
			continue
		}
		parentFD, base, err := openBootstrapParentAt(rootFD, item.prepared.rel, true, &createdDirs)
		if err != nil {
			return err
		}
		if bootstrapCommitHook != nil {
			if err := bootstrapCommitHook(item.prepared.rel); err != nil {
				unix.Close(parentFD)
				return err
			}
		}
		err = unix.Linkat(stageFD, item.name, parentFD, base, 0)
		if err == nil {
			createdFiles = append(createdFiles, createdBootstrapEntry{parentFD: parentFD, name: base})
			err = bootstrapFsync(parentFD)
		}
		if err != nil {
			if len(createdFiles) == 0 || createdFiles[len(createdFiles)-1].parentFD != parentFD {
				unix.Close(parentFD)
			}
			return fmt.Errorf("publish bootstrap file %q: %w", item.prepared.rel, err)
		}
	}
	rollback = false
	return nil
}

func createStageDirAt(rootFD int) (string, error) {
	for i := 0; i < 16; i++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		name := ".anban-bootstrap-stage-" + hex.EncodeToString(random[:])
		if err := unix.Mkdirat(rootFD, name, 0o700); err == nil {
			return name, nil
		} else if !errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("create bootstrap staging directory: %w", err)
		}
	}
	return "", fmt.Errorf("allocate bootstrap staging directory")
}

func cleanupStageAt(rootFD, stageFD int, stageName string, staged []stagedBootstrapFile) {
	for _, item := range staged {
		_ = unix.Unlinkat(stageFD, item.name, 0)
	}
	if duplicate, err := unix.Dup(stageFD); err == nil {
		directory := os.NewFile(uintptr(duplicate), stageName)
		if names, readErr := directory.Readdirnames(-1); readErr == nil || errors.Is(readErr, io.EOF) {
			for _, name := range names {
				_ = unix.Unlinkat(stageFD, name, 0)
			}
		}
		_ = directory.Close()
	}
	_ = unix.Close(stageFD)
	_ = unix.Unlinkat(rootFD, stageName, unix.AT_REMOVEDIR)
}

func precheckBootstrapTarget(rootFD, stageFD int, item stagedBootstrapFile) (bool, error) {
	parentFD, base, err := openBootstrapParentAt(rootFD, item.prepared.rel, false, nil)
	if errors.Is(err, errBootstrapParentMissing) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(parentFD)
	var stat unix.Stat_t
	err = unix.Fstatat(parentFD, base, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect bootstrap target %q: %w", item.prepared.rel, err)
	}
	if uint32(stat.Mode)&unix.S_IFMT != unix.S_IFREG || uint32(stat.Mode)&0o7777 != uint32(item.prepared.mode.Perm()) {
		return false, fmt.Errorf("bootstrap target %q conflicts with existing file", item.prepared.rel)
	}
	targetFD, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, fmt.Errorf("open bootstrap target %q: %w", item.prepared.rel, err)
	}
	stageFileFD, err := unix.Openat(stageFD, item.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		unix.Close(targetFD)
		return false, err
	}
	equal, compareErr := equalOpenFiles(os.NewFile(uintptr(targetFD), base), os.NewFile(uintptr(stageFileFD), item.name))
	if compareErr != nil {
		return false, compareErr
	}
	if !equal {
		return false, fmt.Errorf("bootstrap target %q conflicts with existing file", item.prepared.rel)
	}
	return true, nil
}

func equalOpenFiles(first, second *os.File) (bool, error) {
	defer first.Close()
	defer second.Close()
	firstInfo, err := first.Stat()
	if err != nil {
		return false, err
	}
	secondInfo, err := second.Stat()
	if err != nil {
		return false, err
	}
	if firstInfo.Size() != secondInfo.Size() || firstInfo.Size() > maxBootstrapFileBytes {
		return false, nil
	}
	firstHash, err := hashOpenFile(first)
	if err != nil {
		return false, err
	}
	secondHash, err := hashOpenFile(second)
	if err != nil {
		return false, err
	}
	return firstHash == secondHash, nil
}

func hashOpenFile(file *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxBootstrapFileBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func openBootstrapParentAt(rootFD int, rel string, create bool, createdDirs *[]createdBootstrapEntry) (int, string, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	base := parts[len(parts)-1]
	current, err := unix.Dup(rootFD)
	if err != nil {
		return -1, "", err
	}
	currentRel := ""
	for _, component := range parts[:len(parts)-1] {
		if currentRel == "" {
			currentRel = component
		} else {
			currentRel += "/" + component
		}
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			mkdirErr := unix.Mkdirat(current, component, 0o755)
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(current)
				return -1, "", mkdirErr
			}
			if createdDirs != nil && mkdirErr == nil {
				rollbackFD, dupErr := bootstrapRollbackDup(current)
				if dupErr != nil {
					cleanupErr := unix.Unlinkat(current, component, unix.AT_REMOVEDIR)
					unix.Close(current)
					if cleanupErr != nil {
						return -1, "", fmt.Errorf("track created bootstrap parent %q: %w", currentRel, errors.Join(dupErr, fmt.Errorf("remove untracked directory: %w", cleanupErr)))
					}
					return -1, "", fmt.Errorf("track created bootstrap parent %q: %w", currentRel, dupErr)
				}
				*createdDirs = append(*createdDirs, createdBootstrapEntry{parentFD: rollbackFD, name: component})
			}
			if mkdirErr == nil {
				if syncErr := unix.Fsync(current); syncErr != nil {
					unix.Close(current)
					return -1, "", syncErr
				}
			}
			next, openErr = unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		unix.Close(current)
		if errors.Is(openErr, unix.ENOENT) {
			return -1, "", errBootstrapParentMissing
		}
		if openErr != nil {
			return -1, "", fmt.Errorf("open bootstrap parent %q: %w", currentRel, openErr)
		}
		current = next
	}
	return current, base, nil
}

func rollbackBootstrapAt(_ int, files, dirs []createdBootstrapEntry) {
	for i := len(files) - 1; i >= 0; i-- {
		_ = unix.Unlinkat(files[i].parentFD, files[i].name, 0)
		_ = unix.Fsync(files[i].parentFD)
		_ = unix.Close(files[i].parentFD)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = unix.Unlinkat(dirs[i].parentFD, dirs[i].name, unix.AT_REMOVEDIR)
		_ = unix.Fsync(dirs[i].parentFD)
		_ = unix.Close(dirs[i].parentFD)
	}
}

func closeCreatedBootstrapEntries(groups ...[]createdBootstrapEntry) {
	for _, entries := range groups {
		for _, entry := range entries {
			_ = unix.Close(entry.parentFD)
		}
	}
}
