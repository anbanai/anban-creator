package agent

import (
	"os/user"
	"strings"
	"testing"
)

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
