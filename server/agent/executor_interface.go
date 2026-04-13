package agent

import "context"

// TaskExecutor is the interface for executing content generation tasks.
// Implementations include LocalExecutor (subprocess) and DockerExecutor (container).
type TaskExecutor interface {
	Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error)
}
