package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/agentpack"
	claudecode "github.com/severity1/claude-agent-sdk-go"
)

type messageIteratorStub struct {
	messages []claudecode.Message
	index    int
}

func (i *messageIteratorStub) Next(context.Context) (claudecode.Message, error) {
	if i.index >= len(i.messages) {
		return nil, claudecode.ErrNoMoreMessages
	}
	message := i.messages[i.index]
	i.index++
	return message, nil
}

func (i *messageIteratorStub) Close() error { return nil }

func TestRunnerConsumesToolResultsFromSDKUserMessages(t *testing.T) {
	workspace := t.TempDir()
	server := imageArtifactServer(t, testPNG, "image/png", nil)
	defer server.Close()
	path := "output/cover.png"
	iterator := &messageIteratorStub{messages: []claudecode.Message{
		&claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
			&claudecode.ToolUseBlock{ToolUseID: "tool-1", Name: "mcp__plugin_anban_creator__generate_image", Input: map[string]any{"output_path": path}},
		}},
		&claudecode.UserMessage{Content: []claudecode.ContentBlock{
			&claudecode.ToolResultBlock{ToolUseID: "tool-1", Content: artifactToolResult(t, path, server.URL+"/image", testPNG, "image/png")},
		}},
	}}
	runner := NewRunner(&Config{Workspace: workspace, TaskType: "seednote"}, nil, NewDownloader(&Config{Workspace: workspace, TaskType: "seednote", ServerURL: server.URL}))
	state := newRunnerStreamState()
	err := runner.consumeResponse(context.Background(), iterator, &serveragent.ExecutionResult{}, state)
	if err == nil || !strings.Contains(err.Error(), "ended without a result message") {
		t.Fatalf("consumeResponse terminal error = %v, want post-stream result validation", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "output", "cover.png")); err != nil {
		t.Fatalf("SDK UserMessage tool result was not materialized: %v", err)
	}
	if len(state.toolCalls) != 0 {
		t.Fatalf("resolved tool calls retained: %#v", state.toolCalls)
	}
}

func TestRunnerStopsBeforeNextModelTurnWhenMaterializationFails(t *testing.T) {
	workspace := t.TempDir()
	path := "output/cover.png"
	badPayload := downloadPayload{
		TaskFileID: "file-1", FilePath: path, DownloadURL: "https://example.invalid/image.png",
		MimeType: "image/png", FileSize: int64(len(testPNG)), ContentHash: strings.Repeat("0", 64),
	}
	data, err := json.Marshal(badPayload)
	if err != nil {
		t.Fatal(err)
	}
	iterator := &messageIteratorStub{messages: []claudecode.Message{
		&claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
			&claudecode.ToolUseBlock{ToolUseID: "tool-1", Name: "generate_image", Input: map[string]any{"output_path": path}},
		}},
		&claudecode.UserMessage{Content: []claudecode.ContentBlock{
			&claudecode.ToolResultBlock{ToolUseID: "tool-1", Content: string(data)},
		}},
		&claudecode.AssistantMessage{Content: []claudecode.ContentBlock{
			&claudecode.TextBlock{Text: "must not be consumed"},
		}},
	}}
	downloadError := errors.New("injected download failure")
	downloader := NewDownloader(&Config{Workspace: workspace, TaskType: "seednote"})
	downloader.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, downloadError })
	runner := NewRunner(&Config{Workspace: workspace, TaskType: "seednote"}, nil, downloader)
	state := newRunnerStreamState()
	err = runner.consumeResponse(context.Background(), iterator, &serveragent.ExecutionResult{}, state)
	if err == nil || !strings.Contains(err.Error(), runtimeArtifactMaterializationFailureCode) {
		t.Fatalf("consumeResponse error = %v", err)
	}
	if iterator.index != 2 {
		t.Fatalf("runner consumed %d messages, want exactly assistant tool use + user tool result", iterator.index)
	}
	if state.turnNum != 1 || strings.Contains(state.resultText, "must not be consumed") {
		t.Fatalf("state after materialization failure = %#v", state)
	}
}

