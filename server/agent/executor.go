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

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resolver"
	"github.com/anbanai/anban-creator/server/storage"

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

func isManagedContainerEnv(key string) bool {
	switch key {
	case "HOME", "PATH":
		return true
	default:
		return false
	}
}

func montageSubmoduleRuntimePath(pluginDir string) string {
	if strings.TrimSpace(pluginDir) != "" {
		if absPluginDir, err := filepath.Abs(pluginDir); err == nil {
			candidate := filepath.Join(filepath.Dir(absPluginDir), "third_party", "OpenMontage")
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate
			}
		}
	}
	return ContainerMontageSubmodulePath
}

// UserPromptParams holds the inputs for BuildUserPrompt. Struct keeps call sites
// readable as fields are added and prevents argument-order bugs.
//
// The prompt is BEHAVIORAL ONLY (task type + topic, task/project ids, compact
// runtime controls, goal condition). The visual / writer / author / theme
// dimensions never enter the prompt — they are single-sourced via
// resolver.ResolveStyle and delivered to the agent through two purposeful
// projections: get_project_profile (MCP, for agent reasoning) and settings.json
// (for the app library). Keeping them out of the prompt is what lets the three
// channels never disagree (P2 contract).
type UserPromptParams struct {
	TaskType  string // model.PlatformArticle / model.PlatformSeednote / model.PlatformMoments / ...
	Topic     string // user prompt; empty triggers autonomous research mode
	Goal      string // goal-mode condition; empty = no /goal prefix
	TaskID    string // injected as task_id=<x> into the prompt body
	ProjectID string // injected as project_id=<x> into the prompt body
	// HasContentImage / HasTailImage toggle seednote image composition. Cover is
	// always generated. Ignored for non-seednote task types.
	HasContentImage bool
	HasTailImage    bool
	// ArticleWithCover / ArticleWithContentImages toggle 公众号 article image
	// generation independently (article cover is NOT mandatory, unlike seednote).
	// nil defaults true for legacy tasks/callers. Ignored for non-article task types.
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
}

// BuildUserPrompt constructs the user prompt for Claude Code agent execution.
// The plugin agent runs as the main Claude Code session via --agent, so the user
// message only needs to provide the topic or an autonomous execution instruction.
//
// The prompt is behavioral only — it never carries the visual / writer / author / theme
// style dimensions. Those are resolved once from the task's frozen project
// snapshot, with a legacy override fallback for old rows, and read by the agent
// from get_project_profile(task_id); the app-library projection lands in
// settings.json. Injecting them here too would create a second, divergent copy
// of the same values.
//
// p.Goal, when non-empty, is prepended as a /goal slash command so Claude Code's
// built-in goal loop drives turn-by-turn evaluation inside the same session.
// The CLI registers the condition as a prompt-based Stop hook and keeps working
// across turns until a small fast model confirms the condition holds (or
// max_turns is exhausted). Empty string is a no-op.
//
// For seednote tasks, p.HasContentImage / p.HasTailImage are emitted as a
// structured seednote_image_mode runtime control (cover always generated).
// Non-seednote types ignore it.
//
// For article tasks, p.ArticleWithCover / p.ArticleWithContentImages are emitted
// as a structured article_image_mode runtime control. The agent and skills own
// detailed workflow semantics for each mode; Go only passes state.
//
// Multi-line goal conditions are flattened to a single line (newlines →
// spaces) because Claude Code's slash command parser only registers the first
// line as the condition — anything after a newline would leak into the user
// prompt body and silently drop from evaluation.
func BuildUserPrompt(p UserPromptParams) string {
	var base string
	if p.Topic == "" {
		base = fmt.Sprintf(
			"Run the full %s creation workflow. "+
				"Analyze the project profile, keywords, and historical topics "+
				"to choose the optimal theme, then execute the full creation workflow.",
			p.TaskType)
	} else {
		base = fmt.Sprintf("Run the full %s creation workflow; create content about: %s", p.TaskType, p.Topic)
	}
	if controls := describeRuntimeControls(p); controls != "" {
		base += "\n\n" + controls
	}
	if p.TaskID != "" || p.ProjectID != "" {
		parts := make([]string, 0, 2)
		if p.TaskID != "" {
			parts = append(parts, "task_id="+p.TaskID)
		}
		if p.ProjectID != "" {
			parts = append(parts, "project_id="+p.ProjectID)
		}
		base += "\n\n本任务上下文：" + strings.Join(parts, ", ")
	}
	if trimmedGoal := normalizeGoalCondition(p.Goal); trimmedGoal != "" {
		return "/goal " + trimmedGoal + "\n\n" + base
	}
	return base
}

