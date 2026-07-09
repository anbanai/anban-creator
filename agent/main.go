package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/urfave/cli/v3"
)

var agentHeartbeatInterval = 30 * time.Second

func main() {
	cmd := newAgentCommand(os.Stdout, os.Stderr, func(ctx context.Context, cfg *Config) error {
		return runAgent(ctx, cfg, os.Stdout, os.Stderr)
	})
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type runAgentFunc func(context.Context, *Config) error

func newAgentCommand(stdout, stderr io.Writer, run runAgentFunc) *cli.Command {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if run == nil {
		run = func(context.Context, *Config) error {
			return fmt.Errorf("agent run action is not configured")
		}
	}

	return &cli.Command{
		Name:           "anban",
		Usage:          "Anban agent runner and local media tools",
		Writer:         stdout,
		ErrWriter:      stderr,
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Action: func(context.Context, *cli.Command) error {
			return fmt.Errorf("subcommand is required")
		},
		Commands: []*cli.Command{
			newRunCommand(run),
			newVideoCommand(stdout),
		},
	}
}

func newRunCommand(run runAgentFunc) *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "run an agent task",
		Flags: runFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg, err := ParseConfig(cmd)
			if err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}
			return run(ctx, cfg)
		},
	}
}

func runAgent(ctx context.Context, cfg *Config, stdout, stderr io.Writer) error {
	reporter := NewReporter(cfg)
	downloader := NewDownloader(cfg)
	runner := NewRunner(cfg, reporter, downloader)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := startHeartbeat(heartbeatCtx, reporter)
	defer func() {
		stopHeartbeat()
		<-heartbeatDone
	}()

	result, runErr := runner.Run(ctx)
	if result == nil {
		result = serverExecutionFailure(cfg.Workspace, runErr)
	}

	if ctx.Err() != nil && !result.Success {
		if result.Error == "" {
			result.Error = "agent shutdown: received termination signal"
		}
	}

	if cfg.ArtifactUploadMode == ArtifactUploadDirect {
		uploader := NewArtifactUploader(cfg, reporter)
		if uploadErr := uploader.UploadWorkspaceArtifacts(context.Background(), result); uploadErr != nil {
			_ = reporter.ReportProgress(context.Background(), "artifact upload failed: "+uploadErr.Error())
			fmt.Fprintf(stderr, "failed to upload artifacts: %v\n", uploadErr)
			if result.Success {
				result.Success = false
				result.Error = "artifact upload failed: " + uploadErr.Error()
			}
			if runErr == nil {
				runErr = uploadErr
			}
		}
	}

	if reportErr := reporter.ReportResult(context.Background(), result); reportErr != nil {
		fmt.Fprintf(stderr, "failed to report result: %v\n", reportErr)
	}

	// Signal terminal completion so the server can finalize the task. Safe in
	// both modes: the server no-ops unless this is a local_claimed task still
	// running. Uses a fresh context because the run ctx may be cancelled at
	// shutdown, and this report must land for the task to reach a terminal state.
	if completeErr := reporter.ReportComplete(context.Background(), result); completeErr != nil {
		fmt.Fprintf(stderr, "failed to report completion: %v\n", completeErr)
	}

	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintf(stderr, "failed to print result: %v\n", err)
		if runErr == nil {
			runErr = err
		}
	}

	if runErr != nil || !result.Success {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("agent execution failed")
	}
	return nil
}

func startHeartbeat(ctx context.Context, reporter *Reporter) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = reporter.ReportHeartbeat(ctx)
		ticker := time.NewTicker(agentHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = reporter.ReportHeartbeat(ctx)
			}
		}
	}()
	return done
}

func serverExecutionFailure(workspace string, err error) *serveragent.ExecutionResult {
	msg := "agent execution failed"
	if err != nil {
		msg = err.Error()
	}
	return &serveragent.ExecutionResult{
		Success: false,
		Error:   msg,
		WorkDir: workspace,
	}
}
