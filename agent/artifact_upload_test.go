package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	serveragent "github.com/anbanai/anban-creator/server/agent"
)

type fakeArtifactReporter struct {
	prepared       []ArtifactPrepareRequest
	prepareResult  *ArtifactPrepareResponse
	prepareErrors  []error
	streamed       []ArtifactStreamRequest
	bodies         []string
	manifest       ArtifactManifestRequest
	manifestCalls  int
	manifestErrors []error
	progress       []string
}

func (f *fakeArtifactReporter) StreamArtifactContent(_ context.Context, req ArtifactStreamRequest, body io.Reader) (*ArtifactStreamResponse, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	f.streamed = append(f.streamed, req)
	f.bodies = append(f.bodies, string(data))
	return &ArtifactStreamResponse{
		ObjectKey:   "uploads/users/u/projects/p/tasks/" + req.TaskID + "/executions/" + req.ExecutionID + "/artifacts/staging/" + req.RelativePath,
		ContentType: req.ContentType,
		Size:        req.Size,
		SHA256:      req.SHA256,
	}, nil
}

func (f *fakeArtifactReporter) PrepareArtifactUpload(_ context.Context, req ArtifactPrepareRequest) (*ArtifactPrepareResponse, error) {
	f.prepared = append(f.prepared, req)
	if len(f.prepareErrors) > 0 {
		err := f.prepareErrors[0]
		f.prepareErrors = f.prepareErrors[1:]
		return nil, err
	}
	response := ArtifactPrepareResponse{
		Key:            "uploads/users/u/projects/p/tasks/" + req.TaskID + "/artifacts/" + req.RelativePath,
		Bucket:         "bucket",
		Endpoint:       "oss-cn-hangzhou.aliyuncs.com",
		Headers:        map[string]string{"Content-Type": req.ContentType, "X-Oss-Meta-Sha256": req.SHA256},
		MaxSize:        512 * 1024 * 1024,
		ExpiresAt:      "2026-07-09T10:15:00Z",
		UploadRequired: true,
	}
	if f.prepareResult != nil {
		response = *f.prepareResult
	}
	response.Headers = maps.Clone(response.Headers)
	return &response, nil
}

func (f *fakeArtifactReporter) ReportArtifactManifest(_ context.Context, req ArtifactManifestRequest) error {
	f.manifestCalls++
	f.manifest = req
	if len(f.manifestErrors) > 0 {
		err := f.manifestErrors[0]
		f.manifestErrors = f.manifestErrors[1:]
		return err
	}
	return nil
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

func TestScanWorkspaceArtifactsSkipsDockerRuntimeHome(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "article.md", "# article")
	writeAgentArtifactTestFile(t, root, ".anban-runtime-home/secret.md", "runtime state")

	files, err := ScanWorkspaceArtifacts(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanWorkspaceArtifacts: %v", err)
	}
	if len(files) != 1 || files[0].RelativePath != "article.md" {
		t.Fatalf("files = %#v, want only article.md", files)
	}
}

func TestRunAgentArtifactHashTimeoutStillGetsFreshCompletionBudget(t *testing.T) {
	t.Setenv(homeTemplateEnv, "")
	t.Setenv(jobArtifactTimeoutEnv, "40ms")
	t.Setenv(jobCompletionTimeoutEnv, "250ms")
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", strings.Repeat("x", 256*1024))
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &slowArtifactReader{file: file, delay: 10 * time.Millisecond}, nil
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
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	cfg := &Config{
		ServerURL: server.URL, APIKey: "key", TaskID: "task-1", ExecutionID: "execution-1",
		TaskType: "article", Topic: "write", Workspace: root, MaxTurns: 1,
		ArtifactUploadMode: ArtifactUploadDirect,
	}
	started := time.Now()
	var stderr bytes.Buffer
	err := runAgent(runCtx, cfg, io.Discard, &stderr)
	if err == nil {
		t.Fatal("runAgent unexpectedly succeeded after runner and artifact failure")
	}
	elapsed := time.Since(started)
	if elapsed < 40*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Fatalf("runAgent finalization elapsed = %v, want artifact timeout followed by fresh completion", elapsed)
	}
	if prepared || manifested {
		t.Fatalf("timed-out hash reached remote artifact calls: prepare=%v manifest=%v", prepared, manifested)
	}
	if !completed {
		t.Fatal("artifact timeout prevented the fresh completion callback")
	}
	for _, want := range []string{
		"artifact finalization started: timeout_ms=40",
		"artifact finalization failed: duration_ms=",
		"completion report started: timeout_ms=250",
		"completion report exhausted: duration_ms=",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunAgentShutdownCancelsArtifactsAndStillReportsCompletion(t *testing.T) {
	t.Setenv(homeTemplateEnv, "")
	t.Setenv(jobArtifactTimeoutEnv, "200ms")
	t.Setenv(jobCompletionTimeoutEnv, "250ms")
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")

	previousNotify := notifyRuntimeShutdown
	notifyRuntimeShutdown = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}
	t.Cleanup(func() { notifyRuntimeShutdown = previousNotify })

	var prepared, manifested bool
	completed := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/agent/artifacts/prepare":
			prepared = true
		case "/api/v1/agent/artifacts/manifest":
			manifested = true
		case "/api/v1/agent/complete":
			completed++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &Config{
		ServerURL: server.URL, APIKey: "key", TaskID: "task-1", ExecutionID: "execution-1",
		TaskType: "article", Topic: "write", Workspace: root, MaxTurns: 1,
		ArtifactUploadMode: ArtifactUploadDirect,
	}
	_ = runAgent(context.Background(), cfg, io.Discard, io.Discard)
	if prepared || manifested || completed != 1 {
		t.Fatalf("shutdown finalization = prepare:%v manifest:%v complete:%d", prepared, manifested, completed)
	}
}

