package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/anbanai/anban-creator/server/service"
)

const maxArtifactSnapshotAttempts = 3

var errArtifactSnapshotChanged = errors.New("artifact changed while opening")

type artifactFile interface {
	io.ReadSeeker
	io.Closer
	Stat() (fs.FileInfo, error)
}

type artifactSnapshot struct {
	file        artifactFile
	path        string
	before      fs.FileInfo
	size        int64
	hash        string
	contentType string
}

type artifactHeaderCapture struct {
	bytes []byte
}

func (c *artifactHeaderCapture) Write(p []byte) (int, error) {
	written := len(p)
	if remaining := 512 - len(c.bytes); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		c.bytes = append(c.bytes, p...)
	}
	return written, nil
}

type artifactUploadSource struct {
	io.ReadSeeker
}

type artifactCloseError struct {
	path string
	err  error
}

func (e *artifactCloseError) Error() string {
	return fmt.Sprintf("close artifact %s: %v", e.path, e.err)
}

func (e *artifactCloseError) Unwrap() error {
	return e.err
}

var openArtifactFile = func(path string) (artifactFile, error) {
	return os.Open(path)
}

func openArtifactSnapshot(ctx context.Context, path string) (*artifactSnapshot, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact %s: %w", path, err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file: %s", path)
	}

	file, err := openArtifactFile(path)
	if err != nil {
		primary := fmt.Errorf("open artifact %s: %w", path, err)
		if file != nil {
			return nil, closeArtifactFile(path, file, primary)
		}
		return nil, primary
	}
	info, err := file.Stat()
	if err != nil {
		return nil, closeArtifactFile(path, file, fmt.Errorf("stat artifact %s: %w", path, err))
	}
	if !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) {
		mutationErr := fmt.Errorf("%w: %s", errArtifactSnapshotChanged, path)
		return nil, closeArtifactFile(path, file, mutationErr)
	}

	hash := sha256.New()
	header := &artifactHeaderCapture{}
	if _, err := copyArtifactWithContext(ctx, io.MultiWriter(hash, header), file); err != nil {
		return nil, closeArtifactFile(path, file, fmt.Errorf("hash artifact %s: %w", path, err))
	}
	return &artifactSnapshot{
		file:        file,
		path:        path,
		before:      info,
		size:        info.Size(),
		hash:        hex.EncodeToString(hash.Sum(nil)),
		contentType: service.DetectTaskFileMIMEFromContent(path, header.bytes),
	}, nil
}

func (s *artifactSnapshot) rewind() error {
	_, err := s.file.Seek(0, io.SeekStart)
	return err
}

func (s *artifactSnapshot) unchanged() (bool, error) {
	after, err := s.file.Stat()
	if err != nil {
		return false, fmt.Errorf("stat opened artifact %s: %w", s.path, err)
	}
	if after.Size() != s.before.Size() || !after.ModTime().Equal(s.before.ModTime()) {
		return false, nil
	}
	pathInfo, err := os.Lstat(s.path)
	if err != nil {
		return false, fmt.Errorf("inspect artifact path %s: %w", s.path, err)
	}
	if !pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, after) {
		return false, nil
	}
	return true, nil
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
		if err := artifactContextCause(ctx); err != nil {
			return total, err
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func closeArtifactFile(path string, file artifactFile, primary error) error {
	closeErr := file.Close()
	if closeErr == nil {
		return primary
	}
	contextualCloseErr := &artifactCloseError{path: path, err: closeErr}
	if primary == nil {
		return contextualCloseErr
	}
	return errors.Join(primary, contextualCloseErr)
}

func hasArtifactCloseError(err error) bool {
	var closeErr *artifactCloseError
	return errors.As(err, &closeErr)
}
