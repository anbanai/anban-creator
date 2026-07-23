package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func TestManagedTaskStopHookRunsTaskGateForMainAgent(t *testing.T) {
	tests := []struct {
		name      string
		taskType  string
		agentType string
		script    string
	}{
		{name: "seednote", taskType: "seednote", agentType: "anban:seednote", script: "seednote-quality-gate.sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			pluginRoot := t.TempDir()
			hooksDir := filepath.Join(pluginRoot, "hooks")
			if err := os.MkdirAll(hooksDir, 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(hooksDir, tt.script)
			body := fmt.Sprintf(`#!/bin/sh
input=$(cat)
test "$CLAUDE_PROJECT_DIR" = %q || exit 3
case "$input" in
  *'"agent_type":"%s"'*'"managed_main_session":true'*) printf '%%s' '{"decision":"block","reason":"gate-ran"}' ;;
  *) exit 2 ;;
esac
`, workspace, tt.agentType)
			if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}

			option, err := ManagedTaskStopHook(tt.taskType, workspace, pluginRoot)
			if err != nil {
				t.Fatalf("ManagedTaskStopHook: %v", err)
			}
			opts := claudecode.NewOptions(option)
			hooks, ok := opts.Hooks.(map[claudecode.HookEvent][]claudecode.HookMatcher)
			if !ok || len(hooks[claudecode.HookEventStop]) != 1 {
				t.Fatalf("hooks = %#v", opts.Hooks)
			}
			callback := hooks[claudecode.HookEventStop][0].Hooks[0]
			result, err := callback(context.Background(), &claudecode.StopHookInput{}, nil, claudecode.HookContext{})
			if err != nil {
				t.Fatalf("callback: %v", err)
			}
			if result.Decision == nil || *result.Decision != "block" || result.Reason == nil || *result.Reason != "gate-ran" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestManagedTaskStopHookSkipsTaskTypesWithoutGate(t *testing.T) {
	option, err := ManagedTaskStopHook("article", t.TempDir(), "")
	if err != nil {
		t.Fatalf("ManagedTaskStopHook: %v", err)
	}
	if got := claudecode.NewOptions(option).Hooks; got != nil {
		t.Fatalf("hooks = %#v, want nil", got)
	}
}

func TestSeednoteQualityGateAcceptsAcceptedContentQuality(t *testing.T) {
	workspace := t.TempDir()
	seednoteDir := writeSeednoteGateFixture(t, workspace, true)
	output := runSeednoteQualityGate(t, workspace)
	if strings.TrimSpace(output) != "" {
		t.Fatalf("quality gate blocked accepted content quality in %s: %s", seednoteDir, output)
	}
}

func TestSeednoteQualityGateBlocksRejectedContentQuality(t *testing.T) {
	workspace := t.TempDir()
	writeSeednoteGateFixture(t, workspace, false)
	output := runSeednoteQualityGate(t, workspace)
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("parse quality gate output %q: %v", output, err)
	}
	if result["decision"] != "block" || !strings.Contains(result["reason"].(string), "quality_status=rejected") {
		t.Fatalf("quality gate output = %#v, want rejected content quality block", result)
	}
}

func TestSeednoteQualityGateBlocksMissingRuntimeWorkspaceInjection(t *testing.T) {
	workspace := t.TempDir()
	writeSeednoteGateFixture(t, workspace, true)

	tests := []struct {
		name         string
		workspaceEnv []string
	}{
		{name: "missing"},
		{name: "empty", workspaceEnv: []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := runSeednoteQualityGateInvocation(
				t,
				workspace,
				`{"agent_type":"anban:seednote","managed_main_session":true}`,
				tt.workspaceEnv...,
			)
			var result map[string]any
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("parse quality gate output %q: %v", output, err)
			}
			reason, _ := result["reason"].(string)
			if result["decision"] != "block" ||
				!strings.Contains(reason, "missing runtime workspace injection") ||
				!strings.Contains(reason, "CLAUDE_PROJECT_DIR") {
				t.Fatalf("quality gate output = %#v, want missing runtime workspace injection block", result)
			}
		})
	}
}

func TestSeednoteQualityGateSkipsNonSeednoteWithoutRuntimeWorkspaceInjection(t *testing.T) {
	output := runSeednoteQualityGateInvocation(
		t,
		t.TempDir(),
		`{"agent_type":"anban:article","managed_main_session":true}`,
	)
	if strings.TrimSpace(output) != "" {
		t.Fatalf("quality gate returned output for non-Seednote invocation: %s", output)
	}
}

