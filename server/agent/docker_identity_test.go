package agent

import (
	"context"
	"errors"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/anbanai/anban-creator/server/auth"
)

type workloadContainerInspector struct {
	inspect container.InspectResponse
	err     error
	got     string
}

func (i *workloadContainerInspector) ContainerInspect(_ context.Context, name string) (container.InspectResponse, error) {
	i.got = name
	return i.inspect, i.err
}

func TestWorkloadDockerVerifierValidatesTokenAndLiveContainer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tokens, raw := dockerWorkloadToken(t, now)
	inspector := &workloadContainerInspector{inspect: dockerWorkloadContainer("container-id")}
	verifier, err := NewDockerWorkloadVerifier(inspector, tokens)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := verifier.Verify(context.Background(), raw, "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	if inspector.got != "exec-1" {
		t.Fatalf("inspected container = %q, want claimed workload", inspector.got)
	}
	if identity.Target != "docker" || identity.Scope != "docker" || identity.Workload != "exec-1" || identity.InstanceID != "container-id" ||
		identity.ExecutionID != "execution-1" || identity.TaskID != "task-1" || identity.ProjectID != "project-1" || identity.UserID != "user-1" ||
		!identity.Deadline.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestWorkloadDockerVerifierRejectsInvalidTokenInspectionAndRestartingState(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tokens, raw := dockerWorkloadToken(t, now)
	expired, err := tokens.IssueAt(auth.WorkloadClaims{
		RuntimeScope: "docker", RuntimeWorkload: "exec-1", RuntimeInstanceID: "container-id",
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1",
	}, now.Add(-10*time.Minute), now.Add(-5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	inspectErr := errors.New("Docker unavailable")
	for _, tc := range []struct {
		name      string
		token     string
		inspector *workloadContainerInspector
	}{
		{name: "malformed token", token: "not-a-jwt", inspector: &workloadContainerInspector{inspect: dockerWorkloadContainer("container-id")}},
		{name: "expired token", token: expired, inspector: &workloadContainerInspector{inspect: dockerWorkloadContainer("container-id")}},
		{name: "inspect error", token: raw, inspector: &workloadContainerInspector{err: inspectErr}},
		{name: "nil inspection base", token: raw, inspector: &workloadContainerInspector{inspect: container.InspectResponse{}}},
		{name: "nil state", token: raw, inspector: &workloadContainerInspector{inspect: container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{ID: "container-id"}}}},
		{name: "nil config", token: raw, inspector: &workloadContainerInspector{inspect: container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{ID: "container-id", State: &container.State{Status: container.StateRunning, Running: true}}}}},
		{name: "restarting", token: raw, inspector: &workloadContainerInspector{inspect: func() container.InspectResponse {
			inspected := dockerWorkloadContainer("container-id")
			inspected.State.Status = container.StateRestarting
			inspected.State.Running = false
			inspected.State.Restarting = true
			return inspected
		}()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier, err := NewDockerWorkloadVerifier(tc.inspector, tokens)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := verifier.Verify(context.Background(), tc.token, "execution-1"); err == nil {
				t.Fatal("invalid Docker workload accepted")
			}
		})
	}
}

func TestWorkloadDockerVerifierRejectsOwnershipSpoofStaleAndTerminalContainers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tokens, raw := dockerWorkloadToken(t, now)
	tests := []struct {
		name        string
		executionID string
		mutate      func(*container.InspectResponse)
	}{
		{name: "requested execution spoof", executionID: "other-execution"},
		{name: "execution owner label", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.Config.Labels[dockerExecutionIDLabel] = "other" }},
		{name: "task owner label", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.Config.Labels[dockerTaskIDLabel] = "other" }},
		{name: "project owner label", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.Config.Labels[dockerProjectIDLabel] = "other" }},
		{name: "user owner label", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.Config.Labels[dockerUserIDLabel] = "other" }},
		{name: "stale container instance", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.ID = "replacement-id" }},
		{name: "exited container", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.State.Running = false; c.State.Status = container.StateExited }},
		{name: "dead container", executionID: "execution-1", mutate: func(c *container.InspectResponse) {
			c.State.Running = false
			c.State.Status = container.StateDead
			c.State.Dead = true
		}},
		{name: "paused container", executionID: "execution-1", mutate: func(c *container.InspectResponse) { c.State.Status = container.StatePaused; c.State.Paused = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inspected := dockerWorkloadContainer("container-id")
			if tc.mutate != nil {
				tc.mutate(&inspected)
			}
			verifier, err := NewDockerWorkloadVerifier(&workloadContainerInspector{inspect: inspected}, tokens)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := verifier.Verify(context.Background(), raw, tc.executionID); err == nil {
				t.Fatal("spoofed or inactive Docker workload accepted")
			}
		})
	}
}

