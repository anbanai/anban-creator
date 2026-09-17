package agent

import (
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

	appconfig "github.com/anbanai/anban-creator/server/app/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
)

// fakeStore is a minimal storage.Provider for runtime materialization tests.
// It records the keys it was asked to Read and can be configured to return
// a specific byte slice, error, or ownedURL predicate.
type fakeStore struct {
	readKeys  []string
	readData  map[string][]byte
	readErr   error
	ownedPred func(string) bool
}

type boundedOnlyStore struct {
	*fakeStore
	actualSize   int64
	boundedCalls []int64
	unbounded    bool
}

func (f *boundedOnlyStore) Read(context.Context, string) ([]byte, error) {
	f.unbounded = true
	return nil, errors.New("unbounded storage read invoked")
}

func (f *boundedOnlyStore) ReadObject(_ context.Context, _ string, maxBytes int64) ([]byte, error) {
	f.boundedCalls = append(f.boundedCalls, maxBytes)
	if f.actualSize > maxBytes {
		return nil, fmt.Errorf("object too large: size=%d max=%d", f.actualSize, maxBytes)
	}
	return []byte("bounded"), nil
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
func (f *fakeStore) ReadObject(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	data, err := f.Read(ctx, key)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: size=%d max=%d", storage.ErrObjectExceedsMaxSize, len(data), maxBytes)
	}
	return data, nil
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

func TestMaterializeReferenceAssetUsesBoundedRepositoryKeyRead(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readData: map[string][]byte{key: []byte("image")}}
	workDir := t.TempDir()
	asset := &model.Asset{StorageKey: key, Size: 5}
	dir := filepath.Join(workDir, appconfig.ConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskReferenceImageFileName), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MaterializeReferenceAsset(context.Background(), store, workDir, asset); err != nil {
		t.Fatalf("MaterializeReferenceAsset: %v", err)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != key {
		t.Fatalf("read keys = %#v, want [%s]", store.readKeys, key)
	}
	got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, taskReferenceImageFileName))
	if err != nil || string(got) != "image" {
		t.Fatalf("materialized bytes = %q, err=%v", got, err)
	}
	info, err := os.Stat(filepath.Join(workDir, appconfig.ConfigDir, taskReferenceImageFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("reference mode = %v, want 0600", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("reference directory mode = %v, want 0700", dirInfo.Mode().Perm())
	}
}

func TestMaterializeReferenceAssetFailsClosed(t *testing.T) {
	asset := &model.Asset{StorageKey: "assets/users/user-1/asset-1/ref.png", Size: 5}
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		store storage.Provider
	}{
		{name: "nil store", ctx: context.Background()},
		{name: "missing", ctx: context.Background(), store: &fakeStore{readData: map[string][]byte{}}},
		{name: "oversize", ctx: context.Background(), store: &boundedOnlyStore{fakeStore: &fakeStore{}, actualSize: maxReferenceImageBytes + 1}},
		{name: "cancelled", ctx: cancelledContext(), store: &fakeStore{readData: map[string][]byte{asset.StorageKey: []byte("image")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := MaterializeReferenceAsset(tc.ctx, tc.store, t.TempDir(), asset); err == nil {
				t.Fatal("expected materialization error")
			}
		})
	}
}

func TestMaterializeReferenceAssetReplacesSymlinkWithoutWritingTarget(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readData: map[string][]byte{key: []byte("image")}}
	workDir := t.TempDir()
	dir := filepath.Join(workDir, appconfig.ConfigDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, taskReferenceImageFileName)
	if err := os.Symlink(target, dest); err != nil {
		t.Fatal(err)
	}

	if err := MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: 5}); err != nil {
		t.Fatal(err)
	}
	outside, err := os.ReadFile(target)
	if err != nil || string(outside) != "outside" {
		t.Fatalf("symlink target = %q, err=%v, want unchanged", outside, err)
	}
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("destination remains a symlink: mode=%v", info.Mode())
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "image" {
		t.Fatalf("destination = %q, err=%v", got, err)
	}
}

func TestMaterializeReferenceAssetRenameFailureCleansTempAndPreservesDestination(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readData: map[string][]byte{key: []byte("image")}}
	workDir := t.TempDir()
	dir := filepath.Join(workDir, appconfig.ConfigDir)
	dest := filepath.Join(dir, taskReferenceImageFileName)
	if err := os.MkdirAll(filepath.Join(dest, "marker"), 0o700); err != nil {
		t.Fatal(err)
	}

	err := MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: 5})
	if err == nil {
		t.Fatal("expected rename failure for directory destination")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "marker")); statErr != nil {
		t.Fatalf("existing destination changed: %v", statErr)
	}
	temps, globErr := filepath.Glob(filepath.Join(dir, ".reference-*.tmp"))
	if globErr != nil || len(temps) != 0 {
		t.Fatalf("temporary files after failure = %#v, err=%v", temps, globErr)
	}
}

