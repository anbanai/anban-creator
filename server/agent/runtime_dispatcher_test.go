package agent

import (
	"context"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type runtimeDispatcherTestFake struct{}

func (*runtimeDispatcherTestFake) Scope() string { return "docker" }

func (*runtimeDispatcherTestFake) ResolveRuntime(string) srvconfig.RuntimeImageSelection {
	return srvconfig.RuntimeImageSelection{Profile: "article", Image: "registry/content@sha256:test"}
}

func (*runtimeDispatcherTestFake) Dispatch(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "daemon-a", Workload: "container-1"}, nil
}

func (*runtimeDispatcherTestFake) Inspect(context.Context, *model.TaskExecution) (*RuntimeExecutionState, error) {
	return &RuntimeExecutionState{Phase: RuntimePhaseRunning, InstanceID: "container-1"}, nil
}

func (*runtimeDispatcherTestFake) Delete(context.Context, *model.TaskExecution) error { return nil }

func TestRuntimeDispatcherContractSupportsProviderNeutralState(t *testing.T) {
	var dispatcher RuntimeDispatcher = &runtimeDispatcherTestFake{}
	state, err := dispatcher.Inspect(context.Background(), &model.TaskExecution{ID: "execution-1"})
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.Scope() != "docker" || state.Phase != RuntimePhaseRunning || state.InstanceID != "container-1" {
		t.Fatalf("dispatcher scope/state = %q/%#v", dispatcher.Scope(), state)
	}
}
