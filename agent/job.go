package main

import (
	"context"
	"fmt"
	"strings"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/urfave/cli/v3"
)

type bootstrapJobFunc func(context.Context, JobConfig) (*BootstrapResponse, error)

func newJobCommand(bootstrap bootstrapJobFunc, run runAgentFunc) *cli.Command {
	return &cli.Command{
		Name:  "job",
		Usage: "run one Kubernetes agent execution",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "server-url", Usage: "base server URL", Required: true, Config: cli.StringConfig{TrimSpace: true}},
			&cli.StringFlag{Name: "execution-id", Usage: "task execution ID", Required: true, Config: cli.StringConfig{TrimSpace: true}},
			&cli.StringFlag{Name: "workspace", Usage: "workspace directory", Value: "/workspace", Config: cli.StringConfig{TrimSpace: true}},
			&cli.StringFlag{Name: "workload-token-file", Usage: "projected Kubernetes workload token file", Required: true, Config: cli.StringConfig{TrimSpace: true}},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			jobCfg := JobConfig{
				ServerURL:         strings.TrimRight(cmd.String("server-url"), "/"),
				ExecutionID:       cmd.String("execution-id"),
				Workspace:         cmd.String("workspace"),
				WorkloadTokenFile: cmd.String("workload-token-file"),
			}
			response, err := bootstrap(ctx, jobCfg)
			if err != nil {
				if response != nil && strings.TrimSpace(response.ExecutionToken) != "" {
					cfg := jobRuntimeConfig(jobCfg, response)
					result := serverExecutionFailure(jobCfg.Workspace, err)
					reporter := NewReporter(cfg)
					window := newFinalizationWindow(cfg)
					completeCtx, cancelComplete := window.completionContext()
					_ = reporter.ReportComplete(completeCtx, result)
					cancelComplete()
				}
				return fmt.Errorf("bootstrap job: %w", err)
			}
			return run(ctx, jobRuntimeConfig(jobCfg, response))
		},
	}
}

func jobRuntimeConfig(jobCfg JobConfig, response *BootstrapResponse) *Config {
	if response == nil {
		response = &BootstrapResponse{}
	}
	cfg := &Config{
		ServerURL:           jobCfg.ServerURL,
		APIKey:              response.ExecutionToken,
		ExecutionID:         jobCfg.ExecutionID,
		TaskID:              response.TaskID,
		ProjectID:           response.ProjectID,
		TaskType:            response.TaskType,
		Topic:               response.Prompt,
		BootstrapPrompt:     response.Prompt,
		RuntimeEnv:          serveragent.ClaudeRuntimeEnv(response.RuntimeEnv),
		ModelUsageAliases:   cloneRuntimeModelUsageAliases(response.ModelUsageAliases),
		Workspace:           jobCfg.Workspace,
		Model:               response.Model,
		AgentFlag:           response.AgentFlag,
		AutoMemoryDirectory: response.AutoMemoryDirectory,
		ResumeSessionID:     response.ResumeSessionID,
		ResumeContextPath:   response.ResumeContextPath,
		MaxTurns:            response.MaxTurns,
		ArtifactUploadMode:  ArtifactUploadDirect,
	}
	if response.TaskType == "montage" {
		cfg.Env = response.Env
	}
	return cfg
}

func cloneRuntimeModelUsageAliases(source map[string]serveragent.ModelUsageIdentity) map[string]serveragent.ModelUsageIdentity {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]serveragent.ModelUsageIdentity, len(source))
	for raw, identity := range source {
		result[raw] = identity
	}
	return result
}
