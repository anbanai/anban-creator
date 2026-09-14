//go:build !unix

package memory

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	root, err := os.OpenRoot(projectDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	parts := strings.Split(clean, string(filepath.Separator))
	for index := range parts {
		info, err := root.Lstat(filepath.Join(parts[:index+1]...))
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, errUnsafePath
		}
	}
	file, err := root.Open(clean)
	if err != nil {
		return nil, err
	}
	if directory {
		info, statErr := file.Stat()
		if statErr != nil || !info.IsDir() {
			_ = file.Close()
			if statErr != nil {
				return nil, statErr
			}
			return nil, errUnsafePath
		}
	}
	return file, nil
}
