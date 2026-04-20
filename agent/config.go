package main

import (
	"flag"
	"fmt"
	"strings"

	serveragent "github.com/royalrick/anbanwriter/server/agent"
)

type Config struct {
	ServerURL string
	APIKey    string
	TaskID    string
	TaskType  string
	Topic     string
	Workspace string
	Model     string
	AgentFlag string
	MaxTurns  int
}

func ParseConfig() (*Config, error) {
	cfg := &Config{}
	flag.StringVar(&cfg.ServerURL, "server-url", "", "base server URL, e.g. http://host.docker.internal:18060")
	flag.StringVar(&cfg.APIKey, "api-key", "", "agent API key")
	flag.StringVar(&cfg.TaskID, "task-id", "", "task ID")
	flag.StringVar(&cfg.TaskType, "task-type", "", "task type")
	flag.StringVar(&cfg.Topic, "topic", "", "task topic/prompt")
	flag.StringVar(&cfg.Workspace, "workspace", "/workspace", "workspace directory")
	flag.StringVar(&cfg.Model, "model", "", "Claude model override")
	flag.StringVar(&cfg.AgentFlag, "agent-flag", "", "Claude Code --agent flag")
	flag.IntVar(&cfg.MaxTurns, "max-turns", 0, "maximum Claude turns")
	flag.Parse()

	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.TaskID = strings.TrimSpace(cfg.TaskID)
	cfg.TaskType = strings.TrimSpace(cfg.TaskType)
	cfg.Topic = strings.TrimSpace(cfg.Topic)
	cfg.Workspace = strings.TrimSpace(cfg.Workspace)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.AgentFlag = strings.TrimSpace(cfg.AgentFlag)

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
		cfg.AgentFlag = "anbanwriter:" + serveragent.TaskTypeToAgent(cfg.TaskType)
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = serveragent.DefaultMaxTurns(cfg.TaskType, nil)
	}

	return cfg, nil
}

func (c *Config) UserPrompt() string {
	return serveragent.BuildUserPrompt(c.TaskType, c.Topic)
}