func TestRunnerUsesManagedAgentRuntimePolicy(t *testing.T) {
	data, err := os.ReadFile("runner.go")
	if err != nil {
		t.Fatalf("read runner.go: %v", err)
	}
	if !strings.Contains(string(data), "serveragent.WithManagedAgentRuntimePolicy()") {
		t.Fatal("runner must apply serveragent.WithManagedAgentRuntimePolicy()")
	}
}

func TestRunnerReconcilesOnlyTransportedExactModelAliases(t *testing.T) {
	runner := NewRunner(&Config{ModelUsageAliases: map[string]serveragent.ModelUsageIdentity{
		"doubao-seed-evolving-latest-version": {Provider: "volcengine_ark", Model: "doubao-seed-evolving"},
	}}, nil, nil)
	result := &serveragent.ExecutionResult{}
	runner.populateTerminalModelUsage(result, &claudecode.ResultMessage{ModelUsage: map[string]claudecode.ModelUsage{
		"doubao-seed-evolving-latest-version": {InputTokens: 7},
		"unknown-model":                       {OutputTokens: 3},
	}})
	if len(result.ModelUsage) != 2 || result.ModelUsage[1].Provider != "volcengine_ark" || result.CostStatus != serveragent.CostStatusUnreconciled {
		t.Fatalf("terminal usage = %+v status=%q diagnostics=%+v", result.ModelUsage, result.CostStatus, result.CostDiagnostics)
	}
}

func TestRuntimeCwd(t *testing.T) {
	workspace := "/workspace"
	if got := runtimeCwd(workspace, agentpack.AdapterOpenMontage); got != "/workspace/openmontage" {
		t.Fatalf("Montage cwd = %q", got)
	}
	if got := runtimeCwd(workspace, agentpack.AdapterStandard); got != workspace {
		t.Fatalf("Seednote cwd = %q, want %q", got, workspace)
	}
	if got := runtimeCwd(workspace, ""); got != workspace {
		t.Fatalf("Live Slicer cwd = %q, want %q", got, workspace)
	}
	if got := montageRuntimePath(workspace); got != "/workspace/openmontage" {
		t.Fatalf("Montage runtime path = %q", got)
	}
}