type slowArtifactReader struct {
	file  *os.File
	delay time.Duration
}

func (r *slowArtifactReader) Close() error {
	return r.file.Close()
}

func (r *slowArtifactReader) Seek(offset int64, whence int) (int64, error) {
	return r.file.Seek(offset, whence)
}

func (r *slowArtifactReader) Stat() (fs.FileInfo, error) {
	return r.file.Stat()
}

func TestArtifactFinalizationUsesFreshContextAfterRunCancellation(t *testing.T) {
	t.Setenv(homeTemplateEnv, "")
	t.Setenv(jobArtifactTimeoutEnv, "250ms")
	t.Setenv(jobCompletionTimeoutEnv, "250ms")
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")

	var prepared atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/artifacts/prepare" {
			prepared.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"upload_required": false,
				"key":             "objects/article.md",
			}})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	cfg := &Config{
		ServerURL: server.URL, APIKey: "key", TaskID: "task-1", ExecutionID: "execution-1", TaskType: "article",
		Topic: "write", Workspace: root, MaxTurns: 1, ArtifactUploadMode: ArtifactUploadDirect,
	}
	var stderr bytes.Buffer
	if err := runAgent(runCtx, cfg, io.Discard, &stderr); err == nil {
		t.Fatal("canceled agent run unexpectedly succeeded")
	}
	if !prepared.Load() {
		t.Fatal("local artifact finalization inherited the canceled run context")
	}
	for _, want := range []string{
		"artifact finalization started: timeout_ms=250",
		"artifact finalization completed: files=1 duration_ms=",
		"completion report started: timeout_ms=250",
		"completion report acknowledged: duration_ms=",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func (r *slowArtifactReader) Read(p []byte) (int, error) {
	time.Sleep(r.delay)
	if len(p) > 1024 {
		p = p[:1024]
	}
	return r.file.Read(p)
}

type artifactTestFile struct {
	*os.File
	read       func([]byte) (int, error)
	stat       func() (fs.FileInfo, error)
	closeErr   error
	closeCalls *int
}

func (f *artifactTestFile) Read(p []byte) (int, error) {
	if f.read != nil {
		return f.read(p)
	}
	return f.File.Read(p)
}

func (f *artifactTestFile) Stat() (fs.FileInfo, error) {
	if f.stat != nil {
		return f.stat()
	}
	return f.File.Stat()
}

func (f *artifactTestFile) Close() error {
	if f.closeCalls != nil {
		(*f.closeCalls)++
	}
	fileErr := f.File.Close()
	return errors.Join(fileErr, f.closeErr)
}

type artifactShortWriter struct{}

func (artifactShortWriter) Write(p []byte) (int, error) {
	return len(p) - 1, nil
}

func TestCopyArtifactWithContextRejectsShortWrite(t *testing.T) {
	_, err := copyArtifactWithContext(context.Background(), artifactShortWriter{}, strings.NewReader("payload"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("copyArtifactWithContext error = %v, want io.ErrShortWrite", err)
	}
}

func TestArtifactHeaderCaptureCapsBytesWithoutShortWrite(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 1024)
	capture := &artifactHeaderCapture{}

	written, err := capture.Write(payload)
	if err != nil || written != len(payload) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", written, err, len(payload))
	}
	if len(capture.bytes) != 512 || !bytes.Equal(capture.bytes, payload[:512]) {
		t.Fatalf("captured %d bytes, want first 512 bytes", len(capture.bytes))
	}
}

