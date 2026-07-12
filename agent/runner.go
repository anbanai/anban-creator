package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	serveragent "github.com/anbanai/anban-creator/server/agent"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

type Runner struct {
	cfg        *Config
	reporter   *Reporter
	downloader *Downloader
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
		Success: false,
		WorkDir: r.cfg.Workspace,
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
	toolCalls := make(map[string]trackedToolCall)
	toolUseSummary := make(map[string]int)

	err = claudecode.WithClient(ctx, func(client claudecode.Client) error {
		if err := client.Query(ctx, r.cfg.UserPrompt()); err != nil {
			return fmt.Errorf("send query: %w", err)
		}

		for msg := range client.ReceiveMessages(ctx) {
			switch m := msg.(type) {
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
				result.Success = !m.IsError
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
					if m.Result != nil {
						result.Error = *m.Result
					} else if lastToolError != "" {
						if lastToolErrorTool != "" {
							result.Error = "last tool error from " + lastToolErrorTool + ": " + lastToolError
						} else {
							result.Error = "last tool error: " + lastToolError
						}
					} else {
						result.Error = "unknown agent error"
					}
					serveragent.PopulateUsageFields(result, m)
					return errors.New(result.Error)
				}
				serveragent.PopulateUsageFields(result, m)
				if toolUseCount == 0 {
					result.AgentLikelyFailed = true
				}
				return nil
			}
		}
		return nil
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

func (r *Runner) buildSDKOptions(ctx context.Context) ([]claudecode.Option, error) {
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(r.cfg.MaxTurns),
		claudecode.WithCwd(r.cfg.Workspace),
		claudecode.WithPermissionMode(claudecode.PermissionModeBypassPermissions),
		// Load both user and project setting sources so a CLAUDE.md in the
		// workspace (e.g., written by the desktop shell in the future) is picked up
		// by Claude Code as project memory.
		claudecode.WithSettingSources(claudecode.SettingSourceUser, claudecode.SettingSourceProject),
		serveragent.WithManagedAgentRuntimePolicy(),
		claudecode.WithExtraArgs(map[string]*string{
			"agent": &r.cfg.AgentFlag,
		}),
		claudecode.WithEnvVar("ANBAN_API_KEY", r.cfg.APIKey),
		claudecode.WithEnvVar("ANBAN_API_URL", r.cfg.ServerURL),
		claudecode.WithStderrCallback(func(line string) {
			_ = r.reporter.ReportProgress(ctx, strings.TrimSpace(line))
		}),
	}
	if pluginRoot := strings.TrimSpace(os.Getenv("CLAUDE_PLUGIN_ROOT")); pluginRoot != "" {
		sdkOpts = append(sdkOpts, claudecode.WithLocalPlugin(pluginRoot))
	}
	for key, value := range montageProviderEnvFromProcess(r.cfg.TaskType) {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar(key, value))
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
