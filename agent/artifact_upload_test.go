package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

type fakeArtifactReporter struct {
	prepared []ArtifactPrepareRequest
	streamed []ArtifactStreamRequest
	bodies   []string
	manifest ArtifactManifestRequest
	progress []string
}

func (f *fakeArtifactReporter) PrepareArtifactUpload(_ context.Context, req ArtifactPrepareRequest) (*ArtifactPrepareResponse, error) {
	f.prepared = append(f.prepared, req)
	return &ArtifactPrepareResponse{
		Key:       "uploads/users/u/projects/p/tasks/" + req.TaskID + "/artifacts/" + req.RelativePath,
		Bucket:    "bucket",
		Endpoint:  "oss-cn-hangzhou.aliyuncs.com",
		Headers:   map[string]string{"Content-Type": req.ContentType},
		MaxSize:   512 * 1024 * 1024,
		ExpiresAt: "2026-07-09T10:15:00Z",
	}, nil
}

func (f *fakeArtifactReporter) ReportArtifactManifest(_ context.Context, req ArtifactManifestRequest) error {
	f.manifest = req
	return nil
}

func (f *fakeArtifactReporter) StreamArtifactContent(_ context.Context, req ArtifactStreamRequest, body io.Reader) (*ArtifactStreamResponse, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	f.streamed = append(f.streamed, req)
	f.bodies = append(f.bodies, string(data))
	return &ArtifactStreamResponse{
		ObjectKey:   "uploads/users/u/projects/p/tasks/" + req.TaskID + "/executions/" + req.ExecutionID + "/artifacts/" + req.RelativePath,
		ContentType: req.ContentType,
		Size:        req.Size,
		SHA256:      req.SHA256,
	}, nil
}

func (f *fakeArtifactReporter) ReportProgress(_ context.Context, message string) error {
	f.progress = append(f.progress, message)
	return nil
}

func TestParseConfigArtifactUploadModeDefaultsOffAndParsesDirect(t *testing.T) {
	err := newAgentCommand(nil, nil, func(_ context.Context, cfg *Config) error {
		if cfg.ArtifactUploadMode != ArtifactUploadOff {
			t.Fatalf("ArtifactUploadMode default = %q, want off", cfg.ArtifactUploadMode)
		}
		return nil
	}).Run(context.Background(), []string{
		"anban", "run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = newAgentCommand(nil, nil, func(_ context.Context, cfg *Config) error {
		if cfg.ArtifactUploadMode != ArtifactUploadDirect {
			t.Fatalf("ArtifactUploadMode = %q, want direct", cfg.ArtifactUploadMode)
		}
		return nil
	}).Run(context.Background(), []string{
		"anban", "run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
		"--artifact-upload-mode", "direct",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = newAgentCommand(nil, nil, func(_ context.Context, _ *Config) error {
		return nil
	}).Run(context.Background(), []string{
		"anban", "run",
		"--server-url", "http://localhost:18060",
		"--api-key", "key",
		"--task-id", "task-1",
		"--task-type", "article",
		"--artifact-upload-mode", "bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "artifact-upload-mode must be one of") {
		t.Fatalf("invalid artifact-upload-mode error = %v", err)
	}
}

func TestScanWorkspaceArtifactsPrefersOutputAndSkipsRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "notes.md", "root note")
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	writeAgentArtifactTestFile(t, root, "output/images/cover.png", "png")
	writeAgentArtifactTestFile(t, root, "output/final.mp4", "video")
	writeAgentArtifactTestFile(t, root, "output/.env", "secret")
	writeAgentArtifactTestFile(t, root, "output/.claude/session.json", "{}")
	writeAgentArtifactTestFile(t, root, "output/node_modules/pkg/index.js", "module.exports = {}")
	writeAgentArtifactTestFile(t, root, "output/package.json", "{}")
	writeAgentArtifactTestFile(t, root, "montage/projects/task-1/checkpoint_assets.json", "{}")
	writeAgentArtifactTestFile(t, root, ".anban-runtime-home/.claude/projects/session.jsonl", "{}")

	files, err := ScanWorkspaceArtifacts(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanWorkspaceArtifacts: %v", err)
	}
	var rels []string
	for _, file := range files {
		rels = append(rels, file.RelativePath)
	}
	sort.Strings(rels)
	want := []string{"output/article.md", "output/final.mp4", "output/images/cover.png"}
	if len(rels) != len(want) {
		t.Fatalf("rels = %#v, want %#v", rels, want)
	}
	for i := range want {
		if rels[i] != want[i] {
			t.Fatalf("rels = %#v, want %#v", rels, want)
		}
	}
}

func TestScanWorkspaceArtifactsDoesNotFallbackToWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "article.md", "# article")
	writeAgentArtifactTestFile(t, root, ".anban-runtime-home/secret.md", "runtime state")

	files, err := ScanWorkspaceArtifacts(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanWorkspaceArtifacts: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %#v, want no workspace-root artifacts", files)
	}
}

