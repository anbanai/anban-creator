package agent

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	dockerExecutionIDLabel = "anban.ai/execution-id"
	dockerTaskIDLabel      = "anban.ai/task-id"
	dockerProjectIDLabel   = "anban.ai/project-id"
	dockerUserIDLabel      = "anban.ai/user-id"
)

type dockerContainerInspector interface {
	ContainerInspect(context.Context, string) (container.InspectResponse, error)
}

type DockerWorkloadVerifier struct {
	docker dockerContainerInspector
	tokens *auth.WorkloadTokenService
}

func NewDockerWorkloadVerifier(docker dockerContainerInspector, tokens *auth.WorkloadTokenService) (*DockerWorkloadVerifier, error) {
	if docker == nil || tokens == nil {
		return nil, errors.New("Docker workload verifier requires client and token service")
	}
	return &DockerWorkloadVerifier{docker: docker, tokens: tokens}, nil
}

func (v *DockerWorkloadVerifier) Verify(ctx context.Context, rawToken, requestedExecutionID string) (*WorkloadIdentity, error) {
	if v == nil || v.docker == nil || v.tokens == nil {
		return nil, errors.New("Docker workload verifier is not configured")
	}
	requestedExecutionID = strings.TrimSpace(requestedExecutionID)
	if strings.TrimSpace(rawToken) == "" || requestedExecutionID == "" {
		return nil, errors.New("workload token and execution ID are required")
	}
	claims, err := v.tokens.Validate(rawToken)
	if err != nil {
		return nil, err
	}
	if claims.ExecutionID != requestedExecutionID {
		return nil, errors.New("workload token execution identity mismatch")
	}
	inspected, err := v.docker.ContainerInspect(ctx, claims.RuntimeWorkload)
	if err != nil {
		return nil, fmt.Errorf("inspect claimed Docker workload: %w", err)
	}
	if inspected.ContainerJSONBase == nil || inspected.ID == "" || inspected.ID != claims.RuntimeInstanceID {
		return nil, errors.New("Docker workload container identity mismatch")
	}
	if inspected.State == nil || inspected.State.Status != container.StateRunning || !inspected.State.Running || inspected.State.Paused || inspected.State.Restarting || inspected.State.Dead {
		return nil, errors.New("Docker workload container is not live")
	}
	if inspected.Config == nil {
		return nil, errors.New("Docker workload container has no ownership labels")
	}
	for _, expected := range []struct {
		label string
		value string
	}{
		{label: dockerExecutionIDLabel, value: claims.ExecutionID},
		{label: dockerTaskIDLabel, value: claims.TaskID},
		{label: dockerProjectIDLabel, value: claims.ProjectID},
		{label: dockerUserIDLabel, value: claims.UserID},
	} {
		if inspected.Config.Labels[expected.label] == "" || inspected.Config.Labels[expected.label] != expected.value {
			return nil, errors.New("Docker workload ownership labels mismatch")
		}
	}
	return &WorkloadIdentity{
		RuntimeIdentity: model.RuntimeIdentity{Scope: claims.RuntimeScope, Workload: claims.RuntimeWorkload, InstanceID: claims.RuntimeInstanceID},
		Target:          "docker",
		ExecutionID:     claims.ExecutionID,
		TaskID:          claims.TaskID,
		ProjectID:       claims.ProjectID,
		UserID:          claims.UserID,
		Deadline:        claims.ExpiresAt.Time,
	}, nil
}

var _ WorkloadVerifier = (*DockerWorkloadVerifier)(nil)

type dockerCurrentUserFunc func() (*user.User, error)

func currentDockerRuntimeUser() (string, error) {
	return resolveDockerRuntimeUser(runtime.GOOS, user.Current)
}

func resolveDockerRuntimeUser(goos string, current dockerCurrentUserFunc) (string, error) {
	if goos == "windows" {
		// Docker Desktop mediates Windows bind-mount ACLs. Keep the image identity
		// rather than attempting to map Windows account identifiers into Linux IDs.
		return ContainerRuntimeUser, nil
	}
	currentUser, err := current()
	if err != nil {
		return "", fmt.Errorf("resolve current host user for Docker: %w", err)
	}
	uid, err := strictDockerDecimalID("UID", currentUser.Uid)
	if err != nil {
		return "", err
	}
	gid, err := strictDockerDecimalID("GID", currentUser.Gid)
	if err != nil {
		return "", err
	}
	if uid == 0 {
		return "", fmt.Errorf("local Docker bind mounts require the server to run as a non-root or rootless user; host UID 0 is not allowed")
	}
	return fmt.Sprintf("%d:%d", uid, gid), nil
}

func strictDockerDecimalID(label, value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("host %s is empty", label)
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("host %s %q is not a decimal numeric ID", label, value)
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse host %s %q: %w", label, value, err)
	}
	return parsed, nil
}

func dockerRuntimeHome(workDirInContainer string) string {
	return path.Join(workDirInContainer, DockerRuntimeHomeDirName)
}
