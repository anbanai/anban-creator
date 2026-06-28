package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	serveragent "github.com/royalrick/anbanwriter/server/agent"
)

func main() {
	cfg, err := ParseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(2)
	}

	reporter := NewReporter(cfg)
	downloader := NewDownloader(cfg)
	runner := NewRunner(cfg, reporter, downloader)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	result, runErr := runner.Run(ctx)
	if result == nil {
		result = serverExecutionFailure(cfg.Workspace, runErr)
	}

	if ctx.Err() != nil && !result.Success {
		if result.Error == "" {
			result.Error = "agent shutdown: received termination signal"
		}
	}

	if reportErr := reporter.ReportResult(context.Background(), result); reportErr != nil {
		fmt.Fprintf(os.Stderr, "failed to report result: %v\n", reportErr)
	}

	// Signal terminal completion so the server can finalize the task. Safe in
	// both modes: the server no-ops unless this is a local_claimed task still
	// running. Uses a fresh context because the run ctx may be cancelled at
	// shutdown, and this report must land for the task to reach a terminal state.
	if completeErr := reporter.ReportComplete(context.Background(), result); completeErr != nil {
		fmt.Fprintf(os.Stderr, "failed to report completion: %v\n", completeErr)
	}

	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "failed to print result: %v\n", err)
		if runErr == nil {
			runErr = err
		}
	}

	if runErr != nil || !result.Success {
		os.Exit(1)
	}
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
