//go:build unix

package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openProjectFileNoSymlinks(projectDir, relativePath string) (*os.File, error) {
	return openProjectPathNoSymlinks(projectDir, relativePath, false)
}

func openProjectDirectoryNoSymlinks(projectDir, relativePath string) (*os.File, error) {
	return openProjectPathNoSymlinks(projectDir, relativePath, true)
}

func openProjectPathNoSymlinks(projectDir, relativePath string, directory bool) (*os.File, error) {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if (!directory && clean == ".") || !filepath.IsLocal(clean) {
		return nil, errUnsafePath
	}
	if clean == "." {
		clean = ""
	}
	parts := strings.Split(clean, string(filepath.Separator))
	rootFD, err := unix.Open(projectDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, classifyProjectOpenError(err)
	}
	if clean == "" {
		file := os.NewFile(uintptr(rootFD), projectDir)
		if file == nil {
			_ = unix.Close(rootFD)
			return nil, errors.New("open project memory directory")
		}
		return file, nil
	}
	currentFD := rootFD
	for _, part := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
		if openErr != nil {
			_ = unix.Close(rootFD)
			return nil, classifyProjectOpenError(openErr)
		}
		currentFD = nextFD
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, openErr := unix.Openat(currentFD, parts[len(parts)-1], flags, 0)
	if currentFD != rootFD {
		_ = unix.Close(currentFD)
	}
	_ = unix.Close(rootFD)
	if openErr != nil {
		return nil, classifyProjectOpenError(openErr)
	}
	file := os.NewFile(uintptr(fd), relativePath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open project memory file")
	}
	return file, nil
}

func classifyProjectOpenError(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return errUnsafePath
	}
	return err
}