func TestJobArtifactUploaderRejectsSymlinkOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "output")); err != nil {
		t.Fatal(err)
	}
	_, err := scanWorkspaceArtifacts(context.Background(), root, "article")
	if err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("symlink output error = %v, want real-directory rejection", err)
	}
}

func TestJobArtifactUploaderUsesCanonicalWorkspaceOutput(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	runtimePath := filepath.Join(root, "openmontage")
	if err := os.Mkdir(runtimePath, 0o755); err != nil {
		t.Fatal(err)
	}
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{
		TaskID: "task-1", ExecutionID: "execution-1", TaskType: "montage", Workspace: root, ArtifactUploadMode: ArtifactUploadDirect,
	}, reporter)
	uploader.putObject = func(context.Context, *ArtifactPrepareResponse, string, string) (string, error) {
		return "etag", nil
	}
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{WorkDir: runtimePath}); err != nil {
		t.Fatal(err)
	}
	if len(reporter.prepared) != 1 || reporter.prepared[0].RelativePath != "output/article.md" {
		t.Fatalf("prepared = %#v, want canonical output/article.md", reporter.prepared)
	}
}

func TestJobArtifactHashCancellationPreservesCompletionReserve(t *testing.T) {
	t.Setenv(jobFinalizationTimeoutEnv, "120ms")
	previousReserve := jobCompletionReserve
	jobCompletionReserve = 40 * time.Millisecond
	t.Cleanup(func() { jobCompletionReserve = previousReserve })

	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", strings.Repeat("x", 256*1024))
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (io.ReadCloser, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &slowArtifactReader{ReadCloser: file, delay: 10 * time.Millisecond}, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	var prepared, manifested, completed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/agent/artifacts/prepare":
			prepared = true
		case "/api/v1/agent/artifacts/manifest":
			manifested = true
		case "/api/v1/agent/complete":
			completed = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &Config{ServerURL: server.URL, APIKey: "key", TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}
	reporter := NewReporter(cfg)
	window := newFinalizationWindow(cfg)
	workCtx, cancelWork := window.workContext()
	started := time.Now()
	err := NewArtifactUploader(cfg, reporter).UploadWorkspaceArtifacts(workCtx, &serveragent.ExecutionResult{Success: true, WorkDir: root})
	cancelWork()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("canceled artifact scan returned after %v", elapsed)
	}
	if prepared || manifested {
		t.Fatalf("canceled scan reached remote artifact calls: prepare=%v manifest=%v", prepared, manifested)
	}

	completionCtx, cancelCompletion := window.completionContext()
	defer cancelCompletion()
	if err := reporter.ReportComplete(completionCtx, &serveragent.ExecutionResult{Success: false}); err != nil {
		t.Fatalf("ReportComplete: %v", err)
	}
	if !completed {
		t.Fatal("completion reserve did not reach ReportComplete")
	}
}

type slowArtifactReader struct {
	io.ReadCloser
	delay time.Duration
}

func TestLocalArtifactFinalizationUsesFreshContextAfterRunCancellation(t *testing.T) {
	t.Setenv(homeTemplateEnv, "")
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")

	var prepared atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/artifacts/prepare" {
			prepared.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{}})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	cfg := &Config{
		ServerURL: server.URL, APIKey: "key", TaskID: "task-1", TaskType: "article",
		Topic: "write", Workspace: root, MaxTurns: 1, ArtifactUploadMode: ArtifactUploadDirect,
	}
	if err := runAgent(runCtx, cfg, io.Discard, io.Discard); err == nil {
		t.Fatal("canceled agent run unexpectedly succeeded")
	}
	if !prepared.Load() {
		t.Fatal("local artifact finalization inherited the canceled run context")
	}
}

func (r *slowArtifactReader) Read(p []byte) (int, error) {
	time.Sleep(r.delay)
	if len(p) > 1024 {
		p = p[:1024]
	}
	return r.ReadCloser.Read(p)
}

