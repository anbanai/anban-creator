package agent

import (
	"context"
	"errors"
	"strings"
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
	Container   string
	Reason      string
	Message     string
	ExitCode    *int32
	CompletedAt *time.Time
}

// CleanupDiagnostic stores only allowlisted failure categories. Provider errors
// may contain credentials, signed URLs, or environment values and stay private.
func CleanupDiagnostic(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "image"):
		return "runtime_image_identity_conflict"
	case strings.Contains(text, "ownership") || strings.Contains(text, "label"):
		return "runtime_ownership_conflict"
	case strings.Contains(text, "identity") || strings.Contains(text, "uid mismatch"):
		return "runtime_identity_conflict"
	default:
		return "permanent_runtime_cleanup_failure"
	}
}