func TestOpenArtifactSnapshotPreservesHashAndCloseErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "article.md")
	writeAgentArtifactTestFile(t, root, "article.md", "content")
	hashErr := errors.New("hash read failed")
	closeErr := errors.New("close failed")
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &artifactTestFile{
			File: file,
			read: func([]byte) (int, error) {
				return 0, hashErr
			},
			closeErr: closeErr,
		}, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	_, err := openArtifactSnapshot(context.Background(), path)
	if !errors.Is(err, hashErr) || !errors.Is(err, closeErr) {
		t.Fatalf("openArtifactSnapshot error = %v, want hash and close errors", err)
	}
}

func TestOpenArtifactSnapshotPreservesOpenAndCloseErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "article.md")
	writeAgentArtifactTestFile(t, root, "article.md", "content")
	openErr := errors.New("open failed")
	closeErr := errors.New("close failed")
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &artifactTestFile{File: file, closeErr: closeErr}, openErr
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	_, err := openArtifactSnapshot(context.Background(), path)
	if !errors.Is(err, openErr) || !errors.Is(err, closeErr) {
		t.Fatalf("openArtifactSnapshot error = %v, want open and close errors", err)
	}
}

func TestOpenArtifactSnapshotPreservesStatAndCloseErrors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "article.md")
	writeAgentArtifactTestFile(t, root, "article.md", "content")
	statErr := errors.New("fstat failed")
	closeErr := errors.New("close failed")
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &artifactTestFile{
			File:     file,
			stat:     func() (fs.FileInfo, error) { return nil, statErr },
			closeErr: closeErr,
		}, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	_, err := openArtifactSnapshot(context.Background(), path)
	if !errors.Is(err, statErr) || !errors.Is(err, closeErr) {
		t.Fatalf("openArtifactSnapshot error = %v, want stat and close errors", err)
	}
}

func TestOpenArtifactSnapshotRejectsCancellationAfterFinalRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "article.md")
	writeAgentArtifactTestFile(t, root, "article.md", "final bytes")
	cancelCause := errors.New("hash canceled")
	ctx, cancel := context.WithCancelCause(context.Background())
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		read := false
		return &artifactTestFile{
			File: file,
			read: func(p []byte) (int, error) {
				if read {
					return 0, io.EOF
				}
				read = true
				n := copy(p, "final bytes")
				cancel(cancelCause)
				return n, io.EOF
			},
		}, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	snapshot, err := openArtifactSnapshot(ctx, path)
	if snapshot != nil || !errors.Is(err, cancelCause) {
		t.Fatalf("openArtifactSnapshot = (%v, %v), want nil snapshot and cancellation cause", snapshot, err)
	}
}