func TestMaterializeReferenceAssetReadFailurePreservesExistingDestination(t *testing.T) {
	key := "assets/users/user-1/asset-1/ref.png"
	store := &fakeStore{readErr: errors.New("storage unavailable")}
	workDir := t.TempDir()
	dir := filepath.Join(workDir, appconfig.ConfigDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, taskReferenceImageFileName)
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: 5}); err == nil {
		t.Fatal("expected storage read failure")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "old" {
		t.Fatalf("destination = %q, err=%v, want old", got, err)
	}
}

func TestMaterializeReferenceAssetRejectsObjectSizeMismatchAndPreservesDestination(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		size int64
	}{
		{name: "short object", data: []byte("four"), size: 5},
		{name: "long object", data: []byte("sixsix"), size: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := "assets/users/user-1/asset-1/ref.png"
			store := &fakeStore{readData: map[string][]byte{key: tc.data}}
			workDir := t.TempDir()
			dir := filepath.Join(workDir, appconfig.ConfigDir)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(dir, taskReferenceImageFileName)
			if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := MaterializeReferenceAsset(t.Context(), store, workDir, &model.Asset{StorageKey: key, Size: tc.size}); err == nil {
				t.Fatal("expected object size mismatch")
			}
			got, err := os.ReadFile(dest)
			if err != nil || string(got) != "old" {
				t.Fatalf("destination = %q, err=%v, want old", got, err)
			}
		})
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestDownloadKeyFirstSourcesReadStorageForSharedExecutors(t *testing.T) {
	t.Run("input attachment", func(t *testing.T) {
		workDir := t.TempDir()
		key := "uploads/finalized/user-1/attachment/brief.pdf"
		store := &fakeStore{readData: map[string][]byte{key: []byte("attachment-bytes")}}
		attachments := []model.EntryAttachment{{
			Type: "document", UploadID: "attachment", Key: key,
			FileName: "brief.pdf", ContentType: "application/pdf", Size: 16,
		}}

		if count := DownloadInputAttachments(context.Background(), store, noopLogger(), workDir, "user-1", attachments); count != 1 {
			t.Fatalf("DownloadInputAttachments count = %d, want 1", count)
		}
		got, err := os.ReadFile(filepath.Join(workDir, appconfig.ConfigDir, "input-attachments", "attachment_01_brief.pdf"))
		if err != nil || string(got) != "attachment-bytes" {
			t.Fatalf("attachment = %q, %v", got, err)
		}
	})
}

