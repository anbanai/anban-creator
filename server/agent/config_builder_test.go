package agent

import (
	"bytes"
	"context"
	"encoding/json"
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

	appconfig "github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
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
func (f *fakeStore) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}
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

func TestDownloadInputAttachmentsMaterializesFilesAndIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ref.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-bytes"))
	})
	mux.HandleFunc("/brief.pdf", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pdf-bytes"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	workDir := t.TempDir()
	attachments := []model.EntryAttachment{
		{
			Type:        "image",
			URL:         srv.URL + "/ref.png",
			FileName:    "ref.png",
			ContentType: "image/png",
			Size:        11,
		},
		{
			Type:        "document",
			URL:         srv.URL + "/brief.pdf",
			FileName:    "brief.pdf",
			ContentType: "application/pdf",
			Size:        9,
		},
	}

	if n := DownloadInputAttachments(context.Background(), nil, noopLogger(), workDir, attachments); n != 2 {
		t.Fatalf("DownloadInputAttachments count = %d, want 2", n)
	}
	base := filepath.Join(workDir, appconfig.ConfigDir, "input-attachments")
	for name, want := range map[string]string{
		"attachment_01_ref.png":   "image-bytes",
		"attachment_02_brief.pdf": "pdf-bytes",
	} {
		got, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	raw, err := os.ReadFile(filepath.Join(base, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var index []MaterializedInputAttachment
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if len(index) != 2 || index[0].Path != ".anban-creator/input-attachments/attachment_01_ref.png" || index[1].ContentType != "application/pdf" {
		t.Fatalf("index = %#v", index)
	}
}

func TestDownloadInputAttachmentsSkipsResumeRoles(t *testing.T) {
	workDir := t.TempDir()
	attachments := []model.EntryAttachment{
		{Type: "document", Text: "brief", FileName: "brief.txt"},
		{Type: "document", Text: "resume", FileName: "latest.md", Role: model.EntryAttachmentRoleResumeLatest},
		{Type: "document", Text: "resume file", FileName: "resume.txt", Role: model.EntryAttachmentRoleResumeFile},
	}

	if n := DownloadInputAttachments(context.Background(), nil, noopLogger(), workDir, attachments); n != 1 {
		t.Fatalf("DownloadInputAttachments count = %d, want 1", n)
	}
	base := filepath.Join(workDir, appconfig.ConfigDir, "input-attachments")
	if _, err := os.Stat(filepath.Join(base, "attachment_01_brief.txt")); err != nil {
		t.Fatalf("expected regular attachment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "attachment_02_latest.md")); !os.IsNotExist(err) {
		t.Fatalf("resume latest should not be materialized as regular input attachment, statErr=%v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "attachment_03_resume.txt")); !os.IsNotExist(err) {
		t.Fatalf("resume file should not be materialized as regular input attachment, statErr=%v", err)
	}
}

func TestMaterializeResumeInputsWritesLatestAndAttachments(t *testing.T) {
	workDir := t.TempDir()
	staleDir := filepath.Join(workDir, appconfig.ConfigDir, "resume", "attachments")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		readData: map[string][]byte{
			"resume/task-1/attachments/feedback.txt": []byte("feedback bytes"),
		},
	}
	body := "# 继续执行补充\n\n## 补充文件\n\n- 相对路径：attachments/feedback.txt\n"
	attachments := []model.EntryAttachment{
		{Type: "document", Text: body, FileName: "latest.md", Role: model.EntryAttachmentRoleResumeLatest},
		{Type: "document", Key: "resume/task-1/attachments/feedback.txt", FileName: "feedback.txt", Role: model.EntryAttachmentRoleResumeFile},
	}

	n, err := MaterializeResumeInputs(context.Background(), store, noopLogger(), workDir, attachments)
	if err != nil {
		t.Fatalf("MaterializeResumeInputs error = %v", err)
	}
	if n != 2 {
		t.Fatalf("MaterializeResumeInputs count = %d, want 2", n)
	}
	latest, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "resume", "latest.md"))
	if err != nil {
		t.Fatalf("read latest.md: %v", err)
	}
	if string(latest) != body {
		t.Fatalf("latest.md = %q, want %q", latest, body)
	}
	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "resume", "attachments", "feedback.txt"))
	if err != nil {
		t.Fatalf("read resume attachment: %v", err)
	}
	if string(got) != "feedback bytes" {
		t.Fatalf("resume attachment = %q, want feedback bytes", got)
	}
	if _, err := os.Stat(filepath.Join(staleDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale resume attachment survived materialization: %v", err)
	}
}

func TestMaterializeResumeInputsFailsWhenResumeAttachmentMissing(t *testing.T) {
	workDir := t.TempDir()
	body := "# 继续执行补充\n\n## 补充文件\n\n- 相对路径：attachments/missing.txt\n"
	attachments := []model.EntryAttachment{
		{Type: "document", Text: body, FileName: "latest.md", Role: model.EntryAttachmentRoleResumeLatest},
		{Type: "document", Key: "resume/task-1/attachments/missing.txt", FileName: "missing.txt", Role: model.EntryAttachmentRoleResumeFile},
	}

	if _, err := MaterializeResumeInputs(context.Background(), &fakeStore{}, noopLogger(), workDir, attachments); err == nil {
		t.Fatal("MaterializeResumeInputs succeeded with missing resume attachment")
	}
	if _, err := os.Stat(filepath.Join(workDir, appconfig.ConfigDir, "resume", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("latest.md should not be published when resume attachment is missing, statErr=%v", err)
	}
}

func TestMaterializeResumeInputsRejectsPortableFilenameCollision(t *testing.T) {
	for _, names := range [][2]string{
		{"Foo.txt", "foo.txt"},
		{"Straße.txt", "STRASSE.txt"},
		{"Résumé.txt", "Re\u0301sume\u0301.txt"},
	} {
		workDir := t.TempDir()
		attachments := []model.EntryAttachment{
			{Text: "read attachments/" + names[0], Role: model.EntryAttachmentRoleResumeLatest},
			{Text: "one", FileName: names[0], Role: model.EntryAttachmentRoleResumeFile},
			{Text: "two", FileName: names[1], Role: model.EntryAttachmentRoleResumeFile},
		}
		if _, err := MaterializeResumeInputs(context.Background(), nil, noopLogger(), workDir, attachments); err == nil {
			t.Fatalf("case-folded duplicate resume filenames %q accepted", names)
		}
		if _, err := os.Stat(filepath.Join(workDir, appconfig.ConfigDir, "resume", "latest.md")); !os.IsNotExist(err) {
			t.Fatalf("latest.md published for invalid resume set: %v", err)
		}
	}
}

func TestMaterializeResumeInputsRejectsOverlongFilenameBeforeFilesystemWrites(t *testing.T) {
	workDir := t.TempDir()
	attachments := []model.EntryAttachment{
		{Text: "read attachment", Role: model.EntryAttachmentRoleResumeLatest},
		{Text: "content", FileName: strings.Repeat("a", 256), Role: model.EntryAttachmentRoleResumeFile},
	}
	if _, err := MaterializeResumeInputs(context.Background(), nil, noopLogger(), workDir, attachments); err == nil {
		t.Fatal("overlong resume filename accepted")
	}
	if _, err := os.Stat(filepath.Join(workDir, appconfig.ConfigDir, "resume")); !os.IsNotExist(err) {
		t.Fatalf("resume directory created before portable validation: %v", err)
	}
}

func TestWriteProjectCLAUDEMD(t *testing.T) {
	t.Run("writes fixed positioning template", func(t *testing.T) {
		dir := t.TempDir()
		p := &model.Project{Instructions: "  \n# Rules\nAlways use 你好.\n  "}

		if err := writeProjectCLAUDEMD(dir, p); err != nil {
			t.Fatalf("writeProjectCLAUDEMD failed: %v", err)
		}

		got, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}
		want := "# CLAUDE.md\n\n## 项目定位\n\n# Rules\nAlways use 你好."
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
