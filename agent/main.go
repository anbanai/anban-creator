package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

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

	ctx := context.Background()
	result, runErr := runner.Run(ctx)
	if result == nil {
		result = serverExecutionFailure(cfg.Workspace, runErr)
	}

	if reportErr := reporter.ReportResult(ctx, result); reportErr != nil {
		fmt.Fprintf(os.Stderr, "failed to report result: %v\n", reportErr)
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