func TestArtifactUploaderUploadsAndReportsManifest(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	reporter := &fakeArtifactReporter{}
	var uploaded []string
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root, ArtifactUploadMode: ArtifactUploadDirect}, reporter)
	uploader.putObject = func(_ context.Context, prepared *ArtifactPrepareResponse, localPath string, contentType string) (string, error) {
		uploaded = append(uploaded, prepared.Key+"|"+filepath.Base(localPath)+"|"+contentType)
		return "etag-1", nil
	}

	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if len(reporter.prepared) != 1 || reporter.prepared[0].RelativePath != "output/article.md" {
		t.Fatalf("prepared = %#v, want output/article.md", reporter.prepared)
	}
	if len(uploaded) != 1 || uploaded[0] == "" {
		t.Fatalf("uploaded = %#v, want one upload", uploaded)
	}
	if reporter.manifest.TaskID != "task-1" || len(reporter.manifest.Files) != 1 {
		t.Fatalf("manifest = %#v, want one task file", reporter.manifest)
	}
	file := reporter.manifest.Files[0]
	if file.RelativePath != "output/article.md" || file.ObjectKey == "" || file.SHA256 == "" || file.ETag != "etag-1" {
		t.Fatalf("manifest file = %#v, want relative path, object key, sha, etag", file)
	}
}

func TestArtifactUploaderUsesProviderMode(t *testing.T) {
	for _, mode := range []string{ArtifactUploadDirect, ArtifactUploadStream} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeAgentArtifactTestFile(t, root, "output/article.md", "artifact-body")
			reporter := &fakeArtifactReporter{}
			uploader := NewArtifactUploader(&Config{
				TaskID: "task-1", ExecutionID: "execution-1", TaskType: "article",
				Workspace: root, ArtifactUploadMode: mode,
			}, reporter)
			directCalls := 0
			uploader.putObject = func(context.Context, *ArtifactPrepareResponse, string, string) (string, error) {
				directCalls++
				return "etag", nil
			}

			if err := uploader.UploadWorkspaceArtifacts(t.Context(), &serveragent.ExecutionResult{WorkDir: root}); err != nil {
				t.Fatal(err)
			}
			if mode == ArtifactUploadDirect {
				if directCalls != 1 || len(reporter.prepared) != 1 || len(reporter.streamed) != 0 {
					t.Fatalf("direct calls=%d prepared=%d streamed=%d", directCalls, len(reporter.prepared), len(reporter.streamed))
				}
			} else if directCalls != 0 || len(reporter.prepared) != 0 || len(reporter.streamed) != 1 || reporter.bodies[0] != "artifact-body" {
				t.Fatalf("stream calls=%d prepared=%d streamed=%d bodies=%#v", directCalls, len(reporter.prepared), len(reporter.streamed), reporter.bodies)
			}
			if len(reporter.manifest.Files) != 1 || reporter.manifest.Files[0].ObjectKey == "" {
				t.Fatalf("manifest = %#v", reporter.manifest)
			}
		})
	}
}

func TestArtifactUploaderUploadsFailureArtifactsForUnsuccessfulResult(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/failure-state.json", `{"stage":"quality-gate"}`)
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root, ArtifactUploadMode: ArtifactUploadDirect}, reporter)
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, _ string, _ string) (string, error) {
		return "etag-failure", nil
	}

	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if len(reporter.prepared) != 1 || reporter.prepared[0].RelativePath != "output/failure-state.json" {
		t.Fatalf("prepared = %#v, want failure-state.json", reporter.prepared)
	}
	if len(reporter.manifest.Files) != 1 || reporter.manifest.Files[0].RelativePath != "output/failure-state.json" {
		t.Fatalf("manifest = %#v, want failure-state.json", reporter.manifest)
	}
}

func TestJobArtifactUploaderDoesNotFallbackOutsideOutput(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "content.md", "internal draft")
	writeAgentArtifactTestFile(t, root, ".task-context", "TASK_ID=task-1")
	writeAgentArtifactTestFile(t, root, ".anban-creator/settings.json", "{}")
	writeAgentArtifactTestFile(t, root, ".claude/memory/MEMORY.md", "memory")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	uploader.putObject = func(context.Context, *ArtifactPrepareResponse, string, string) (string, error) {
		t.Fatal("job without output must not upload")
		return "", nil
	}
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if len(reporter.prepared) != 0 || len(reporter.manifest.Files) != 0 || len(reporter.progress) != 0 {
		t.Fatalf("job uploaded workspace internals: prepared=%v manifest=%v progress=%v", reporter.prepared, reporter.manifest, reporter.progress)
	}
}

func TestJobArtifactUploaderDoesNotFallbackWhenOutputIsEmpty(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "content.md", "internal draft")
	if err := os.Mkdir(filepath.Join(root, "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	if err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if len(reporter.prepared) != 0 || len(reporter.manifest.Files) != 0 {
		t.Fatalf("empty output fell back to workspace: %+v", reporter)
	}
}

func writeAgentArtifactTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
