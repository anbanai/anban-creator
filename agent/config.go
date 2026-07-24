package main

import (
	"fmt"
	"os"
	"strings"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/urfave/cli/v3"
)

type Config struct {
	ServerURL                string
	APIKey                   string
	ExecutionID              string
	TaskID                   string
	ProjectID                string
	TaskType                 string
	Topic                    string
	Goal                     string
	Workspace                string
	Model                    string
	AgentFlag                string
	AutoMemoryDirectory      string
	MaxTurns                 int
	HasContentImage          bool
	HasTailImage             bool
	ArticleWithCover         bool
	ArticleWithContentImages bool
	ArtifactUploadMode       string
	BootstrapPrompt          string
	ResumeSessionID          string
	ResumeContextPath        string
	RuntimeEnv               map[string]string
	ModelUsageAliases        map[string]serveragent.ModelUsageIdentity
	Env                      map[string]string
}

const (
	ArtifactUploadOff    = "off"
	ArtifactUploadDirect = "direct"
	ArtifactUploadStream = "stream"
)

func runFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "server-url", Usage: "base server URL, e.g. http://host.docker.internal:18060", Required: true, Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "api-key", Usage: "agent API key", Required: true, Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "task-id", Usage: "task ID", Required: true, Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "task-type", Usage: "task type", Required: true, Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "topic", Usage: "task topic/prompt", Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "goal", Usage: "goal-mode condition (prepended as /goal slash command so Claude Code runs its built-in goal loop)", Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "workspace", Usage: "workspace directory", Value: "/workspace", Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "model", Usage: "Claude model override", Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringSliceFlag{Name: "model-usage-alias", Usage: "exact raw=provider/model terminal usage identity"},
		&cli.StringFlag{Name: "agent-flag", Usage: "Claude Code --agent flag", Config: cli.StringConfig{TrimSpace: true}},
		&cli.StringFlag{Name: "auto-memory-directory", Usage: "Claude Code auto memory directory", Config: cli.StringConfig{TrimSpace: true}},
		&cli.IntFlag{Name: "max-turns", Usage: "maximum Claude turns"},
		// Seednote image composition defaults match the task model column defaults
		// (content on, tail off). Server overrides via CLI when dispatching the agent
		// so the in-prompt directive reflects the user's task/plan choice.
		&cli.BoolFlag{Name: "has-content-image", Usage: "seednote: generate image_01.png (content page)", Value: true},
		&cli.BoolFlag{Name: "has-tail-image", Usage: "seednote: generate tail.png"},
		&cli.BoolFlag{Name: "article-with-cover", Usage: "article: generate cover image", Value: true},
		&cli.BoolFlag{Name: "article-with-content-images", Usage: "article: generate in-text images", Value: true},
		&cli.StringFlag{Name: "artifact-upload-mode", Usage: "artifact upload mode: off, direct, or stream", Value: ArtifactUploadOff, Sources: cli.EnvVars("ANBAN_ARTIFACT_UPLOAD_MODE"), Config: cli.StringConfig{TrimSpace: true}},
	}
}

func ParseConfig(cmd *cli.Command) (*Config, error) {
	cfg := &Config{
		ServerURL:                strings.TrimRight(cmd.String("server-url"), "/"),
		APIKey:                   cmd.String("api-key"),
		TaskID:                   cmd.String("task-id"),
		TaskType:                 cmd.String("task-type"),
		Topic:                    cmd.String("topic"),
		Goal:                     cmd.String("goal"),
		Workspace:                cmd.String("workspace"),
		Model:                    cmd.String("model"),
		AgentFlag:                cmd.String("agent-flag"),
		AutoMemoryDirectory:      cmd.String("auto-memory-directory"),
		MaxTurns:                 cmd.Int("max-turns"),
		HasContentImage:          cmd.Bool("has-content-image"),
		HasTailImage:             cmd.Bool("has-tail-image"),
		ArticleWithCover:         cmd.Bool("article-with-cover"),
		ArticleWithContentImages: cmd.Bool("article-with-content-images"),
		ArtifactUploadMode:       strings.ToLower(cmd.String("artifact-upload-mode")),
	}
	aliases, err := serveragent.ParseModelUsageAliases(cmd.StringSlice("model-usage-alias"))
	if err != nil {
		return nil, err
	}
	cfg.ModelUsageAliases = aliases

	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("server-url is required")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("api-key is required")
	}
	if cfg.TaskID == "" {
		return nil, fmt.Errorf("task-id is required")
	}
	if cfg.TaskType == "" {
		return nil, fmt.Errorf("task-type is required")
	}
	if cfg.Workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	switch cfg.ArtifactUploadMode {
	case "", ArtifactUploadOff:
		cfg.ArtifactUploadMode = ArtifactUploadOff
	case ArtifactUploadDirect, ArtifactUploadStream:
	default:
		return nil, fmt.Errorf("artifact-upload-mode must be one of: off, direct, stream")
	}
	if cfg.AgentFlag == "" {
		cfg.AgentFlag = "anban:" + serveragent.TaskTypeToAgent(cfg.TaskType)
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = serveragent.DefaultMaxTurns(cfg.TaskType, nil)
	}

	return cfg, nil
}

func (c *Config) UserPrompt() string {
	if strings.TrimSpace(c.ResumeContextPath) != "" {
		return serveragent.AppendResumeContextFileToPrompt(c.BootstrapPrompt, c.Workspace, c.ResumeContextPath)
	}
	if strings.TrimSpace(c.BootstrapPrompt) != "" {
		return serveragent.AppendResumeContextToPrompt(c.BootstrapPrompt, c.Workspace)
	}
	prompt := serveragent.BuildUserPrompt(serveragent.UserPromptParams{
		TaskType:                 c.TaskType,
		Topic:                    c.Topic,
		Goal:                     c.Goal,
		TaskID:                   c.TaskID,
		ProjectID:                firstNonEmpty(c.ProjectID, os.Getenv("ANBAN_DEFAULT_PROJECT")),
		HasContentImage:          c.HasContentImage,
		HasTailImage:             c.HasTailImage,
		ArticleWithCover:         &c.ArticleWithCover,
		ArticleWithContentImages: &c.ArticleWithContentImages,
	})
	return serveragent.AppendResumeContextToPrompt(prompt, c.Workspace)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
