package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anbanai/anban-creator/server/agentpack"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nartifact-image")

func TestToolBaseNameHandlesPluginMCPNames(t *testing.T) {
	cases := map[string]string{
		"generate_image": "generate_image",
		"mcp__plugin_anban_creator__generate_image":        "generate_image",
		"mcp__plugin_anban_creator__create_video_asr_task": "create_video_asr_task",
	}

	for name, want := range cases {
		if got := toolBaseName(name); got != want {
			t.Fatalf("toolBaseName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDownloaderResolvesRelativePathsFromTaskRuntimeCwd(t *testing.T) {
	workspace := t.TempDir()
	montage := NewDownloader(&Config{Workspace: workspace, TaskType: "montage", RuntimeAdapter: agentpack.AdapterOpenMontage})
	got, err := montage.resolveWorkspacePath("output/final.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace, "openmontage", "output", "final.mp4"); got != want {
		t.Fatalf("Montage download path = %q, want %q", got, want)
	}

	article := NewDownloader(&Config{Workspace: workspace, TaskType: "article"})
	got, err = article.resolveWorkspacePath("output/article.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workspace, "output", "article.md"); got != want {
		t.Fatalf("Article download path = %q, want %q", got, want)
	}
}

func TestDownloaderMaterializesFourGeneratedImages(t *testing.T) {
	workspace := t.TempDir()
	server := imageArtifactServer(t, testPNG, "image/png", nil)
	defer server.Close()
	downloader := NewDownloader(&Config{Workspace: workspace, ServerURL: server.URL, TaskType: "seednote"})

	for index, name := range []string{"cover.png", "image_01.png", "image_02.png", "tail.png"} {
		path := "output/" + name
		content := artifactToolResult(t, path, server.URL+"/image/"+name, testPNG, "image/png")
		call := trackedToolCall{Name: "mcp__plugin_anban_creator__generate_image", Input: map[string]any{"output_path": path}}
		if err := downloader.HandleToolResult(context.Background(), call, content); err != nil {
			t.Fatalf("materialize image %d: %v", index, err)
		}
		got, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(testPNG) {
			t.Fatalf("materialized %s bytes = %q", name, got)
		}
	}
}

func TestDownloaderRejectsInvalidGeneratedImageArtifacts(t *testing.T) {
	tests := []struct {
		name       string
		outputPath string
		filePath   string
		body       []byte
		mimeType   string
		fileSize   int64
		hash       string
		want       string
	}{
		{name: "hash mismatch", outputPath: "output/cover.png", filePath: "output/cover.png", body: testPNG, mimeType: "image/png", fileSize: int64(len(testPNG)), hash: strings.Repeat("0", 64), want: "SHA-256 mismatch"},
		{name: "truncated", outputPath: "output/cover.png", filePath: "output/cover.png", body: testPNG, mimeType: "image/png", fileSize: int64(len(testPNG) + 1), hash: hashBytes(testPNG), want: "size mismatch"},
		{name: "wrong MIME", outputPath: "output/cover.png", filePath: "output/cover.png", body: []byte("plain text"), mimeType: "image/png", fileSize: 10, hash: hashBytes([]byte("plain text")), want: "MIME mismatch"},
		{name: "path escape", outputPath: "../cover.png", filePath: "../cover.png", body: testPNG, mimeType: "image/png", fileSize: int64(len(testPNG)), hash: hashBytes(testPNG), want: "escapes workspace or is not clean"},
		{name: "path mismatch", outputPath: "output/cover.png", filePath: "output/other.png", body: testPNG, mimeType: "image/png", fileSize: int64(len(testPNG)), hash: hashBytes(testPNG), want: "does not match requested output_path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			server := imageArtifactServer(t, tt.body, tt.mimeType, nil)
			defer server.Close()
			downloader := NewDownloader(&Config{Workspace: workspace, ServerURL: server.URL, TaskType: "seednote"})
			payload := downloadPayload{
				TaskFileID: "file-1", FilePath: tt.filePath, DownloadURL: server.URL + "/image",
				MimeType: tt.mimeType, FileSize: tt.fileSize, ContentHash: tt.hash,
			}
			content, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			err = downloader.HandleToolResult(context.Background(), trackedToolCall{
				Name: "generate_image", Input: map[string]any{"output_path": tt.outputPath},
			}, string(content))
			if err == nil || !strings.Contains(err.Error(), runtimeArtifactMaterializationFailureCode) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("HandleToolResult error = %v, want coded %q", err, tt.want)
			}
			if _, statErr := os.Stat(filepath.Join(workspace, "output", "cover.png")); !os.IsNotExist(statErr) {
				t.Fatalf("invalid artifact left target file: %v", statErr)
			}
		})
	}
}

func TestDownloaderSameHashIsNoOpAndDifferentHashReplacesAtomically(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "output", "cover.png")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, testPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	replacement := []byte("\x89PNG\r\n\x1a\nreplacement-image")
	server := imageArtifactServer(t, replacement, "image/png", &requests)
	defer server.Close()
	downloader := NewDownloader(&Config{Workspace: workspace, ServerURL: server.URL, TaskType: "seednote"})
	call := trackedToolCall{Name: "generate_image", Input: map[string]any{"output_path": "output/cover.png"}}

	if err := downloader.HandleToolResult(context.Background(), call, artifactToolResult(t, "output/cover.png", server.URL+"/same", testPNG, "image/png")); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("same-hash materialization made %d HTTP requests", requests.Load())
	}

	if err := downloader.HandleToolResult(context.Background(), call, artifactToolResult(t, "output/cover.png", server.URL+"/replacement", replacement, "image/png")); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("replacement made %d HTTP requests, want 1", requests.Load())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(replacement) {
		t.Fatalf("replacement bytes = %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(workspace, "output", ".anban-artifact-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after replacement: %v", matches)
	}
}