func TestRunnerOptionsUseWritableMontageRoot(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/anbanai")
	workspace := t.TempDir()
	runner := NewRunner(&Config{
		Workspace: workspace, AgentFlag: "anban:montage", TaskType: "montage", RuntimeAdapter: agentpack.AdapterOpenMontage, MaxTurns: 10,
		Env: map[string]string{serveragent.MontageSubmoduleEnvName: "/provider/override"},
	}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := claudecode.NewOptions(opts...)
	wantRoot := filepath.Join(workspace, "openmontage")
	if got.Cwd == nil || *got.Cwd != wantRoot {
		t.Fatalf("Montage cwd = %#v, want %q", got.Cwd, wantRoot)
	}
	if got.ExtraEnv[serveragent.MontageSubmoduleEnvName] != wantRoot {
		t.Fatalf("Montage runtime env = %#v, want platform-owned %q", got.ExtraEnv, wantRoot)
	}

	content := NewRunner(&Config{Workspace: workspace, AgentFlag: "anban:seednote", TaskType: "seednote", RuntimeAdapter: agentpack.AdapterStandard, MaxTurns: 10}, nil, nil)
	contentOpts, err := content.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	contentGot := claudecode.NewOptions(contentOpts...)
	if contentGot.Cwd == nil || *contentGot.Cwd != workspace {
		t.Fatalf("content cwd = %#v, want %q", contentGot.Cwd, workspace)
	}
	if _, exists := contentGot.ExtraEnv[serveragent.MontageSubmoduleEnvName]; exists {
		t.Fatalf("content runtime env includes Montage path: %#v", contentGot.ExtraEnv)
	}
}

func TestRunnerOptionsLoadImmutablePluginRootAndKeepAgentFlag(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/anbanai")
	runner := NewRunner(&Config{Workspace: t.TempDir(), AgentFlag: "anban:seednote", MaxTurns: 10}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...)
	if len(got.Plugins) != 1 || got.Plugins[0].Type != claudecode.SdkPluginTypeLocal || got.Plugins[0].Path != "/anbanai" {
		t.Fatalf("plugins = %#v, want local /anbanai", got.Plugins)
	}
	if got.ExtraArgs["agent"] == nil || *got.ExtraArgs["agent"] != "anban:seednote" {
		t.Fatalf("agent extra arg = %#v, want existing AgentFlag", got.ExtraArgs["agent"])
	}
}

func TestRunnerOptionsLeaveLocalPluginUnsetWithoutEnvironment(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	runner := NewRunner(&Config{Workspace: t.TempDir(), AgentFlag: "anban:article", MaxTurns: 10}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	if got := claudecode.NewOptions(opts...).Plugins; len(got) != 0 {
		t.Fatalf("plugins = %#v, want none without CLAUDE_PLUGIN_ROOT", got)
	}
}

func TestRunnerOptionsResumeExactBootstrapSession(t *testing.T) {
	const sessionID = "bba21f1d-70b8-4157-917b-f9802c2b1740"
	runner := NewRunner(&Config{Workspace: t.TempDir(), AgentFlag: "anban:article", MaxTurns: 10, ResumeSessionID: sessionID}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...)
	if got.Resume == nil || *got.Resume != sessionID {
		t.Fatalf("resume = %#v, want %q", got.Resume, sessionID)
	}
}

func TestRunnerOptionsInjectBootstrapClaudeEnvironmentWithoutOverridingExecutionIdentity(t *testing.T) {
	runner := NewRunner(&Config{
		Workspace: t.TempDir(), AgentFlag: "anban:article", MaxTurns: 10,
		ServerURL: "https://server.example.com", APIKey: "execution-jwt",
		RuntimeEnv: map[string]string{
			"ANTHROPIC_AUTH_TOKEN": "runtime-token",
			"ANTHROPIC_BASE_URL":   "https://anthropic.example.com",
			"ANBAN_API_KEY":        "must-not-override",
		},
	}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...).ExtraEnv
	if got["ANTHROPIC_AUTH_TOKEN"] != "runtime-token" || got["ANTHROPIC_BASE_URL"] != "https://anthropic.example.com" {
		t.Fatalf("Claude runtime environment = %#v", got)
	}
	for _, key := range []string{"ANBAN_API_KEY", "ANBAN_API_URL"} {
		if _, exposed := got[key]; exposed {
			t.Fatalf("managed Agent environment exposes %s: %#v", key, got)
		}
	}
	if got["ANBAN_DEFAULT_PROJECT"] != "" {
		t.Fatalf("unexpected project environment = %#v", got)
	}
}

func TestRunnerOptionsRemoveInheritedClaudeEnvironmentBeforeFrozenInjection(t *testing.T) {
	runner := NewRunner(&Config{
		Workspace: t.TempDir(), AgentFlag: "anban:article", MaxTurns: 10,
		RuntimeEnv: map[string]string{"ANTHROPIC_MODEL": "frozen-model"},
	}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...)
	want := append(serveragent.ClaudeEnvironmentKeysToUnset(), "ANBAN_API_KEY", "ANBAN_API_URL")
	if len(got.UnsetEnv) != len(want) {
		t.Fatalf("unset Claude environment = %#v, want %#v", got.UnsetEnv, want)
	}
	for i := range want {
		if got.UnsetEnv[i] != want[i] {
			t.Fatalf("unset Claude environment = %#v, want %#v", got.UnsetEnv, want)
		}
	}
	for _, key := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "ANBAN_API_KEY", "ANBAN_API_URL"} {
		if !slices.Contains(got.UnsetEnv, key) {
			t.Fatalf("inherited Claude routing key %q was not unset: %#v", key, got.UnsetEnv)
		}
	}
	if got.ExtraEnv["ANTHROPIC_MODEL"] != "frozen-model" {
		t.Fatalf("frozen Claude environment = %#v", got.ExtraEnv)
	}
}

func TestRunnerOptionsApplyFrozenClaudeControlsFromRuntimeEnvironment(t *testing.T) {
	runner := NewRunner(&Config{
		Workspace: t.TempDir(), AgentFlag: "anban:article", MaxTurns: 10,
		RuntimeEnv: map[string]string{
			"ANTHROPIC_MODEL": "k3", "CLAUDE_CODE_EFFORT_LEVEL": "high", "MAX_THINKING_TOKENS": "0", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "0", "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "262144", "ENABLE_TOOL_SEARCH": "false",
		},
	}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...)
	if got.Model != nil || got.Effort != nil || got.ExtraArgs["thinking"] != nil {
		t.Fatalf("legacy SDK controls = model %#v, effort %#v, thinking %#v", got.Model, got.Effort, got.ExtraArgs["thinking"])
	}
	if got.ExtraEnv["CLAUDE_CODE_EFFORT_LEVEL"] != "high" || got.ExtraEnv["MAX_THINKING_TOKENS"] != "0" || got.ExtraEnv["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] != "1" || got.ExtraEnv["CLAUDE_CODE_DISABLE_AUTO_MEMORY"] != "0" || got.ExtraEnv["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "262144" || got.ExtraEnv["ENABLE_TOOL_SEARCH"] != "false" {
		t.Fatalf("frozen Claude control environment = %#v", got.ExtraEnv)
	}
}

func TestRunnerOptionsInjectExplicitMontageEnv(t *testing.T) {
	runner := NewRunner(&Config{
		Workspace: t.TempDir(), AgentFlag: "anban:montage", TaskType: "montage", RuntimeAdapter: agentpack.AdapterOpenMontage, MaxTurns: 10,
		ServerURL: "https://server.example.com", APIKey: "execution-jwt", ProjectID: "project-1",
		RuntimeEnv: map[string]string{"ANTHROPIC_AUTH_TOKEN": "runtime-token"},
		Env: map[string]string{
			"NEW_PROVIDER_TOKEN":           "future-secret",
			"ANBAN_API_KEY":                "montage-override",
			"ANBAN_API_URL":                "https://montage.invalid",
			"ANBAN_DEFAULT_PROJECT":        "montage-project",
			"ANTHROPIC_AUTH_TOKEN":         "montage-token",
			"CLAUDE_CODE_DISABLE_THINKING": "true",
		},
	}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...).ExtraEnv
	if got["NEW_PROVIDER_TOKEN"] != "future-secret" {
		t.Fatalf("Montage env = %#v, want future provider key", got)
	}
	for _, key := range []string{"ANBAN_API_KEY", "ANBAN_API_URL"} {
		if _, exposed := got[key]; exposed {
			t.Fatalf("Montage Agent environment exposes %s: %#v", key, got)
		}
	}
	if got["ANBAN_DEFAULT_PROJECT"] != "project-1" {
		t.Fatalf("Montage env overrode project identity: %#v", got)
	}
	if got["ANTHROPIC_AUTH_TOKEN"] != "runtime-token" {
		t.Fatalf("Montage env overrode managed Claude runtime: %#v", got)
	}
	if _, exists := got["CLAUDE_CODE_DISABLE_THINKING"]; exists {
		t.Fatalf("Montage env injected an unfrozen Claude control: %#v", got)
	}
}

func TestRunnerOptionsInjectManagedMCPAndProjectIdentity(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/anbanai")
	runner := NewRunner(&Config{
		Workspace: t.TempDir(), AgentFlag: "anban:seednote", MaxTurns: 10,
		ServerURL: "https://server.example.com", APIKey: "execution-jwt", ProjectID: "project-1",
	}, nil, nil)
	opts, err := runner.buildSDKOptions(context.Background())
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	got := claudecode.NewOptions(opts...)
	server, ok := got.McpServers[serveragent.ManagedMCPServerName].(*claudecode.McpHTTPServerConfig)
	if !ok || server.URL != "https://server.example.com/mcp" || server.Headers["Authorization"] != "Bearer execution-jwt" {
		t.Fatalf("managed MCP server = %#v", got.McpServers)
	}
	if got.ExtraEnv["ANBAN_DEFAULT_PROJECT"] != "project-1" {
		t.Fatalf("project environment = %#v", got.ExtraEnv)
	}
	if got.PermissionMode == nil || *got.PermissionMode != claudecode.PermissionModeDefault {
		t.Fatalf("permission mode = %#v, want default", got.PermissionMode)
	}
}
