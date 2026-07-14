package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

const ManagedMCPServerName = "anban"

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
	}
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
		decision := "block"
		reason := "Anban MCP must be called through the Claude Code MCP tools injected by the Agent SDK. Do not probe /mcp or build a custom client. If the native tools are unavailable, record the recoverable failure and stop."
		return claudecode.HookJSONOutput{Decision: &decision, Reason: &reason}, nil
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