func TestDownloaderSendsExecutionTokenOnlyToServerOrigin(t *testing.T) {
	var sameOriginAuth, externalAuth string
	sameOrigin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sameOriginAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	defer sameOrigin.Close()
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG)
	}))
	defer external.Close()

	workspace := t.TempDir()
	downloader := NewDownloader(&Config{
		Workspace: workspace, TaskType: "seednote", ServerURL: sameOrigin.URL, APIKey: "execution-token",
	})
	for _, test := range []struct {
		path string
		url  string
	}{
		{path: "output/same-origin.png", url: sameOrigin.URL + "/image"},
		{path: "output/external.png", url: external.URL + "/image"},
	} {
		call := trackedToolCall{Name: "generate_image", Input: map[string]any{"output_path": test.path}}
		if err := downloader.HandleToolResult(context.Background(), call, artifactToolResult(t, test.path, test.url, testPNG, "image/png")); err != nil {
			t.Fatalf("materialize %s: %v", test.path, err)
		}
	}
	if sameOriginAuth != "Bearer execution-token" {
		t.Fatalf("same-origin authorization = %q", sameOriginAuth)
	}
	if externalAuth != "" {
		t.Fatalf("execution token leaked to external artifact host: %q", externalAuth)
	}

	redirectRequest, err := http.NewRequest(http.MethodGet, external.URL+"/redirected", nil)
	if err != nil {
		t.Fatal(err)
	}
	redirectRequest.Header.Set("Authorization", "Bearer execution-token")
	if err := downloader.client.CheckRedirect(redirectRequest, nil); err != nil {
		t.Fatal(err)
	}
	if got := redirectRequest.Header.Get("Authorization"); got != "" {
		t.Fatalf("redirect retained execution token for external origin: %q", got)
	}
}

func artifactToolResult(t *testing.T, filePath, downloadURL string, body []byte, mimeType string) string {
	t.Helper()
	payload := downloadPayload{
		TaskFileID: "file-1", FilePath: filePath, DownloadURL: downloadURL,
		MimeType: mimeType, FileSize: int64(len(body)), ContentHash: hashBytes(body),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func imageArtifactServer(t *testing.T, body []byte, contentType string, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests != nil {
			requests.Add(1)
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