// AppendResumeContextToPrompt asks the agent to continue from supplemental
// operator input when a resumed task wrote .anban-creator/resume/latest.md.
func AppendResumeContextToPrompt(prompt, workDir string) string {
	if strings.TrimSpace(workDir) == "" {
		return prompt
	}
	resumePath := filepath.Join(workDir, appconfig.ConfigDir, "resume", "latest.md")
	if info, err := os.Stat(resumePath); err != nil || info.IsDir() {
		return prompt
	}
	return prompt + "\n\n继续执行模式：\n" +
		"- 这是一个基于原任务工作目录的继续执行，不是全新任务。\n" +
		"- 请先读取 `.anban-creator/resume/latest.md`，理解用户补充指令、补充文件说明和附件相对路径。\n" +
		"- 基于当前工作目录已有草稿、素材和产物继续完成任务；不要清空、删除或整体覆盖已有产物，除非补充指令明确要求替换。\n" +
		"- 如果补充文件中存在同名或相近用途文件，优先按 latest.md 中的文件说明区分使用。"
}

// describeRuntimeControls emits compact, machine-readable controls for agents.
// Detailed semantics live in claudecode agents/skills and docs/plugin-development.md so server code
// does not duplicate workflow prose.
func describeRuntimeControls(p UserPromptParams) string {
	var controls []string
	switch p.TaskType {
	case model.PlatformSeednote:
		controls = append(controls, "seednote_image_mode="+seednoteImageMode(p.HasContentImage, p.HasTailImage))
	case model.PlatformArticle:
		controls = append(controls, "article_image_mode="+articleImageMode(defaultTrue(p.ArticleWithCover), defaultTrue(p.ArticleWithContentImages)))
	}
	if len(controls) == 0 {
		return ""
	}
	return "运行控制：\n- " + strings.Join(controls, "\n- ")
}

func defaultTrue(v *bool) bool {
	return v == nil || *v
}

func seednoteImageMode(hasContent, hasTail bool) string {
	switch {
	case hasContent && hasTail:
		return "full"
	case hasContent:
		return "cover_content"
	case hasTail:
		return "cover_tail"
	default:
		return "cover_only"
	}
}

func articleImageMode(withCover, withContent bool) string {
	switch {
	case withCover && withContent:
		return "cover_and_content"
	case withCover && !withContent:
		return "cover_only"
	case !withCover && withContent:
		return "content_only"
	default:
		return "text_only"
	}
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

	// NOTE: only description/tools/model are parsed. The agent frontmatter's
	// `maxTurns` is intentionally NOT parsed here — the SDK AgentDefinition has
	// no MaxTurns field, so it cannot be forwarded via WithAgent(). The
	// production turn budget is governed solely by the server's per-task-type
	// `max_turns` config (applied via WithMaxTurns in the Execute path). The frontmatter
	// maxTurns only affects interactive Claude Code runs where the CLI parses
	// it natively. Keep the two values in sync (config.yaml + agent .md).
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
	memoryMgr         *projectmemory.ProjectMemoryManager
}

// Compile-time interface check.
var _ TaskExecutor = (*LocalExecutor)(nil)

