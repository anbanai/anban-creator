package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/storage"
)

// fakeStore is a minimal storage.Provider for DownloadReferenceImage tests.
// It records the keys it was asked to Read and can be configured to return
// a specific byte slice, error, or ownedURL predicate.
type fakeStore struct {
	readKeys  []string
	readData  map[string][]byte
	readErr   error
	ownedPred func(string) bool
}

var _ storage.Provider = (*fakeStore)(nil)

func (f *fakeStore) Name() string { return "fake" }
func (f *fakeStore) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetURL(string) string { return "" }
func (f *fakeStore) Read(_ context.Context, key string) ([]byte, error) {
	f.readKeys = append(f.readKeys, key)
	if f.readErr != nil {
		return nil, f.readErr
	}
	if data, ok := f.readData[key]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("not found: %s", key)
}
func (f *fakeStore) Delete(context.Context, string) error { return nil }
func (f *fakeStore) DownloadURL(context.Context, string, int) (string, error) {
	return "", errors.New("not implemented")
}
func (f *fakeStore) HasCustomDomain() bool { return false }
func (f *fakeStore) IsOwnedURL(rawURL string) bool {
	if f.ownedPred != nil {
		return f.ownedPred(rawURL)
	}
	return false
}

// noopLogger returns a zerolog logger that discards all output.
func noopLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// writeReference writes the test bytes to a temporary httptest server and
// returns its URL. The server is cleaned up via t.Cleanup.
func serveBytes(t *testing.T, status int, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDownloadReferenceImage_NilStoreExternalURL(t *testing.T) {
	body := []byte("external-image-bytes")
	url := serveBytes(t, http.StatusOK, body)

	workDir := t.TempDir()
	if err := DownloadReferenceImage(context.Background(), nil, nil, workDir, url); err != nil {
		t.Fatalf("DownloadReferenceImage: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "reference.png"))
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("dest = %q, want %q", got, body)
	}
}

func TestDownloadReferenceImage_OwnedStoreReadHitsDisk(t *testing.T) {
	// HTTP server that must NEVER be hit when store.Read succeeds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("HTTP fallback should not be reached when store.Read succeeds")
	}))
	t.Cleanup(srv.Close)

	body := []byte("oss-stored-image")
	store := &fakeStore{
		readData:  map[string][]byte{"uploads/references/u/pic.jpg": body},
		ownedPred: func(u string) bool { return strings.HasPrefix(u, "https://oss.example.com/") },
	}
	imageURL := "https://oss.example.com/uploads/references/u/pic.jpg"

	workDir := t.TempDir()
	if err := DownloadReferenceImage(context.Background(), store, noopLogger(), workDir, imageURL); err != nil {
		t.Fatalf("DownloadReferenceImage: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "reference.png"))
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("dest = %q, want %q", got, body)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != "uploads/references/u/pic.jpg" {
		t.Fatalf("readKeys = %#v, want [uploads/references/u/pic.jpg]", store.readKeys)
	}
}

func TestDownloadReferenceImage_LocalRelativePathReadsStore(t *testing.T) {
	// Regression test for C1: local storage returns relative URLs like
	// /api/v1/files/<key>. The HTTP path cannot fetch those (unsupported
	// protocol scheme ""); store.Read must be used instead.
	body := []byte("local-stored-image")
	store := &fakeStore{
		readData:  map[string][]byte{"uploads/references/u/pic.jpg": body},
		ownedPred: func(u string) bool { return strings.HasPrefix(u, "/api/v1/files/") },
	}
	imageURL := "/api/v1/files/uploads/references/u/pic.jpg"

	workDir := t.TempDir()
	if err := DownloadReferenceImage(context.Background(), store, noopLogger(), workDir, imageURL); err != nil {
		t.Fatalf("DownloadReferenceImage: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "reference.png"))
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("dest = %q, want %q", got, body)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != "uploads/references/u/pic.jpg" {
		t.Fatalf("readKeys = %#v, want [uploads/references/u/pic.jpg]", store.readKeys)
	}
}

func TestDownloadReferenceImage_OwnedStoreReadFailsFallsBackToHTTP(t *testing.T) {
	body := []byte("fallback-via-http")
	url := serveBytes(t, http.StatusOK, body)

	store := &fakeStore{
		readErr:   errors.New("oss internal error"),
		ownedPred: func(u string) bool { return strings.HasPrefix(u, url) },
	}
	imageURL := url + "/uploads/references/u/pic.jpg"

	workDir := t.TempDir()
	if err := DownloadReferenceImage(context.Background(), store, noopLogger(), workDir, imageURL); err != nil {
		t.Fatalf("DownloadReferenceImage: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "reference.png"))
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("dest = %q, want %q", got, body)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != "uploads/references/u/pic.jpg" {
		t.Fatalf("expected 1 read attempt for the key, got %#v", store.readKeys)
	}
}

func TestDownloadReferenceImage_NotOwnedTakesDirectHTTP(t *testing.T) {
	body := []byte("external-link")
	url := serveBytes(t, http.StatusOK, body)

	store := &fakeStore{ownedPred: func(string) bool { return false }}

	workDir := t.TempDir()
	if err := DownloadReferenceImage(context.Background(), store, noopLogger(), workDir, url); err != nil {
		t.Fatalf("DownloadReferenceImage: %v", err)
	}

	if len(store.readKeys) != 0 {
		t.Fatalf("store.Read should not be called for external URLs, got %#v", store.readKeys)
	}
	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "reference.png"))
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("dest = %q, want %q", got, body)
	}
}

func TestDownloadReferenceImage_HTTPNonOKReturnsError(t *testing.T) {
	url := serveBytes(t, http.StatusForbidden, []byte("forbidden"))

	workDir := t.TempDir()
	err := DownloadReferenceImage(context.Background(), nil, nil, workDir, url)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("err = %v, want HTTP 403", err)
	}
	if _, statErr := os.Stat(filepath.Join(workDir, appconfig.ConfigDir, "reference.png")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no dest file on failure, statErr=%v", statErr)
	}
}

func TestDownloadReferenceImage_OversizedStoreReadReturnsError(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), int(maxReferenceImageBytes)+1)
	store := &fakeStore{
		readData:  map[string][]byte{"big": oversized},
		ownedPred: func(string) bool { return true },
	}

	workDir := t.TempDir()
	err := DownloadReferenceImage(context.Background(), store, noopLogger(), workDir, "https://oss.example.com/big")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v, want 'too large'", err)
	}
}

