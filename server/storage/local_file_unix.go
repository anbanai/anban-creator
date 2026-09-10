//go:build !windows

package storage

import "os"

func openLocalObjectFile(path string) (*os.File, error) {
	return os.Open(path)
}

func replaceLocalObjectFile(source, destination string) error {
	return os.Rename(source, destination)
}
