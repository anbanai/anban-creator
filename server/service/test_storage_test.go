package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/storage"
)

type fakeTaskStorage struct {
	name              string
	files             map[string][]byte
	uploadContentType string
	downloadURL       string
	signedKey         string
	signedKeys        []string
	readKey           string
	ownedPrefix       string
}

func (f *fakeTaskStorage) Name() string {
	if f.name != "" {
		return f.name
	}
	return "fake"
}

func (f *fakeTaskStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[key] = append([]byte(nil), data...)
	return &storage.UploadResult{URL: f.GetURL(key), Key: key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (f *fakeTaskStorage) UploadFile(ctx context.Context, key, filePath, contentType string) (*storage.UploadResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return f.Upload(ctx, key, file, contentType)
}

func (f *fakeTaskStorage) UploadURL(_ context.Context, key, contentType string, _ int) (string, error) {
	f.uploadContentType = contentType
	return "https://upload.example.com/" + key, nil
}
func (f *fakeTaskStorage) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (f *fakeTaskStorage) Read(_ context.Context, key string) ([]byte, error) {
	f.readKey = key
	data, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), data...), nil
}
func (f *fakeTaskStorage) Delete(context.Context, string) error { return nil }
func (f *fakeTaskStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	f.signedKey = key
	f.signedKeys = append(f.signedKeys, key)
	if f.downloadURL != "" {
		return f.downloadURL, nil
	}
	return "https://download.example.com/" + key, nil
}
func (f *fakeTaskStorage) HasCustomDomain() bool { return true }
func (f *fakeTaskStorage) IsOwnedURL(rawURL string) bool {
	return f.ownedPrefix != "" && strings.HasPrefix(rawURL, f.ownedPrefix)
}

func writeWorkspaceFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
