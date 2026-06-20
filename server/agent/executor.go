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
	"gopkg.in/yaml.v3"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/storage"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// filterAgentEnv returns a copy of the server-owned Claude Code environment.
// User model configuration must never be merged here; it is only for MCP
// writing/image tools. The agent may use only config.yaml's claude.model/env.
func filterAgentEnv(env map[string]string) map[string]string {
	filtered := make(map[string]string, len(env))
	for k, v := range env {
		filtered[k] = v
	}
	return filtered
}

// BuildUserPrompt constructs the user prompt for Claude Code agent execution.
// The agent definition is loaded via WithAgent() (system prompt), so the user
// message only needs to provide the topic or an autonomous execution instruction.
//
// style, when non-empty, is the effective visual style resolved at task creation
// (task > plan > channel). It is injected into the prompt as the image-gen style
// override; the agent picks this up naturally when composing image prompts and
// calls generate_image with a full prompt string. This keeps BuildAppConfig pure
// (channel-only) — task-specific overrides flow through the prompt, not config.
//
// goal, when non-empty, is prepended as a /goal slash command so Claude Code's
// built-in goal loop drives turn-by-turn evaluation inside the same session.
// The CLI registers the condition as a prompt-based Stop hook and keeps working
// across turns until a small fast model confirms the condition holds (or
// max_turns is exhausted). Empty string is a no-op.
//
// For seednote tasks, the variadic imageOpts controls image composition:
//   imageOpts[0] = hasContentImage (default false; cover+content when true)
//   imageOpts[1] = hasTailImage    (default false; adds tail when true)
// Cover is always on. Non-seednote types ignore the directive. Production callers
// (executor, agent binary) always pass two explicit bools from the task model;
// the variadic form keeps existing one-arg test calls compiling.
//
// Multi-line goal conditions are flattened to a single line (newlines →
// spaces) because Claude Code's slash command parser only registers the first
// line as the condition — anything after a newline would leak into the user
// prompt body and silently drop from evaluation.
func BuildUserPrompt(taskType, topic, agentName, style, goal, taskID, channelID string, imageOpts ...bool) string {
	var hasContentImage, hasTailImage bool
	if len(imageOpts) > 0 {
		hasContentImage = imageOpts[0]
	}
	if len(imageOpts) > 1 {
		hasTailImage = imageOpts[1]
	}
	var base string
	if topic == "" {
		base = fmt.Sprintf(
			"Use the %s agent to research and create content. "+
				"Analyze the channel profile, keywords, and historical topics "+
				"to choose the optimal theme, then execute the full creation workflow.",
			agentName)
	} else {
		base = fmt.Sprintf("Use the %s agent to create content about: %s", agentName, topic)
	}
	if style != "" {
		base += fmt.Sprintf(
			"\n\n视觉风格要求（覆盖账号默认风格，请在生成图片时遵守）：%s",
			style,
		)
	}
	if taskType == model.PlatformSeednote {
		base += "\n\n" + describeSeednoteImageComposition(hasContentImage, hasTailImage)
	}
	if taskID != "" || channelID != "" {
		parts := make([]string, 0, 2)
		if taskID != "" {
			parts = append(parts, "task_id="+taskID)
		}
		if channelID != "" {
			parts = append(parts, "channel_id="+channelID)
		}
		base += "\n\n本任务上下文：" + strings.Join(parts, ", ")
	}
	if trimmedGoal := normalizeGoalCondition(goal); trimmedGoal != "" {
		return "/goal " + trimmedGoal + "\n\n" + base
	}
	return base
}

// describeSeednoteImageComposition renders the image composition directive that
// overrides the seednote-visual-design skill's default count rules. Cover is
// always generated; content and tail are toggled by the two flags. The skill
// and its SubagentStop hook rely on image-plan.md declaring the same count.
func describeSeednoteImageComposition(hasContent, hasTail bool) string {
	parts := []string{"封面图（cover.png）"}
	if hasContent {
		parts = append(parts, "1 张内容图（image_01.png）")
	}
	if hasTail {
		parts = append(parts, "尾图（tail.png）")
	}
	total := 1 + len(parts) - 1 // cover + optional content + optional tail
	return fmt.Sprintf(
		"图片构成要求（必须严格遵守，覆盖 skill 默认数量规则）：生成 %s，共 %d 张。"+
			"image-plan.md 必须在「计划图片数量」字段写入此数字。",
		strings.Join(parts, "、"), total,
	)
}