func TestRuntimeMaterializersRejectPendingDirectUploadKeys(t *testing.T) {
	t.Run("input attachment", func(t *testing.T) {
		key := "uploads/pending/user-1/attachment/brief.pdf"
		store := &fakeStore{readData: map[string][]byte{key: []byte("mutable")}}
		attachments := []model.EntryAttachment{{Type: "document", UploadID: "attachment", Key: key, FileName: "brief.pdf", ContentType: "application/pdf"}}
		if count := DownloadInputAttachments(context.Background(), store, noopLogger(), t.TempDir(), "user-1", attachments); count != 0 {
			t.Fatalf("pending attachment materialized count = %d", count)
		}
		if len(store.readKeys) != 0 {
			t.Fatalf("pending attachment reached storage read: %#v", store.readKeys)
		}
	})

	t.Run("resume attachment", func(t *testing.T) {
		key := "uploads/pending/user-1/resume-upload/feedback.pdf"
		store := &fakeStore{readData: map[string][]byte{key: []byte("mutable")}}
		attachments := []model.EntryAttachment{
			{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue", FileName: "latest.md"},
			{Role: model.EntryAttachmentRoleResumeFile, UploadID: "resume-upload", Key: key, FileName: "feedback.pdf"},
		}
		if _, err := MaterializeResumeInputs(context.Background(), store, noopLogger(), t.TempDir(), "user-1", attachments); err == nil {
			t.Fatal("pending resume attachment was materialized")
		}
		if len(store.readKeys) != 0 {
			t.Fatalf("pending resume attachment reached storage read: %#v", store.readKeys)
		}
	})
}

func TestRuntimeMaterializersRejectMismatchedFinalizedIdentity(t *testing.T) {
	t.Run("attachment upload", func(t *testing.T) {
		key := "uploads/finalized/user-1/other-upload/brief.pdf"
		store := &fakeStore{readData: map[string][]byte{key: []byte("other-upload")}}
		attachments := []model.EntryAttachment{{Type: "document", UploadID: "expected-upload", Key: key, FileName: "brief.pdf"}}
		if count := DownloadInputAttachments(context.Background(), store, noopLogger(), t.TempDir(), "user-1", attachments); count != 0 {
			t.Fatalf("mismatched upload materialized count = %d", count)
		}
		if len(store.readKeys) != 0 {
			t.Fatalf("mismatched attachment reached storage read: %#v", store.readKeys)
		}
	})
}

func TestKeyFirstMaterializationUsesBoundedStorageReads(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int64
		wantErr  bool
		run      func(context.Context, storage.Provider, string) error
	}{
		{
			name: "reference image", maxBytes: maxReferenceImageBytes, wantErr: true,
			run: func(ctx context.Context, store storage.Provider, workDir string) error {
				return MaterializeReferenceAsset(ctx, store, workDir, &model.Asset{StorageKey: "assets/users/user-1/reference/image.png", Size: maxReferenceImageBytes})
			},
		},
		{
			name: "input attachment", maxBytes: maxInputAttachmentBytes,
			run: func(ctx context.Context, store storage.Provider, workDir string) error {
				if got := DownloadInputAttachments(ctx, store, noopLogger(), workDir, "user-1", []model.EntryAttachment{{
					Type: "document", UploadID: "attachment", Key: "uploads/finalized/user-1/attachment/brief.pdf",
					FileName: "brief.pdf", ContentType: "application/pdf", Size: 1,
				}}); got != 0 {
					return fmt.Errorf("materialized %d oversized attachments", got)
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &boundedOnlyStore{fakeStore: &fakeStore{}, actualSize: tt.maxBytes + 1}
			err := tt.run(context.Background(), store, t.TempDir())
			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), "too large")) {
				t.Fatalf("error = %v, want oversized object rejection", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatal(err)
			}
			if store.unbounded || len(store.boundedCalls) != 1 || store.boundedCalls[0] != tt.maxBytes {
				t.Fatalf("read boundary: unbounded=%v bounded=%#v, want only max=%d", store.unbounded, store.boundedCalls, tt.maxBytes)
			}
		})
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

	if n := DownloadInputAttachments(context.Background(), nil, noopLogger(), workDir, "user-1", attachments); n != 2 {
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

	if n := DownloadInputAttachments(context.Background(), nil, noopLogger(), workDir, "user-1", attachments); n != 1 {
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

	n, err := MaterializeResumeInputs(context.Background(), store, noopLogger(), workDir, "user-1", attachments)
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

	if _, err := MaterializeResumeInputs(context.Background(), &fakeStore{}, noopLogger(), workDir, "user-1", attachments); err == nil {
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
		if _, err := MaterializeResumeInputs(context.Background(), nil, noopLogger(), workDir, "user-1", attachments); err == nil {
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
	if _, err := MaterializeResumeInputs(context.Background(), nil, noopLogger(), workDir, "user-1", attachments); err == nil {
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

func TestDownloadInputAttachmentsKeepsOriginalIndicesAndWritesErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/first.png", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	})
	mux.HandleFunc("/second.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("second-image"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	workDir := t.TempDir()
	attachments := []model.EntryAttachment{
		{
			Type:        "image",
			URL:         srv.URL + "/first.png",
			FileName:    "first.png",
			ContentType: "image/png",
			Instruction: "正面",
			UploadID:    "up-1",
		},
		{
			Type:        "image",
			URL:         srv.URL + "/second.png",
			FileName:    "second.png",
			ContentType: "image/png",
			Instruction: "侧面",
			UploadID:    "up-2",
		},
	}

	if count := DownloadInputAttachments(context.Background(), nil, nil, workDir, "user-1", attachments); count != 1 {
		t.Fatalf("DownloadInputAttachments count = %d, want 1", count)
	}
	base := filepath.Join(workDir, appconfig.ConfigDir, "input-attachments")
	indexRaw, err := os.ReadFile(filepath.Join(base, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var index []MaterializedInputAttachment
	if err := json.Unmarshal(indexRaw, &index); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if len(index) != 1 {
		t.Fatalf("index = %#v, want one entry", index)
	}
	if index[0].AttachmentIndex != 2 {
		t.Fatalf("attachment_index = %d, want 2", index[0].AttachmentIndex)
	}
	if got := filepath.Base(index[0].Path); got != "attachment_02_second.png" {
		t.Fatalf("materialized basename = %q, want attachment_02_second.png", got)
	}
	if index[0].Instruction != "侧面" {
		t.Fatalf("instruction = %q, want 侧面", index[0].Instruction)
	}
	if index[0].UploadID != "up-2" {
		t.Fatalf("upload_id = %q, want up-2", index[0].UploadID)
	}

	failureRaw, err := os.ReadFile(filepath.Join(base, "errors.json"))
	if err != nil {
		t.Fatalf("read errors.json: %v", err)
	}
	var failures []MaterializedInputAttachmentError
	if err := json.Unmarshal(failureRaw, &failures); err != nil {
		t.Fatalf("decode errors.json: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("failures = %#v, want one entry", failures)
	}
	if failures[0].AttachmentIndex != 1 {
		t.Fatalf("failure attachment_index = %d, want 1", failures[0].AttachmentIndex)
	}
	if !strings.Contains(failures[0].Error, "500") {
		t.Fatalf("failure error = %q, want HTTP 500 detail", failures[0].Error)
	}
	if failures[0].Instruction != "正面" {
		t.Fatalf("failure instruction = %q, want 正面", failures[0].Instruction)
	}
}

func TestDownloadInputAttachmentsSkipsResumeRolesWithoutRenumbering(t *testing.T) {
	workDir := t.TempDir()
	attachments := []model.EntryAttachment{
		{
			Type:     "document",
			Text:     "resume",
			FileName: "latest.md",
			Role:     model.EntryAttachmentRoleResumeLatest,
		},
		{
			Type:        "text",
			Text:        "product details",
			FileName:    "product.txt",
			ContentType: "text/plain",
		},
	}

	if count := DownloadInputAttachments(context.Background(), nil, nil, workDir, "user-1", attachments); count != 1 {
		t.Fatalf("DownloadInputAttachments count = %d, want 1", count)
	}
	base := filepath.Join(workDir, appconfig.ConfigDir, "input-attachments")
	if _, err := os.Stat(filepath.Join(base, "attachment_02_product.txt")); err != nil {
		t.Fatalf("expected original index filename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "attachment_01_product.txt")); !os.IsNotExist(err) {
		t.Fatalf("attachment should not be renumbered, statErr=%v", err)
	}
	indexRaw, err := os.ReadFile(filepath.Join(base, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var index []MaterializedInputAttachment
	if err := json.Unmarshal(indexRaw, &index); err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	if len(index) != 1 || index[0].AttachmentIndex != 2 {
		t.Fatalf("index = %#v, want attachment index 2", index)
	}
}

func TestDownloadInputAttachmentsRemovesStaleErrorsOnSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/fail.png", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusBadGateway)
	})
	mux.HandleFunc("/success.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("success"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	workDir := t.TempDir()
	failure := []model.EntryAttachment{{
		Type: "image", URL: srv.URL + "/fail.png", FileName: "fail.png", ContentType: "image/png",
	}}
	if count := DownloadInputAttachments(context.Background(), nil, nil, workDir, "user-1", failure); count != 0 {
		t.Fatalf("failure materialization count = %d, want 0", count)
	}
	base := filepath.Join(workDir, appconfig.ConfigDir, "input-attachments")
	if _, err := os.Stat(filepath.Join(base, "errors.json")); err != nil {
		t.Fatalf("expected errors.json after failed run: %v", err)
	}

	success := []model.EntryAttachment{{
		Type: "image", URL: srv.URL + "/success.png", FileName: "success.png", ContentType: "image/png",
	}}
	if count := DownloadInputAttachments(context.Background(), nil, nil, workDir, "user-1", success); count != 1 {
		t.Fatalf("success materialization count = %d, want 1", count)
	}
	if _, err := os.Stat(filepath.Join(base, "errors.json")); !os.IsNotExist(err) {
		t.Fatalf("stale errors.json should be removed, statErr=%v", err)
	}
	indexRaw, err := os.ReadFile(filepath.Join(base, "index.json"))
	if err != nil {
		t.Fatalf("read rewritten index.json: %v", err)
	}
	var index []MaterializedInputAttachment
	if err := json.Unmarshal(indexRaw, &index); err != nil {
		t.Fatalf("decode rewritten index.json: %v", err)
	}
	if len(index) != 1 || filepath.Base(index[0].Path) != "attachment_01_success.png" {
		t.Fatalf("rewritten index = %#v", index)
	}
}