func TestArtifactUploaderUploadsAndReportsManifest(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	reporter := &fakeArtifactReporter{}
	var uploaded []string
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	uploader.putObject = func(_ context.Context, prepared *ArtifactPrepareResponse, source io.Reader, contentType string) (string, error) {
		body, err := io.ReadAll(source)
		if err != nil {
			return "", err
		}
		uploaded = append(uploaded, prepared.Key+"|"+string(body)+"|"+contentType)
		return "etag-1", nil
	}

	count, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if count != 1 {
		t.Fatalf("UploadWorkspaceArtifacts count = %d, want 1", count)
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
	if file.RelativePath != "output/article.md" || file.ObjectKey == "" || file.SHA256 == "" {
		t.Fatalf("manifest file = %#v, want relative path, object key, and sha", file)
	}
	if got := reporter.progress; len(got) != 1 || got[0] != "collected 1 workspace artifact(s)" {
		t.Fatalf("progress = %#v, want collected artifact progress", got)
	}
}

func TestArtifactUploaderUsesConfiguredTransport(t *testing.T) {
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
			uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
				directCalls++
				_, err := io.Copy(io.Discard, source)
				return "etag", err
			}

			if _, err := uploader.UploadWorkspaceArtifacts(t.Context(), &serveragent.ExecutionResult{WorkDir: root}); err != nil {
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

func TestArtifactUploaderOpensStableArtifactOnce(t *testing.T) {
	root := t.TempDir()
	const body = "stable artifact"
	writeAgentArtifactTestFile(t, root, "output/article.md", body)
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)

	previousOpen := openArtifactFile
	openCalls := 0
	openArtifactFile = func(path string) (artifactFile, error) {
		openCalls++
		return os.Open(path)
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	var uploaded string
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		content, err := io.ReadAll(source)
		uploaded = string(content)
		return "etag-1", err
	}

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if openCalls != 1 {
		t.Fatalf("open calls = %d, want 1", openCalls)
	}
	if uploaded != body {
		t.Fatalf("uploaded body = %q, want %q", uploaded, body)
	}
	wantHash := sha256Hex(body)
	if len(reporter.prepared) != 1 || reporter.prepared[0].Size != int64(len(body)) || reporter.prepared[0].SHA256 != wantHash {
		t.Fatalf("prepare = %#v, want size %d and SHA-256 %q", reporter.prepared, len(body), wantHash)
	}
	if len(reporter.manifest.Files) != 1 || reporter.manifest.Files[0].Size != int64(len(body)) || reporter.manifest.Files[0].SHA256 != wantHash {
		t.Fatalf("manifest = %#v, want size %d and SHA-256 %q", reporter.manifest, len(body), wantHash)
	}
}

func TestArtifactUploaderSkipsEmptyLegacyManifest(t *testing.T) {
	root := t.TempDir()
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if reporter.manifestCalls != 0 {
		t.Fatalf("manifest calls = %d, want 0 for legacy empty scan", reporter.manifestCalls)
	}
}

func TestArtifactUploaderRejectsMissingOrMismatchedPreparedSHA256(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{name: "missing", headers: map[string]string{"Content-Type": "text/markdown"}},
		{name: "mismatched", headers: map[string]string{"Content-Type": "text/markdown", "X-Oss-Meta-Sha256": "wrong"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
			reporter := &fakeArtifactReporter{prepareResult: &ArtifactPrepareResponse{
				Key:            "uploads/article.md",
				UploadRequired: true,
				Headers:        tc.headers,
				MaxSize:        512 * 1024 * 1024,
			}}
			uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
			putCalls := 0
			uploader.putObject = func(context.Context, *ArtifactPrepareResponse, io.Reader, string) (string, error) {
				putCalls++
				return "etag", nil
			}

			_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
			if err == nil || !strings.Contains(err.Error(), "SHA-256") {
				t.Fatalf("UploadWorkspaceArtifacts error = %v, want SHA-256 validation error", err)
			}
			if putCalls != 0 {
				t.Fatalf("put calls = %d, want 0", putCalls)
			}
		})
	}
}

func TestArtifactUploaderSkipsMatchingObjectButStillManifestsIt(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	reporter := &fakeArtifactReporter{prepareResult: &ArtifactPrepareResponse{
		Key:            "uploads/existing/article.md",
		ETag:           "existing-etag",
		UploadRequired: false,
		Headers:        map[string]string{"Content-Type": "text/markdown"},
		MaxSize:        512 * 1024 * 1024,
	}}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	uploader.putObject = func(context.Context, *ArtifactPrepareResponse, io.Reader, string) (string, error) {
		t.Fatal("matching object must not be uploaded")
		return "", nil
	}

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if reporter.manifestCalls != 1 || len(reporter.manifest.Files) != 1 {
		t.Fatalf("manifest calls/files = %d/%d, want 1/1", reporter.manifestCalls, len(reporter.manifest.Files))
	}
	if got := reporter.progress; len(got) != 1 || got[0] != "collected 1 workspace artifact(s)" {
		t.Fatalf("progress = %#v, want collected artifact progress", got)
	}
}

func TestArtifactUploaderUploadsFailureArtifactsForUnsuccessfulResult(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/failure-state.json", `{"stage":"quality-gate"}`)
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, _ io.Reader, _ string) (string, error) {
		return "etag-failure", nil
	}

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if len(reporter.prepared) != 1 || reporter.prepared[0].RelativePath != "output/failure-state.json" {
		t.Fatalf("prepared = %#v, want failure-state.json", reporter.prepared)
	}
	if len(reporter.manifest.Files) != 1 || reporter.manifest.Files[0].RelativePath != "output/failure-state.json" {
		t.Fatalf("manifest = %#v, want failure-state.json", reporter.manifest)
	}
}

func TestJobArtifactUploaderSubmitsEmptyManifestWhenOutputIsMissing(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "content.md", "internal draft")
	writeAgentArtifactTestFile(t, root, ".task-context", "TASK_ID=task-1")
	writeAgentArtifactTestFile(t, root, ".anban-creator/settings.json", "{}")
	writeAgentArtifactTestFile(t, root, ".claude/memory/MEMORY.md", "memory")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	uploader.putObject = func(context.Context, *ArtifactPrepareResponse, io.Reader, string) (string, error) {
		t.Fatal("job without output must not upload")
		return "", nil
	}
	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if len(reporter.prepared) != 0 || reporter.manifestCalls != 1 || len(reporter.manifest.Files) != 0 || len(reporter.progress) != 0 {
		t.Fatalf("job uploaded workspace internals: prepared=%v manifest=%v calls=%d progress=%v", reporter.prepared, reporter.manifest, reporter.manifestCalls, reporter.progress)
	}
	if reporter.manifest.ExecutionID != "execution-1" {
		t.Fatalf("manifest execution ID = %q, want execution-1", reporter.manifest.ExecutionID)
	}
}

