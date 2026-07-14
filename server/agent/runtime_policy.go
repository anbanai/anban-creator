package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

const ManagedMCPServerName = "anban"

const (
	managedMCPReadyTimeout      = 30 * time.Second
	managedMCPReadyPollInterval = 250 * time.Millisecond
)

type managedMCPStatusGetter interface {
	GetMcpStatus(context.Context) (*claudecode.McpStatusResponse, error)
}

var managedAgentAllowedTools = []string{
	"Read",
	"Write",
	"Edit",
	"Glob",
	"Grep",
	"Bash",
	"Skill",
	"TaskCreate",
	"TaskUpdate",
	"TaskList",
	"TaskGet",
	"TaskOutput",
	"TaskStop",
	"TodoWrite",
	"WebSearch",
	"WebFetch",
	"NotebookEdit",
	"mcp__" + ManagedMCPServerName + "__*",
}

func ManagedAgentDisallowedTools() []string {
	return []string{"Agent", "ScheduleWakeup", "AskUserQuestion"}
}

func ManagedAgentAllowedTools() []string {
	return append([]string(nil), managedAgentAllowedTools...)
}

func WithManagedAgentRuntimePolicy() claudecode.Option {
	return func(opts *claudecode.Options) {
		claudecode.WithAllowedTools(ManagedAgentAllowedTools()...)(opts)
		claudecode.WithDisallowedTools(ManagedAgentDisallowedTools()...)(opts)
		// The current Go SDK does not expose Claude Code's dontAsk mode. Allowed
		// rules approve the managed surface first; this callback deterministically
		// denies every unlisted tool that would otherwise require interaction.
		claudecode.WithCanUseTool(func(
			_ context.Context,
			toolName string,
			_ map[string]any,
			_ claudecode.ToolPermissionContext,
		) (claudecode.PermissionResult, error) {
			return claudecode.NewPermissionResultDeny(
				fmt.Sprintf("tool %q is outside the managed Agent SDK allowlist", toolName),
			), nil
		})(opts)
		managedMCPBoundaryHook()(opts)
	}
}

// WithManagedMCPAccess injects the remote Anban MCP server through the Agent
// SDK. Plugin-shipped agents cannot declare mcpServers in frontmatter, and the
// managed runtime must not depend on interactive plugin user_config values.
func WithManagedMCPAccess(serverURL, apiKey string) claudecode.Option {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	return func(opts *claudecode.Options) {
		claudecode.WithMcpServers(map[string]claudecode.McpServerConfig{
			ManagedMCPServerName: &claudecode.McpHTTPServerConfig{
				Type: claudecode.McpServerTypeHTTP,
				URL:  serverURL + "/mcp",
				Headers: map[string]string{
					"Authorization": "Bearer " + apiKey,
				},
			},
		})(opts)
		if !containsString(opts.AllowedTools, "mcp__"+ManagedMCPServerName+"__*") {
			opts.AllowedTools = append(opts.AllowedTools, "mcp__"+ManagedMCPServerName+"__*")
		}
		// The Agent SDK exposes skipMcpDiscovery when the host application owns
		// a plugin's MCP connection. The current Go SDK does not, so use Claude
		// Code's strict flag to ignore plugin/user MCP configs
		// and retain only the execution-token-scoped --mcp-config above.
		if opts.ExtraArgs == nil {
			opts.ExtraArgs = make(map[string]*string)
		}
		opts.ExtraArgs["strict-mcp-config"] = nil
	}
}

// WaitForManagedMCPReady allows Claude Code's asynchronous MCP initialization
// to leave the pending state before the first user prompt is sent. Terminal
// states still fail immediately so their connection or authentication detail
// is not hidden behind the readiness timeout.
func WaitForManagedMCPReady(ctx context.Context, client managedMCPStatusGetter, taskType string) error {
	return waitForManagedMCPReady(ctx, client, taskType, managedMCPReadyTimeout, managedMCPReadyPollInterval)
}

func waitForManagedMCPReady(ctx context.Context, client managedMCPStatusGetter, taskType string, timeout, pollInterval time.Duration) error {
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		status, err := client.GetMcpStatus(readyCtx)
		if err != nil {
			return fmt.Errorf("check managed MCP readiness: %w", err)
		}
		validationErr := ValidateManagedMCPStatus(status, taskType)
		if validationErr == nil {
			return nil
		}
		if !managedMCPIsPending(status) {
			return validationErr
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-readyCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if ctx.Err() != nil {
				return fmt.Errorf("check managed MCP readiness: %w", ctx.Err())
			}
			return fmt.Errorf("%w after waiting %s", validationErr, timeout)
		case <-timer.C:
		}
	}
}

func managedMCPIsPending(status *claudecode.McpStatusResponse) bool {
	if status == nil {
		return false
	}
	for i := range status.McpServers {
		server := &status.McpServers[i]
		if server.Name == ManagedMCPServerName {
			return server.Status == claudecode.McpServerConnectionStatusPending
		}
	}
	return false
}

