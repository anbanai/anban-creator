package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// BuildUserPrompt constructs the user prompt with a command prefix based on task type.
// This ensures the model receives an explicit command (e.g., "/rednote topic")
// instead of a raw topic that could be misinterpreted as a Q&A question.
func BuildUserPrompt(taskType, topic string) string {
	if topic == "" {
		return fmt.Sprintf("/%s", taskType)
	}
	switch taskType {
	case "rednote":
		return fmt.Sprintf("/rednote %s", topic)
	case "article":
		return fmt.Sprintf("/article %s", topic)
	case "xls":
		return fmt.Sprintf("/xls %s", topic)
	default:
		return topic
	}
}

// DefaultMaxTurns returns the max turns for a given task type from the config map.
// Falls back to 40 if the task type is not configured.
func DefaultMaxTurns(taskType string, maxTurns map[string]int) int {
	if v, ok := maxTurns[taskType]; ok && v > 0 {
		return v
	}
	return 40
}

// LocalExecutor runs agent tasks as local Claude CLI subprocesses via the SDK.
type LocalExecutor struct {
	logger            *zerolog.Logger
	imageAPICfg       *srvconfig.ImageAPIConfig
	claudeEnv         map[string]string
	pluginDir         string
	sandbox           bool
	defaultModel      string // configured model; empty means use env vars
	keyProvider       UserKeyProvider
	maxTurnsOverrides map[string]int
	workspaceDir      string
	serverBaseURL     string // server base URL for MCP (e.g. "http://localhost:8080")
}

// Compile-time interface check.
var _ TaskExecutor = (*LocalExecutor)(nil)

// NewLocalExecutor creates a new LocalExecutor.
func NewLocalExecutor(logger *zerolog.Logger, imageAPICfg *srvconfig.ImageAPIConfig, claudeEnv map[string]string, pluginDir string, sandbox bool, defaultModel string, keyProvider UserKeyProvider, maxTurnsOverrides map[string]int, workspaceDir string, serverBaseURL string) *LocalExecutor {
	return &LocalExecutor{
		logger:            logger,
		imageAPICfg:       imageAPICfg,
		claudeEnv:         claudeEnv,
		pluginDir:         pluginDir,
		sandbox:           sandbox,
		defaultModel:      defaultModel,
		keyProvider:       keyProvider,
		maxTurnsOverrides: maxTurnsOverrides,
		workspaceDir:      workspaceDir,
		serverBaseURL:     serverBaseURL,
	}
}

// ExecutionOptions configures a single agent execution.
type ExecutionOptions struct {
	Task          *model.Task
	Channel       *model.Channel
	Model         string
	MaxTurns      int
	OnProgress    func(taskID string, message string) // callback for SSE
	HeartbeatFunc func(taskID string)                 // periodic heartbeat for stuck-task detection
	LogWriter     *TaskLogWriter                      // optional per-task log file writer; nil = no log file
}