func TestJobArtifactUploaderSubmitsEmptyManifest(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "content.md", "internal draft")
	if err := os.Mkdir(filepath.Join(root, "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", ExecutionID: "execution-1", Workspace: root}, reporter)
	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: false, WorkDir: root}); err != nil {
		t.Fatal(err)
	}
	if len(reporter.prepared) != 0 || reporter.manifestCalls != 1 || len(reporter.manifest.Files) != 0 || len(reporter.progress) != 0 {
		t.Fatalf("empty output fell back to workspace: %+v", reporter)
	}
	if reporter.manifest.ExecutionID != "execution-1" {
		t.Fatalf("manifest execution ID = %q, want execution-1", reporter.manifest.ExecutionID)
	}
}

func TestArtifactUploaderRetryAfterManifestFailureReusesUploadedObject(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	manifestErr := errors.New("manifest unavailable")
	reporter := &fakeArtifactReporter{manifestErrors: []error{manifestErr}}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	putCalls := 0
	uploader.putObject = func(_ context.Context, prepared *ArtifactPrepareResponse, _ io.Reader, contentType string) (string, error) {
		putCalls++
		reporter.prepareResult = &ArtifactPrepareResponse{
			Key:            prepared.Key,
			ETag:           "reused-etag",
			UploadRequired: false,
			Headers:        map[string]string{"Content-Type": contentType},
			MaxSize:        prepared.MaxSize,
		}
		return "reused-etag", nil
	}

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if !errors.Is(err, manifestErr) {
		t.Fatalf("first collection error = %v, want manifest error", err)
	}
	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("second collection: %v", err)
	}
	if putCalls != 1 {
		t.Fatalf("put calls = %d, want 1", putCalls)
	}
	if reporter.manifestCalls != 2 {
		t.Fatalf("manifest calls = %d, want 2", reporter.manifestCalls)
	}
}

func TestApplyArtifactUploadFailureRejectsOtherwiseSuccessfulRun(t *testing.T) {
	result := &serveragent.ExecutionResult{Success: true}
	uploadErr := errors.New("manifest unavailable")

	err := applyArtifactUploadFailure(result, nil, uploadErr)

	if !errors.Is(err, uploadErr) {
		t.Fatalf("error = %v, want upload error", err)
	}
	if result.Success {
		t.Fatal("successful result remained successful after artifact upload failure")
	}
	if result.Error != "artifact upload failed: manifest unavailable" {
		t.Fatalf("result error = %q", result.Error)
	}
}

func TestApplyArtifactUploadFailurePreservesExistingRunFailure(t *testing.T) {
	runErr := errors.New("runner failed")
	uploadErr := errors.New("manifest unavailable")
	result := &serveragent.ExecutionResult{Success: false, Error: "runner failed"}

	got := applyArtifactUploadFailure(result, runErr, uploadErr)

	if !errors.Is(got, runErr) {
		t.Fatalf("error = %v, want original run error", got)
	}
	if result.Error != "runner failed" || result.Success {
		t.Fatalf("failed result was overwritten: %#v", result)
	}
	if got := applyArtifactUploadFailure(nil, runErr, uploadErr); !errors.Is(got, runErr) {
		t.Fatalf("nil result error = %v, want original run error", got)
	}
}

func TestPutOSSObjectFromBucketAddsSHA256Metadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "article.md")
	if err := os.WriteFile(path, []byte("# article"), 0o644); err != nil {
		t.Fatal(err)
	}
	var sha256Header string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sha256Header = r.Header.Get("X-Oss-Meta-Sha256")
		w.Header().Set("ETag", "test-etag")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := oss.New(server.URL, "access-key", "secret", oss.UseCname(true))
	if err != nil {
		t.Fatalf("create OSS client: %v", err)
	}
	bucket, err := client.Bucket("bucket")
	if err != nil {
		t.Fatalf("open bucket: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := putOSSObjectFromBucket(context.Background(), bucket, "artifacts/article.md", file, "text/markdown", "ABCDEF"); err != nil {
		t.Fatalf("put object: %v", err)
	}
	if sha256Header != "abcdef" {
		t.Fatalf("X-Oss-Meta-Sha256 = %q, want abcdef", sha256Header)
	}
}

