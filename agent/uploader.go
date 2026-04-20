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

const (
	uploadHTTPTimeout = 5 * time.Minute
	uploadMaxRetries  = 3
)

type Uploader struct {
	cfg    *Config
	client *http.Client
}

func NewUploader(cfg *Config) *Uploader {
	return &Uploader{
		cfg: cfg,
		client: &http.Client{
			Timeout: uploadHTTPTimeout,
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

		if err := u.uploadFileWithRetry(ctx, path, relPath); err != nil && firstErr == nil {
			firstErr = err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return firstErr
}

// uploadFileWithRetry retries the upload up to uploadMaxRetries times with
// exponential backoff. Only network/timeout errors are retried; 4xx client
// errors are not retried.
func (u *Uploader) uploadFileWithRetry(ctx context.Context, filePath, relPath string) error {
	var lastErr error
	for attempt := range uploadMaxRetries {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		lastErr = u.uploadFile(ctx, filePath, relPath)
		if lastErr == nil {
			return nil
		}
		// Don't retry client errors (4xx).
		if isClientError(lastErr) {
			return lastErr
		}
	}
	return fmt.Errorf("after %d attempts: %w", uploadMaxRetries, lastErr)
}

// isClientError checks if the error is an HTTP 4xx response.
func isClientError(err error) bool {
	type httpStatus interface{ StatusCode() int }
	if he, ok := err.(httpStatus); ok {
		return he.StatusCode() >= 400 && he.StatusCode() < 500
	}
	return false
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

	uploadCtx, cancel := context.WithTimeout(ctx, u.client.Timeout)
	defer cancel()

	bodyReader, bodyWriter := multipartPipe(uploadCtx)
	writeErrCh := make(chan error, 1)

	go func() {
		defer close(writeErrCh)
		defer bodyWriter.Close()

		part, err := bodyWriter.CreateFormFile("file", filepath.Base(cleanRelPath))
		if err != nil {
			writeErrCh <- err
			return
		}
		if _, err := io.Copy(part, f); err != nil {
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

	req, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, u.cfg.ServerURL+"/api/v1/agent/upload", bodyReader)
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
		return &httpUploadError{path: cleanRelPath, statusCode: resp.StatusCode}
	}
	return nil
}

// httpUploadError wraps an HTTP error response with StatusCode() for retry logic.
type httpUploadError struct {
	path        string
	statusCode  int
}

func (e *httpUploadError) Error() string {
	return fmt.Sprintf("upload file %s failed: HTTP %d", e.path, e.statusCode)
}

func (e *httpUploadError) StatusCode() int {
	return e.statusCode
}

func multipartPipe(ctx context.Context) (*io.PipeReader, *multipart.Writer) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			_ = pw.CloseWithError(ctx.Err())
		}()
	}
	return pr, writer
}
