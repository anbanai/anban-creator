package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

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
type Executor struct {
	logger *zerolog.Logger
}

// NewExecutor creates a new Executor.
func NewExecutor(logger *zerolog.Logger) *Executor {
	return &Executor{logger: logger}
}

// ExecutionOptions configures a single agent execution.
type ExecutionOptions struct {
	Task       *model.Task
	UserConfig *model.UserConfig
	Model      string
	MaxTurns   int
	OnProgress func(taskID string, message string) // callback for SSE
}

// ExecutionResult captures the outcome of an agent execution.
type ExecutionResult struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	WorkDir   string `json:"work_dir,omitempty"`
	LogText   string `json:"log_text,omitempty"`
}

// Execute runs the Claude Code agent for the given task.
//
// It creates a per-task workspace directory, builds MCP tools from the user's
// configuration, constructs the system prompt, and launches the agent via the
// claude-agent-sdk-go SDK.
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
		Msg("starting agent execution")

	// 3. Build MCP server with user's config.
	mcpServer, err := CreateMCPTools(workDir, opts.UserConfig, e.logger)
	if err != nil {
		return nil, fmt.Errorf("create MCP tools: %w", err)
	}

	// 4. Get system prompt.
	systemPrompt := GetSystemPrompt(opts.Task.Type, opts.UserConfig)

	// 5. Build user prompt from task topic.
	userPrompt := opts.Task.Topic
	if userPrompt == "" {
		userPrompt = fmt.Sprintf("Generate a %s content.", opts.Task.Type)
	}

	// 6. Build allowed MCP tool names.
	allowedTools := []string{
		"Read", "Write", "Glob", "Grep", "Bash", "Edit",
		// Our MCP tools.
		"mcp__anbanwriter__generate_image",
		"mcp__anbanwriter__generate_cover_image",
		"mcp__anbanwriter__generate_batch_images",
		"mcp__anbanwriter__upload_image",
		"mcp__anbanwriter__compress_image",
		"mcp__anbanwriter__save_file",
		"mcp__anbanwriter__read_file",
		"mcp__anbanwriter__list_files",
		"mcp__anbanwriter__create_article_draft",
		"mcp__anbanwriter__create_xls_draft",
		"mcp__anbanwriter__get_account_info",
		"mcp__anbanwriter__validate_cover_image",
		"mcp__anbanwriter__check_draft_history",
	}

	// 7. Execute via SDK.
	var resultText string
	var execErr error

	err = claudecode.WithClient(ctx, func(client claudecode.Client) error {
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
						// Collect final text for the result.
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
					execErr = fmt.Errorf("agent execution failed (is_error)")
					return execErr
				}
				return nil
			}
		}
		return nil
	},
		claudecode.WithSystemPrompt(systemPrompt),
		claudecode.WithSdkMcpServer("anbanwriter", mcpServer),
		claudecode.WithAllowedTools(allowedTools...),
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithModel(model),
		claudecode.WithCwd(workDir),
		claudecode.WithPermissionMode(claudecode.PermissionModeBypassPermissions),
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
			"name":  e.Name(),
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
