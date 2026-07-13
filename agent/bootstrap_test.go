package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapJobUsesProjectedTokenAndMaterializesFiles(t *testing.T) {
	workspace := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("workload-token\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	executionToken := testExecutionToken(t, "execution-1", "task-1", "project-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/bootstrap" || r.Header.Get("Authorization") != "Bearer workload-token" {
			t.Fatalf("request = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["execution_id"] != "execution-1" {
			t.Fatalf("body=%#v err=%v", body, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]any{
			"execution_token": executionToken, "task_id": "task-1", "task_type": "article", "project_id": "project-1",
			"prompt": "write", "model": "sonnet", "max_turns": 12, "agent_flag": "anban:wechatarticle",
			"auto_memory_directory": ".claude/memory", "files": []map[string]any{{"path": ".task-context", "text": "TASK_ID=task-1\n", "mode": 420}},
		}})
	}))
	defer server.Close()

	response, err := BootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "execution-1", Workspace: workspace, WorkloadTokenFile: tokenFile})
	if err != nil {
		t.Fatalf("BootstrapJob: %v", err)
	}
	if response.ExecutionToken != executionToken {
		t.Fatal("execution token was not returned")
	}
	got, err := os.ReadFile(filepath.Join(workspace, ".task-context"))
	if err != nil || string(got) != "TASK_ID=task-1\n" {
		t.Fatalf("materialized=%q err=%v", got, err)
	}
	projected, _ := os.ReadFile(tokenFile)
	if string(projected) != "workload-token\n" {
		t.Fatalf("projected token was overwritten: %q", projected)
	}
}

func TestBootstrapJobRejectsSymlinkProjectedToken(t *testing.T) {
	dir := t.TempDir()
	realToken := filepath.Join(dir, "real")
	if err := os.WriteFile(realToken, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "token")
	if err := os.Symlink(realToken, link); err != nil {
		t.Fatal(err)
	}
	_, err := BootstrapJob(context.Background(), JobConfig{ServerURL: "http://127.0.0.1", ExecutionID: "e", Workspace: t.TempDir(), WorkloadTokenFile: link})
	if err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("err=%v", err)
	}
}

func TestBootstrapJobRejectsTrailingResponseGarbage(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{}} trailing`))
	}))
	defer server.Close()
	_, err := BootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "e", Workspace: t.TempDir(), WorkloadTokenFile: tokenFile})
	if err == nil || !strings.Contains(err.Error(), "decode bootstrap response") {
		t.Fatalf("err=%v", err)
	}
}

func TestBootstrapJobDoesNotFollowRedirectWithWorkloadToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	redirectHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectHits++
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := BootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "e", Workspace: t.TempDir(), WorkloadTokenFile: tokenFile})
	if err == nil || redirectHits != 0 {
		t.Fatalf("err=%v redirectHits=%d", err, redirectHits)
	}
}

func TestMaterializeBootstrapRejectsUnsafePathsAndCollisions(t *testing.T) {
	tests := []struct {
		name  string
		files []BootstrapFile
	}{
		{"escape", []BootstrapFile{{Path: "../secret", Text: "x", Mode: 0o644}}},
		{"absolute", []BootstrapFile{{Path: "/secret", Text: "x", Mode: 0o644}}},
		{"portable absolute", []BootstrapFile{{Path: "C:/secret", Text: "x", Mode: 0o644}}},
		{"duplicate", []BootstrapFile{{Path: "a", Text: "x", Mode: 0o644}, {Path: "a", Text: "y", Mode: 0o644}}},
		{"portable collision", []BootstrapFile{{Path: "A.txt", Text: "x", Mode: 0o644}, {Path: "a.txt", Text: "y", Mode: 0o644}}},
		{"both sources", []BootstrapFile{{Path: "a", Text: "x", DownloadURL: "https://example.com/a", Mode: 0o644}}},
		{"unsafe mode", []BootstrapFile{{Path: "a", Text: "x", Mode: 0o777}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := materializeBootstrap(context.Background(), t.TempDir(), tc.files, nil); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestMaterializeBootstrapRejectsExistingPrivilegedFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	if err := os.WriteFile(target, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, os.ModeSetuid|0o644); err != nil {
		t.Skipf("filesystem does not support setuid mode: %v", err)
	}
	err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "settings.json", Text: "same", Mode: 0o644}}, nil)
	if err == nil {
		t.Fatal("expected privileged existing target rejection")
	}
}

func TestMaterializeBootstrapRejectsExistingSymlinkParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "link/file", Text: "x", Mode: 0o644}}, nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err=%v", err)
	}
}

func TestMaterializeBootstrapDownloadIsBoundedAndSendsNoAuthorization(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()
	root := t.TempDir()
	client := bootstrapDownloadClient()
	if err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "input.bin", DownloadURL: server.URL + "/input", Mode: 0o644}}, client); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("download leaked authorization: %q", auth)
	}
	got, _ := os.ReadFile(filepath.Join(root, "input.bin"))
	if string(got) != "downloaded" {
		t.Fatalf("got=%q", got)
	}
}

func TestMaterializeBootstrapSanitizesSignedDownloadErrors(t *testing.T) {
	const secret = "signed-secret-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://example.com/file?signature="+secret, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	err := materializeBootstrap(context.Background(), t.TempDir(), []BootstrapFile{{Path: "input.bin", DownloadURL: server.URL + "/input", Mode: 0o644}}, bootstrapDownloadClient())
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked signed URL credential: %v", err)
	}
}

func testExecutionToken(t *testing.T, executionID, taskID, projectID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"execution_id": executionID, "task_id": taskID, "project_id": projectID})
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
