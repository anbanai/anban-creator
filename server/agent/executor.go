package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	appconfig "github.com/anbanai/anban-creator/server/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type UserPromptParams struct {
	TaskType  string // model.PlatformArticle / model.PlatformSeednote / model.PlatformMoments / ...
	Topic     string // user prompt; empty triggers autonomous research mode
	TaskID    string // injected as task_id=<x> into the prompt body
	ProjectID string // injected as project_id=<x> into the prompt body
	// ImageRatio and HasReferenceImage describe the frozen Montage video and
	// portrait inputs. They are ignored for non-Montage task types.
	ImageRatio        string
	HasReferenceImage bool
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
// For seednote tasks, p.HasContentImage / p.HasTailImage are emitted as a
// structured seednote_image_mode runtime control (cover always generated).
// Non-seednote types ignore it.
//
// For article tasks, p.ArticleWithCover / p.ArticleWithContentImages are emitted
// as a structured article_image_mode runtime control. The agent and skills own
// detailed workflow semantics for each mode; Go only passes state.
func BuildUserPrompt(p UserPromptParams) string {
	var base string
	if p.Topic == "" {
		base = fmt.Sprintf(
			"Run the full %s creation workflow. "+
				"Analyze the project profile, keywords, and historical topics "+
				"to choose the optimal theme, then execute the full creation workflow.",
			p.TaskType)
	} else {
		topic := p.Topic
		if model.IsMontagePlatform(p.TaskType) {
			topic = strings.ReplaceAll(strings.ReplaceAll(topic, "\r\n", "\n"), "\r", "\n")
			topic = strings.ReplaceAll(topic, "\n", "\n> ")
		}
		base = fmt.Sprintf("Run the full %s creation workflow; create content about: %s", p.TaskType, topic)
	}
	if controls := describeRuntimeControls(p); controls != "" {
		base += "\n\n" + controls
	}
	if model.IsMontagePlatform(p.TaskType) {
		portraitReference := "Portrait reference: no system portrait selected"
		if p.HasReferenceImage {
			portraitReference = "Portrait reference: use the system-provided portrait at " + TaskReferenceImagePath
		}
		base += "\n\nVideo aspect ratio: " + p.ImageRatio + "\n" + portraitReference
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
	return base
}

// AppendResumeContextToPrompt asks the agent to continue from supplemental
// operator input when a resumed task wrote .anban-creator/resume/latest.md.
func AppendResumeContextToPrompt(prompt, workDir string) string {
	return AppendResumeContextFileToPrompt(prompt, workDir, path.Join(appconfig.ConfigDir, "resume", "latest.md"))
}

func AppendResumeContextFileToPrompt(prompt, workDir, relativePath string) string {
	if strings.TrimSpace(workDir) == "" {
		return prompt
	}
	relativePath = filepath.ToSlash(strings.TrimSpace(relativePath))
	if relativePath == "" || path.Clean(relativePath) != relativePath || path.IsAbs(relativePath) || strings.HasPrefix(relativePath, "../") {
		return prompt
	}
	resumePath := filepath.Join(workDir, filepath.FromSlash(relativePath))
	if info, err := os.Stat(resumePath); err != nil || info.IsDir() {
		return prompt
	}
	return prompt + "\n\n继续执行模式：\n" +
		"- 这是一个基于原任务工作目录的继续执行，不是全新任务。\n" +
		"- 请先读取 `" + relativePath + "`，理解用户补充指令、补充文件说明和附件相对路径。\n" +
		"- 基于当前工作目录已有草稿、素材和产物继续完成任务；不要清空、删除或整体覆盖已有产物，除非补充指令明确要求替换。\n" +
		"- 如果补充文件中存在同名或相近用途文件，优先按 latest.md 中的文件说明区分使用。"
}

// describeRuntimeControls emits compact, machine-readable controls for agents.
// Detailed semantics live in harness agents/skills and docs/plugin-development.md so server code
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

// DefaultMaxTurns returns the max turns for a given task type from the config map.
// Falls back to 40 if the task type is not configured.
func DefaultMaxTurns(taskType string, maxTurns map[string]int) int {
	if v, ok := maxTurns[taskType]; ok && v > 0 {
		return v
	}
	if pack, ok := agentpack.Default().ForTaskType(taskType); ok {
		if v, ok := maxTurns[pack.ID]; ok && v > 0 {
			return v
		}
		if pack.Runtime.MaxTurns > 0 {
			return pack.Runtime.MaxTurns
		}
	}
	return 40
}

// ExecutionOptions configures a single agent execution.
type ExecutionOptions struct {
	Task           *model.Task
	Project        *model.Project
	ReferenceAsset *model.Asset
	MaxTurns       int
	OnProgress     func(taskID string, message string) // callback for SSE
	HeartbeatFunc  func(taskID string)                 // periodic heartbeat for stuck-task detection
	LogWriter      *TaskLogWriter                      // optional per-task log file writer; nil = no log file
	// Montage runtime configuration is only used for montage tasks. Env
	// may contain secrets and must only be injected into the agent process env,
	// never written to workspace files, MCP profile responses, or logs.
	MontageEnv              map[string]string
	MontageToolPolicy       map[string]srvconfig.MontageToolCapabilityPolicy
	MontagePipelineDefaults map[string]map[string]any
}

const (
	CostStatusReconciled   = "reconciled"
	CostStatusUnreconciled = "unreconciled"

	CostDiagnosticMissingTerminalModelUsage = "missing_terminal_model_usage"
	CostDiagnosticUnmappedModelUsageAlias   = "unmapped_model_usage_alias"
	CostDiagnosticInvalidModelUsageAlias    = "invalid_model_usage_alias"
	CostDiagnosticInvalidModelUsageTokens   = "invalid_model_usage_tokens"
	CostDiagnosticModelUsageTokenOverflow   = "model_usage_token_overflow"
)

// ModelUsageIdentity is an explicitly configured raw-to-canonical model mapping.
// Unknown raw model names are never guessed.
type ModelUsageIdentity = model.ModelUsageIdentity

// ModelTokenUsage is authoritative terminal token evidence for one provider model.
type ModelTokenUsage = model.ModelTokenUsage

// CostDiagnostic records why terminal provider-cost evidence cannot reconcile.
type CostDiagnostic struct {
	Code     string `json:"code"`
	RawModel string `json:"raw_model,omitempty"`
}

type ArtifactTransferFailure struct {
	Operation  string `json:"operation"`
	Code       string `json:"code"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Attempts   int    `json:"attempts"`
	Retryable  bool   `json:"retryable"`
	RequestID  string `json:"request_id,omitempty"`
}

type ArtifactUploadFailure struct {
	Path string `json:"path"`
	ArtifactTransferFailure
}

// ExecutionResult captures the outcome of an agent execution.
type ExecutionResult struct {
	Success           bool   `json:"success"`
	Error             string `json:"error,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	PolicyDomain      string `json:"policy_domain,omitempty"`
	ProviderCode      string `json:"provider_code,omitempty"`
	HTTPStatus        int    `json:"http_status,omitempty"`
	ContentDirection  string `json:"content_direction,omitempty"`
	Recoverable       bool   `json:"recoverable,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	RootErrorCode     string `json:"root_error_code,omitempty"`
	WorkflowErrorCode string `json:"workflow_error_code,omitempty"`
	FailureStage      string `json:"failure_stage,omitempty"`
	ResumeFrom        string `json:"resume_from,omitempty"`
	ResultSubtype     string `json:"result_subtype,omitempty"`
	TerminalReason    string `json:"terminal_reason,omitempty"`
	WorkDir           string `json:"work_dir,omitempty"`
	RemoteArtifacts   bool   `json:"remote_artifacts,omitempty"`
	LogText           string `json:"log_text,omitempty"`
	NumTurns          int    `json:"num_turns,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
	DurationMs        int    `json:"duration_ms,omitempty"`

	DurationAPIMs   int               `json:"duration_api_ms,omitempty"`
	ModelUsage      []ModelTokenUsage `json:"model_usage,omitempty"`
	CostStatus      string            `json:"cost_status,omitempty"`
	CostDiagnostics []CostDiagnostic  `json:"cost_diagnostics,omitempty"`

	// Post-execution diagnostics.
	NoOutputFiles               bool                     `json:"no_output_files,omitempty"`
	AgentLikelyFailed           bool                     `json:"agent_likely_failed,omitempty"`
	ToolUseCount                int                      `json:"tool_use_count,omitempty"`
	ToolUseSummary              map[string]int           `json:"tool_use_summary,omitempty"`
	ToolErrorCount              int                      `json:"tool_error_count,omitempty"`
	LastToolErrorTool           string                   `json:"last_tool_error_tool,omitempty"`
	LastToolError               string                   `json:"last_tool_error,omitempty"`
	Model                       string                   `json:"model,omitempty"` // Claude Code agent model (from claude.models.default)
	ArtifactUploadFailures      []ArtifactUploadFailure  `json:"artifact_upload_failures,omitempty"`
	ArtifactFinalizationFailure *ArtifactTransferFailure `json:"artifact_finalization_failure,omitempty"`
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

func montageEnvForTask(opts *ExecutionOptions) map[string]string {
	if opts == nil || opts.Task == nil || !model.IsMontagePlatform(opts.Task.Type) {
		return nil
	}
	env := make(map[string]string, len(opts.MontageEnv))
	for key, value := range opts.MontageEnv {
		if strings.TrimSpace(value) == "" {
			continue
		}
		env[key] = value
	}
	return env
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