// NewLocalExecutor creates a new LocalExecutor.
func NewLocalExecutor(logger *zerolog.Logger, imageAPICfg *srvconfig.ImageAPIConfig, claudeEnv map[string]string, pluginDir string, sandbox bool, defaultModel string, keyProvider UserKeyProvider, maxTurnsOverrides map[string]int, workspaceDir string, serverBaseURL string, store storage.Provider, memoryMgr *projectmemory.ProjectMemoryManager) *LocalExecutor {
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
		memoryMgr:         memoryMgr,
	}
}

// ExecutionOptions configures a single agent execution.
type ExecutionOptions struct {
	Task          *model.Task
	Project       *model.Project
	MaxTurns      int
	OnProgress    func(taskID string, message string) // callback for SSE
	HeartbeatFunc func(taskID string)                 // periodic heartbeat for stuck-task detection
	LogWriter     *TaskLogWriter                      // optional per-task log file writer; nil = no log file
	// AutoMemoryDirectory is the Claude Code-visible memory directory for this task.
	AutoMemoryDirectory string
	// Montage runtime configuration is only used for montage tasks. ProviderEnv
	// may contain secrets and must only be injected into the agent process env,
	// never written to workspace files, MCP profile responses, or logs.
	MontageProviderEnv      map[string]string
	MontageToolPolicy       map[string]srvconfig.MontageToolCapabilityPolicy
	MontagePipelineDefaults map[string]map[string]any
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
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	ResultSubtype   string `json:"result_subtype,omitempty"`
	WorkDir         string `json:"work_dir,omitempty"`
	RemoteArtifacts bool   `json:"remote_artifacts,omitempty"`
	LogText         string `json:"log_text,omitempty"`
	NumTurns        int    `json:"num_turns,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	DurationMs      int    `json:"duration_ms,omitempty"`

	// LLM usage metrics (populated from SDK ResultMessage).
	DurationAPIMs int         `json:"duration_api_ms,omitempty"`
	TotalCostUSD  *float64    `json:"total_cost_usd,omitempty"`
	TokenUsage    *TokenUsage `json:"token_usage,omitempty"`

	// Post-execution diagnostics.
	NoOutputFiles       bool           `json:"no_output_files,omitempty"`
	AgentLikelyFailed   bool           `json:"agent_likely_failed,omitempty"`
	ToolUseCount        int            `json:"tool_use_count,omitempty"`
	ToolUseSummary      map[string]int `json:"tool_use_summary,omitempty"`
	ToolErrorCount      int            `json:"tool_error_count,omitempty"`
	LastToolErrorTool   string         `json:"last_tool_error_tool,omitempty"`
	LastToolError       string         `json:"last_tool_error,omitempty"`
	Model               string         `json:"model,omitempty"` // Claude Code agent model (from config.yaml claude.model)
	RemoteMemoryArchive []byte         `json:"-"`
}

// Execute runs the Claude Code agent for the given task.
//
// It creates a per-task workspace directory, writes the project config as
// .anban-creator/settings.json, loads the Anban Creator plugin with the matching
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
		workDir = DefaultWorkspaceDir(opts.Task.ID)
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

	// 3. Write project config to workspace settings.json.
	// Resolve the effective style dimensions ONCE from the task snapshot (legacy
	// rows without a snapshot may still use old task overrides). settings.json
	// here and the MCP profile read the same source so the two channels cannot
	// diverge. resolved is zero-valued when there is no project.
	var resolved resolver.Resolved
	if opts.Project != nil {
		effectiveProject := EffectiveProject(opts.Project, opts.Task)
		resolved = resolver.ResolveStyle(effectiveProject, opts.Task)
		cfg, err := BuildAppConfig(effectiveProject, resolved, e.imageAPICfg, opts.Task.ImageRatio, opts.Task.SkipReferenceImage, opts.Task.ReferenceImageURL)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}
		// Write project positioning as CLAUDE.md so Claude Code loads it as
		// persistent project memory. Non-fatal: missing the file should not abort
		// a task; the agent can still rely on its default agent definition.
		if err := writeProjectCLAUDEMD(workDir, effectiveProject); err != nil {
			e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to write project CLAUDE.md, continuing")
		}
		if e.memoryMgr != nil && e.memoryMgr.Enabled() {
			runtimeDir, err := e.memoryMgr.Stage(ctx, effectiveProject.ID, opts.Task.ID, workDir)
			if err != nil {
				return nil, fmt.Errorf("stage project memory: %w", err)
			}
			opts.AutoMemoryDirectory = runtimeDir
		}

		// Download effective reference image.
		// Task-level image takes priority over project brand image.
		// SkipReferenceImage only controls the project brand image, not task-level.
		if opts.Task.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, workDir, opts.Task.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Str("url", opts.Task.ReferenceImageURL).
					Msg("failed to download task reference image, continuing without it")
			}
		} else if opts.Project.ReferenceImageURL != "" && !opts.Task.SkipReferenceImage {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, workDir, opts.Project.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Str("url", opts.Project.ReferenceImageURL).
					Msg("failed to download reference image, continuing without it")
			}
		}
	}

	// E-commerce: materialize the task's product photos into the workspace so the
	// agent can reference local paths (analyze_image / generate_image ref).
	if opts.Task.Type == model.PlatformEcommerce {
		photos := opts.Task.Ecommerce.Data().ProductPhotos
		if n := DownloadProductImages(ctx, e.store, e.logger, workDir, photos); n == 0 && len(photos) > 0 {
			// E-commerce output is a consistency contract on the uploaded product
			// photos. If none materialized, the agent has no product reference and
			// would hallucinate inconsistent assets. Fail fast (task error → refund)
			// rather than ship a broken workspace and discover the gap at generation.
			e.logger.Error().Str("task_id", opts.Task.ID).Int("provided", len(photos)).Msg("failed to download any product photos, aborting")
			return nil, fmt.Errorf("ecommerce task: %d product photo(s) provided but none could be downloaded to the workspace; aborting to avoid inconsistent output", len(photos))
		}
	}
	if attachments := opts.Task.InputAttachments.Data(); len(attachments) > 0 {
		if _, err := MaterializeResumeInputs(ctx, e.store, e.logger, workDir, attachments); err != nil {
			return nil, fmt.Errorf("materialize resume inputs: %w", err)
		}
		if n := DownloadInputAttachments(ctx, e.store, e.logger, workDir, attachments); n == 0 && hasNonResumeInputAttachments(attachments) {
			e.logger.Warn().Str("task_id", opts.Task.ID).Int("provided", len(attachments)).Msg("no AI entry input attachments could be materialized")
		}
	}
	if err := writeMontageInputJSON(workDir, opts.Task); err != nil {
		return nil, err
	}
	if err := writeMontageRuntimeFiles(workDir, opts); err != nil {
		return nil, err
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
	agentName := TaskToAgent(opts.Task)

	// 5. Build user prompt that references the agent by name.
	projectID := ""
	if opts.Project != nil {
		projectID = opts.Project.ID
	}
	userPrompt := BuildUserPrompt(UserPromptParams{
		TaskType:                 opts.Task.Type,
		Topic:                    opts.Task.Prompt,
		Goal:                     opts.Task.Goal,
		TaskID:                   opts.Task.ID,
		ProjectID:                projectID,
		HasContentImage:          opts.Task.HasContentImage,
		HasTailImage:             opts.Task.HasTailImage,
		ArticleWithCover:         opts.Task.ArticleWithCover,
		ArticleWithContentImages: opts.Task.ArticleWithContentImages,
	})
	userPrompt = AppendResumeContextToPrompt(userPrompt, workDir)

	// Validate the plugin agent definition before starting it as the main
	// session. A programmatic WithAgent definition would only register a
	// delegatable subagent and would not load the agent's declared skills.
	if _, agentErr := loadAgentDefinition(e.pluginDir, agentName); agentErr != nil {
		return nil, fmt.Errorf("load agent definition %q: %w", agentName, agentErr)
	}
	agentFlag := "anban:" + agentName

	// 6. Build SDK options.
	sdkOpts := []claudecode.Option{
		claudecode.WithMaxTurns(maxTurns),
		claudecode.WithCwd(workDir),
		claudecode.WithPermissionMode(claudecode.PermissionModeDefault),
		// Load both user and project setting sources so the per-task CLAUDE.md
		// written into workDir is picked up by Claude Code as project memory.
		claudecode.WithSettingSources(claudecode.SettingSourceUser, claudecode.SettingSourceProject),
		claudecode.WithExtraArgs(map[string]*string{"agent": &agentFlag}),
		WithManagedAgentRuntimePolicy(),
	}

	if e.pluginDir != "" {
		sdkOpts = append(sdkOpts, claudecode.WithLocalPlugin(e.pluginDir))
	}
	stopHook, err := ManagedTaskStopHook(opts.Task.Type, workDir, e.pluginDir)
	if err != nil {
		return nil, err
	}
	sdkOpts = append(sdkOpts, stopHook)

	// Only set model if explicitly configured; otherwise let Claude CLI use env vars.
	if agentModel != "" {
		sdkOpts = append(sdkOpts, claudecode.WithModel(agentModel))
	}

	// Sandbox isolation (recommended in k8s).
	if e.sandbox {
		sdkOpts = append(sdkOpts, claudecode.WithSandboxEnabled(true))
		sdkOpts = append(sdkOpts, claudecode.WithAutoAllowBashIfSandboxed(true))
	}
	if opts.AutoMemoryDirectory != "" {
		settings, err := buildAutoMemorySettingsJSON(opts.AutoMemoryDirectory)
		if err != nil {
			return nil, fmt.Errorf("build auto memory settings: %w", err)
		}
		sdkOpts = append(sdkOpts, claudecode.WithSettings(settings))
	}

	// Environment variables (auth tokens, API keys, etc.).
	sdkOpts = append(sdkOpts, claudecode.WithEnv(e.claudeEnv))
	sdkOpts = append(sdkOpts, claudecode.WithEnvVar(MontageSubmoduleEnvName, montageSubmoduleRuntimePath(e.pluginDir)))
	for key, value := range montageProviderEnvForTask(opts) {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar(key, value))
	}

	// Inject MCP server API key so plugin/.mcp.json can resolve
	// ${ANBAN_API_KEY} for the Anban Creator MCP server.
	if mcpAPIKey != "" {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBAN_API_KEY", mcpAPIKey))
	}
	// Inject MCP server base URL so plugin/.mcp.json can resolve
	// ${ANBAN_API_URL} for the Anban Creator MCP server.
	if e.serverBaseURL != "" {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBAN_API_URL", e.serverBaseURL))
	}

	// Inject the task's project ID so the agent definition can use it directly
	// instead of discovering projects via list_projects (which may pick the wrong
	// one when the user has multiple projects of the same platform type).
	if opts.Project != nil {
		sdkOpts = append(sdkOpts, claudecode.WithEnvVar("ANBAN_DEFAULT_PROJECT", opts.Project.ID))
	}

	// Inject MCP server config directly via SDK (--mcp-config), bypassing
	// .mcp.json env var substitution. This ensures URL and API key are always
	// correct regardless of how .mcp.json resolves ${ANBAN_API_URL} and
	// ${ANBAN_API_KEY}.
	// Note: WithMcpServers replaces the entire map, so this must be the only call.
	mcpInjected := e.serverBaseURL != ""
	if mcpInjected {
		sdkOpts = append(sdkOpts, WithManagedMCPAccess(e.serverBaseURL, mcpAPIKey))
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
	pluginInitValidated := false

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

	err = claudecode.WithClient(ctx, func(client claudecode.Client) error {
		if mcpInjected {
			mcpStatus, err := client.GetMcpStatus(ctx)
			if err != nil {
				return fmt.Errorf("check managed MCP readiness: %w", err)
			}
			if err := ValidateManagedMCPStatus(mcpStatus, opts.Task.Type); err != nil {
				return err
			}
		}
		if err := client.Query(ctx, userPrompt); err != nil {
			return fmt.Errorf("send query: %w", err)
		}

		for msg := range client.ReceiveMessages(ctx) {
			switch m := msg.(type) {
			case *claudecode.SystemMessage:
				if m.Subtype == "init" {
					if err := ValidateManagedPluginInit(m, opts.Task.Type); err != nil {
						return err
					}
					pluginInitValidated = true
				}
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
					errMsg := ResultMessageError(m, lastToolErrorTool, lastToolError)
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
				if err := ValidateManagedPluginResult(pluginInitValidated, m); err != nil {
					return err
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
		return ValidateManagedPluginResult(pluginInitValidated, nil)
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
			result.ResultSubtype = resultMsg.Subtype
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
		result.ResultSubtype = resultMsg.Subtype
		result.NumTurns = resultMsg.NumTurns
		result.SessionID = resultMsg.SessionID
		result.DurationMs = resultMsg.DurationMs
		populateUsageFields(result, resultMsg)
	}
	return result, nil
}

func writeMontageInputJSON(workDir string, task *model.Task) error {
	if task == nil || !model.IsMontagePlatform(task.Type) {
		return nil
	}
	input := task.MontageInput.Data()
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal montage input: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "montage-input.json"), data, 0o644); err != nil {
		return fmt.Errorf("write montage-input.json: %w", err)
	}
	return nil
}

func writeMontageRuntimeFiles(workDir string, opts *ExecutionOptions) error {
	if opts == nil || opts.Task == nil || !model.IsMontagePlatform(opts.Task.Type) {
		return nil
	}
	toolPolicy := opts.MontageToolPolicy
	if toolPolicy == nil {
		toolPolicy = map[string]srvconfig.MontageToolCapabilityPolicy{}
	}
	pipelineDefaults := opts.MontagePipelineDefaults
	if pipelineDefaults == nil {
		pipelineDefaults = map[string]map[string]any{}
	}
	if err := writeJSONFile(filepath.Join(workDir, "montage-tool-policy.json"), toolPolicy); err != nil {
		return fmt.Errorf("write montage-tool-policy.json: %w", err)
	}
	if err := writeJSONFile(filepath.Join(workDir, "montage-pipeline-defaults.json"), pipelineDefaults); err != nil {
		return fmt.Errorf("write montage-pipeline-defaults.json: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func montageProviderEnvForTask(opts *ExecutionOptions) map[string]string {
	if opts == nil || opts.Task == nil || !model.IsMontagePlatform(opts.Task.Type) {
		return nil
	}
	env := make(map[string]string, len(opts.MontageProviderEnv))
	for key, value := range opts.MontageProviderEnv {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if !srvconfig.IsSupportedMontageProviderEnv(key) {
			continue
		}
		env[key] = value
	}
	return env
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
// .anban-creator/, .claude/, and dotfiles. Prefers the output/ subdirectory
// (created by the agent via mkdir -p) to exclude agent runtime artifacts.
// Returns the count of actual files (not directories) at any nesting depth.
func CountMeaningfulFiles(workDir string) int {
	_, count := collectWorkDirArtifacts(workDir)
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

func fallbackAgentError(defaultMsg, toolName, toolError string) string {
	if strings.TrimSpace(toolError) == "" {
		return defaultMsg
	}
	if strings.TrimSpace(toolName) == "" {
		return "last tool error: " + strings.TrimSpace(toolError)
	}
	return fmt.Sprintf("last tool error from %s: %s", strings.TrimSpace(toolName), strings.TrimSpace(toolError))
}
