package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// DefaultMaxTurns returns the default max turns for a given task type.
func DefaultMaxTurns(taskType string) int {
	switch taskType {
	case model.ScopeArticle:
		return 50
	case model.ScopeXls:
		return 25
	case model.ScopeRednote:
		return 20
	default:
		return 20
	}
}

// DefaultModel returns the default Claude model for agent execution.
func DefaultModel() string {
	return "claude-sonnet-4-6"
}

// Executor orchestrates Claude Code agent execution for content generation tasks.
// It loads the anbanwriter plugin (agents + skills) and invokes the appropriate
// agent based on task type.
type Executor struct {
	logger      *zerolog.Logger
	imageAPICfg *srvconfig.ImageAPIConfig
	claudeEnv   map[string]string
	pluginDir   string // path to the anbanwriter plugin directory
	sandbox     bool   // enable sandbox isolation (recommended in k8s)
}

// NewExecutor creates a new Executor.
func NewExecutor(logger *zerolog.Logger, imageAPICfg *srvconfig.ImageAPIConfig, claudeEnv map[string]string, pluginDir string, sandbox bool) *Executor {
	return &Executor{
		logger:      logger,
		imageAPICfg: imageAPICfg,
		claudeEnv:   claudeEnv,
		pluginDir:   pluginDir,
		sandbox:     sandbox,
	}
}

// ExecutionOptions configures a single agent execution.
type ExecutionOptions struct {
	Task       *model.Task
	Channel    *model.Channel
	Model      string
	MaxTurns   int
	OnProgress func(taskID string, message string) // callback for SSE
}

// ExecutionResult captures the outcome of an agent execution.
type ExecutionResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
	LogText string `json:"log_text,omitempty"`
}

// Execute runs the Claude Code agent for the given task.
//
// It creates a per-task workspace directory, writes the channel config as
// .anbanwriter/settings.json, loads the anbanwriter plugin with the matching
// agent definition, and launches execution via the claude-agent-sdk-go SDK.
func (e *Executor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	// 1. Resolve defaults.
	model := opts.Model
	if model == "" {
		model = DefaultModel()
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type)
	}

	// 2. Create workspace directory.
	workDir := filepath.Join(os.TempDir(), "anbanwriter", opts.Task.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	e.logger.Info().
		Str("task_id", opts.Task.ID).
		Str("type", opts.Task.Type).
		Str("topic", opts.Task.Topic).
		Str("model", model).
		Int("max_turns", maxTurns).
		Str("work_dir", workDir).
		Str("plugin_dir", e.pluginDir).
		Bool("sandbox", e.sandbox).
		Msg("starting agent execution")

	// 3. Write channel config to workspace settings.json.
	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}
	}

	// 4. Build user prompt from task topic.
	userPrompt := opts.Task.Topic
	if userPrompt == "" {
		userPrompt = fmt.Sprintf("Generate a %s content.", opts.Task.Type)
	}

	// 5. Map task type to agent name and build --agent flag.
	agentName := taskTypeToAgent(opts.Task.Type)
	agentFlag := "anbanwriter:" + agentName

	// 6. Build SDK options.
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithModel(model),
		claudecode.WithCwd(workDir),
		claudecode.WithPermissionMode(claudecode.PermissionModeBypassPermissions),
		claudecode.WithExtraArgs(map[string]*string{
			"agent": &agentFlag,
		}),
	}

	// Load the anbanwriter plugin (agents + skills).
	if e.pluginDir != "" {
		sdkOpts = append(sdkOpts, claudecode.WithLocalPlugin(e.pluginDir))
	}

	// Sandbox isolation (recommended in k8s).
	if e.sandbox {
		sdkOpts = append(sdkOpts, claudecode.WithSandboxEnabled(true))
		sdkOpts = append(sdkOpts, claudecode.WithAutoAllowBashIfSandboxed(true))
	}

	// Environment variables (auth tokens, API keys, etc.).
	for k, v := range e.claudeEnv {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar(k, v))
	}

	// 7. Execute via SDK with streaming.
	var resultText string
	var execErr error

	err := claudecode.WithClient(ctx, func(client claudecode.Client) error {
		if err := client.Query(ctx, userPrompt); err != nil {
			return fmt.Errorf("send query: %w", err)
		}

		for msg := range client.ReceiveMessages(ctx) {
			switch m := msg.(type) {
			case *claudecode.AssistantMessage:
				for _, block := range m.Content {
					switch b := block.(type) {
					case *claudecode.TextBlock:
						if opts.OnProgress != nil {
							opts.OnProgress(opts.Task.ID, b.Text)
						}
						if len(resultText) > 0 {
							resultText += "\n"
						}
						resultText += b.Text
					case *claudecode.ToolUseBlock:
						if opts.OnProgress != nil {
							e.logger.Debug().
								Str("task_id", opts.Task.ID).
								Str("tool", b.Name).
								Msg("agent tool use")
							opts.OnProgress(opts.Task.ID, fmt.Sprintf("Using tool: %s", b.Name))
						}
					}
				}
			case *claudecode.ResultMessage:
				if m.IsError {
					errMsg := "unknown error"
					if m.Result != nil {
						errMsg = *m.Result
					}
					e.logger.Error().
						Str("task_id", opts.Task.ID).
						Str("subtype", m.Subtype).
						Int("duration_ms", m.DurationMs).
						Int("num_turns", m.NumTurns).
						Str("session_id", m.SessionID).
						Str("result", errMsg).
						Msg("agent returned error result")
					execErr = fmt.Errorf("agent execution failed: %s", errMsg)
					return execErr
				}
				return nil
			}
		}
		return nil
	},
		sdkOpts...,
	)

	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   err.Error(),
			WorkDir: workDir,
		}, nil
	}

	if execErr != nil {
		return &ExecutionResult{
			Success: false,
			Error:   execErr.Error(),
			WorkDir: workDir,
			LogText: resultText,
		}, nil
	}

	return &ExecutionResult{
		Success: true,
		WorkDir: workDir,
		LogText: resultText,
	}, nil
}

// ListWorkDirFiles returns a summary of files in a task's work directory.
func ListWorkDirFiles(workDir string) ([]map[string]any, error) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return nil, err
	}
	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		f := map[string]any{
			"name":   e.Name(),
			"is_dir": e.IsDir(),
		}
		if info != nil {
			f["size"] = info.Size()
		}
		files = append(files, f)
	}
	return files, nil
}

// MarshalResultJSON serializes an ExecutionResult to JSON.
func MarshalResultJSON(r *ExecutionResult) (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
