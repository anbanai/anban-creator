package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

func TestManagedAgentDisallowedTools(t *testing.T) {
	got := ManagedAgentDisallowedTools()
	want := []string{"Agent", "ScheduleWakeup", "AskUserQuestion"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedAgentDisallowedTools() = %#v, want %#v", got, want)
	}

	got[0] = "Bash"
	again := ManagedAgentDisallowedTools()
	if !reflect.DeepEqual(again, want) {
		t.Fatalf("ManagedAgentDisallowedTools() did not return a copy: got %#v, want %#v", again, want)
	}
}

func TestManagedAgentRuntimePolicySetsSDKDisallowedTools(t *testing.T) {
	opts := claudecode.NewOptions(WithManagedAgentRuntimePolicy())
	want := []string{"Agent", "ScheduleWakeup", "AskUserQuestion"}
	if !reflect.DeepEqual(opts.DisallowedTools, want) {
		t.Fatalf("DisallowedTools = %#v, want %#v", opts.DisallowedTools, want)
	}
}

func TestManagedAgentRuntimePolicyUsesExplicitAllowedTools(t *testing.T) {
	opts := claudecode.NewOptions(WithManagedAgentRuntimePolicy())
	for _, want := range []string{"Read", "Write", "Bash", "Skill", "TaskCreate", "mcp__anban__*"} {
		if !containsString(opts.AllowedTools, want) {
			t.Fatalf("AllowedTools = %#v, missing %q", opts.AllowedTools, want)
		}
	}
	if opts.Hooks == nil {
		t.Fatal("managed policy must register SDK hooks")
	}
}

func TestWithManagedMCPAccessUsesProgrammaticHTTPServer(t *testing.T) {
	opts := claudecode.NewOptions(
		WithManagedAgentRuntimePolicy(),
		WithManagedMCPAccess("https://server.example.com/", "execution-jwt"),
	)
	server, ok := opts.McpServers[ManagedMCPServerName].(*claudecode.McpHTTPServerConfig)
	if !ok {
		t.Fatalf("McpServers = %#v, want HTTP server", opts.McpServers)
	}
	if server.URL != "https://server.example.com/mcp" || server.Headers["Authorization"] != "Bearer execution-jwt" {
		t.Fatalf("server = %#v", server)
	}
}

func TestManagedAgentRuntimePolicyBlocksAdHocMCPClients(t *testing.T) {
	opts := claudecode.NewOptions(WithManagedAgentRuntimePolicy())
	hooks, ok := opts.Hooks.(map[claudecode.HookEvent][]claudecode.HookMatcher)
	if !ok || len(hooks[claudecode.HookEventPreToolUse]) == 0 {
		t.Fatalf("hooks = %#v, want PreToolUse hook", opts.Hooks)
	}
	callback := hooks[claudecode.HookEventPreToolUse][0].Hooks[0]
	result, err := callback(context.Background(), &claudecode.PreToolUseHookInput{
		ToolInput: map[string]any{"command": `curl -s "$ANBAN_API_URL/mcp"`},
	}, nil, claudecode.HookContext{})
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if result.Decision == nil || *result.Decision != "block" || result.Reason == nil || !strings.Contains(*result.Reason, "Claude Code MCP tools") {
		t.Fatalf("result = %#v, want ad hoc MCP client block", result)
	}
}

func TestValidateManagedMCPStatusRequiresConnectedSeednoteTools(t *testing.T) {
	tools := make([]claudecode.McpToolInfo, 0)
	for _, name := range managedRequiredMCPTools("seednote") {
		tools = append(tools, claudecode.McpToolInfo{Name: name})
	}
	status := &claudecode.McpStatusResponse{McpServers: []claudecode.McpServerStatus{{
		Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusConnected, Tools: tools,
	}}}
	if err := ValidateManagedMCPStatus(status, "seednote"); err != nil {
		t.Fatalf("ValidateManagedMCPStatus: %v", err)
	}
	status.McpServers[0].Tools = status.McpServers[0].Tools[1:]
	if err := ValidateManagedMCPStatus(status, "seednote"); err == nil || !strings.Contains(err.Error(), "analyze_image") {
		t.Fatalf("error = %v, want missing analyze_image", err)
	}
}

func TestValidateManagedMCPStatusRejectsDisconnectedServer(t *testing.T) {
	detail := "401 unauthorized"
	status := &claudecode.McpStatusResponse{McpServers: []claudecode.McpServerStatus{{
		Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusFailed, Error: &detail,
	}}}
	if err := ValidateManagedMCPStatus(status, "seednote"); err == nil || !strings.Contains(err.Error(), "401 unauthorized") {
		t.Fatalf("error = %v, want connection detail", err)
	}
}

func TestManagedAgentExecutorsUseRuntimePolicy(t *testing.T) {
	for _, path := range []string{
		"executor.go",
		"../../agent/runner.go",
	} {
		t.Run(path, func(t *testing.T) {
			text := readRepoFile(t, path)
			if !strings.Contains(text, "WithManagedAgentRuntimePolicy()") {
				t.Fatalf("%s must apply WithManagedAgentRuntimePolicy()", path)
			}
		})
	}

	localExecutor := readRepoFile(t, "executor.go")
	for _, want := range []string{
		`agentFlag := "anban:" + agentName`,
		`claudecode.WithExtraArgs(map[string]*string{"agent": &agentFlag})`,
	} {
		if !strings.Contains(localExecutor, want) {
			t.Fatalf("local executor must start the plugin agent as the main session via %q", want)
		}
	}
	if strings.Contains(localExecutor, "claudecode.WithAgent(agentName") {
		t.Fatal("local executor must not register the workflow agent as a delegatable subagent")
	}
}
