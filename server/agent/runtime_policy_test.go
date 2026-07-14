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
	if opts.CanUseTool == nil {
		t.Fatal("managed policy must reject permission prompts for unlisted tools")
	}
	result, err := opts.CanUseTool(context.Background(), "UnknownTool", nil, claudecode.ToolPermissionContext{})
	if err != nil {
		t.Fatalf("CanUseTool: %v", err)
	}
	if _, ok := result.(claudecode.PermissionResultDeny); !ok {
		t.Fatalf("CanUseTool result = %#v, want deny", result)
	}
}

func TestWithManagedMCPAccessUsesProgrammaticHTTPServer(t *testing.T) {
	agentName := "anban:seednote"
	opts := claudecode.NewOptions(
		claudecode.WithExtraArgs(map[string]*string{"agent": &agentName}),
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
	if _, ok := opts.ExtraArgs["strict-mcp-config"]; !ok {
		t.Fatalf("ExtraArgs = %#v, want strict-mcp-config for application-owned MCP", opts.ExtraArgs)
	}
	if opts.ExtraArgs["agent"] == nil || *opts.ExtraArgs["agent"] != agentName {
		t.Fatalf("ExtraArgs = %#v, managed MCP must preserve the main Agent flag", opts.ExtraArgs)
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
	if result.Decision != nil || result.Reason != nil {
		t.Fatalf("result = %#v, PreToolUse must not use deprecated top-level decision fields", result)
	}
	specific, ok := result.HookSpecificOutput.(claudecode.PreToolUseHookSpecificOutput)
	if !ok || specific.HookEventName != "PreToolUse" || specific.PermissionDecision == nil || *specific.PermissionDecision != "deny" || specific.PermissionDecisionReason == nil || !strings.Contains(*specific.PermissionDecisionReason, "Claude Code MCP tools") {
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

func TestValidateManagedMCPStatusRejectsConnectedServerWithoutTools(t *testing.T) {
	status := &claudecode.McpStatusResponse{McpServers: []claudecode.McpServerStatus{{
		Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusConnected,
	}}}
	if err := ValidateManagedMCPStatus(status, "article"); err == nil || !strings.Contains(err.Error(), "exposed no tools") {
		t.Fatalf("error = %v, want empty tool inventory failure", err)
	}
}

func TestValidateManagedPluginInitRequiresTaskSkills(t *testing.T) {
	for _, tc := range []struct {
		taskType string
		skills   []any
		missing  string
	}{
		{taskType: "seednote", skills: []any{"anban:seednote", "anban:humanizer"}, missing: "anban:seednote"},
		{taskType: "article", skills: []any{"anban:humanizer"}, missing: "anban:humanizer"},
		{taskType: "ecommerce", skills: []any{"anban:humanizer"}, missing: "anban:humanizer"},
	} {
		t.Run(tc.taskType, func(t *testing.T) {
			message := &claudecode.SystemMessage{
				Subtype: "init",
				Data: map[string]any{
					"plugins": []any{map[string]any{"name": "anban", "path": "/plugins/anban"}},
					"skills":  tc.skills,
				},
			}
			if err := ValidateManagedPluginInit(message, tc.taskType); err != nil {
				t.Fatalf("ValidateManagedPluginInit: %v", err)
			}
			message.Data["skills"] = []any{}
			if err := ValidateManagedPluginInit(message, tc.taskType); err == nil || !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("error = %v, want missing skill %s", err, tc.missing)
			}
		})
	}
}

func TestValidateManagedPluginInitRequiresAnbanPlugin(t *testing.T) {
	message := &claudecode.SystemMessage{Subtype: "init", Data: map[string]any{
		"plugins": []any{map[string]any{"name": "other"}},
	}}
	if err := ValidateManagedPluginInit(message, "article"); err == nil || !strings.Contains(err.Error(), "anban") {
		t.Fatalf("error = %v, want missing plugin", err)
	}
}

func TestValidateManagedPluginResultPreservesProtocolErrorsAndFailsClosed(t *testing.T) {
	if err := ValidateManagedPluginResult(false, &claudecode.ResultMessage{IsError: true}); err != nil {
		t.Fatalf("protocol error must retain its original diagnostic: %v", err)
	}
	if err := ValidateManagedPluginResult(false, &claudecode.ResultMessage{}); err == nil || !strings.Contains(err.Error(), "system/init") {
		t.Fatalf("successful result without init error = %v", err)
	}
	if err := ValidateManagedPluginResult(true, nil); err == nil || !strings.Contains(err.Error(), "without a result message") {
		t.Fatalf("missing result error = %v", err)
	}
	if err := ValidateManagedPluginResult(true, &claudecode.ResultMessage{}); err != nil {
		t.Fatalf("validated successful result: %v", err)
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
	for _, path := range []string{"executor.go", "../../agent/runner.go"} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "claudecode.WithPermissionMode(claudecode.PermissionModeDefault)") {
			t.Fatalf("%s must use default permission evaluation with the managed fail-closed callback", path)
		}
		if strings.Contains(body, "claudecode.PermissionModeBypassPermissions") {
			t.Fatalf("%s must not bypass the managed allowlist", path)
		}
	}
}
