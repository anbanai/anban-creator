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
	result := &serveragent.ExecutionResult{
		Success:    false,
		WorkDir:    r.cfg.Workspace,
		CostStatus: serveragent.CostStatusUnreconciled,
		CostDiagnostics: []serveragent.CostDiagnostic{{
			Code: serveragent.CostDiagnosticMissingTerminalModelUsage,
		}},
	}

	sdkOpts, err := r.buildSDKOptions(ctx)
	if err != nil {
		return result, err
	}

	var resultText string
	var toolUseCount int
	var toolErrorCount int
	var lastToolErrorTool string
	var lastToolError string
	var turnNum int
	pluginInitValidated := false
	toolCalls := make(map[string]trackedToolCall)
	toolUseSummary := make(map[string]int)

	err = claudecode.WithClient(ctx, func(client claudecode.Client) error {
		if err := serveragent.WaitForManagedMCPReady(ctx, client, r.cfg.TaskType); err != nil {
			return err
		}
		if err := client.Query(ctx, r.cfg.UserPrompt()); err != nil {
			return fmt.Errorf("send query: %w", err)
		}

		response := client.ReceiveResponse(ctx)
		defer response.Close()
		for {
			msg, receiveErr := response.Next(ctx)
			if errors.Is(receiveErr, claudecode.ErrNoMoreMessages) {
				return serveragent.ValidateManagedPluginResult(pluginInitValidated, nil)
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
					pluginInitValidated = true
				}
			case *claudecode.AssistantMessage:
				turnNum++
				for _, block := range m.Content {
					switch b := block.(type) {
					case *claudecode.TextBlock:
						text := strings.TrimSpace(b.Text)
						if text != "" {
							if resultText != "" {
								resultText += "\n"
							}
							resultText += text
							_ = r.reporter.ReportProgress(ctx, text)
						}
					case *claudecode.ToolUseBlock:
						toolUseCount++
						toolUseSummary[b.Name]++
						toolCalls[b.ToolUseID] = trackedToolCall{Name: b.Name, Input: b.Input}
						_ = r.reporter.ReportProgress(ctx, "Using tool: "+b.Name)
					case *claudecode.ToolResultBlock:
						call, ok := toolCalls[b.ToolUseID]
						if !ok {
							continue
						}
						if b.IsError != nil && *b.IsError {
							toolErrorCount++
							lastToolErrorTool = call.Name
							lastToolError = serveragent.CompactToolResultContent(b.Content)
						}
						if err := r.downloader.HandleToolResult(ctx, call, b.Content); err != nil {
							return fmt.Errorf("handle tool result for %s: %w", call.Name, err)
						}
					}
				}
			case *claudecode.ResultMessage:
				r.populateTerminalModelUsage(result, m)
				result.Success = !m.IsError
				result.ResultSubtype = m.Subtype
				result.LogText = resultText
				result.NumTurns = m.NumTurns
				result.SessionID = m.SessionID
				result.DurationMs = m.DurationMs
				result.ToolUseCount = toolUseCount
				result.ToolUseSummary = toolUseSummary
				result.ToolErrorCount = toolErrorCount
				result.LastToolErrorTool = lastToolErrorTool
				result.LastToolError = lastToolError
				if m.IsError {
					result.Error = serveragent.ResultMessageError(m, lastToolErrorTool, lastToolError)
					return errors.New(result.Error)
				}
				if err := serveragent.ValidateManagedPluginResult(pluginInitValidated, m); err != nil {
					return err
				}
				if toolUseCount == 0 {
					result.AgentLikelyFailed = true
				}
				return nil
			}
		}
	}, sdkOpts...)
	if err != nil {
		if result.Error == "" {
			result.Error = err.Error()
		}
		result.LogText = resultText
		result.ToolUseSummary = toolUseSummary
		result.ToolErrorCount = toolErrorCount
		result.LastToolErrorTool = lastToolErrorTool
		result.LastToolError = lastToolError
		return result, err
	}

	result.Success = true
	result.LogText = resultText
	result.ToolUseCount = toolUseCount
	result.ToolUseSummary = toolUseSummary
	result.ToolErrorCount = toolErrorCount
	result.LastToolErrorTool = lastToolErrorTool
	result.LastToolError = lastToolError
	if turnNum <= 1 && toolUseCount == 0 {
		result.AgentLikelyFailed = true
	}
	return result, nil
}

func (r *Runner) populateTerminalModelUsage(result *serveragent.ExecutionResult, message claudecode.Message) {
	serveragent.PopulateTerminalModelUsage(result, message, r.cfg.ModelUsageAliases)
}

func (r *Runner) buildSDKOptions(ctx context.Context) ([]claudecode.Option, error) {
	if err := materializeHomeTemplateFromEnvironment(); err != nil {
		return nil, fmt.Errorf("materialize runtime home template: %w", err)
	}
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(r.cfg.MaxTurns),
		claudecode.WithCwd(runtimeCwd(r.cfg.Workspace, r.cfg.TaskType)),
		claudecode.WithPermissionMode(claudecode.PermissionModeDefault),
		// Load both user and project setting sources so a CLAUDE.md in the
		// workspace (e.g., written by the desktop shell in the future) is picked up
		// by Claude Code as project memory.
		claudecode.WithSettingSources(claudecode.SettingSourceUser, claudecode.SettingSourceProject),
		serveragent.WithManagedAgentRuntimePolicy(),
		claudecode.WithExtraArgs(map[string]*string{
			"agent": &r.cfg.AgentFlag,
		}),
		claudecode.WithStderrCallback(func(line string) {
			_ = r.reporter.ReportProgress(ctx, strings.TrimSpace(line))
		}),
	}
	if r.cfg.TaskType == "montage" && len(r.cfg.Env) > 0 {
		sdkOpts = append(sdkOpts, claudecode.WithEnv(r.cfg.Env))
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
	if r.cfg.Model != "" {
		sdkOpts = append(sdkOpts, claudecode.WithModel(r.cfg.Model))
		if r.reporter != nil {
			_ = r.reporter.ReportProgress(ctx, fmt.Sprintf("agent model: %s", r.cfg.Model))
		}
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
