package agent

import (
	"context"
	"errors"
	"time"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	RuntimePhasePending   = "pending"
	RuntimePhaseRunning   = "running"
	RuntimePhaseSucceeded = "succeeded"
	RuntimePhaseFailed    = "failed"
)

var ErrRuntimeWorkloadNotFound = errors.New("runtime workload not found")

type RuntimeDispatcher interface {
	Scope() string
	ResolveRuntime(string) srvconfig.RuntimeImageSelection
	Prepare(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error)
	ResolvePrepared(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error)
	Activate(context.Context, *model.TaskExecution) error
	Inspect(context.Context, *model.TaskExecution) (*RuntimeExecutionState, error)
	Delete(context.Context, *model.TaskExecution) error
}

type RuntimeExecutionState struct {
	Phase       string
	InstanceID  string
	Reason      string
	Message     string
	ExitCode    *int32
	CompletedAt *time.Time
}
