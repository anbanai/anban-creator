package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

type Runner struct {
	cfg        *Config
	reporter   *Reporter
	downloader *Downloader
}

type runnerStreamState struct {
	resultText          string
	toolUseCount        int
	toolErrorCount      int
	lastToolErrorTool   string
	lastToolError       string
	turnNum             int
	pluginInitValidated bool
	toolCalls           map[string]trackedToolCall
	toolUseSummary      map[string]int
}

func newRunnerStreamState() *runnerStreamState {
	return &runnerStreamState{
		toolCalls:      make(map[string]trackedToolCall),
		toolUseSummary: make(map[string]int),
	}
}

func runtimeCwd(workspace, taskType string) string {
	if model.IsMontagePlatform(strings.TrimSpace(taskType)) {
		return montageRuntimePath(workspace)
	}
	return workspace
}

func montageRuntimePath(workspace string) string {
	return filepath.Join(workspace, serveragent.MontageRuntimeDirName)
}

func NewRunner(cfg *Config, reporter *Reporter, downloader *Downloader) *Runner {
	return &Runner{
		cfg:        cfg,
		reporter:   reporter,
		downloader: downloader,
	}
}

func (r *Runner) Run(ctx context.Context) (*serveragent.ExecutionResult, error) {
	workDir := runtimeCwd(r.cfg.Workspace, r.cfg.TaskType)
	result := &serveragent.ExecutionResult{
		Success:    false,
		WorkDir:    workDir,
		CostStatus: serveragent.CostStatusUnreconciled,
		CostDiagnostics: []serveragent.CostDiagnostic{{
			Code: serveragent.CostDiagnosticMissingTerminalModelUsage,
		}},
	}
	sdkOpts, err := r.buildSDKOptions(ctx)
	if err != nil {
		result.TerminalReason = model.TaskBillingTerminalPlatformError
		return result, err
	}

	state := newRunnerStreamState()

	err = claudecode.WithClient(ctx, func(client claudecode.Client) error {
		if err := serveragent.WaitForManagedMCPReady(ctx, client, r.cfg.TaskType); err != nil {
			return err
		}
		if err := client.Query(ctx, r.cfg.UserPrompt()); err != nil {
			return fmt.Errorf("send query: %w", err)
		}

		response := client.ReceiveResponse(ctx)
		defer response.Close()
		return r.consumeResponse(ctx, response, result, state)
	}, sdkOpts...)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TerminalReason = model.TaskBillingTerminalExecutionTimeout
		} else if result.TerminalReason == "" {
			result.TerminalReason = model.TaskBillingTerminalProviderError
		}
		if result.Error == "" {
			result.Error = err.Error()
		}
		state.copyToResult(result)
		return result, err
	}

	result.Success = true
	state.copyToResult(result)
	if state.turnNum <= 1 && state.toolUseCount == 0 {
		result.AgentLikelyFailed = true
	}
	return result, nil
}

func (r *Runner) consumeResponse(ctx context.Context, response claudecode.MessageIterator, result *serveragent.ExecutionResult, state *runnerStreamState) error {
	for {
		msg, receiveErr := response.Next(ctx)
		if errors.Is(receiveErr, claudecode.ErrNoMoreMessages) {
			return serveragent.ValidateManagedPluginResult(state.pluginInitValidated, nil)
		}
		if receiveErr != nil {
			return fmt.Errorf("receive managed agent stream: %w", receiveErr)
		}
		if msg == nil {
			continue
		}
		switch m := msg.(type) {
		case *claudecode.SystemMessage:
			if m.Subtype == "init" {
				if err := serveragent.ValidateManagedPluginInit(m, r.cfg.TaskType); err != nil {
					return err
				}
				state.pluginInitValidated = true
			}
		case *claudecode.AssistantMessage:
			r.consumeAssistantMessage(ctx, m, state)
		case *claudecode.UserMessage:
			if err := r.consumeUserMessage(ctx, m, state); err != nil {
				result.TerminalReason = model.TaskBillingTerminalPlatformError
				return err
			}
		case *claudecode.ResultMessage:
			r.populateTerminalModelUsage(result, m)
			result.Success = !m.IsError
			result.ResultSubtype = m.Subtype
			result.NumTurns = m.NumTurns
			result.SessionID = m.SessionID
			result.DurationMs = m.DurationMs
			state.copyToResult(result)
			if m.IsError {
				result.TerminalReason = model.TaskBillingTerminalProviderError
				result.Error = serveragent.ResultMessageError(m, state.lastToolErrorTool, state.lastToolError)
				return errors.New(result.Error)
			}
			if err := serveragent.ValidateManagedPluginResult(state.pluginInitValidated, m); err != nil {
				return err
			}
			if state.toolUseCount == 0 {
				result.AgentLikelyFailed = true
			}
			return nil
		}
	}
}

func (r *Runner) consumeAssistantMessage(ctx context.Context, message *claudecode.AssistantMessage, state *runnerStreamState) {
	state.turnNum++
	for _, block := range message.Content {
		switch b := block.(type) {
		case *claudecode.TextBlock:
			text := strings.TrimSpace(b.Text)
			if text == "" {
				continue
			}
			if state.resultText != "" {
				state.resultText += "\n"
			}
			state.resultText += text
			if r.reporter != nil {
				_ = r.reporter.ReportProgress(ctx, text)
			}
		case *claudecode.ToolUseBlock:
			state.toolUseCount++
			state.toolUseSummary[b.Name]++
			state.toolCalls[b.ToolUseID] = trackedToolCall{Name: b.Name, Input: b.Input}
			if r.reporter != nil {
				_ = r.reporter.ReportProgress(ctx, "Using tool: "+b.Name)
			}
		}
	}
}