func TestArtifactUploaderRetainsOwnershipOfOSSUploadFile(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "owned by uploader")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read request body: %v", err)
		}
		w.Header().Set("ETag", "test-etag")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := oss.New(server.URL, "access-key", "secret", oss.UseCname(true))
	if err != nil {
		t.Fatalf("create OSS client: %v", err)
	}
	bucket, err := client.Bucket("bucket")
	if err != nil {
		t.Fatalf("open bucket: %v", err)
	}

	previousOpen := openArtifactFile
	ownerCloseCalls := 0
	var uploadFile *artifactTestFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		uploadFile = &artifactTestFile{File: file, closeCalls: &ownerCloseCalls}
		return uploadFile, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	uploader.putObject = func(ctx context.Context, prepared *ArtifactPrepareResponse, source io.Reader, contentType string) (string, error) {
		if _, err := putOSSObjectFromBucket(ctx, bucket, prepared.Key, source, contentType, prepared.Headers[artifactSHA256Header]); err != nil {
			return "", err
		}
		if _, ok := source.(io.Closer); ok {
			return "", errors.New("OSS upload source exposes Close")
		}
		if _, ok := source.(io.Seeker); !ok {
			return "", errors.New("OSS upload source does not expose Seek")
		}
		if _, err := uploadFile.Stat(); err != nil {
			return "", fmt.Errorf("stat descriptor after OSS upload: %w", err)
		}
		if _, err := uploadFile.Seek(0, io.SeekCurrent); err != nil {
			return "", fmt.Errorf("seek descriptor after OSS upload: %w", err)
		}
		return "etag-owned", nil
	}

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if ownerCloseCalls != 1 {
		t.Fatalf("owner close calls = %d, want 1", ownerCloseCalls)
	}
}

func TestArtifactUploaderRetriesFileChangedDuringUpload(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output", "article.md")
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-one")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	putCalls := 0
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		putCalls++
		if _, err := io.Copy(io.Discard, source); err != nil {
			return "", err
		}
		if putCalls == 1 {
			if err := os.WriteFile(path, []byte("version-two-expanded"), 0o644); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("etag-%d", putCalls), nil
	}

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if putCalls != 2 {
		t.Fatalf("put calls = %d, want 2", putCalls)
	}
	if reporter.manifestCalls != 1 || len(reporter.manifest.Files) != 1 {
		t.Fatalf("manifest calls/files = %d/%d, want 1/1", reporter.manifestCalls, len(reporter.manifest.Files))
	}
	if got := reporter.manifest.Files[0].SHA256; got != sha256Hex("version-two-expanded") {
		t.Fatalf("manifest SHA-256 = %q, want %q", got, sha256Hex("version-two-expanded"))
	}
}

func TestArtifactUploaderFailsWhenFileNeverStabilizes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output", "article.md")
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-0")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	putCalls := 0
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		putCalls++
		if _, err := io.Copy(io.Discard, source); err != nil {
			return "", err
		}
		body := strings.Repeat("expanded-", putCalls) + fmt.Sprintf("version-%d", putCalls)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("etag-%d", putCalls), nil
	}

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if err == nil || !strings.Contains(err.Error(), "changed during final collection") {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want mutation error", err)
	}
	if putCalls != maxArtifactSnapshotAttempts {
		t.Fatalf("put calls = %d, want %d", putCalls, maxArtifactSnapshotAttempts)
	}
	if reporter.manifestCalls != 0 {
		t.Fatalf("manifest calls = %d, want 0", reporter.manifestCalls)
	}
}

func TestArtifactUploaderPreservesPrepareAndCloseErrors(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "stable")
	prepareErr := errors.New("prepare failed")
	closeErr := errors.New("close failed")
	reporter := &fakeArtifactReporter{prepareErrors: []error{prepareErr}}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	installArtifactCloseFailure(t, closeErr)

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if !errors.Is(err, prepareErr) || !errors.Is(err, closeErr) {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want prepare and close errors", err)
	}
	if !strings.Contains(err.Error(), "prepare artifact upload output/article.md") || !strings.Contains(err.Error(), "close artifact output/article.md") {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want scoped prepare and close messages", err)
	}
}

