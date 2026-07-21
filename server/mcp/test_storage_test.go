package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/anbanai/anban-creator/server/storage"
)

type fakeObjectStorage struct {
	name              string
	url               string
	ownedPrefix       string
	key               string
	files             map[string][]byte
	uploadContentType string
	downloadURL       string
	rejectUnbounded   bool
	boundedReadLimits []int64
	actualSize        int64
}

func (f *fakeObjectStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func (f *fakeObjectStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	f.key = key
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[key] = append([]byte(nil), data...)
	url := f.url
	if url == "" {
		url = f.GetURL(key)
	}
	return &storage.UploadResult{URL: url, Key: key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeObjectStorage) UploadFile(ctx context.Context, key, filePath, contentType string) (*storage.UploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return f.Upload(ctx, key, file, contentType)
}

func (f *fakeObjectStorage) GetURL(key string) string {
	if f.url != "" {
		return f.url
	}
	prefix := f.ownedPrefix
	if prefix == "" {
		prefix = "https://oss.example.com/"
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(key, "/")
}

func (f *fakeObjectStorage) Read(_ context.Context, key string) ([]byte, error) {
	if f.rejectUnbounded {
		return nil, errors.New("unbounded storage read invoked")
	}
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeObjectStorage) ReadObject(_ context.Context, key string, maxBytes int64) ([]byte, error) {
	f.boundedReadLimits = append(f.boundedReadLimits, maxBytes)
	if f.actualSize > maxBytes {
		return nil, fmt.Errorf("object too large: size=%d max=%d", f.actualSize, maxBytes)
	}
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeObjectStorage) Delete(context.Context, string) error { return nil }
func (f *fakeObjectStorage) UploadURL(_ context.Context, _ string, contentType string, _ int) (string, error) {
	f.uploadContentType = contentType
	return "https://upload.example.com/put?signature=1", nil
}
func (f *fakeObjectStorage) DownloadURL(context.Context, string, int) (string, error) {
	if f.downloadURL != "" {
		return f.downloadURL, nil
	}
	return "https://download.example.com/get?signature=1", nil
}
func (f *fakeObjectStorage) HasCustomDomain() bool {
	return f.ownedPrefix != "" || strings.HasPrefix(f.url, "https://")
}
func (f *fakeObjectStorage) IsOwnedURL(rawURL string) bool {
	return f.ownedPrefix != "" && strings.HasPrefix(rawURL, f.ownedPrefix)
}