func (r *Runner) consumeUserMessage(ctx context.Context, message *claudecode.UserMessage, state *runnerStreamState) error {
	blocks, ok := message.Content.([]claudecode.ContentBlock)
	if !ok {
		return nil
	}
	for _, block := range blocks {
		toolResult, ok := block.(*claudecode.ToolResultBlock)
		if !ok {
			continue
		}
		call, tracked := state.toolCalls[toolResult.ToolUseID]
		if !tracked {
			continue
		}
		delete(state.toolCalls, toolResult.ToolUseID)
		if toolResult.IsError != nil && *toolResult.IsError {
			state.toolErrorCount++
			state.lastToolErrorTool = call.Name
			state.lastToolError = serveragent.CompactToolResultContent(toolResult.Content)
			continue
		}
		if r.downloader == nil {
			return artifactMaterializationError("", errors.New("runtime artifact downloader is not configured"))
		}
		if err := r.downloader.HandleToolResult(ctx, call, toolResult.Content); err != nil {
			return fmt.Errorf("handle tool result for %s: %w", call.Name, err)
		}
	}
	return nil
}

func (s *runnerStreamState) copyToResult(result *serveragent.ExecutionResult) {
	result.LogText = s.resultText
	result.ToolUseCount = s.toolUseCount
	result.ToolUseSummary = s.toolUseSummary
	result.ToolErrorCount = s.toolErrorCount
	result.LastToolErrorTool = s.lastToolErrorTool
	result.LastToolError = s.lastToolError
}

func (r *Runner) populateTerminalModelUsage(result *serveragent.ExecutionResult, message claudecode.Message) {
	serveragent.PopulateTerminalModelUsage(result, message, r.cfg.ModelUsageAliases)
}

func (r *Runner) buildSDKOptions(ctx context.Context) ([]claudecode.Option, error) {
	if err := materializeHomeTemplateFromEnvironment(); err != nil {
		return nil, fmt.Errorf("materialize runtime home template: %w", err)
	}
	extraArgs := map[string]*string{"agent": &r.cfg.AgentFlag}
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(r.cfg.MaxTurns),
		claudecode.WithCwd(runtimeCwd(r.cfg.Workspace, r.cfg.TaskType)),
		claudecode.WithUnsetEnv(serveragent.ClaudeEnvironmentKeysToUnset()...),
		claudecode.WithPermissionMode(claudecode.PermissionModeDefault),
		// Load both user and project setting sources so a CLAUDE.md in the
		// workspace (e.g., written by the desktop shell in the future) is picked up
		// by Claude Code as project memory.
		claudecode.WithSettingSources(claudecode.SettingSourceUser, claudecode.SettingSourceProject),
		serveragent.WithManagedAgentRuntimePolicy(),
		claudecode.WithExtraArgs(extraArgs),
		claudecode.WithStderrCallback(func(line string) {
			_ = r.reporter.ReportProgress(ctx, strings.TrimSpace(line))
		}),
	}
	if r.cfg.TaskType == "montage" && len(r.cfg.Env) > 0 {
		sdkOpts = append(sdkOpts, claudecode.WithEnv(withoutClaudeRuntimeEnv(r.cfg.Env)))
	}
	sdkOpts = append(sdkOpts,
		claudecode.WithEnvVar("ANBAN_API_KEY", r.cfg.APIKey),
		claudecode.WithEnvVar("ANBAN_API_URL", r.cfg.ServerURL),
	)
	if r.cfg.ServerURL != "" && r.cfg.APIKey != "" {
		sdkOpts = append(sdkOpts, serveragent.WithManagedMCPAccess(r.cfg.ServerURL, r.cfg.APIKey))
	}
	if r.cfg.ProjectID != "" {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBAN_DEFAULT_PROJECT", r.cfg.ProjectID))
	}
	pluginRoot := strings.TrimSpace(os.Getenv("CLAUDE_PLUGIN_ROOT"))
	if pluginRoot != "" {
		sdkOpts = append(sdkOpts, claudecode.WithLocalPlugin(pluginRoot))
	}
	stopHook, err := serveragent.ManagedTaskStopHook(r.cfg.TaskType, r.cfg.Workspace, pluginRoot)
	if err != nil {
		return nil, err
	}
	sdkOpts = append(sdkOpts, stopHook)
	if r.cfg.ResumeSessionID != "" {
		sdkOpts = append(sdkOpts, claudecode.WithResume(r.cfg.ResumeSessionID))
	}
	for _, key := range serveragent.ClaudeRuntimeEnvKeys() {
		if value, ok := r.cfg.RuntimeEnv[key]; ok {
			sdkOpts = append(sdkOpts, claudecode.WithEnvVar(key, value))
		}
	}
	if model.IsMontagePlatform(strings.TrimSpace(r.cfg.TaskType)) {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar(serveragent.MontageSubmoduleEnvName, montageRuntimePath(r.cfg.Workspace)))
	}
	if r.cfg.AutoMemoryDirectory != "" {
		settings, err := serveragent.BuildAutoMemorySettingsJSON(r.cfg.AutoMemoryDirectory)
		if err != nil {
			return nil, err
		}
		sdkOpts = append(sdkOpts, claudecode.WithSettings(settings))
	}
	return sdkOpts, nil
}

func withoutClaudeRuntimeEnv(configured map[string]string) map[string]string {
	excluded := make(map[string]struct{}, len(serveragent.ClaudeEnvironmentKeysToUnset()))
	for _, key := range serveragent.ClaudeEnvironmentKeysToUnset() {
		excluded[key] = struct{}{}
	}
	result := make(map[string]string, len(configured))
	for key, value := range configured {
		if _, isClaudeRuntimeEnv := excluded[key]; !isClaudeRuntimeEnv {
			result[key] = value
		}
	}
	return result
}
