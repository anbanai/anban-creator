package agent

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
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

func TestManagedAgentRuntimePolicyAllowsLargeStructuredMessages(t *testing.T) {
	opts := claudecode.NewOptions(WithManagedAgentRuntimePolicy())
	if opts.MaxBufferSize == nil || *opts.MaxBufferSize != managedAgentMaxBufferSize {
		t.Fatalf("MaxBufferSize = %v, want %d", opts.MaxBufferSize, managedAgentMaxBufferSize)
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
	wantTimeout := (15 * time.Minute).Milliseconds()
	if server.Timeout != wantTimeout {
		t.Fatalf("server timeout = %dms, want %dms", server.Timeout, wantTimeout)
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
		ToolInput: map[string]any{"command": `curl -s "https://server.example.com/mcp"`},
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

func TestManagedRequiredMCPToolsKeepsSeednoteResearchOptional(t *testing.T) {
	got := managedRequiredMCPTools("seednote")
	want := []string{
		"analyze_image",
		"claim_topic",
		"finalize_task_title",
		"generate_image",
		"get_project_profile",
		"list_project_titles",
		"submit_agent_feedback",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managedRequiredMCPTools(seednote) = %#v, want %#v", got, want)
	}
}

func TestManagedRequiredMCPToolsCoversViralAnalysisRuntime(t *testing.T) {
	want := []string{
		"get_project_profile",
		"list_project_titles",
		"submit_agent_feedback",
	}
	if got := managedRequiredMCPTools(model.TaskTypeViralAnalysis); !reflect.DeepEqual(got, want) {
		t.Fatalf("managedRequiredMCPTools(viral_analysis) = %#v, want %#v", got, want)
	}
}

func TestManagedRequiredMCPToolsExcludeLegacyProgressForEveryManagedTaskType(t *testing.T) {
	for _, taskType := range []string{
		model.PlatformArticle,
		model.PlatformSeednote,
		model.TaskTypeViralAnalysis,
		model.PlatformMoments,
		model.PlatformEcommerce,
		model.TaskTypeLiveSlicer,
		model.PlatformMontage,
	} {
		t.Run(taskType, func(t *testing.T) {
			if slices.Contains(managedRequiredMCPTools(taskType), "update_task_progress") {
				t.Fatalf("managedRequiredMCPTools(%q) retains legacy progress compatibility tool", taskType)
			}
		})
	}
}

func TestManagedMCPBoundaryHookBlocksDirectMCPAndSeednoteSidecar(t *testing.T) {
	options := claudecode.NewOptions(managedMCPBoundaryHook())
	hooks, ok := options.Hooks.(map[claudecode.HookEvent][]claudecode.HookMatcher)
	if !ok || len(hooks[claudecode.HookEventPreToolUse]) != 1 {
		t.Fatalf("PreToolUse hooks = %#v, want one matcher", options.Hooks)
	}
	matcher := hooks[claudecode.HookEventPreToolUse][0]
	if matcher.Matcher != "Bash|WebFetch" || len(matcher.Hooks) != 1 {
		t.Fatalf("matcher = %#v, want one Bash/WebFetch callback", matcher)
	}

	for _, tc := range []struct {
		name    string
		command string
		denied  bool
	}{
		{name: "server mcp", command: "curl https://creator.anbanai.com/mcp", denied: true},
		{name: "seednote mcp", command: "node -e 'fetch(\"http://sidecar-seednote:18060/mcp\")'", denied: true},
		{name: "seednote rest", command: "curl http://sidecar-seednote:18060/api/v1/feeds", denied: true},
		{name: "ordinary command", command: "printf done", denied: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := matcher.Hooks[0](context.Background(), &claudecode.PreToolUseHookInput{
				HookEventName: "PreToolUse",
				ToolName:      "Bash",
				ToolInput:     map[string]any{"command": tc.command},
			}, nil, claudecode.HookContext{})
			if err != nil {
				t.Fatal(err)
			}
			decision := ""
			if specific, ok := output.HookSpecificOutput.(claudecode.PreToolUseHookSpecificOutput); ok && specific.PermissionDecision != nil {
				decision = *specific.PermissionDecision
			}
			if got := decision == "deny"; got != tc.denied {
				t.Fatalf("permission decision = %q, denied=%v, want %v", decision, got, tc.denied)
			}
			if tc.denied {
				specific := output.HookSpecificOutput.(claudecode.PreToolUseHookSpecificOutput)
				if specific.PermissionDecisionReason == nil || strings.Contains(strings.ToLower(*specific.PermissionDecisionReason), "recoverable failure") {
					t.Fatalf("permission reason = %v, want task-neutral MCP guidance", specific.PermissionDecisionReason)
				}
			}
		})
	}

	output, err := matcher.Hooks[0](context.Background(), &claudecode.PreToolUseHookInput{
		HookEventName: "PreToolUse",
		ToolName:      "WebFetch",
		ToolInput:     map[string]any{"url": "http://sidecar-seednote:18060/api/v1/feeds"},
	}, nil, claudecode.HookContext{})
	if err != nil {
		t.Fatal(err)
	}
	specific, ok := output.HookSpecificOutput.(claudecode.PreToolUseHookSpecificOutput)
	if !ok || specific.PermissionDecision == nil || *specific.PermissionDecision != "deny" {
		t.Fatalf("WebFetch output = %#v, want deny", output)
	}
}

func TestManagedRequiredMCPToolsRequiresAnalyzeVideoForMontage(t *testing.T) {
	if got := managedRequiredMCPTools(model.PlatformMontage); !reflect.DeepEqual(got, []string{"analyze_video"}) {
		t.Fatalf("managedRequiredMCPTools(montage) = %#v", got)
	}
}

func TestValidateManagedMCPStatusRequiresNamespacedLiveSlicerTools(t *testing.T) {
	want := []string{
		"analyze_video",
		"build_live_clip_manifest",
		"build_live_clip_plan",
		"build_live_subject_clip_plan",
		"create_live_analysis_task",
		"get_media_pipeline_status",
		"prepare_file_upload",
		"query_live_analysis_task",
		"submit_agent_feedback",
	}
	if got := managedRequiredMCPTools(model.TaskTypeLiveSlicer); !reflect.DeepEqual(got, want) {
		t.Fatalf("managedRequiredMCPTools(live-slicer) = %#v, want %#v", got, want)
	}
	tools := make([]claudecode.McpToolInfo, 0, len(want))
	for _, name := range want {
		tools = append(tools, claudecode.McpToolInfo{Name: "mcp__anban__" + name})
	}
	status := &claudecode.McpStatusResponse{McpServers: []claudecode.McpServerStatus{{
		Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusConnected, Tools: tools,
	}}}
	if err := ValidateManagedMCPStatus(status, model.TaskTypeLiveSlicer); err != nil {
		t.Fatalf("ValidateManagedMCPStatus namespaced tools: %v", err)
	}
	status.McpServers[0].Tools = status.McpServers[0].Tools[1:]
	if err := ValidateManagedMCPStatus(status, model.TaskTypeLiveSlicer); err == nil || !strings.Contains(err.Error(), "analyze_video") {
		t.Fatalf("error = %v, want missing analyze_video", err)
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

type managedMCPStatusSequence struct {
	statuses []*claudecode.McpStatusResponse
	calls    int
}

func (s *managedMCPStatusSequence) GetMcpStatus(context.Context) (*claudecode.McpStatusResponse, error) {
	index := s.calls
	s.calls++
	if index >= len(s.statuses) {
		index = len(s.statuses) - 1
	}
	return s.statuses[index], nil
}

func TestWaitForManagedMCPReadyPollsPendingUntilConnected(t *testing.T) {
	client := &managedMCPStatusSequence{statuses: []*claudecode.McpStatusResponse{
		{McpServers: []claudecode.McpServerStatus{{Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusPending}}},
		{McpServers: []claudecode.McpServerStatus{{Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusConnected, Tools: []claudecode.McpToolInfo{{Name: "analyze_video"}}}}},
	}}
	if err := waitForManagedMCPReady(context.Background(), client, model.PlatformMontage, time.Second, time.Millisecond); err != nil {
		t.Fatalf("waitForManagedMCPReady: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("GetMcpStatus calls = %d, want 2", client.calls)
	}
}

func TestWaitForManagedMCPReadyFailsTerminalStateImmediately(t *testing.T) {
	detail := "tls: failed to verify certificate"
	client := &managedMCPStatusSequence{statuses: []*claudecode.McpStatusResponse{{
		McpServers: []claudecode.McpServerStatus{{Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusFailed, Error: &detail}},
	}}}
	err := waitForManagedMCPReady(context.Background(), client, "article", time.Second, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), detail) {
		t.Fatalf("error = %v, want terminal TLS detail", err)
	}
	if client.calls != 1 {
		t.Fatalf("GetMcpStatus calls = %d, want 1", client.calls)
	}
}

func TestWaitForManagedMCPReadyTimesOutWithPendingStatus(t *testing.T) {
	client := &managedMCPStatusSequence{statuses: []*claudecode.McpStatusResponse{{
		McpServers: []claudecode.McpServerStatus{{Name: ManagedMCPServerName, Status: claudecode.McpServerConnectionStatusPending}},
	}}}
	err := waitForManagedMCPReady(context.Background(), client, "article", 5*time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), `status is "pending" after waiting 5ms`) {
		t.Fatalf("error = %v, want bounded pending timeout", err)
	}
	if client.calls < 2 {
		t.Fatalf("GetMcpStatus calls = %d, want polling", client.calls)
	}
}

func TestValidateManagedPluginInitRequiresTaskSkills(t *testing.T) {
	for _, tc := range []struct {
		taskType string
		skills   []any
		missing  string
	}{
		{
			taskType: "seednote",
			skills: []any{
				"anban:seednote-research",
				"anban:seednote-viral-analysis",
				"anban:seednote-writing",
				"anban:seednote-visual-design",
				"anban:humanizer",
			},
			missing: "anban:seednote-research",
		},
		{
			taskType: model.TaskTypeViralAnalysis,
			skills: []any{
				"anban:seednote-research",
				"anban:seednote-viral-analysis",
			},
			missing: "anban:seednote-research",
		},
		{taskType: "article", skills: []any{"anban:humanizer"}, missing: "anban:humanizer"},
		{taskType: "ecommerce", skills: []any{"anban:humanizer"}, missing: "anban:humanizer"},
		{taskType: model.TaskTypeLiveSlicer, skills: []any{"anban:live-slice", "anban:capcut-draft"}, missing: "anban:live-slice"},
	} {
		t.Run(tc.taskType, func(t *testing.T) {
			message := &claudecode.SystemMessage{
				Subtype: "init",
				Data: map[string]any{
					"plugins": []any{map[string]any{"name": "anban", "path": "/plugins"}},
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

func TestStandaloneAgentRunnerUsesRuntimePolicy(t *testing.T) {
	body := readRepoFile(t, "../../agent-ts/src/runner.ts")
	for _, want := range []string{
		`permissionMode: "default"`,
		`allowedTools: MANAGED_ALLOWED_TOOLS`,
		`canUseTool: async (toolName)`,
		`disallowedTools: ["Agent", "ScheduleWakeup", "AskUserQuestion"]`,
		"strictMcpConfig: true",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("standalone Agent runner must retain managed runtime contract %q", want)
		}
	}
	for _, forbidden := range []string{
		`permissionMode: "bypassPermissions"`,
		`allowDangerouslySkipPermissions: true`,
		`mcpServers: { anban: { command:`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("standalone Agent runner retains forbidden runtime behavior %q", forbidden)
		}
	}
}
