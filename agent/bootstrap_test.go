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

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/service"
)

func testModelUsageAliases() map[string]serveragent.ModelUsageIdentity {
	return map[string]serveragent.ModelUsageIdentity{
		"sonnet": {Provider: "volcengine_ark", Model: "sonnet"},
	}
}

func testClaudeRuntimeEnv() map[string]string {
	return map[string]string{
		"ANTHROPIC_AUTH_TOKEN":           "token",
		"ANTHROPIC_BASE_URL":             "https://ark.cn-beijing.volces.com/api/compatible",
		"ANTHROPIC_MODEL":                "sonnet",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "sonnet",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  "sonnet",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "sonnet",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "haiku",
	}
}

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
			"prompt": "write", "model": "sonnet", "max_turns": 12, "agent_flag": "anban:article",
			"auto_memory_directory": ".claude/memory", "files": []map[string]any{{"path": ".task-context", "text": "TASK_ID=task-1\n", "mode": 420}},
			"model_usage_aliases": testModelUsageAliases(),
			"runtime_env":         testClaudeRuntimeEnv(),
			"artifact_transport":  map[string]any{"mode": "direct"},
		}})
	}))
	defer server.Close()

	response, err := testBootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "execution-1", Workspace: workspace, WorkloadTokenFile: tokenFile})
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

func TestBootstrapCreatesRuntimeOwnedOutput(t *testing.T) {
	workspace := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("workload-token\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	executionToken := testExecutionToken(t, "execution-1", "task-1", "project-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]any{
			"execution_token": executionToken, "task_id": "task-1", "task_type": "article", "project_id": "project-1",
			"prompt": "write", "model": "sonnet", "max_turns": 12, "agent_flag": "anban:article",
			"auto_memory_directory": ".claude/memory", "files": []map[string]any{},
			"model_usage_aliases": testModelUsageAliases(), "runtime_env": testClaudeRuntimeEnv(),
			"artifact_transport": map[string]any{"mode": "stream"},
		}})
	}))
	defer server.Close()

	if _, err := testBootstrapJob(context.Background(), JobConfig{
		ServerURL: server.URL, ExecutionID: "execution-1", Workspace: workspace, WorkloadTokenFile: tokenFile,
	}); err != nil {
		t.Fatalf("BootstrapJob: %v", err)
	}
	info, err := os.Lstat(filepath.Join(workspace, "output"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o750 {
		t.Fatalf("runtime output = %#v, err=%v", info, err)
	}
}

func TestBootstrapJobMaterializesMontageInputsInsideRuntime(t *testing.T) {
	template := filepath.Join(t.TempDir(), "template")
	if err := os.MkdirAll(template, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(template, "README.md"), []byte("template"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Setenv(serveragent.MontageTemplateEnvName, template)

	workspace := t.TempDir()
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("workload-token\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	executionToken := testExecutionToken(t, "execution-1", "task-1", "project-1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "success", "data": map[string]any{
			"execution_token": executionToken, "task_id": "task-1", "task_type": "montage", "project_id": "project-1",
			"prompt": "render", "model": "sonnet", "max_turns": 40, "agent_flag": "anban:montage",
			"auto_memory_directory": ".claude/memory", "files": []map[string]any{{"path": "montage-input.json", "text": "{}", "mode": 420}},
			"model_usage_aliases": testModelUsageAliases(), "runtime_env": testClaudeRuntimeEnv(),
			"artifact_transport": map[string]any{"mode": "stream"},
		}})
	}))
	defer server.Close()

	if _, err := testBootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "execution-1", Workspace: workspace, WorkloadTokenFile: tokenFile}); err != nil {
		t.Fatalf("BootstrapJob: %v", err)
	}
	for _, name := range []string{"README.md", "montage-input.json"} {
		if _, err := os.Stat(filepath.Join(workspace, "openmontage", name)); err != nil {
			t.Fatalf("Montage runtime missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "montage-input.json")); !os.IsNotExist(err) {
		t.Fatalf("Montage input must not be materialized outside runtime: %v", err)
	}
}

func TestReadProjectedTokenAcceptsKubernetesAtomicSymlinkLayout(t *testing.T) {
	root := t.TempDir()
	timestamp := filepath.Join(root, "..2026_07_13_12_00_00")
	if err := os.Mkdir(timestamp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(timestamp, "token"), []byte("projected-token\n"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(timestamp), filepath.Join(root, "..data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..data", "token"), filepath.Join(root, "token")); err != nil {
		t.Fatal(err)
	}
	got, err := readProjectedToken(filepath.Join(root, "token"))
	if err != nil || got != "projected-token" {
		t.Fatalf("token=%q err=%v", got, err)
	}
}

func TestReadProjectedTokenAcceptsDirectRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("direct-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readProjectedToken(path)
	if err != nil || got != "direct-token" {
		t.Fatalf("token=%q err=%v", got, err)
	}
}

func TestReadProjectedTokenRejectsWritableModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o666, 0o670} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte("token"), mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := readProjectedToken(path); err == nil {
				t.Fatal("expected writable token rejection")
			}
		})
	}
}