// normalizeGoalCondition trims surrounding whitespace and collapses internal
// newlines into spaces so the condition fits on a single /goal command line.
func normalizeGoalCondition(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	lines := strings.Split(goal, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

// truncateKey returns the first 8 characters of a key for safe logging.
func truncateKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:8] + "..."
}

// loadAgentDefinition reads an agent markdown file from the plugin directory,
// parses its YAML frontmatter, and returns an SDK AgentDefinition.
func loadAgentDefinition(pluginDir, agentName string) (*claudecode.AgentDefinition, error) {
	path := filepath.Join(pluginDir, "agents", agentName+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file %s: %w", path, err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("agent file %s missing frontmatter delimiter", path)
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return nil, fmt.Errorf("agent file %s missing closing frontmatter delimiter", path)
	}
	fm := content[3 : 3+end]
	prompt := strings.TrimSpace(content[3+end+6:])

	var frontmatter struct {
		Description string   `yaml:"description"`
		Tools       []string `yaml:"tools"`
		Model       string   `yaml:"model"`
	}
	if err := yaml.Unmarshal([]byte(fm), &frontmatter); err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", path, err)
	}

	var model claudecode.AgentModel
	switch frontmatter.Model {
	case "sonnet":
		model = claudecode.AgentModelSonnet
	case "haiku":
		model = claudecode.AgentModelHaiku
	case "opus":
		model = claudecode.AgentModelOpus
	default:
		model = claudecode.AgentModelInherit
	}

	return &claudecode.AgentDefinition{
		Description: frontmatter.Description,
		Prompt:      prompt,
		Tools:       frontmatter.Tools,
		Model:       model,
	}, nil
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
	store             storage.Provider
}

// Compile-time interface check.
var _ TaskExecutor = (*LocalExecutor)(nil)

// NewLocalExecutor creates a new LocalExecutor.
func NewLocalExecutor(logger *zerolog.Logger, imageAPICfg *srvconfig.ImageAPIConfig, claudeEnv map[string]string, pluginDir string, sandbox bool, defaultModel string, keyProvider UserKeyProvider, maxTurnsOverrides map[string]int, workspaceDir string, serverBaseURL string, store storage.Provider) *LocalExecutor {
	return &LocalExecutor{
		logger:            logger,
		imageAPICfg:       imageAPICfg,
		claudeEnv:         filterAgentEnv(claudeEnv),
		pluginDir:         pluginDir,
		sandbox:           sandbox,
		defaultModel:      defaultModel,
		keyProvider:       keyProvider,
		maxTurnsOverrides: maxTurnsOverrides,
		workspaceDir:      workspaceDir,
		serverBaseURL:     serverBaseURL,
		store:             store,
	}
}

// ExecutionOptions configures a single agent execution.
type ExecutionOptions struct {
	Task          *model.Task
	Channel       *model.Channel
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
	NoOutputFiles     bool           `json:"no_output_files,omitempty"`
	AgentLikelyFailed bool           `json:"agent_likely_failed,omitempty"`
	ToolUseCount      int            `json:"tool_use_count,omitempty"`
	ToolUseSummary    map[string]int `json:"tool_use_summary,omitempty"`
	ToolErrorCount    int            `json:"tool_error_count,omitempty"`
	LastToolErrorTool string         `json:"last_tool_error_tool,omitempty"`
	LastToolError     string         `json:"last_tool_error,omitempty"`
	Model             string         `json:"model,omitempty"` // Claude Code agent model (from config.yaml claude.model)
}

