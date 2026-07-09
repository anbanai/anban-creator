package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
)

type fakeArtifactReporter struct {
	prepared []ArtifactPrepareRequest
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
	writeAgentArtifactTestFile(t, root, "output/.env", "secret")
	writeAgentArtifactTestFile(t, root, "output/.claude/session.json", "{}")
	writeAgentArtifactTestFile(t, root, "output/node_modules/pkg/index.js", "module.exports = {}")
	writeAgentArtifactTestFile(t, root, "output/package.json", "{}")

	files, err := ScanWorkspaceArtifacts(root)
	if err != nil {
		t.Fatalf("ScanWorkspaceArtifacts: %v", err)
	}
	var rels []string
	for _, file := range files {
		rels = append(rels, file.RelativePath)
	}
	sort.Strings(rels)
	want := []string{"output/article.md", "output/images/cover.png"}
	if len(rels) != len(want) {
		t.Fatalf("rels = %#v, want %#v", rels, want)
	}
	for i := range want {
		if rels[i] != want[i] {
			t.Fatalf("rels = %#v, want %#v", rels, want)
		}
	}
}

func TestArtifactUploaderUploadsAndReportsManifest(t *testing.T) {
	root := t.TempDir()
	writeAgentArtifactTestFile(t, root, "output/article.md", "# article")
	reporter := &fakeArtifactReporter{}
	var uploaded []string
	uploader := NewArtifactUploader(&Config{TaskID: "task-1", Workspace: root}, reporter)
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
