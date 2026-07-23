package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/urfave/cli/v3"
)

var agentHeartbeatInterval = 30 * time.Second
var jobFinalizationTimeout = 20 * time.Second
var jobCompletionReserve = 5 * time.Second
var finalizationNow = time.Now

const jobFinalizationTimeoutEnv = "ANBAN_JOB_FINALIZATION_TIMEOUT"

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
		Usage:          "Anban agent runner",
		Writer:         stdout,
		ErrWriter:      stderr,
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Action: func(context.Context, *cli.Command) error {
			return fmt.Errorf("subcommand is required")
		},
		Commands: []*cli.Command{
			newRunCommand(run),
			newJobCommand(BootstrapJob, run),
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
	if err := prepareRuntimeWorkspace(cfg.Workspace, cfg.TaskType); err != nil {
		return fmt.Errorf("prepare runtime workspace: %w", err)
	}
	reporter := NewReporter(cfg)
	downloader := NewDownloader(cfg)
	runner := NewRunner(cfg, reporter, downloader)

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := startHeartbeat(heartbeatCtx, reporter, stderr)
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
	finalization := newFinalizationWindow(cfg)
	workCtx, cancelWork := finalization.workContext()
	defer cancelWork()

	if cfg.ArtifactUploadMode == ArtifactUploadDirect || cfg.ArtifactUploadMode == ArtifactUploadStream {
		uploader := NewArtifactUploader(cfg, reporter)
		if uploadErr := uploader.UploadWorkspaceArtifacts(workCtx, result); uploadErr != nil {
			_ = reporter.ReportProgress(workCtx, "artifact upload failed: "+uploadErr.Error())
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

	cancelWork()

	// Signal terminal completion so the server can finalize the task. Safe in
	// both modes: the server no-ops unless this is a local_claimed task still
	// running. Uses a fresh context because the run ctx may be cancelled at
	// shutdown, and this report must land for the task to reach a terminal state.
	completionCtx, cancelCompletion := finalization.completionContext()
	defer cancelCompletion()
	if completeErr := reporter.ReportComplete(completionCtx, result); completeErr != nil {
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

type finalizationWindow struct {
	job              bool
	workDeadline     time.Time
	completeDeadline time.Time
}

func newFinalizationWindow(cfg *Config) finalizationWindow {
	if cfg == nil || strings.TrimSpace(cfg.ExecutionID) == "" {
		return finalizationWindow{}
	}
	total := jobFinalizationTimeout
	if configured, err := time.ParseDuration(strings.TrimSpace(os.Getenv(jobFinalizationTimeoutEnv))); err == nil && configured > 0 && configured <= 5*time.Minute {
		total = configured
	}
	reserve := jobCompletionReserve
	if reserve >= total {
		reserve = total / 2
	}
	deadline := finalizationNow().Add(total)
	return finalizationWindow{job: true, workDeadline: deadline.Add(-reserve), completeDeadline: deadline}
}

func (w finalizationWindow) workContext() (context.Context, context.CancelFunc) {
	if w.job {
		return context.WithDeadline(context.Background(), w.workDeadline)
	}
	return context.WithCancel(context.Background())
}

func (w finalizationWindow) completionContext() (context.Context, context.CancelFunc) {
	if w.job {
		return context.WithDeadline(context.Background(), w.completeDeadline)
	}
	return context.WithCancel(context.Background())
}

func startHeartbeat(ctx context.Context, reporter *Reporter, stderr io.Writer) <-chan struct{} {
	if stderr == nil {
		stderr = io.Discard
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		failures := 0
		report := func() {
			err := reporter.ReportHeartbeat(ctx)
			if err == nil {
				if failures > 0 {
					fmt.Fprintf(stderr, "agent heartbeat restored after %d failure(s)\n", failures)
				}
				failures = 0
				return
			}
			failures++
			if failures == 1 || failures%5 == 0 {
				fmt.Fprintf(stderr, "agent heartbeat failed (%d consecutive): %v\n", failures, err)
			}
		}
		report()
		ticker := time.NewTicker(agentHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report()
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
		Success:        false,
		Error:          msg,
		TerminalReason: model.TaskBillingTerminalPlatformError,
		WorkDir:        workspace,
	}
}
