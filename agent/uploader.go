package main

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/royalrick/anbanwriter/server/service"
)

type Uploader struct {
	cfg    *Config
	client *http.Client
}

func NewUploader(cfg *Config) *Uploader {
	return &Uploader{
		cfg: cfg,
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (u *Uploader) UploadWorkspace(ctx context.Context) error {
	var firstErr error

	err := filepath.WalkDir(u.cfg.Workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}

		if d.IsDir() {
			if service.ShouldSkipTaskFileDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if service.ShouldSkipTaskFile(d.Name()) {
			return nil
		}

		relPath, err := filepath.Rel(u.cfg.Workspace, path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return nil
		}

		if err := u.uploadFile(ctx, path, relPath); err != nil && firstErr == nil {
			firstErr = err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
}

func (u *Uploader) uploadFile(ctx context.Context, filePath, relPath string) error {
	cleanRelPath, err := service.CleanTaskFileRelativePath(relPath)
	if err != nil {
		return err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open upload file %s: %w", filePath, err)
	}
	defer f.Close()

	bodyReader, bodyWriter := multipartPipe(ctx)
	writeErrCh := make(chan error, 1)

	go func() {
		defer close(writeErrCh)
		defer bodyWriter.Close()

		part, err := bodyWriter.CreateFormFile("file", filepath.Base(cleanRelPath))
		if err != nil {
			writeErrCh <- err
			return
		}
		if _, err := ioCopy(part, f); err != nil {
			writeErrCh <- err
			return
		}
		if err := bodyWriter.WriteField("task_id", u.cfg.TaskID); err != nil {
			writeErrCh <- err
			return
		}
		if err := bodyWriter.WriteField("relative_path", filepath.ToSlash(cleanRelPath)); err != nil {
			writeErrCh <- err
			return
		}
		writeErrCh <- nil
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.cfg.ServerURL+"/api/v1/agent/upload", bodyReader)
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+u.cfg.APIKey)
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())

	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("upload request for %s: %w", cleanRelPath, err)
	}
	defer resp.Body.Close()

	if writeErr := <-writeErrCh; writeErr != nil {
		return fmt.Errorf("stream multipart upload for %s: %w", cleanRelPath, writeErr)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload file %s failed: HTTP %d", cleanRelPath, resp.StatusCode)
	}
	return nil
}

func multipartPipe(ctx context.Context) (*io.PipeReader, *multipart.Writer) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		<-ctx.Done()
		_ = pw.CloseWithError(ctx.Err())
	}()
	return pr, writer
}

func ioCopy(dst io.Writer, src *os.File) (int64, error) {
	return io.Copy(dst, src)
}