func TestSeednoteQualityGateDoesNotFallBackToProcessWorkingDirectory(t *testing.T) {
	hook := readRepoFile(t, filepath.Join(repoRoot(t), "plugins", "hooks", "seednote-quality-gate.sh"))
	for _, forbidden := range []string{"$PWD", "os.getcwd()", "CLAUDE_PROJECT_DIR:-"} {
		if strings.Contains(hook, forbidden) {
			t.Errorf("seednote quality gate contains forbidden CWD fallback %q", forbidden)
		}
	}
}

func TestSeednoteQualityGateBlocksMissingPlannedContentImages(t *testing.T) {
	workspace := t.TempDir()
	seednoteDir := writeSeednoteGateFixture(t, workspace, true)
	if err := os.WriteFile(filepath.Join(seednoteDir, "image-plan.md"), []byte("计划图片数量: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output := runSeednoteQualityGate(t, workspace)
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("parse quality gate output %q: %v", output, err)
	}
	reason, _ := result["reason"].(string)
	if result["decision"] != "block" || !strings.Contains(reason, "当前 1 张，应等于 image-plan.md 声明的 4 张") {
		t.Fatalf("quality gate output = %#v, want missing planned image block", result)
	}
}

func TestSeednoteQualityGateAcceptsRecoverableFailure(t *testing.T) {
	workspace := t.TempDir()
	failure := map[string]any{
		"status":      "recoverable_failure",
		"stage":       "visual_generation",
		"error_code":  "image_provider_unavailable",
		"message":     "image provider is unavailable",
		"resume_from": "generate_images",
	}
	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(workspace, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "failure-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if output := runSeednoteQualityGate(t, workspace); strings.TrimSpace(output) != "" {
		t.Fatalf("quality gate blocked a valid recoverable failure: %s", output)
	}
}

func TestSeednoteQualityGateRejectsLegacyNestedFailureState(t *testing.T) {
	workspace := t.TempDir()
	failure := map[string]any{
		"status":      "recoverable_failure",
		"stage":       "visual_generation",
		"error_code":  "image_provider_unavailable",
		"message":     "image provider is unavailable",
		"resume_from": "generate_images",
	}
	data, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	legacyDir := filepath.Join(workspace, "output", "seednote", "title")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "failure-state.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	output := runSeednoteQualityGate(t, workspace)
	if strings.TrimSpace(output) == "" {
		t.Fatal("quality gate accepted a failure state outside canonical output/")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("parse quality gate output %q: %v", output, err)
	}
	reason, _ := result["reason"].(string)
	if result["decision"] != "block" || !strings.Contains(reason, "content.md（缺少最终正文）") {
		t.Fatalf("quality gate output = %#v, want missing canonical output block", result)
	}
}

func TestSeednoteQualityGateBlocksMalformedRecoverableFailure(t *testing.T) {
	workspace := t.TempDir()
	outputDir := filepath.Join(workspace, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(outputDir, "failure-state.json"),
		[]byte(`{"status":"recoverable_failure","stage":"visual_generation"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output := runSeednoteQualityGate(t, workspace)
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("parse quality gate output %q: %v", output, err)
	}
	reason, _ := result["reason"].(string)
	if result["decision"] != "block" || !strings.Contains(reason, "失败态文件不完整") {
		t.Fatalf("quality gate output = %#v, want malformed failure-state block", result)
	}
}

func TestSeednoteQualityGateAcceptsCanonicalManagedOutput(t *testing.T) {
	workspace := t.TempDir()
	writeSeednoteGateFixture(t, workspace, true)
	output := runSeednoteQualityGate(t, workspace)
	if strings.TrimSpace(output) != "" {
		t.Fatalf("quality gate blocked canonical managed output: %s", output)
	}
}

func TestSeednoteQualityGateAcceptsCanonicalModeImageSets(t *testing.T) {
	tests := []struct {
		name   string
		images []string
	}{
		{name: "cover_only", images: []string{"cover.png"}},
		{name: "cover_tail", images: []string{"cover.png", "tail.png"}},
		{name: "cover_content_one", images: []string{"cover.png", "image_01.png"}},
		{name: "cover_content_two", images: []string{"cover.png", "image_01.png", "image_02.png"}},
		{name: "full_three", images: []string{"cover.png", "image_01.png", "image_02.png", "image_03.png", "tail.png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeSeednoteGateImageSet(t, workspace, tt.images)
			if output := runSeednoteQualityGate(t, workspace); strings.TrimSpace(output) != "" {
				t.Fatalf("quality gate blocked canonical %s image set: %s", tt.name, output)
			}
		})
	}
}

func TestSeednoteQualityGateRejectsInvalidContentImageNames(t *testing.T) {
	tests := []struct {
		name       string
		images     []string
		wantReason string
	}{
		{name: "arbitrary suffix", images: []string{"cover.png", "image_bad.png"}, wantReason: "非规范内容图文件名"},
		{name: "non-padded index", images: []string{"cover.png", "image_1.png"}, wantReason: "非规范内容图文件名"},
		{name: "index above maximum", images: []string{"cover.png", "image_04.png"}, wantReason: "非规范内容图文件名"},
		{name: "starts at second image", images: []string{"cover.png", "image_02.png"}, wantReason: "内容图编号必须从 image_01.png 开始连续"},
		{name: "gap in sequence", images: []string{"cover.png", "image_01.png", "image_03.png"}, wantReason: "内容图编号必须从 image_01.png 开始连续"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeSeednoteGateImageSet(t, workspace, tt.images)
			output := runSeednoteQualityGate(t, workspace)
			if strings.TrimSpace(output) == "" {
				t.Fatalf("quality gate accepted invalid image set: %v", tt.images)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("parse quality gate output %q: %v", output, err)
			}
			reason, _ := result["reason"].(string)
			if result["decision"] != "block" || !strings.Contains(reason, tt.wantReason) {
				t.Fatalf("quality gate output = %#v, want block containing %q", result, tt.wantReason)
			}
		})
	}
}

func TestSeednoteArchiveScriptIsRemoved(t *testing.T) {
	script := filepath.Join(repoRoot(t), "plugins", "scripts", "archive-seednote-workspace.sh")
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatalf("archive script still exists or could not be checked: %v", err)
	}
}

func TestSeednoteQualityGateStaysMirroredForClaudeAndCodex(t *testing.T) {
	root := repoRoot(t)
	claudeHook := readRepoFile(t, filepath.Join(root, "plugins", "hooks", "seednote-quality-gate.sh"))
	codexHook := readRepoFile(t, filepath.Join(root, "plugins", "hooks", "seednote-quality-gate.sh"))
	if codexHook != claudeHook {
		t.Fatal("Claude and Codex seednote quality gates must stay byte-identical")
	}
}

func writeSeednoteGateFixture(t *testing.T, workspace string, passed bool) string {
	t.Helper()
	dir := filepath.Join(workspace, "output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"content.md",
		"request-analysis.json",
		"request-analysis.md",
		"reference-analysis.json",
		"reference-analysis.md",
		"image-prompts.md",
		"image-review.md",
		"cover.png",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "image-plan.md"), []byte("计划图片数量: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	qualityStatus := "rejected"
	if passed {
		qualityStatus = "accepted"
	}
	summary, err := json.Marshal(map[string]any{
		"version": "1.0",
		"outputs": []any{map[string]any{
			"file_name":      "cover.png",
			"quality_status": qualityStatus,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reference-usage-summary.json"), summary, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeSeednoteGateImageSet(t *testing.T, workspace string, images []string) string {
	t.Helper()
	dir := writeSeednoteGateFixture(t, workspace, true)
	for _, name := range images {
		if name == "cover.png" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(dir, "image-plan.md"),
		[]byte(fmt.Sprintf("计划图片数量: %d\n", len(images))),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	outputs := make([]map[string]any, 0, len(images))
	for _, name := range images {
		outputs = append(outputs, map[string]any{
			"file_name":      name,
			"quality_status": "accepted",
		})
	}
	summary, err := json.Marshal(map[string]any{
		"version": "1.0",
		"outputs": outputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reference-usage-summary.json"), summary, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runSeednoteQualityGate(t *testing.T, workspace string) string {
	t.Helper()
	return runSeednoteQualityGateInvocation(
		t,
		workspace,
		`{"agent_type":"anban:seednote","managed_main_session":true}`,
		workspace,
	)
}

func runSeednoteQualityGateInvocation(t *testing.T, workspace, input string, workspaceEnv ...string) string {
	t.Helper()
	script := filepath.Join(repoRoot(t), "plugins", "hooks", "seednote-quality-gate.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = workspace
	cmd.Env = make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CLAUDE_PROJECT_DIR=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if len(workspaceEnv) > 0 {
		cmd.Env = append(cmd.Env, "CLAUDE_PROJECT_DIR="+workspaceEnv[0])
	}
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run seednote quality gate: %v\nstdout: %s\nstderr: %s", err, output, stderr.String())
	}
	return string(output)
}