func TestBootstrapProductionServerURLRequiresHTTPS(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080", "https://user@example.com", "https://example.com/path#fragment",
		"https://example.com:443:444",
	} {
		if _, err := validateBootstrapServerURL(raw, false); err == nil {
			t.Fatalf("URL %q accepted", raw)
		}
	}
	if _, err := validateBootstrapServerURL("https://creator-api.example.com", false); err != nil {
		t.Fatal(err)
	}
	if _, err := validateBootstrapServerURL("http://127.0.0.1:8080", true); err != nil {
		t.Fatalf("test loopback rejected: %v", err)
	}
}

func TestBootstrapJobRejectsHTTPByDefault(t *testing.T) {
	_, err := BootstrapJob(context.Background(), JobConfig{
		ServerURL: "http://creator-server:8080", ExecutionID: "execution-1", Workspace: t.TempDir(), WorkloadTokenFile: filepath.Join(t.TempDir(), "token"),
	})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("BootstrapJob HTTP error = %v, want default HTTPS rejection", err)
	}
}

func TestValidateBootstrapServerURLAllowsExplicitManagedHTTPServer(t *testing.T) {
	parsed, err := validateBootstrapServerURL("http://creator-server:8080", true)
	if err != nil {
		t.Fatalf("explicit managed HTTP server rejected: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != "creator-server:8080" {
		t.Fatalf("parsed URL = %s", parsed)
	}
}

func TestReadProjectedTokenRejectsUnsafeTargets(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outsideDir := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(outsideDir, "token")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(root, "oversized")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", maxWorkloadTokenBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "absolute")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "outside", "token"), filepath.Join(root, "relative-outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "outside", "token"), filepath.Join(nested, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"absolute", "relative-outside", "directory", "dangling", "nested/escape", "oversized"} {
		t.Run(name, func(t *testing.T) {
			if _, err := readProjectedToken(filepath.Join(root, filepath.FromSlash(name))); err == nil {
				t.Fatal("expected rejection")
			}
		})
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
	_, err := testBootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "e", Workspace: t.TempDir(), WorkloadTokenFile: tokenFile})
	if err == nil || !strings.Contains(err.Error(), "decode bootstrap response") {
		t.Fatalf("err=%v", err)
	}
}

func TestDecodeBoundedJSONRejectsUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"code":0,"msg":"ok","data":{},"unexpected":true}`,
		`{"code":0,"msg":"ok","data":{"unexpected":true}}`,
		`{"code":0,"msg":"ok","data":{"files":[{"path":"a","text":"x","mode":420,"unexpected":true}]}}`,
	} {
		var envelope bootstrapEnvelope
		err := decodeBoundedJSON(strings.NewReader(raw), maxBootstrapResponse, &envelope)
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("raw=%s err=%v", raw, err)
		}
	}
}

func TestValidateBootstrapResponseRejectsInvalidRuntimeContracts(t *testing.T) {
	valid := func() BootstrapResponse {
		return BootstrapResponse{
			ExecutionToken: testExecutionToken(t, "execution-1", "task-1", "project-1"),
			TaskID:         "task-1", TaskType: "article", ProjectID: "project-1", Prompt: "write",
			Model: "sonnet", MaxTurns: 40, AgentFlag: "anban:article", AutoMemoryDirectory: ".claude/memory",
			ModelUsageAliases: testModelUsageAliases(),
			RuntimeEnv:        testClaudeRuntimeEnv(),
			ArtifactTransport: service.ArtifactTransport{Mode: ArtifactUploadDirect},
		}
	}
	tests := []struct {
		name   string
		mutate func(*BootstrapResponse)
	}{
		{"empty task type", func(r *BootstrapResponse) { r.TaskType = "" }},
		{"unknown task type", func(r *BootstrapResponse) { r.TaskType = "unknown"; r.AgentFlag = "anban:seednote" }},
		{"empty prompt", func(r *BootstrapResponse) { r.Prompt = "" }},
		{"zero max turns", func(r *BootstrapResponse) { r.MaxTurns = 0 }},
		{"excess max turns", func(r *BootstrapResponse) { r.MaxTurns = maxBootstrapTurns + 1 }},
		{"empty agent flag", func(r *BootstrapResponse) { r.AgentFlag = "" }},
		{"wrong agent flag", func(r *BootstrapResponse) { r.AgentFlag = "anban:seednote" }},
		{"wrong auto memory", func(r *BootstrapResponse) { r.AutoMemoryDirectory = ".claude/other" }},
		{"invalid resume session", func(r *BootstrapResponse) {
			r.ResumeSessionID = " invalid-session"
			r.ResumeContextPath = ".anban-creator/resume/executions/execution-1/latest.md"
		}},
		{"resume session without context", func(r *BootstrapResponse) { r.ResumeSessionID = "bba21f1d-70b8-4157-917b-f9802c2b1740" }},
		{"foreign resume context", func(r *BootstrapResponse) { r.ResumeContextPath = ".anban-creator/resume/executions/other/latest.md" }},
		{"long model", func(r *BootstrapResponse) { r.Model = strings.Repeat("m", maxBootstrapModelBytes+1) }},
		{"unknown runtime environment", func(r *BootstrapResponse) { r.RuntimeEnv = map[string]string{"PATH": "/tmp/bin"} }},
		{"empty runtime environment value", func(r *BootstrapResponse) { r.RuntimeEnv = map[string]string{"ANTHROPIC_AUTH_TOKEN": ""} }},
		{"oversized runtime environment value", func(r *BootstrapResponse) {
			r.RuntimeEnv = map[string]string{"ANTHROPIC_AUTH_TOKEN": strings.Repeat("x", 16<<10+1)}
		}},
		{"missing model usage aliases", func(r *BootstrapResponse) { r.ModelUsageAliases = nil }},
		{"invalid model usage alias", func(r *BootstrapResponse) {
			r.ModelUsageAliases = map[string]serveragent.ModelUsageIdentity{"raw": {Provider: "", Model: "sonnet"}}
		}},
		{"Montage environment on article task", func(r *BootstrapResponse) {
			r.Env = map[string]string{"NEW_PROVIDER_TOKEN": "future-secret"}
		}},
		{"too many files", func(r *BootstrapResponse) { r.Files = make([]BootstrapFile, maxBootstrapFiles+1) }},
		{"missing artifact transport", func(r *BootstrapResponse) { r.ArtifactTransport.Mode = "" }},
		{"unknown artifact transport", func(r *BootstrapResponse) { r.ArtifactTransport.Mode = "proxy" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := valid()
			tc.mutate(&response)
			if err := validateBootstrapResponse("execution-1", &response); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestValidateBootstrapResponseAcceptsArbitraryMontageEnv(t *testing.T) {
	response := BootstrapResponse{
		ExecutionToken:      testExecutionToken(t, "execution-1", "task-1", "project-1"),
		TaskID:              "task-1",
		TaskType:            "montage",
		ProjectID:           "project-1",
		Prompt:              "make a video",
		Model:               "sonnet",
		MaxTurns:            40,
		AgentFlag:           "anban:montage",
		AutoMemoryDirectory: ".claude/memory",
		ModelUsageAliases:   testModelUsageAliases(),
		RuntimeEnv:          testClaudeRuntimeEnv(),
		Env:                 map[string]string{"NEW_PROVIDER_TOKEN": "future-secret"},
		ArtifactTransport:   service.ArtifactTransport{Mode: ArtifactUploadStream},
	}
	if err := validateBootstrapResponse("execution-1", &response); err != nil {
		t.Fatalf("validate bootstrap response: %v", err)
	}
}

func TestValidateBootstrapIdentityRejectsOversizedOrNonCompactJWT(t *testing.T) {
	valid := BootstrapResponse{ExecutionToken: testExecutionToken(t, "execution-1", "task-1", "project-1"), TaskID: "task-1", ProjectID: "project-1"}
	for _, token := range []string{
		strings.Repeat("a", maxExecutionTokenBytes+1),
		"header.payload.signature.extra",
		"header.pay+load.signature",
		".payload.signature",
	} {
		response := valid
		response.ExecutionToken = token
		if err := validateBootstrapIdentity("execution-1", &response); err == nil {
			t.Fatalf("token accepted: %.32q", token)
		}
	}
}

func TestWindowsBootstrapModeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		desired, actual os.FileMode
		want            bool
	}{
		{desired: 0o600, actual: 0o666, want: true},
		{desired: 0o644, actual: 0o666, want: true},
		{desired: 0o444, actual: 0o444, want: true},
		{desired: 0o444, actual: 0o666, want: false},
		{desired: 0o644, actual: 0o444, want: false},
	} {
		if got := windowsBootstrapModeCompatible(tc.desired, tc.actual); got != tc.want {
			t.Fatalf("mode(%#o,%#o)=%v want %v", tc.desired, tc.actual, got, tc.want)
		}
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
	_, err := testBootstrapJob(context.Background(), JobConfig{ServerURL: server.URL, ExecutionID: "e", Workspace: t.TempDir(), WorkloadTokenFile: tokenFile})
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
		{"outer whitespace", []BootstrapFile{{Path: " output.txt", Text: "x", Mode: 0o644}}},
		{"portable absolute", []BootstrapFile{{Path: "C:/secret", Text: "x", Mode: 0o644}}},
		{"duplicate", []BootstrapFile{{Path: "a", Text: "x", Mode: 0o644}, {Path: "a", Text: "y", Mode: 0o644}}},
		{"portable collision", []BootstrapFile{{Path: "A.txt", Text: "x", Mode: 0o644}, {Path: "a.txt", Text: "y", Mode: 0o644}}},
		{"file directory conflict", []BootstrapFile{{Path: "input", Text: "x", Mode: 0o644}, {Path: "input/file.txt", Text: "y", Mode: 0o644}}},
		{"unicode normalization collision", []BootstrapFile{{Path: "caf\u00e9.txt", Text: "x", Mode: 0o644}, {Path: "cafe\u0301.txt", Text: "y", Mode: 0o644}}},
		{"unicode fold collision", []BootstrapFile{{Path: "Stra\u00dfe.txt", Text: "x", Mode: 0o644}, {Path: "STRASSE.txt", Text: "y", Mode: 0o644}}},
		{"reserved component", []BootstrapFile{{Path: "input/CON.txt", Text: "x", Mode: 0o644}}},
		{"trailing dot", []BootstrapFile{{Path: "input/file.", Text: "x", Mode: 0o644}}},
		{"overlong component", []BootstrapFile{{Path: strings.Repeat("a", 256), Text: "x", Mode: 0o644}}},
		{"protected memory exact", []BootstrapFile{{Path: ".claude/memory", Text: "x", Mode: 0o644}}},
		{"protected memory backslashes", []BootstrapFile{{Path: `.claude\memory\x`, Text: "x", Mode: 0o644}}},
		{"both sources", []BootstrapFile{{Path: "a", Text: "x", DownloadURL: "https://example.com/a", Mode: 0o644}}},
		{"unsafe mode", []BootstrapFile{{Path: "a", Text: "x", Mode: 0o777}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := materializeBootstrap(context.Background(), root, tc.files, nil); err == nil {
				t.Fatal("expected rejection")
			}
			if tc.name == "file directory conflict" {
				if _, err := os.Stat(filepath.Join(root, "input")); !os.IsNotExist(err) {
					t.Fatalf("preflight left partial path: %v", err)
				}
			}
		})
	}
}

func TestMaterializeBootstrapPreflightsBeforeWritingAndProtectsMemory(t *testing.T) {
	root := t.TempDir()
	memory := filepath.Join(root, ".claude", "memory")
	if err := os.MkdirAll(memory, 0o755); err != nil {
		t.Fatal(err)
	}
	remembered := filepath.Join(memory, "MEMORY.md")
	if err := os.WriteFile(remembered, []byte("preserved"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := []BootstrapFile{
		{Path: ".claude/settings.json", Text: "{}", Mode: 0o644},
		{Path: ".claude/memory/overwrite", Text: "bad", Mode: 0o644},
	}
	if err := materializeBootstrap(context.Background(), root, files, nil); err == nil {
		t.Fatal("expected memory path rejection")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("preflight left partial file: %v", err)
	}
	got, err := os.ReadFile(remembered)
	if err != nil || string(got) != "preserved" {
		t.Fatalf("memory changed: %q err=%v", got, err)
	}
	if err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: ".claude/settings.json", Text: "{}", Mode: 0o644}}, nil); err != nil {
		t.Fatalf("legitimate .claude file: %v", err)
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

func TestMaterializeBootstrapRetryAcceptsOnlyExactExistingFiles(t *testing.T) {
	root := t.TempDir()
	files := []BootstrapFile{{Path: "settings.json", Text: "same", Mode: 0o644}}
	if err := materializeBootstrap(context.Background(), root, files, nil); err != nil {
		t.Fatal(err)
	}
	if err := materializeBootstrap(context.Background(), root, files, nil); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	if err := materializeBootstrap(context.Background(), root, []BootstrapFile{
		{Path: "new.txt", Text: "new", Mode: 0o644},
		{Path: "settings.json", Text: "different", Mode: 0o644},
	}, nil); err == nil {
		t.Fatal("expected conflicting retry rejection")
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("conflict left partial commit: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "settings.json"))
	if string(got) != "same" {
		t.Fatalf("existing file changed: %q", got)
	}
}

func TestMaterializeBootstrapRejectsExistingSymlinkParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "link/file", Text: "x", Mode: 0o644}}, nil)
	if err == nil {
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
	client := bootstrapLoopbackTestDownloadClient()
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

func TestMaterializeBootstrapEnforcesPerFileExpectedSizeAndLimit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		body         []byte
		expectedSize int64
		maxBytes     int64
		wantErr      bool
	}{
		{name: "exact size", body: []byte("image"), expectedSize: 5, maxBytes: 10 << 20},
		{name: "short", body: []byte("four"), expectedSize: 5, maxBytes: 10 << 20, wantErr: true},
		{name: "long", body: []byte("sixsix"), expectedSize: 5, maxBytes: 10 << 20, wantErr: true},
		{name: "over reference limit", body: make([]byte, (10<<20)+1), expectedSize: (10 << 20) + 1, maxBytes: 10 << 20, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()
			root := t.TempDir()
			file := BootstrapFile{Path: ".anban-creator/reference.png", DownloadURL: server.URL, Mode: 0o644, ExpectedSize: tc.expectedSize, MaxBytes: tc.maxBytes}
			err := materializeBootstrap(context.Background(), root, []BootstrapFile{file}, bootstrapLoopbackTestDownloadClient())
			if (err != nil) != tc.wantErr {
				t.Fatalf("materializeBootstrap error = %v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				if _, statErr := os.Stat(filepath.Join(root, ".anban-creator", "reference.png")); !os.IsNotExist(statErr) {
					t.Fatalf("rejected file was committed: %v", statErr)
				}
			}
		})
	}
}

func TestPreflightBootstrapFilesRejectsInvalidPerFileLimits(t *testing.T) {
	for _, file := range []BootstrapFile{
		{Path: "input.bin", Text: "x", Mode: 0o644, ExpectedSize: -1},
		{Path: "input.bin", Text: "x", Mode: 0o644, MaxBytes: -1},
		{Path: "input.bin", Text: "x", Mode: 0o644, ExpectedSize: 2, MaxBytes: 1},
		{Path: "input.bin", Text: "x", Mode: 0o644, MaxBytes: (64 << 20) + 1},
	} {
		if _, err := preflightBootstrapFiles([]BootstrapFile{file}, false); err == nil {
			t.Fatalf("invalid limits accepted: %#v", file)
		}
	}
}

func TestMaterializeBootstrapRejectsDownloadRedirects(t *testing.T) {
	for _, crossOrigin := range []bool{false, true} {
		t.Run(map[bool]string{false: "same origin", true: "cross origin"}[crossOrigin], func(t *testing.T) {
			targetHits := 0
			target := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				targetHits++
				if r.Header.Get("Authorization") != "" {
					t.Fatal("redirect target received authorization")
				}
			}))
			defer target.Close()
			sameOriginTargetHits := 0
			var source *httptest.Server
			source = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/target" {
					sameOriginTargetHits++
					return
				}
				if r.Header.Get("Authorization") != "" {
					t.Fatal("download received authorization")
				}
				location := source.URL + "/target"
				if crossOrigin {
					location = target.URL + "/target"
				}
				http.Redirect(w, r, location, http.StatusTemporaryRedirect)
			}))
			defer source.Close()
			client := bootstrapDownloadClient()
			client.Transport = loopbackTestTransport{base: source.Client().Transport}
			root := t.TempDir()
			err := materializeBootstrap(context.Background(), root, []BootstrapFile{{Path: "input.bin", DownloadURL: source.URL + "/input", Mode: 0o644}}, client)
			if err == nil || targetHits != 0 || sameOriginTargetHits != 0 {
				t.Fatalf("err=%v targetHits=%d sameOriginTargetHits=%d", err, targetHits, sameOriginTargetHits)
			}
			if _, err := os.Stat(filepath.Join(root, "input.bin")); !os.IsNotExist(err) {
				t.Fatalf("redirect wrote output: %v", err)
			}
		})
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

func testBootstrapJob(ctx context.Context, cfg JobConfig) (*BootstrapResponse, error) {
	return bootstrapJobWithPolicy(ctx, cfg, bootstrapRequestPolicy{allowHTTPServer: true})
}