// Execute runs the Claude Code agent for the given task.
//
// It creates a per-task workspace directory, writes the channel config as
// .anbanwriter/settings.json, loads the abwriter plugin with the matching
// agent definition, and launches execution via the claude-agent-sdk-go SDK.
func (e *LocalExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	// 1. Resolve defaults. Agent model comes from config.yaml,
	// with fallback to ANTHROPIC_MODEL env var for diagnostics.
	agentModel := e.defaultModel
	if agentModel == "" {
		agentModel = e.claudeEnv["ANTHROPIC_MODEL"]
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
	if agentModel != "" {
		logEvt = logEvt.Str("model", agentModel)
	}
	logEvt.Msg("starting agent execution")

	if e.pluginDir == "" {
		e.logger.Warn().
			Str("task_id", opts.Task.ID).
			Msg("plugin directory not configured; agent definition loading will fail")
	}

	// 2.5 Write task log header.
	if opts.LogWriter != nil {
		opts.LogWriter.WriteHeader(opts.Task.Type, opts.Task.Prompt, agentModel, maxTurns)
	}

	// 3. Write channel config to workspace settings.json.
	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg, opts.Task.ImageRatio, opts.Task.SkipReferenceImage, opts.Task.ReferenceImageURL)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		// Apply task-level style override at dispatch time (kept out of BuildAppConfig
		// to preserve the channel→config mapping). When a task carries its own style
		// (resolved at creation), clear the channel-level style in settings.json so the
		// agent sees a single source of truth in the user prompt.
		if opts.Task.Style != "" {
			if cfg.Seednote != nil {
				cfg.Seednote.Style = ""
			}
			cfg.Wechat.Article.Style = ""
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}

		// Download effective reference image.
		// Task-level image takes priority over channel brand image.
		// SkipReferenceImage only controls the channel brand image, not task-level.
		if opts.Task.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, workDir, opts.Task.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Str("url", opts.Task.ReferenceImageURL).
					Msg("failed to download task reference image, continuing without it")
			}
		} else if opts.Channel.ReferenceImageURL != "" && !opts.Task.SkipReferenceImage {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, workDir, opts.Channel.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Str("url", opts.Channel.ReferenceImageURL).
					Msg("failed to download reference image, continuing without it")
			}
		}
	}

	// 3.5. Resolve API key for MCP authentication.
	// Tries per-user key first, falls back to system key.
	// Fail fast if no key is available — tasks cannot complete without MCP tools.
	var mcpAPIKey string
	if e.keyProvider != nil {
		if opts.Task.UserID != "" {
			if rawKey, err := e.keyProvider.EnsureUserKey(ctx, opts.Task.UserID); err == nil {
				mcpAPIKey = rawKey
				e.logger.Info().
					Str("task_id", opts.Task.ID).
					Str("user_id", opts.Task.UserID).
					Str("key_prefix", truncateKey(rawKey)).
					Msg("using per-user API key for MCP")
			} else {
				e.logger.Error().Err(err).
					Str("task_id", opts.Task.ID).
					Msg("failed to resolve user API key, falling back to system key")
			}
		}
		if mcpAPIKey == "" {
			if rawKey, err := e.keyProvider.EnsureSystemKey(ctx); err == nil {
				mcpAPIKey = rawKey
				e.logger.Info().
					Str("task_id", opts.Task.ID).
					Str("key_prefix", truncateKey(rawKey)).
					Msg("using system API key for MCP")
			} else {
				e.logger.Error().Err(err).
					Str("task_id", opts.Task.ID).
					Msg("failed to resolve system API key for MCP authentication")
			}
		}
	} else {
		e.logger.Error().
			Str("task_id", opts.Task.ID).
			Msg("keyProvider is nil, MCP authentication is not configured")
	}

	if mcpAPIKey == "" {
		return nil, fmt.Errorf("MCP API key resolution failed for task %q (task_id=%s): no API key available. Check apiKeySvc initialization and api_keys table", opts.Task.Type, opts.Task.ID)
	}

	// 4. Map task type to agent name.
	agentName := TaskTypeToAgent(opts.Task.Type)

	// 5. Build user prompt that references the agent by name.
	channelID := ""
	if opts.Channel != nil {
		channelID = opts.Channel.ID
	}
	userPrompt := BuildUserPrompt(opts.Task.Type, opts.Task.Prompt, agentName, opts.Task.Style, opts.Task.Goal, opts.Task.ID, channelID, opts.Task.HasContentImage, opts.Task.HasTailImage)

	// Load agent definition from plugin directory and pass via WithAgent()
	// (SDK programmatic subagents) instead of --agent CLI flag lookup.
	agentDef, agentErr := loadAgentDefinition(e.pluginDir, agentName)
	if agentErr != nil {
		return nil, fmt.Errorf("load agent definition %q: %w", agentName, agentErr)
	}

	// 6. Build SDK options.
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithCwd(workDir),
		claudecode.WithPermissionMode(claudecode.PermissionModeBypassPermissions),
		claudecode.WithSettingSources(claudecode.SettingSourceUser),
		claudecode.WithAgent(agentName, *agentDef),
	}

	if e.pluginDir != "" {
		sdkOpts = append(sdkOpts, claudecode.WithLocalPlugin(e.pluginDir))
	}

	// Only set model if explicitly configured; otherwise let Claude CLI use env vars.
	if agentModel != "" {
		sdkOpts = append(sdkOpts, claudecode.WithModel(agentModel))
	}

	// Sandbox isolation (recommended in k8s).
	if e.sandbox {
		sdkOpts = append(sdkOpts, claudecode.WithSandboxEnabled(true))
		sdkOpts = append(sdkOpts, claudecode.WithAutoAllowBashIfSandboxed(true))
	}

	// Environment variables (auth tokens, API keys, etc.).
	sdkOpts = append(sdkOpts, claudecode.WithEnv(e.claudeEnv))

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

	// Inject the task's channel ID so the agent definition can use it directly
	// instead of discovering channels via list_channels (which may pick the wrong
	// one when the user has multiple channels of the same platform type).
	if opts.Channel != nil {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBANWRITER_DEFAULT_CHANNEL", opts.Channel.ID))
	}

	// Inject MCP server config directly via SDK (--mcp-config), bypassing
	// .mcp.json env var substitution. This ensures URL and API key are always
	// correct regardless of how .mcp.json resolves ${ANBANWRITER_API_URL} and
	// ${ANBANWRITER_API_KEY}.
	// Note: WithMcpServers replaces the entire map, so this must be the only call.
	mcpInjected := e.serverBaseURL != ""
	if mcpInjected {
		sdkOpts = append(sdkOpts, claudecode.WithMcpServers(map[string]claudecode.McpServerConfig{
			"anbanwriter": &claudecode.McpHTTPServerConfig{
				Type: claudecode.McpServerTypeHTTP,
				URL:  e.serverBaseURL + "/mcp",
				Headers: map[string]string{
					"Authorization": "Bearer " + mcpAPIKey,
				},
			},
		}))
	}

	// Log full MCP config snapshot for debugging.
	e.logger.Info().
		Str("task_id", opts.Task.ID).
		Str("mcp_url", e.serverBaseURL+"/mcp").
		Int("mcp_api_key_len", len(mcpAPIKey)).
		Str("mcp_api_key_prefix", truncateKey(mcpAPIKey)).
		Bool("mcp_injected_via_sdk", mcpInjected).
		Bool("plugin_dir_set", e.pluginDir != "").
		Str("plugin_dir", e.pluginDir).
		Int("env_var_count", len(e.claudeEnv)).
		Bool("env_has_anthropic_api_key", e.claudeEnv["ANTHROPIC_API_KEY"] != "").
		Bool("env_has_anthropic_base_url", e.claudeEnv["ANTHROPIC_BASE_URL"] != "").
		Bool("env_has_anthropic_model", e.claudeEnv["ANTHROPIC_MODEL"] != "").
		Str("env_anthropic_model", e.claudeEnv["ANTHROPIC_MODEL"]).
		Msg("MCP config snapshot for Claude Code subprocess")

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
	var toolErrorCount int
	var lastToolErrorTool string
	var lastToolError string
	toolUseNames := make(map[string]string)
	toolUseSummary := make(map[string]int)
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
				if m.HasError() {
					e.logger.Error().
						Str("task_id", opts.Task.ID).
						Bool("rate_limited", m.IsRateLimited()).
						Str("error", string(m.GetError())).
						Msg("assistant message error")
				}
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
						toolUseSummary[b.Name]++
						if b.ToolUseID != "" {
							toolUseNames[b.ToolUseID] = b.Name
						}
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
						contentJSON, _ := json.Marshal(b.Content)
						contentStr := string(contentJSON)
						if len(contentStr) > 1000 {
							contentStr = contentStr[:1000] + "...(truncated)"
						}
						toolName := toolUseNames[b.ToolUseID]
						if b.IsError != nil && *b.IsError {
							toolErrorCount++
							lastToolErrorTool = toolName
							lastToolError = CompactToolResultContent(b.Content)
							errEvt := e.logger.Error().
								Str("task_id", opts.Task.ID).
								Str("tool_use_id", b.ToolUseID).
								Str("tool", toolName).
								Str("error", lastToolError)
							if lastToolError == "" {
								errEvt = errEvt.Str("content", contentStr)
							}
							errEvt.Msg("claude tool result error")
						}
						if b.Content != nil {
							e.logger.Debug().
								Str("task_id", opts.Task.ID).
								Str("tool_use_id", b.ToolUseID).
								Str("tool", toolName).
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
			Success:           false,
			Error:             err.Error(),
			WorkDir:           workDir,
			ToolUseCount:      toolUseCount,
			ToolUseSummary:    toolUseSummary,
			ToolErrorCount:    toolErrorCount,
			LastToolErrorTool: lastToolErrorTool,
			LastToolError:     lastToolError,
			Model:             agentModel,
		}, nil
	}

	if execErr != nil {
		if opts.LogWriter != nil {
			opts.LogWriter.WriteError(execErr.Error())
		}
		result := &ExecutionResult{
			Success:           false,
			Error:             execErr.Error(),
			WorkDir:           workDir,
			LogText:           resultText,
			ToolUseCount:      toolUseCount,
			ToolUseSummary:    toolUseSummary,
			ToolErrorCount:    toolErrorCount,
			LastToolErrorTool: lastToolErrorTool,
			LastToolError:     lastToolError,
			Model:             agentModel,
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
		Success:           true,
		WorkDir:           workDir,
		LogText:           resultText,
		ToolUseCount:      toolUseCount,
		ToolUseSummary:    toolUseSummary,
		ToolErrorCount:    toolErrorCount,
		LastToolErrorTool: lastToolErrorTool,
		LastToolError:     lastToolError,
		Model:             agentModel,
	}
	if resultMsg != nil {
		result.NumTurns = resultMsg.NumTurns
		result.SessionID = resultMsg.SessionID
		result.DurationMs = resultMsg.DurationMs
		populateUsageFields(result, resultMsg)
	}
	return result, nil
}