// TokenUsage captures LLM token consumption for a task execution.
type TokenUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// ExecutionResult captures the outcome of an agent execution.
type ExecutionResult struct {
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	WorkDir    string `json:"work_dir,omitempty"`
	LogText    string `json:"log_text,omitempty"`
	NumTurns   int    `json:"num_turns,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	DurationMs int    `json:"duration_ms,omitempty"`

	// LLM usage metrics (populated from SDK ResultMessage).
	DurationAPIMs int         `json:"duration_api_ms,omitempty"`
	TotalCostUSD  *float64    `json:"total_cost_usd,omitempty"`
	TokenUsage    *TokenUsage `json:"token_usage,omitempty"`

	// Post-execution diagnostics.
	NoOutputFiles     bool `json:"no_output_files,omitempty"`
	AgentLikelyFailed bool `json:"agent_likely_failed,omitempty"`
}

// Execute runs the Claude Code agent for the given task.
//
// It creates a per-task workspace directory, writes the channel config as
// .anbanwriter/settings.json, loads the abwriter plugin with the matching
// agent definition, and launches execution via the claude-agent-sdk-go SDK.
func (e *LocalExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	// 1. Resolve defaults.
	model := opts.Model
	if model == "" {
		model = e.defaultModel
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type, e.maxTurnsOverrides)
	}

	// 2. Create workspace directory.
	var workDir string
	if e.workspaceDir != "" {
		workDir = filepath.Join(e.workspaceDir, opts.Task.ID)
	} else {
		workDir = filepath.Join(os.TempDir(), "abwriter", opts.Task.ID)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	// Write task context file so the agent knows its task ID.
	// Unlike DockerExecutor (which passes --task-id to the binary),
	// LocalExecutor runs a Claude Code CLI subprocess that has no way
	// to know the task UUID. Without this file, the LLM may pass a
	// made-up ID to prepare_workspace, causing a path mismatch.
	taskContext := fmt.Sprintf("TASK_ID=%s\n", opts.Task.ID)
	if err := os.WriteFile(filepath.Join(workDir, ".task-context"), []byte(taskContext), 0644); err != nil {
		return nil, fmt.Errorf("write task context: %w", err)
	}

	logEvt := e.logger.Info().
		Str("task_id", opts.Task.ID).
		Str("type", opts.Task.Type).
		Str("topic", opts.Task.Prompt).
		Int("max_turns", maxTurns).
		Str("work_dir", workDir).
		Str("plugin_dir", e.pluginDir).
		Bool("sandbox", e.sandbox)
	if model != "" {
		logEvt = logEvt.Str("model", model)
	}
	logEvt.Msg("starting agent execution")

	if e.pluginDir == "" {
		e.logger.Warn().
			Str("task_id", opts.Task.ID).
			Msg("plugin directory not found; --agent flag may not resolve")
	}

	// 2.5 Write task log header.
	if opts.LogWriter != nil {
		opts.LogWriter.WriteHeader(opts.Task.Type, opts.Task.Prompt, model, maxTurns)
	}

	// 3. Write channel config to workspace settings.json.
	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg, opts.Task.ImageRatio)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}

		// Download brand reference image if configured.
		if opts.Channel.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, workDir, opts.Channel.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Str("url", opts.Channel.ReferenceImageURL).
					Msg("failed to download reference image, continuing without it")
			}
		}
	}

	// 3.5. Resolve API key for MCP authentication.
	// Tries per-user key first, falls back to system key.
	// Unlike DockerExecutor, failures are non-fatal here: local execution
	// can still produce output via direct tool calls even without MCP.
	var mcpAPIKey string
	if e.keyProvider != nil {
		if opts.Task.UserID != "" {
			if rawKey, err := e.keyProvider.EnsureUserKey(ctx, opts.Task.UserID); err == nil {
				mcpAPIKey = rawKey
				e.logger.Info().
					Str("task_id", opts.Task.ID).
					Str("user_id", opts.Task.UserID).
					Msg("using per-user API key for MCP")
			} else {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Msg("failed to resolve user API key, falling back to system key")
			}
		}
		if mcpAPIKey == "" {
			if rawKey, err := e.keyProvider.EnsureSystemKey(ctx); err == nil {
				mcpAPIKey = rawKey
				e.logger.Info().
					Str("task_id", opts.Task.ID).
					Msg("using system API key for MCP")
			} else {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Msg("failed to resolve system API key, MCP will not be available")
			}
		}
	}

	// 4. Build user prompt from task topic with command prefix.
	userPrompt := BuildUserPrompt(opts.Task.Type, opts.Task.Prompt)

	// 5. Map task type to agent name and build --agent flag.
	agentName := TaskTypeToAgent(opts.Task.Type)
	agentFlag := "anbanwriter:" + agentName

	// 6. Build SDK options.
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithCwd(workDir),
		claudecode.WithPermissionMode(claudecode.PermissionModeBypassPermissions),
		claudecode.WithSettingSources(claudecode.SettingSourceUser),
		claudecode.WithExtraArgs(map[string]*string{
			"agent": &agentFlag,
		}),
	}

	if e.pluginDir != "" {
		sdkOpts = append(sdkOpts, claudecode.WithLocalPlugin(e.pluginDir))
	}

	// Only set model if explicitly configured; otherwise let Claude CLI use env vars.
	if model != "" {
		sdkOpts = append(sdkOpts, claudecode.WithModel(model))
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

	// Inject MCP server API key so plugin/.mcp.json can resolve
	// ${ANBANWRITER_API_KEY} for the anbanwriter MCP server.
	if mcpAPIKey != "" {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBANWRITER_API_KEY", mcpAPIKey))
	}
	// Inject MCP server base URL so plugin/.mcp.json can resolve
	// ${ANBANWRITER_API_URL} for the anbanwriter MCP server.
	if e.serverBaseURL != "" {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBANWRITER_API_URL", e.serverBaseURL))
	}

	// 7. Execute via SDK with streaming.
	// Start heartbeat goroutine for stuck-task detection.
	if opts.HeartbeatFunc != nil {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					opts.HeartbeatFunc(opts.Task.ID)
				}
			}
		}()
	}

	var resultText string
	var execErr error
	var toolUseCount int
	var turnNum int
	var resultMsg *claudecode.ResultMessage

	// Capture CLI stderr for diagnostics.
	sdkOpts = append(sdkOpts, claudecode.WithStderrCallback(func(line string) {
		e.logger.Debug().
			Str("task_id", opts.Task.ID).
			Str("cli_stderr", line).
			Msg("cli stderr")
		if opts.LogWriter != nil {
			opts.LogWriter.WriteStderr(line)
		}
	}))

	err := claudecode.WithClient(ctx, func(client claudecode.Client) error {
		if err := client.Query(ctx, userPrompt); err != nil {
			return fmt.Errorf("send query: %w", err)
		}

		for msg := range client.ReceiveMessages(ctx) {
			switch m := msg.(type) {
			case *claudecode.AssistantMessage:
				turnNum++
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
						text := b.Text
						if len(text) > 500 {
							text = text[:500] + "...(truncated)"
						}
						e.logger.Debug().
							Str("task_id", opts.Task.ID).
							Str("text", text).
							Msg("claude assistant text")
						if opts.LogWriter != nil {
							opts.LogWriter.WriteAssistantText(turnNum, b.Text)
						}
					case *claudecode.ToolUseBlock:
						toolUseCount++
						if opts.OnProgress != nil {
							opts.OnProgress(opts.Task.ID, fmt.Sprintf("Using tool: %s", b.Name))
						}
						inputJSON, _ := json.Marshal(b.Input)
						inputStr := string(inputJSON)
						if len(inputStr) > 1000 {
							inputStr = inputStr[:1000] + "...(truncated)"
						}
						e.logger.Debug().
							Str("task_id", opts.Task.ID).
							Str("tool", b.Name).
							Str("input", inputStr).
							Msg("claude tool use")
						if opts.LogWriter != nil {
							opts.LogWriter.WriteToolUse(turnNum, b.Name, string(inputJSON))
						}
					case *claudecode.ToolResultBlock:
						if b.Content != nil {
							contentJSON, _ := json.Marshal(b.Content)
							contentStr := string(contentJSON)
							if len(contentStr) > 1000 {
								contentStr = contentStr[:1000] + "...(truncated)"
							}
							e.logger.Debug().
								Str("task_id", opts.Task.ID).
								Str("tool_use_id", b.ToolUseID).
								Str("content", contentStr).
								Msg("claude tool result")
							if opts.LogWriter != nil {
								opts.LogWriter.WriteToolResult(b.ToolUseID, string(contentJSON))
							}
						}
					}
				}
			case *claudecode.ResultMessage:
				resultMsg = m
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
				// Log successful execution summary.
				completeEvt := e.logger.Info().
					Str("task_id", opts.Task.ID).
					Str("subtype", m.Subtype).
					Int("duration_ms", m.DurationMs).
					Int("num_turns", m.NumTurns).
					Int("tool_use_count", toolUseCount).
					Str("session_id", m.SessionID)
				if m.TotalCostUSD != nil {
					completeEvt = completeEvt.Float64("cost_usd", *m.TotalCostUSD)
				}
				if m.Usage != nil {
					tu := ParseTokenUsage(*m.Usage)
					if tu != nil {
						completeEvt = completeEvt.
							Int("input_tokens", tu.InputTokens).
							Int("output_tokens", tu.OutputTokens)
					}
				}
				completeEvt.Msg("agent execution completed")
				if opts.LogWriter != nil {
					var tokenUsage *TokenUsage
					if m.Usage != nil {
						tokenUsage = ParseTokenUsage(*m.Usage)
					}
					opts.LogWriter.WriteResult(true, m.DurationMs, m.NumTurns, m.TotalCostUSD, tokenUsage)
				}
				// Warn if agent produced no tool calls - likely agent definition not loaded.
				if toolUseCount == 0 {
					truncated := resultText
					if len(truncated) > 500 {
						truncated = truncated[:500] + "..."
					}
					e.logger.Warn().
						Str("task_id", opts.Task.ID).
						Str("result_text", truncated).
						Msg("agent completed with zero tool uses; agent definition may not have loaded")
				}
				return nil
			}
		}
		return nil
	},
		sdkOpts...,
	)

	if err != nil {
		if opts.LogWriter != nil {
			opts.LogWriter.WriteError(err.Error())
			opts.LogWriter.WriteResult(false, 0, 0, nil, nil)
		}
		return &ExecutionResult{
			Success: false,
			Error:   err.Error(),
			WorkDir: workDir,
		}, nil
	}

	if execErr != nil {
		if opts.LogWriter != nil {
			opts.LogWriter.WriteError(execErr.Error())
		}
		result := &ExecutionResult{
			Success: false,
			Error:   execErr.Error(),
			WorkDir: workDir,
			LogText: resultText,
		}
		if resultMsg != nil {
			result.NumTurns = resultMsg.NumTurns
			result.SessionID = resultMsg.SessionID
			result.DurationMs = resultMsg.DurationMs
			populateUsageFields(result, resultMsg)
		}
		return result, nil
	}

	// Log workspace contents for diagnostics.
	if files, listErr := ListWorkDirFiles(workDir); listErr == nil {
		e.logger.Info().
			Str("task_id", opts.Task.ID).
			Int("file_count", len(files)).
			Interface("files", files).
			Msg("workspace contents after execution")
	}

	result := &ExecutionResult{
		Success: true,
		WorkDir: workDir,
		LogText: resultText,
	}
	if resultMsg != nil {
		result.NumTurns = resultMsg.NumTurns
		result.SessionID = resultMsg.SessionID
		result.DurationMs = resultMsg.DurationMs
		populateUsageFields(result, resultMsg)
	}
	return result, nil
}

// ListWorkDirFiles returns a summary of files in a task's work directory.
// When an output/ subdirectory exists (created by prepare_workspace), lists
// its contents for meaningful diagnostics.
func ListWorkDirFiles(workDir string) ([]map[string]any, error) {
	listDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		listDir = filepath.Join(workDir, "output")
	}
	entries, err := os.ReadDir(listDir)
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

// CountMeaningfulFiles recursively counts files in workDir, excluding
// .anbanwriter/, .claude/, and dotfiles. Prefers the output/ subdirectory
// (created by prepare_workspace) to exclude agent runtime artifacts.
// Returns the count of actual files (not directories) at any nesting depth.
func CountMeaningfulFiles(workDir string) int {
	scanDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		scanDir = filepath.Join(workDir, "output")
	}

	count := 0
	filepath.WalkDir(scanDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".anbanwriter", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		count++
		return nil
	})
	return count
}

// MarshalResultJSON serializes an ExecutionResult to JSON.
func MarshalResultJSON(r *ExecutionResult) (string, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ParseTokenUsage extracts token counts from the SDK's raw usage map.
// Returns nil if no meaningful data is found.
func ParseTokenUsage(m map[string]any) *TokenUsage {
	tu := &TokenUsage{
		InputTokens:         jsonInt(m, "input_tokens"),
		OutputTokens:        jsonInt(m, "output_tokens"),
		CacheReadTokens:     jsonInt(m, "cache_read_input_tokens"),
		CacheCreationTokens: jsonInt(m, "cache_creation_input_tokens"),
	}
	if tu.InputTokens == 0 && tu.OutputTokens == 0 {
		return nil
	}
	return tu
}

// jsonInt safely extracts an int from a map[string]any,
// handling both float64 (JSON numbers) and int types.
func jsonInt(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// populateUsageFields fills the cost/token fields of an ExecutionResult
// from a SDK ResultMessage.
func populateUsageFields(result *ExecutionResult, resultMsg *claudecode.ResultMessage) {
	if resultMsg == nil {
		return
	}
	result.DurationAPIMs = resultMsg.DurationAPIMs
	result.TotalCostUSD = resultMsg.TotalCostUSD
	if resultMsg.Usage != nil {
		result.TokenUsage = ParseTokenUsage(*resultMsg.Usage)
	}
}

// PopulateUsageFields fills the cost/token fields of an ExecutionResult from a SDK ResultMessage.
func PopulateUsageFields(result *ExecutionResult, resultMsg *claudecode.ResultMessage) {
	populateUsageFields(result, resultMsg)
}