func TestArtifactUploaderPreservesPutAndCloseErrors(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "stable")
	putErr := errors.New("put failed")
	closeErr := errors.New("close failed")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	uploader.putObject = func(context.Context, *ArtifactPrepareResponse, io.Reader, string) (string, error) {
		return "", putErr
	}
	installArtifactCloseFailure(t, closeErr)

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if !errors.Is(err, putErr) || !errors.Is(err, closeErr) {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want PUT and close errors", err)
	}
	if !strings.Contains(err.Error(), "upload artifact output/article.md") || !strings.Contains(err.Error(), "close artifact output/article.md") {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want scoped PUT and close messages", err)
	}
}

func TestArtifactUploaderRetriesFileChangedDuringSnapshotOpen(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output", "article.md")
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-one")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	putCalls := 0
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		putCalls++
		_, err := io.Copy(io.Discard, source)
		return "etag-latest", err
	}
	previousOpen := openArtifactFile
	openCalls := 0
	openArtifactFile = func(openPath string) (artifactFile, error) {
		openCalls++
		if openCalls == 1 {
			if err := os.Remove(openPath); err != nil {
				return nil, err
			}
			if err := os.WriteFile(openPath, []byte("version-two-expanded"), 0o644); err != nil {
				return nil, err
			}
		}
		return os.Open(openPath)
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	if openCalls != 2 || putCalls != 1 {
		t.Fatalf("open/PUT calls = %d/%d, want 2/1", openCalls, putCalls)
	}
	if reporter.manifestCalls != 1 || len(reporter.manifest.Files) != 1 {
		t.Fatalf("manifest calls/files = %d/%d, want 1/1", reporter.manifestCalls, len(reporter.manifest.Files))
	}
	if got := reporter.manifest.Files[0].SHA256; got != sha256Hex("version-two-expanded") {
		t.Fatalf("manifest SHA-256 = %q, want latest hash", got)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "version-two-expanded" {
		t.Fatalf("latest artifact = %q, %v", body, err)
	}
}

func TestArtifactUploaderUsesLatestSnapshotMIMEAfterPathReplacement(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/artifact.bin", "plain text artifact")
	latest := []byte("\x89PNG\r\n\x1a\nlatest png payload")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)

	previousOpen := openArtifactFile
	openCalls := 0
	openArtifactFile = func(openPath string) (artifactFile, error) {
		openCalls++
		if openCalls == 1 {
			if err := os.Remove(openPath); err != nil {
				return nil, err
			}
			if err := os.WriteFile(openPath, latest, 0o644); err != nil {
				return nil, err
			}
		}
		return os.Open(openPath)
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	var uploaded []byte
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		var err error
		uploaded, err = io.ReadAll(source)
		return "etag-latest", err
	}

	if _, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root}); err != nil {
		t.Fatalf("UploadWorkspaceArtifacts: %v", err)
	}
	wantHash := sha256Hex(string(latest))
	if len(reporter.prepared) != 1 {
		t.Fatalf("prepare calls = %d, want 1", len(reporter.prepared))
	}
	prepared := reporter.prepared[0]
	if prepared.ContentType != "image/png" || prepared.Size != int64(len(latest)) || prepared.SHA256 != wantHash {
		t.Fatalf("prepare content type/size/SHA-256 = %q/%d/%q, want image/png/%d/%q", prepared.ContentType, prepared.Size, prepared.SHA256, len(latest), wantHash)
	}
	if len(reporter.manifest.Files) != 1 {
		t.Fatalf("manifest files = %d, want 1", len(reporter.manifest.Files))
	}
	manifestFile := reporter.manifest.Files[0]
	if manifestFile.ContentType != "image/png" || manifestFile.Size != int64(len(latest)) || manifestFile.SHA256 != wantHash {
		t.Fatalf("manifest content type/size/SHA-256 = %q/%d/%q, want image/png/%d/%q", manifestFile.ContentType, manifestFile.Size, manifestFile.SHA256, len(latest), wantHash)
	}
	if !bytes.Equal(uploaded, latest) {
		t.Fatalf("uploaded body = %q, want latest PNG bytes", uploaded)
	}
}

func TestArtifactUploaderDoesNotRetryPostUploadDescriptorStatError(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "stable")
	statErr := errors.New("post-upload fstat failed")
	closeErr := errors.New("close failed")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	putCalls := 0
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		putCalls++
		_, err := io.Copy(io.Discard, source)
		return "etag", err
	}
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		statCalls := 0
		return &artifactTestFile{
			File: file,
			stat: func() (fs.FileInfo, error) {
				statCalls++
				if statCalls == 1 {
					return file.Stat()
				}
				return nil, statErr
			},
			closeErr: closeErr,
		}, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if !errors.Is(err, statErr) || !errors.Is(err, closeErr) {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want stat and close errors", err)
	}
	if strings.Contains(err.Error(), "changed during final collection") {
		t.Fatalf("descriptor stat error mislabeled as mutation: %v", err)
	}
	if len(reporter.prepared) != 1 || putCalls != 1 || reporter.manifestCalls != 0 {
		t.Fatalf("prepare/PUT/manifest calls = %d/%d/%d, want 1/1/0", len(reporter.prepared), putCalls, reporter.manifestCalls)
	}
}

