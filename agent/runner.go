package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	serveragent "github.com/royalrick/anbanwriter/server/agent"

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

	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(r.cfg.MaxTurns),
		claudecode.WithCwd(r.cfg.Workspace),
		claudecode.WithPermissionMode(claudecode.PermissionModeBypassPermissions),
		claudecode.WithSettingSources(claudecode.SettingSourceUser),
		claudecode.WithLocalPlugin("/anbanai"),
		claudecode.WithExtraArgs(map[string]*string{
			"agent": &r.cfg.AgentFlag,
		}),
		claudecode.WithEnvVar("ANBANWRITER_API_KEY", r.cfg.APIKey),
		claudecode.WithEnvVar("ANBANWRITER_API_URL", r.cfg.ServerURL),
		claudecode.WithStderrCallback(func(line string) {
			_ = r.reporter.ReportProgress(ctx, strings.TrimSpace(line))
		}),
	}
	if r.cfg.Model != "" {
		sdkOpts = append(sdkOpts, claudecode.WithModel(r.cfg.Model))
		_ = r.reporter.ReportProgress(ctx, fmt.Sprintf("agent model: %s", r.cfg.Model))
	}

	var resultText string
	var toolUseCount int
	var turnNum int
	toolCalls := make(map[string]trackedToolCall)

	err := claudecode.WithClient(ctx, func(client claudecode.Client) error {
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
						toolCalls[b.ToolUseID] = trackedToolCall{Name: b.Name, Input: b.Input}
						_ = r.reporter.ReportProgress(ctx, "Using tool: "+b.Name)
					case *claudecode.ToolResultBlock:
						call, ok := toolCalls[b.ToolUseID]
						if !ok {
							continue
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
				if m.IsError {
					if m.Result != nil {
						result.Error = *m.Result
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
		return result, err
	}

	result.Success = true
	result.LogText = resultText
	result.ToolUseCount = toolUseCount
	if turnNum <= 1 && toolUseCount == 0 {
		result.AgentLikelyFailed = true
	}
	return result, nil
}
