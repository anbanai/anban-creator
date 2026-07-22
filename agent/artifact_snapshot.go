package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
)

const maxArtifactSnapshotAttempts = 3

type artifactFile interface {
	io.ReadSeeker
	io.Closer
	Stat() (fs.FileInfo, error)
}

type artifactSnapshot struct {
	file   artifactFile
	path   string
	before fs.FileInfo
	size   int64
	hash   string
}

var openArtifactFile = func(path string) (artifactFile, error) {
	return os.Open(path)
}

func openArtifactSnapshot(ctx context.Context, path string) (*artifactSnapshot, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file: %s", path)
	}

	file, err := openArtifactFile(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		_ = file.Close()
		return nil, fmt.Errorf("artifact changed while opening: %s", path)
	}

	hash := sha256.New()
	if _, err := copyArtifactWithContext(ctx, hash, file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &artifactSnapshot{
		file:   file,
		path:   path,
		before: info,
		size:   info.Size(),
		hash:   hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func (s *artifactSnapshot) rewind() error {
	_, err := s.file.Seek(0, io.SeekStart)
	return err
}

func (s *artifactSnapshot) unchanged() bool {
	after, err := s.file.Stat()
	if err != nil || after.Size() != s.before.Size() || !after.ModTime().Equal(s.before.ModTime()) {
		return false
	}
	pathInfo, err := os.Lstat(s.path)
	return err == nil && pathInfo.Mode().IsRegular() && os.SameFile(pathInfo, after)
}

func copyArtifactWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		if err := artifactContextCause(ctx); err != nil {
			return total, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	snapshot, err := openArtifactSnapshot(ctx, path)
	if err != nil {
		return "", err
	}
	defer snapshot.file.Close()
	return snapshot.hash, nil
}