func dockerWorkloadToken(t *testing.T, now time.Time) (*auth.WorkloadTokenService, string) {
	t.Helper()
	tokens, err := auth.NewWorkloadTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tokens.IssueAt(auth.WorkloadClaims{
		RuntimeScope: "docker", RuntimeWorkload: "exec-1", RuntimeInstanceID: "container-id",
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1",
	}, now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return tokens, raw
}

func dockerWorkloadContainer(id string) container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: id, State: &container.State{Status: container.StateRunning, Running: true}},
		Config: &container.Config{Labels: map[string]string{
			dockerExecutionIDLabel: "execution-1", dockerTaskIDLabel: "task-1", dockerProjectIDLabel: "project-1", dockerUserIDLabel: "user-1",
		}},
	}
}

func TestResolveDockerRuntimeUserUsesNonRootUnixHostIdentity(t *testing.T) {
	got, err := resolveDockerRuntimeUser("linux", func() (*user.User, error) {
		return &user.User{Uid: "501", Gid: "20"}, nil
	})
	if err != nil {
		t.Fatalf("resolveDockerRuntimeUser: %v", err)
	}
	if got != "501:20" {
		t.Fatalf("runtime user = %q, want host identity 501:20", got)
	}

	execOptions := dockerAgentExecOptions([]string{"anban"}, []string{"HOME=/workspace/task/.anban-runtime-home"}, "/workspace/task", got)
	if execOptions.User != got {
		t.Fatalf("exec user = %q, want %q", execOptions.User, got)
	}
	containerConfig := dockerAgentContainerConfig("agent:latest", []string{"anban"}, []string{"HOME=/workspace/.anban-runtime-home"}, got)
	if containerConfig.User != got {
		t.Fatalf("container user = %q, want %q", containerConfig.User, got)
	}
	prep := dockerWorkspacePreparationExecOptions("/workspace/task", got)
	if prep.User != got || strings.Contains(strings.Join(prep.Cmd, " "), "chown") {
		t.Fatalf("workspace preparation = %#v, want resolved user and no chown", prep)
	}
}

func TestResolveDockerRuntimeUserRejectsRootAndInvalidUnixIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *user.User
		want string
	}{
		{name: "root", user: &user.User{Uid: "0", Gid: "0"}, want: "non-root"},
		{name: "invalid UID", user: &user.User{Uid: "user", Gid: "20"}, want: "UID"},
		{name: "invalid GID", user: &user.User{Uid: "501", Gid: "group"}, want: "GID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveDockerRuntimeUser("linux", func() (*user.User, error) { return tc.user, nil })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("resolveDockerRuntimeUser error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolveDockerRuntimeUserUsesFixedImageIdentityOnWindows(t *testing.T) {
	called := false
	got, err := resolveDockerRuntimeUser("windows", func() (*user.User, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("resolveDockerRuntimeUser: %v", err)
	}
	if called || got != ContainerRuntimeUser {
		t.Fatalf("called = %t, runtime user = %q; want fixed image identity %q", called, got, ContainerRuntimeUser)
	}
}

func TestDockerAgentConfigHelpersUseProvidedRuntimeUser(t *testing.T) {
	const runtimeUser = "1234:5678"
	execOptions := dockerAgentExecOptions(nil, nil, "/workspace/task", runtimeUser)
	containerConfig := dockerAgentContainerConfig("agent:latest", nil, nil, runtimeUser)
	if execOptions.User != runtimeUser || containerConfig.User != runtimeUser {
		t.Fatalf("exec user = %q, container user = %q, want %q", execOptions.User, containerConfig.User, runtimeUser)
	}
}