func TestWriteProjectCLAUDEMD(t *testing.T) {
	t.Run("writes trimmed instructions", func(t *testing.T) {
		dir := t.TempDir()
		p := &model.Project{Instructions: "  \n# Rules\nAlways use 你好.\n  "}

		if err := writeProjectCLAUDEMD(dir, p); err != nil {
			t.Fatalf("writeProjectCLAUDEMD failed: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}
		want := "# Rules\nAlways use 你好."
		if string(got) != want {
			t.Fatalf("unexpected CLAUDE.md content: got %q, want %q", got, want)
		}
	})

	t.Run("no-op when project is nil", func(t *testing.T) {
		dir := t.TempDir()
		if err := writeProjectCLAUDEMD(dir, nil); err != nil {
			t.Fatalf("writeProjectCLAUDEMD(nil) failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
			t.Fatalf("expected no CLAUDE.md when project is nil")
		}
	})

	t.Run("no-op when instructions are blank", func(t *testing.T) {
		dir := t.TempDir()
		p := &model.Project{Instructions: "   \n  "}
		if err := writeProjectCLAUDEMD(dir, p); err != nil {
			t.Fatalf("writeProjectCLAUDEMD(blank) failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
			t.Fatalf("expected no CLAUDE.md when instructions are blank")
		}
	})
}
