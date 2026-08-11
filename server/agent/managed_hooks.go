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

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

type managedStopGate struct {
	agentType string
	script    string
}

var managedStopGates = map[string]managedStopGate{
	"seednote":       {agentType: "anban:seednote", script: "seednote-quality-gate.sh"},
	"viral_analysis": {agentType: "anban:seednote", script: "seednote-quality-gate.sh"},
}

// ManagedTaskStopHook returns a task-specific SDK Stop hook for sessions that
// run a plugin agent as the main --agent. Plugin-level SubagentStop hooks do not
// cover that lifecycle, while plugin agent frontmatter cannot declare hooks.
func ManagedTaskStopHook(taskType, workspace, pluginRoot string) (claudecode.Option, error) {
	gate, ok := managedStopGates[strings.TrimSpace(taskType)]
	if !ok {
		return func(*claudecode.Options) {}, nil
	}
	pluginRoot = strings.TrimSpace(pluginRoot)
	if pluginRoot == "" {
		return nil, fmt.Errorf("%s managed Stop hook requires plugin root", gate.agentType)
	}
	script := filepath.Join(pluginRoot, "hooks", gate.script)

	return claudecode.WithHook(claudecode.HookEventStop, "", func(
		ctx context.Context,
		input any,
		_ *string,
		_ claudecode.HookContext,
	) (claudecode.HookJSONOutput, error) {
		stop, ok := input.(*claudecode.StopHookInput)
		if !ok {
			return claudecode.HookJSONOutput{}, nil
		}
		payload, err := json.Marshal(map[string]any{
			"agent_type":           gate.agentType,
			"managed_main_session": true,
			"stop_hook_active":     stop.StopHookActive,
			"task_type":            strings.TrimSpace(taskType),
		})
		if err != nil {
			return claudecode.HookJSONOutput{}, err
		}
		cmd := exec.CommandContext(ctx, script)
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+workspace)
		cmd.Stdin = bytes.NewReader(payload)
		output, err := cmd.Output()
		if err != nil {
			decision := "block"
			reason := gate.agentType + " completion gate could not run: " + err.Error()
			return claudecode.HookJSONOutput{Decision: &decision, Reason: &reason}, nil
		}
		if len(bytes.TrimSpace(output)) == 0 {
			return claudecode.HookJSONOutput{}, nil
		}
		var result claudecode.HookJSONOutput
		if err := json.Unmarshal(output, &result); err != nil {
			decision := "block"
			reason := gate.agentType + " completion gate returned invalid output: " + err.Error()
			return claudecode.HookJSONOutput{Decision: &decision, Reason: &reason}, nil
		}
		return result, nil
	}), nil
}