// ValidateManagedMCPStatus performs the SDK-native readiness check before the
// first user prompt is sent. This prevents the model from spending turns trying
// to discover or reimplement a missing MCP connection.
func ValidateManagedMCPStatus(status *claudecode.McpStatusResponse, taskType string) error {
	if status == nil {
		return fmt.Errorf("managed MCP readiness failed: empty status response")
	}
	var managed *claudecode.McpServerStatus
	for i := range status.McpServers {
		if status.McpServers[i].Name == ManagedMCPServerName {
			managed = &status.McpServers[i]
			break
		}
	}
	if managed == nil {
		return fmt.Errorf("managed MCP readiness failed: server %q is not configured", ManagedMCPServerName)
	}
	if managed.Status != claudecode.McpServerConnectionStatusConnected {
		detail := ""
		if managed.Error != nil {
			detail = ": " + strings.TrimSpace(*managed.Error)
		}
		return fmt.Errorf("managed MCP readiness failed: server %q status is %q%s", ManagedMCPServerName, managed.Status, detail)
	}
	if len(managed.Tools) == 0 {
		return fmt.Errorf("managed MCP readiness failed: server %q exposed no tools", ManagedMCPServerName)
	}

	available := make(map[string]bool, len(managed.Tools))
	for _, tool := range managed.Tools {
		name := strings.TrimSpace(tool.Name)
		if idx := strings.LastIndex(name, "__"); idx >= 0 {
			name = name[idx+2:]
		}
		available[name] = true
	}
	var missing []string
	for _, name := range managedRequiredMCPTools(taskType) {
		if !available[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("managed MCP readiness failed: server %q is missing required tools for %s: %s", ManagedMCPServerName, taskType, strings.Join(missing, ", "))
	}
	return nil
}

// ValidateManagedPluginInit verifies the SDK system/init inventory instead of
// assuming that a plugin path on disk was discovered by Claude Code.
func ValidateManagedPluginInit(message *claudecode.SystemMessage, taskType string) error {
	if message == nil || message.Subtype != "init" {
		return fmt.Errorf("managed plugin readiness failed: missing system/init message")
	}
	if !initPluginNames(message.Data["plugins"])["anban"] {
		return fmt.Errorf("managed plugin readiness failed: plugin %q is not loaded", "anban")
	}
	loadedSkills := initStringValues(message.Data["skills"])
	for _, skill := range managedRequiredPluginSkills(taskType) {
		if !loadedSkills[skill] {
			return fmt.Errorf("managed plugin readiness failed: skill %q is not loaded", skill)
		}
	}
	return nil
}

func managedRequiredPluginSkills(taskType string) []string {
	switch strings.TrimSpace(taskType) {
	case "seednote":
		return []string{"anban:seednote", "anban:humanizer"}
	case "article", "ecommerce":
		return []string{"anban:humanizer"}
	default:
		return nil
	}
}

// ValidateManagedPluginResult applies the plugin-init gate only to successful
// Claude Code results. Protocol errors must retain their original diagnostic,
// while a stream that closes without a result is always invalid.
func ValidateManagedPluginResult(initValidated bool, message *claudecode.ResultMessage) error {
	if message == nil {
		return fmt.Errorf("managed agent stream ended without a result message")
	}
	if message.IsError {
		return nil
	}
	if !initValidated {
		return fmt.Errorf("managed plugin readiness failed: Claude Code did not emit system/init")
	}
	return nil
}

func initPluginNames(value any) map[string]bool {
	result := make(map[string]bool)
	items, _ := value.([]any)
	for _, item := range items {
		plugin, _ := item.(map[string]any)
		if name, _ := plugin["name"].(string); strings.TrimSpace(name) != "" {
			result[strings.TrimSpace(name)] = true
		}
	}
	return result
}

func initStringValues(value any) map[string]bool {
	result := make(map[string]bool)
	items, _ := value.([]any)
	for _, item := range items {
		if name, _ := item.(string); strings.TrimSpace(name) != "" {
			result[strings.TrimSpace(name)] = true
		}
	}
	return result
}

func managedRequiredMCPTools(taskType string) []string {
	if strings.TrimSpace(taskType) != "seednote" {
		return nil
	}
	return []string{
		"analyze_image",
		"archive_workspace",
		"finalize_task_title",
		"generate_image",
		"get_project_profile",
		"list_project_titles",
		"prepare_workspace",
		"submit_agent_feedback",
		"update_task_progress",
	}
}

func managedMCPBoundaryHook() claudecode.Option {
	return claudecode.WithPreToolUseHook("Bash", func(
		_ context.Context,
		input any,
		_ *string,
		_ claudecode.HookContext,
	) (claudecode.HookJSONOutput, error) {
		pre, ok := input.(*claudecode.PreToolUseHookInput)
		if !ok {
			return claudecode.HookJSONOutput{}, nil
		}
		command, _ := pre.ToolInput["command"].(string)
		lower := strings.ToLower(command)
		probesMCP := strings.Contains(lower, "mcp-session-id") ||
			strings.Contains(lower, "mcp_client") ||
			(strings.Contains(lower, "/mcp") && containsAny(lower, "curl", "wget", "python", "requests", "http", "mcporter", "jsonrpc"))
		if !probesMCP {
			return claudecode.HookJSONOutput{}, nil
		}
		decision := "deny"
		reason := "Anban MCP must be called through the Claude Code MCP tools injected by the Agent SDK. Do not probe /mcp or build a custom client. If the native tools are unavailable, record the recoverable failure and stop."
		return claudecode.HookJSONOutput{HookSpecificOutput: claudecode.PreToolUseHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       &decision,
			PermissionDecisionReason: &reason,
		}}, nil
	})
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
