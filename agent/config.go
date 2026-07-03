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
	TaskID                   string
	TaskType                 string
	Topic                    string
	Goal                     string
	Workspace                string
	Model                    string
	AgentFlag                string
	MaxTurns                 int
	HasContentImage          bool
	HasTailImage             bool
	ArticleWithCover         bool
	ArticleWithContentImages bool
}

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
		&cli.StringFlag{Name: "agent-flag", Usage: "Claude Code --agent flag", Config: cli.StringConfig{TrimSpace: true}},
		&cli.IntFlag{Name: "max-turns", Usage: "maximum Claude turns"},
		// Seednote image composition defaults match the task model column defaults
		// (content on, tail off). Server overrides via CLI when dispatching the agent
		// so the in-prompt directive reflects the user's task/plan choice.
		&cli.BoolFlag{Name: "has-content-image", Usage: "seednote: generate image_01.png (content page)", Value: true},
		&cli.BoolFlag{Name: "has-tail-image", Usage: "seednote: generate tail.png"},
		&cli.BoolFlag{Name: "article-with-cover", Usage: "article: generate cover image", Value: true},
		&cli.BoolFlag{Name: "article-with-content-images", Usage: "article: generate in-text images", Value: true},
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
		MaxTurns:                 cmd.Int("max-turns"),
		HasContentImage:          cmd.Bool("has-content-image"),
		HasTailImage:             cmd.Bool("has-tail-image"),
		ArticleWithCover:         cmd.Bool("article-with-cover"),
		ArticleWithContentImages: cmd.Bool("article-with-content-images"),
	}

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
	if cfg.AgentFlag == "" {
		cfg.AgentFlag = "anban:" + serveragent.TaskTypeToAgent(cfg.TaskType)
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = serveragent.DefaultMaxTurns(cfg.TaskType, nil)
	}

	return cfg, nil
}

func (c *Config) UserPrompt() string {
	return serveragent.BuildUserPrompt(serveragent.UserPromptParams{
		TaskType:                 c.TaskType,
		Topic:                    c.Topic,
		Goal:                     c.Goal,
		TaskID:                   c.TaskID,
		ProjectID:                os.Getenv("ANBAN_DEFAULT_PROJECT"),
		HasContentImage:          c.HasContentImage,
		HasTailImage:             c.HasTailImage,
		ArticleWithCover:         &c.ArticleWithCover,
		ArticleWithContentImages: &c.ArticleWithContentImages,
	})
}