func TestArtifactUploaderDoesNotRetryPostUploadPathStatError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output", "article.md")
	writeAgentArtifactTestFile(t, root, "output/article.md", "stable")
	closeErr := errors.New("close failed")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	putCalls := 0
	uploader.putObject = func(_ context.Context, _ *ArtifactPrepareResponse, source io.Reader, _ string) (string, error) {
		putCalls++
		if _, err := io.Copy(io.Discard, source); err != nil {
			return "", err
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
		return "etag", nil
	}
	installArtifactCloseFailure(t, closeErr)

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if !errors.Is(err, fs.ErrNotExist) || !errors.Is(err, closeErr) {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want path stat and close errors", err)
	}
	if !strings.Contains(err.Error(), "recheck artifact output/article.md") || !strings.Contains(err.Error(), "inspect artifact path") || strings.Contains(err.Error(), "changed during final collection") {
		t.Fatalf("path stat error has wrong classification: %v", err)
	}
	if len(reporter.prepared) != 1 || putCalls != 1 || reporter.manifestCalls != 0 {
		t.Fatalf("prepare/PUT/manifest calls = %d/%d/%d, want 1/1/0", len(reporter.prepared), putCalls, reporter.manifestCalls)
	}
}

func TestArtifactUploaderFailsWhenFileKeepsChangingWhileOpening(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-zero")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	previousOpen := openArtifactFile
	openCalls := 0
	openArtifactFile = func(openPath string) (artifactFile, error) {
		openCalls++
		if err := os.Remove(openPath); err != nil {
			return nil, err
		}
		body := strings.Repeat("expanded-", openCalls) + fmt.Sprintf("version-%d", openCalls)
		if err := os.WriteFile(openPath, []byte(body), 0o644); err != nil {
			return nil, err
		}
		return os.Open(openPath)
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if err == nil || !strings.Contains(err.Error(), "artifact output/article.md changed during final collection") {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want persistent open-race error", err)
	}
	if attempts := openCalls; attempts != maxArtifactSnapshotAttempts {
		t.Fatalf("snapshot attempts = %d, want %d", attempts, maxArtifactSnapshotAttempts)
	}
	if len(reporter.prepared) != 0 || reporter.manifestCalls != 0 {
		t.Fatalf("persistent open races reached remote calls: prepares=%d manifests=%d", len(reporter.prepared), reporter.manifestCalls)
	}
}

func TestArtifactUploaderPreservesOpenRaceAndCloseErrors(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "version-one")
	closeErr := errors.New("close failed")
	reporter := &fakeArtifactReporter{}
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
	previousOpen := openArtifactFile
	openCalls := 0
	openArtifactFile = func(openPath string) (artifactFile, error) {
		openCalls++
		file, err := os.Open(openPath)
		if openCalls == 1 {
			if err != nil {
				return nil, err
			}
			if err := os.Remove(openPath); err != nil {
				file.Close()
				return nil, err
			}
			if err := os.WriteFile(openPath, []byte("version-two"), 0o644); err != nil {
				file.Close()
				return nil, err
			}
			newFile, openErr := os.Open(openPath)
			file.Close()
			if openErr != nil {
				return nil, openErr
			}
			return &artifactTestFile{File: newFile, closeErr: closeErr}, nil
		}
		return file, err
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })

	_, err := uploader.UploadWorkspaceArtifacts(context.Background(), &serveragent.ExecutionResult{Success: true, WorkDir: root})
	if !errors.Is(err, closeErr) || !strings.Contains(err.Error(), "artifact changed while opening") {
		t.Fatalf("UploadWorkspaceArtifacts error = %v, want mutation and close errors", err)
	}
	if reporter.manifestCalls != 0 {
		t.Fatalf("manifest calls = %d, want 0", reporter.manifestCalls)
	}
}

func installArtifactCloseFailure(t *testing.T, closeErr error) {
	t.Helper()
	previousOpen := openArtifactFile
	openArtifactFile = func(path string) (artifactFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &artifactTestFile{File: file, closeErr: closeErr}, nil
	}
	t.Cleanup(func() { openArtifactFile = previousOpen })
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
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