// ListWorkDirFiles returns a recursive listing of files in workDir.
// When an output/ subdirectory exists, it lists that instead.
// Returns entries with "path" (relative), "size", and "is_dir", limited to 50 entries.
func ListWorkDirFiles(workDir string) ([]map[string]any, error) {
	listDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		listDir = filepath.Join(workDir, "output")
	}
	const maxEntries = 50
	var files []map[string]any
	filepath.WalkDir(listDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= maxEntries {
			return nil
		}
		rel, _ := filepath.Rel(listDir, path)
		if rel == "." {
			return nil
		}
		info, _ := d.Info()
		f := map[string]any{
			"path":   rel,
			"is_dir": d.IsDir(),
		}
		if info != nil {
			f["size"] = info.Size()
		}
		files = append(files, f)
		return nil
	})
	return files, nil
}

// ListWorkDirFilesRoot lists the workDir root (not output/) recursively.
// Used to detect files the agent may have written to the wrong location.
func ListWorkDirFilesRoot(workDir string) ([]map[string]any, error) {
	const maxEntries = 50
	var files []map[string]any
	filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= maxEntries {
			return nil
		}
		rel, _ := filepath.Rel(workDir, path)
		if rel == "." || rel == "output" {
			if d.IsDir() && rel == "output" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, _ := d.Info()
		f := map[string]any{
			"path":   rel,
			"is_dir": d.IsDir(),
		}
		if info != nil {
			f["size"] = info.Size()
		}
		files = append(files, f)
		return nil
	})
	return files, nil
}

// CountMeaningfulFiles recursively counts files in workDir, excluding
// .anbanwriter/, .claude/, and dotfiles. Prefers the output/ subdirectory
// (created by the agent via mkdir -p) to exclude agent runtime artifacts.
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

func CompactToolResultContent(content any) string {
	var text string
	switch v := content.(type) {
	case string:
		text = v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch typed := item.(type) {
			case string:
				parts = append(parts, typed)
			case map[string]any:
				if s, ok := typed["text"].(string); ok {
					parts = append(parts, s)
					continue
				}
				b, _ := json.Marshal(typed)
				if len(b) > 0 {
					parts = append(parts, string(b))
				}
			default:
				b, _ := json.Marshal(typed)
				if len(b) > 0 {
					parts = append(parts, string(b))
				}
			}
		}
		text = strings.Join(parts, " ")
	default:
		b, _ := json.Marshal(v)
		text = string(b)
	}

	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if len([]rune(text)) > 1000 {
		text = string([]rune(text)[:1000]) + "...(truncated)"
	}
	return text
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
