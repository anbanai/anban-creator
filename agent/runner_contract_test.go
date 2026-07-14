package main

import (
	"context"
	"os"
	"strings"
	"testing"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func TestRunnerUsesManagedAgentRuntimePolicy(t *testing.T) {
	data, err := os.ReadFile("runner.go")
	if err != nil {
		t.Fatalf("read runner.go: %v", err)
	}
	if !strings.Contains(string(data), "serveragent.WithManagedAgentRuntimePolicy()") {
		t.Fatal("runner must apply serveragent.WithManagedAgentRuntimePolicy()")
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
	if got["ANBAN_API_KEY"] != "execution-jwt" || got["ANBAN_API_URL"] != "https://server.example.com" {
		t.Fatalf("execution identity environment = %#v", got)
	}
	if got["ANBAN_DEFAULT_PROJECT"] != "" {
		t.Fatalf("unexpected project environment = %#v", got)
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
