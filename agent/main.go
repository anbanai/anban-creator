package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/urfave/cli/v3"
)

var agentHeartbeatInterval = 30 * time.Second
var jobArtifactTimeout = 120 * time.Second
var jobCompletionTimeout = 20 * time.Second

const (
	jobArtifactTimeoutEnv   = "ANBAN_JOB_ARTIFACT_TIMEOUT"
	jobCompletionTimeoutEnv = "ANBAN_JOB_COMPLETION_TIMEOUT"
)

var phaseTimeoutPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)(ms|s|m)$`)
var notifyRuntimeShutdown = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func main() {
	cmd := newAgentCommand(os.Stdout, os.Stderr, func(ctx context.Context, cfg *Config) error {
		return runAgent(ctx, cfg, os.Stdout, os.Stderr)
	})
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(processExitCode(err))
	}
}

type completionReportError struct {
	err error
}

func (e *completionReportError) Error() string {
	return "failed to report completion: " + e.err.Error()
}

func (e *completionReportError) Unwrap() error {
	return e.err
}

func processExitCode(err error) int {
	var completionErr *completionReportError
	if errors.As(err, &completionErr) {
		return 2
	}
	return 1
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
	if err := prepareRuntimeWorkspace(cfg.Workspace, cfg.TaskType, cfg.RuntimeAdapter); err != nil {
		return fmt.Errorf("prepare runtime workspace: %w", err)
	}
	reporter := NewReporter(cfg)
	downloader := NewDownloader(cfg)
	runner := NewRunner(cfg, reporter, downloader)

	shutdownCtx, stopShutdown := notifyRuntimeShutdown()
	defer stopShutdown()
	runCtx, cancelRun := context.WithCancel(ctx)
	stopRunOnShutdown := context.AfterFunc(shutdownCtx, cancelRun)
	defer func() {
		stopRunOnShutdown()
		cancelRun()
	}()
	if shutdownCtx.Err() != nil {
		cancelRun()
	}

	heartbeatCtx, stopHeartbeat := context.WithCancel(runCtx)
	heartbeatDone := startHeartbeat(heartbeatCtx, reporter, stderr)
	defer func() {
		stopHeartbeat()
		<-heartbeatDone
	}()

	result, runErr := runner.Run(runCtx)
	if result == nil {
		result = serverExecutionFailure(cfg.Workspace, runErr)
	}

	if shutdownCtx.Err() != nil && !result.Success {
		if result.Error == "" {
			result.Error = "agent shutdown: received termination signal"
		}
	}
	finalization := newFinalizationTimeouts(cfg)
	artifactCtx, cancelArtifact := finalization.artifactContext(shutdownCtx)

	if cfg.ArtifactUploadMode == ArtifactUploadDirect || cfg.ArtifactUploadMode == ArtifactUploadStream {
		uploader := NewArtifactUploader(cfg, reporter)
		if _, uploadErr := uploader.UploadWorkspaceArtifacts(artifactCtx, result); uploadErr != nil {
			_ = reporter.ReportProgress(artifactCtx, "artifact upload failed: "+uploadErr.Error())
			fmt.Fprintf(stderr, "failed to upload artifacts: %v\n", uploadErr)
			runErr = applyArtifactUploadFailure(result, runErr, uploadErr)
		}
	}
	cancelArtifact()

	// Signal terminal completion so the server can finalize the task. Safe in
	// both modes: the server no-ops unless this is a local_claimed task still
	// running. Uses a fresh context because the run ctx may be cancelled at
	// shutdown, and this report must land for the task to reach a terminal state.
	completionCtx, cancelCompletion := finalization.completionContext()
	completeErr := reporter.ReportComplete(completionCtx, result)
	cancelCompletion()
	if completeErr != nil {
		fmt.Fprintf(stderr, "failed to report completion: %v\n", completeErr)
	}

	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintf(stderr, "failed to print result: %v\n", err)
		if runErr == nil {
			runErr = err
		}
	}

	if completeErr != nil {
		return &completionReportError{err: completeErr}
	}
	if runErr != nil || !result.Success {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("agent execution failed")
	}
	return nil
}

func applyArtifactUploadFailure(result *serveragent.ExecutionResult, runErr, uploadErr error) error {
	if uploadErr == nil {
		return runErr
	}
	if result != nil && result.Success {
		result.Success = false
		result.Error = "artifact upload failed: " + uploadErr.Error()
	}
	if runErr == nil {
		return uploadErr
	}
	return runErr
}

type finalizationTimeouts struct {
	job        bool
	artifact   time.Duration
	completion time.Duration
}

func newFinalizationTimeouts(cfg *Config) finalizationTimeouts {
	if cfg == nil || strings.TrimSpace(cfg.ExecutionID) == "" {
		return finalizationTimeouts{}
	}
	return finalizationTimeouts{
		job:        true,
		artifact:   parsePhaseTimeout(os.Getenv(jobArtifactTimeoutEnv), jobArtifactTimeout, 5*time.Minute),
		completion: parsePhaseTimeout(os.Getenv(jobCompletionTimeoutEnv), jobCompletionTimeout, time.Minute),
	}
}

func parsePhaseTimeout(raw string, fallback, maximum time.Duration) time.Duration {
	match := phaseTimeoutPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return fallback
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return fallback
	}
	unit := time.Millisecond
	switch match[2] {
	case "s":
		unit = time.Second
	case "m":
		unit = time.Minute
	}
	timeout := time.Duration(value * float64(unit))
	if timeout <= 0 || timeout > maximum {
		return fallback
	}
	return timeout
}

func (t finalizationTimeouts) artifactContext(shutdown context.Context) (context.Context, context.CancelFunc) {
	if shutdown == nil {
		shutdown = context.Background()
	}
	if t.job {
		return context.WithTimeout(shutdown, t.artifact)
	}
	return context.WithCancel(shutdown)
}

func (t finalizationTimeouts) completionContext() (context.Context, context.CancelFunc) {
	if t.job {
		return context.WithTimeout(context.Background(), t.completion)
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
