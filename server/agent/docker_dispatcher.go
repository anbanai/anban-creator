package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	imageTypes "github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/anbanai/anban-creator/server/auth"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	dockerRuntimeScope         = "docker"
	dockerWorkloadTokenMaxAge  = time.Hour
	dockerContainerStopTimeout = 10
)

type dockerEngine interface {
	ImageInspect(context.Context, string, ...dockerclient.ImageInspectOption) (imageTypes.InspectResponse, error)
	VolumeInspect(context.Context, string) (volume.Volume, error)
	VolumeCreate(context.Context, volume.CreateOptions) (volume.Volume, error)
	VolumeRemove(context.Context, string, bool) error
	ContainerInspect(context.Context, string) (containertypes.InspectResponse, error)
	ContainerCreate(context.Context, *containertypes.Config, *containertypes.HostConfig, *networktypes.NetworkingConfig, *ocispec.Platform, string) (containertypes.CreateResponse, error)
	CopyToContainer(context.Context, string, string, io.Reader, containertypes.CopyToContainerOptions) error
	ContainerStart(context.Context, string, containertypes.StartOptions) error
	ContainerStop(context.Context, string, containertypes.StopOptions) error
	ContainerRemove(context.Context, string, containertypes.RemoveOptions) error
}

type DockerDispatcher struct {
	runtimeImages srvconfig.RuntimeImages
	config        srvconfig.DockerConfig
	serverURL     string
	tokens        *auth.WorkloadTokenService
	engine        dockerEngine
	now           func() time.Time
}

var _ RuntimeDispatcher = (*DockerDispatcher)(nil)
var _ dockerEngine = (*dockerclient.Client)(nil)

func NewDockerDispatcher(
	runtimeImages srvconfig.RuntimeImages,
	cfg srvconfig.DockerConfig,
	serverURL string,
	tokens *auth.WorkloadTokenService,
	engine dockerEngine,
	now func() time.Time,
) (*DockerDispatcher, error) {
	if tokens == nil || engine == nil || now == nil {
		return nil, fmt.Errorf("Docker dispatcher requires token service, Engine client, and clock")
	}
	if strings.TrimSpace(serverURL) == "" {
		return nil, fmt.Errorf("Docker dispatcher Server URL is required")
	}
	if strings.TrimSpace(cfg.Network) == "" || cfg.CPUCores <= 0 || cfg.MemoryMB <= 0 || cfg.PidsLimit <= 0 || cfg.TimeoutSec <= 0 {
		return nil, fmt.Errorf("Docker dispatcher configuration is incomplete")
	}
	for _, profile := range []string{model.PlatformArticle, model.PlatformSeednote, model.PlatformMontage} {
		if strings.TrimSpace(runtimeImages[profile]) == "" {
			return nil, fmt.Errorf("Docker runtime image %q is required", profile)
		}
	}
	return &DockerDispatcher{
		runtimeImages: runtimeImages,
		config:        cfg,
		serverURL:     strings.TrimRight(strings.TrimSpace(serverURL), "/"),
		tokens:        tokens,
		engine:        engine,
		now:           now,
	}, nil
}

func (*DockerDispatcher) Scope() string { return dockerRuntimeScope }

func (d *DockerDispatcher) ResolveRuntime(taskType string) srvconfig.RuntimeImageSelection {
	if d == nil {
		return srvconfig.RuntimeImageSelection{}
	}
	return RuntimeImageForTask(d.runtimeImages, taskType)
}

func (d *DockerDispatcher) Prepare(ctx context.Context, execution *model.TaskExecution, task *model.Task) (*model.RuntimeIdentity, error) {
	if err := d.validateDispatch(execution, task); err != nil {
		return nil, NewPermanentDispatchError(err)
	}

	dispatchCtx, cancel := context.WithTimeout(ctx, time.Duration(d.config.TimeoutSec)*time.Second)
	defer cancel()

	inspectedImage, err := d.engine.ImageInspect(dispatchCtx, execution.RuntimeImage)
	if err != nil {
		if errdefs.IsNotFound(err) || errdefs.IsInvalidParameter(err) || errdefs.IsForbidden(err) || errdefs.IsUnauthorized(err) {
			err = NewPermanentDispatchError(err)
		}
		return nil, fmt.Errorf("inspect Docker runtime image %q: %w", execution.RuntimeImage, err)
	}
	if strings.TrimSpace(inspectedImage.ID) == "" || inspectedImage.Config == nil {
		return nil, NewPermanentDispatchError(fmt.Errorf("Docker runtime image %q has incomplete trusted identity", execution.RuntimeImage))
	}

	imageConfig := dockerContainerConfigFromInspect(inspectedImage.Config)
	spec := buildDockerRuntimeSpec(dockerRuntimeConfig{
		DockerConfig: d.config,
		ServerURL:    d.serverURL,
		ImageID:      inspectedImage.ID,
		ImageConfig:  imageConfig,
	}, execution, task)
	if execution.RuntimeInstanceID != "" {
		if err := d.verifyPersistedContainer(dispatchCtx, spec, execution.RuntimeInstanceID); err != nil {
			return nil, err
		}
	}
	if err := d.ensureVolume(dispatchCtx, spec.ProjectVolume, "project memory", true); err != nil {
		return nil, err
	}
	allowTaskVolumeCreation := execution.Attempt <= 1 && strings.TrimSpace(execution.ParentExecutionID) == ""
	if err := d.ensureVolume(dispatchCtx, spec.TaskVolume, "task workspace", allowTaskVolumeCreation); err != nil {
		return nil, err
	}

	containerID, needsStart, err := d.ensureContainer(dispatchCtx, spec, execution.RuntimeInstanceID)
	if err != nil {
		return nil, err
	}
	identity := &model.RuntimeIdentity{Scope: dockerRuntimeScope, Workload: spec.ContainerName, InstanceID: containerID}
	if !needsStart {
		if execution.RuntimeInstanceID == "" {
			return nil, NewPermanentDispatchError(fmt.Errorf("Docker container %q was activated before its runtime identity was persisted", spec.ContainerName))
		}
		return identity, nil
	}

	issuedAt := d.now().UTC()
	tokenLifetime := spec.Timeout
	if tokenLifetime > dockerWorkloadTokenMaxAge {
		tokenLifetime = dockerWorkloadTokenMaxAge
	}
	rawToken, err := d.tokens.IssueAt(auth.WorkloadClaims{
		RuntimeScope:      identity.Scope,
		RuntimeWorkload:   identity.Workload,
		RuntimeInstanceID: identity.InstanceID,
		UserID:            task.UserID,
		ProjectID:         task.ProjectID,
		TaskID:            task.ID,
		ExecutionID:       execution.ID,
	}, issuedAt, issuedAt.Add(tokenLifetime))
	if err != nil {
		return nil, NewPermanentDispatchError(fmt.Errorf("issue Docker workload token: %w", err))
	}
	archive, err := dockerWorkloadTokenArchive(rawToken, issuedAt)
	if err != nil {
		return nil, NewPermanentDispatchError(err)
	}
	if err := d.engine.CopyToContainer(dispatchCtx, containerID, path.Dir(dockerWorkloadTokenFile), bytes.NewReader(archive), containertypes.CopyToContainerOptions{CopyUIDGID: true}); err != nil {
		return nil, fmt.Errorf("copy workload token into Docker container %q: %w", spec.ContainerName, classifyDockerContainerOperationError(err, execution.RuntimeInstanceID != ""))
	}
	return identity, nil
}

func (d *DockerDispatcher) ResolvePrepared(ctx context.Context, execution *model.TaskExecution, task *model.Task) (*model.RuntimeIdentity, error) {
	if err := d.validateDispatch(execution, task); err != nil {
		return nil, NewPermanentDispatchError(err)
	}

	resolveCtx, cancel := context.WithTimeout(ctx, time.Duration(d.config.TimeoutSec)*time.Second)
	defer cancel()
	inspectedImage, err := d.engine.ImageInspect(resolveCtx, execution.RuntimeImage)
	if err != nil {
		if errdefs.IsNotFound(err) || errdefs.IsInvalidParameter(err) || errdefs.IsForbidden(err) || errdefs.IsUnauthorized(err) {
			err = NewPermanentDispatchError(err)
		}
		return nil, fmt.Errorf("inspect Docker runtime image %q while resolving prepared container: %w", execution.RuntimeImage, err)
	}
	if strings.TrimSpace(inspectedImage.ID) == "" || inspectedImage.Config == nil {
		return nil, NewPermanentDispatchError(fmt.Errorf("Docker runtime image %q has incomplete trusted identity", execution.RuntimeImage))
	}

	spec := buildDockerRuntimeSpec(dockerRuntimeConfig{
		DockerConfig: d.config,
		ServerURL:    d.serverURL,
		ImageID:      inspectedImage.ID,
		ImageConfig:  dockerContainerConfigFromInspect(inspectedImage.Config),
	}, execution, task)
	inspected, err := d.engine.ContainerInspect(resolveCtx, spec.ContainerName)
	if errdefs.IsNotFound(err) {
		return nil, fmt.Errorf("resolve prepared Docker container %q: %w", spec.ContainerName, ErrRuntimeWorkloadNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect prepared Docker container %q: %w", spec.ContainerName, classifyDockerAccessOrInputError(err))
	}
	if err := verifyExistingDockerContainer(inspected, spec, execution.RuntimeInstanceID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(inspected.ID) == "" {
		return nil, NewPermanentDispatchError(fmt.Errorf("Docker container %q has no instance identity", spec.ContainerName))
	}
	return &model.RuntimeIdentity{Scope: dockerRuntimeScope, Workload: spec.ContainerName, InstanceID: inspected.ID}, nil
}

func (d *DockerDispatcher) Activate(ctx context.Context, execution *model.TaskExecution) error {
	if err := d.validatePersistedContainerIdentity(execution); err != nil {
		return NewPermanentDispatchError(err)
	}
	name := dockerRuntimeContainerName(execution.ID)
	activateCtx, cancel := context.WithTimeout(ctx, time.Duration(d.config.TimeoutSec)*time.Second)
	defer cancel()
	inspected, err := d.engine.ContainerInspect(activateCtx, name)
	if errdefs.IsNotFound(err) {
		return NewPermanentDispatchError(fmt.Errorf("persisted Docker container %q is missing: %w", name, err))
	}
	if err != nil {
		return fmt.Errorf("inspect Docker container %q before activation: %w", name, classifyDockerAccessOrInputError(err))
	}
	if err := verifyDockerExecutionContainer(inspected, execution); err != nil {
		return NewPermanentDispatchError(err)
	}
	if inspected.State != nil && inspected.State.Running {
		return nil
	}
	if inspected.State == nil || inspected.State.Status != containertypes.StateCreated {
		return NewPermanentDispatchError(fmt.Errorf("Docker container %q cannot be activated from state %q", name, dockerContainerState(inspected.State)))
	}
	if err := d.engine.ContainerStart(activateCtx, inspected.ID, containertypes.StartOptions{}); err != nil && !errdefs.IsNotModified(err) {
		return fmt.Errorf("start Docker container %q: %w", name, classifyDockerContainerOperationError(err, true))
	}
	return nil
}

func dockerContainerState(state *containertypes.State) containertypes.ContainerState {
	if state == nil {
		return ""
	}
	return state.Status
}

func (d *DockerDispatcher) ensureVolume(ctx context.Context, desired volume.CreateOptions, kind string, allowCreation bool) error {
	existing, err := d.engine.VolumeInspect(ctx, desired.Name)
	if errdefs.IsNotFound(err) {
		if !allowCreation {
			return NewPermanentDispatchError(fmt.Errorf("original task workspace volume %q is missing; resume cannot continue", desired.Name))
		}
		existing, err = d.engine.VolumeCreate(ctx, desired)
		if errdefs.IsConflict(err) {
			existing, err = d.engine.VolumeInspect(ctx, desired.Name)
			if err != nil {
				return fmt.Errorf("inspect %s Docker volume %q after create conflict: %w", kind, desired.Name, classifyDockerAccessOrInputError(err))
			}
		} else if err != nil {
			return fmt.Errorf("create %s Docker volume %q: %w", kind, desired.Name, classifyDockerCreateError(err))
		}
	} else if err != nil {
		return fmt.Errorf("inspect %s Docker volume %q: %w", kind, desired.Name, classifyDockerAccessOrInputError(err))
	}
	if err := verifyDockerVolume(existing, desired); err != nil {
		return NewPermanentDispatchError(fmt.Errorf("%s volume %q: %w", kind, desired.Name, err))
	}
	return nil
}

func (d *DockerDispatcher) ensureContainer(ctx context.Context, desired dockerRuntimeSpec, persistedInstanceID string) (string, bool, error) {
	existing, err := d.engine.ContainerInspect(ctx, desired.ContainerName)
	if err == nil {
		if err := verifyExistingDockerContainer(existing, desired, persistedInstanceID); err != nil {
			return "", false, err
		}
		return existing.ID, dockerContainerNeedsStart(existing.State), nil
	}
	if !errdefs.IsNotFound(err) {
		return "", false, fmt.Errorf("inspect Docker container %q: %w", desired.ContainerName, classifyDockerAccessOrInputError(err))
	}
	if persistedInstanceID != "" {
		return "", false, NewPermanentDispatchError(fmt.Errorf("persisted Docker container %q instance identity mismatch: container %q is missing", desired.ContainerName, persistedInstanceID))
	}

	created, err := d.engine.ContainerCreate(ctx, desired.ContainerConfig, desired.HostConfig, nil, nil, desired.ContainerName)
	if errdefs.IsConflict(err) {
		existing, err = d.engine.ContainerInspect(ctx, desired.ContainerName)
		if err != nil {
			return "", false, fmt.Errorf("inspect Docker container %q after create conflict: %w", desired.ContainerName, classifyDockerAccessOrInputError(err))
		}
		if err := verifyExistingDockerContainer(existing, desired, persistedInstanceID); err != nil {
			return "", false, err
		}
		return existing.ID, dockerContainerNeedsStart(existing.State), nil
	}
	if err != nil {
		return "", false, fmt.Errorf("create Docker container %q: %w", desired.ContainerName, classifyDockerCreateError(err))
	}
	if strings.TrimSpace(created.ID) == "" {
		return "", false, NewPermanentDispatchError(fmt.Errorf("create Docker container %q returned no instance ID", desired.ContainerName))
	}
	return created.ID, true, nil
}

func (d *DockerDispatcher) verifyPersistedContainer(ctx context.Context, desired dockerRuntimeSpec, persistedInstanceID string) error {
	existing, err := d.engine.ContainerInspect(ctx, desired.ContainerName)
	if errdefs.IsNotFound(err) {
		return NewPermanentDispatchError(fmt.Errorf("persisted Docker container %q instance identity mismatch: container %q is missing", desired.ContainerName, persistedInstanceID))
	}
	if err != nil {
		return fmt.Errorf("inspect persisted Docker container %q: %w", desired.ContainerName, classifyDockerAccessOrInputError(err))
	}
	return verifyExistingDockerContainer(existing, desired, persistedInstanceID)
}

func verifyExistingDockerContainer(existing containertypes.InspectResponse, desired dockerRuntimeSpec, persistedInstanceID string) error {
	if err := verifyDockerContainer(existing, desired); err != nil {
		return NewPermanentDispatchError(fmt.Errorf("Docker container %q: %w", desired.ContainerName, err))
	}
	return verifyPersistedDockerInstance(existing.ID, persistedInstanceID, desired.ContainerName)
}

func verifyPersistedDockerInstance(actual, persisted, containerName string) error {
	if persisted != "" && actual != persisted {
		return NewPermanentDispatchError(fmt.Errorf("Docker container %q instance identity mismatch: got %q, want persisted %q", containerName, actual, persisted))
	}
	return nil
}

func dockerContainerNeedsStart(state *containertypes.State) bool {
	return state == nil || state.Status == "" || state.Status == containertypes.StateCreated
}

func dockerContainerConfigFromInspect(source *dockerspec.DockerOCIImageConfig) *containertypes.Config {
	if source == nil {
		return nil
	}
	config := &containertypes.Config{
		Env:         slices.Clone(source.Env),
		Volumes:     maps.Clone(source.Volumes),
		Labels:      maps.Clone(source.Labels),
		StopSignal:  source.StopSignal,
		Shell:       slices.Clone(source.Shell),
		ArgsEscaped: false,
	}
	if source.ExposedPorts != nil {
		config.ExposedPorts = make(nat.PortSet, len(source.ExposedPorts))
		for port := range source.ExposedPorts {
			config.ExposedPorts[nat.Port(port)] = struct{}{}
		}
	}
	if source.Healthcheck != nil {
		config.Healthcheck = &containertypes.HealthConfig{
			Test:          slices.Clone(source.Healthcheck.Test),
			Interval:      source.Healthcheck.Interval,
			Timeout:       source.Healthcheck.Timeout,
			StartPeriod:   source.Healthcheck.StartPeriod,
			StartInterval: source.Healthcheck.StartInterval,
			Retries:       source.Healthcheck.Retries,
		}
	}
	return config
}

func dockerWorkloadTokenArchive(rawToken string, modifiedAt time.Time) ([]byte, error) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	header := &tar.Header{
		Name: path.Base(dockerWorkloadTokenFile), Mode: 0o400, Uid: 1000, Gid: 1000,
		Size: int64(len(rawToken)), Typeflag: tar.TypeReg, ModTime: modifiedAt,
	}
	if err := writer.WriteHeader(header); err != nil {
		return nil, fmt.Errorf("write Docker workload token archive header: %w", err)
	}
	if _, err := writer.Write([]byte(rawToken)); err != nil {
		return nil, fmt.Errorf("write Docker workload token archive: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close Docker workload token archive: %w", err)
	}
	return archive.Bytes(), nil
}

func classifyDockerAccessOrInputError(err error) error {
	if err == nil {
		return nil
	}
	if errdefs.IsInvalidParameter(err) || errdefs.IsForbidden(err) || errdefs.IsUnauthorized(err) {
		return NewPermanentDispatchError(err)
	}
	return err
}

func classifyDockerCreateError(err error) error {
	if errdefs.IsNotFound(err) {
		return NewPermanentDispatchError(err)
	}
	return classifyDockerAccessOrInputError(err)
}

func classifyDockerContainerOperationError(err error, persistedInstance bool) error {
	if persistedInstance && errdefs.IsNotFound(err) {
		return NewPermanentDispatchError(err)
	}
	return classifyDockerAccessOrInputError(err)
}

func (d *DockerDispatcher) Inspect(ctx context.Context, execution *model.TaskExecution) (*RuntimeExecutionState, error) {
	if err := d.validateExecution(execution); err != nil {
		return nil, err
	}
	name := dockerRuntimeContainerName(execution.ID)
	inspected, err := d.engine.ContainerInspect(ctx, name)
	if errdefs.IsNotFound(err) {
		return nil, fmt.Errorf("inspect Docker container %q: %w", name, ErrRuntimeWorkloadNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Docker container %q: %w", name, err)
	}
	if err := verifyDockerExecutionContainer(inspected, execution); err != nil {
		return nil, err
	}
	return inspectDockerContainer(inspected), nil
}

func inspectDockerContainer(inspected containertypes.InspectResponse) *RuntimeExecutionState {
	state := &RuntimeExecutionState{Phase: RuntimePhasePending, InstanceID: inspected.ID, Reason: "Created"}
	if inspected.State == nil {
		return state
	}
	state.Message = strings.TrimSpace(inspected.State.Error)
	switch inspected.State.Status {
	case containertypes.StateCreated:
		state.Phase = RuntimePhasePending
		state.Reason = "Created"
	case containertypes.StateRunning:
		state.Phase = RuntimePhaseRunning
		state.Reason = "Running"
	case containertypes.StateExited:
		exitCode := int32(inspected.State.ExitCode)
		state.ExitCode = &exitCode
		state.Reason = "Exited"
		if inspected.State.OOMKilled {
			state.Reason = "OOMKilled"
		}
		if inspected.State.ExitCode == 0 && !inspected.State.OOMKilled {
			state.Phase = RuntimePhaseSucceeded
		} else {
			state.Phase = RuntimePhaseFailed
		}
	case containertypes.StateDead:
		exitCode := int32(inspected.State.ExitCode)
		state.ExitCode = &exitCode
		state.Phase = RuntimePhaseFailed
		state.Reason = "Dead"
	default:
		if inspected.State.Running || inspected.State.Paused || inspected.State.Restarting {
			state.Phase = RuntimePhaseRunning
			state.Reason = dockerStateReason(inspected.State.Status)
		}
	}
	if finishedAt, err := time.Parse(time.RFC3339Nano, inspected.State.FinishedAt); err == nil && !finishedAt.IsZero() {
		completed := finishedAt.UTC()
		state.CompletedAt = &completed
	}
	return state
}

func dockerStateReason(status containertypes.ContainerState) string {
	value := strings.TrimSpace(string(status))
	if value == "" {
		return "Unknown"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func (d *DockerDispatcher) Delete(ctx context.Context, execution *model.TaskExecution) error {
	if err := d.validatePersistedContainerIdentity(execution); err != nil {
		return NewPermanentDispatchError(err)
	}
	name := dockerRuntimeContainerName(execution.ID)
	inspected, err := d.engine.ContainerInspect(ctx, name)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Docker container %q before delete: %w", name, err)
	}
	if err := verifyDockerExecutionContainer(inspected, execution); err != nil {
		return err
	}
	if inspected.State != nil && (inspected.State.Running || inspected.State.Paused || inspected.State.Restarting) {
		timeout := dockerContainerStopTimeout
		if err := d.engine.ContainerStop(ctx, inspected.ID, containertypes.StopOptions{Timeout: &timeout}); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsNotModified(err) {
			return fmt.Errorf("stop Docker container %q: %w", name, err)
		}
	}
	if err := d.engine.ContainerRemove(ctx, inspected.ID, containertypes.RemoveOptions{}); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("remove Docker container %q: %w", name, err)
	}
	return nil
}

func (d *DockerDispatcher) DeleteTaskWorkspace(ctx context.Context, task *model.Task) error {
	if d == nil || d.engine == nil {
		return fmt.Errorf("Docker dispatcher is not configured")
	}
	if err := validateDockerTaskIdentity(task); err != nil {
		return err
	}
	desired := dockerTaskWorkspaceVolume(task)
	return d.deleteVolume(ctx, desired, func(existing volume.Volume) error {
		return verifyDockerVolume(existing, desired)
	})
}

func (d *DockerDispatcher) DeleteProjectMemory(ctx context.Context, projectID string) error {
	if d == nil || d.engine == nil {
		return fmt.Errorf("Docker dispatcher is not configured")
	}
	if !isCanonicalDockerIdentity(projectID) {
		return fmt.Errorf("project ID is required and must be canonical")
	}
	desiredName := dockerProjectMemoryVolumeName(projectID)
	return d.deleteVolume(ctx, volume.CreateOptions{Name: desiredName}, func(existing volume.Volume) error {
		if existing.Name != desiredName || existing.Driver != dockerVolumeDriver || len(existing.Labels) != 2 ||
			existing.Labels[dockerProjectIDLabel] != projectID || !isCanonicalDockerIdentity(existing.Labels[dockerUserIDLabel]) || len(existing.Options) != 0 {
			return fmt.Errorf("Docker project memory volume identity or specification drift")
		}
		return nil
	})
}

func (d *DockerDispatcher) deleteVolume(ctx context.Context, desired volume.CreateOptions, verify func(volume.Volume) error) error {
	existing, err := d.engine.VolumeInspect(ctx, desired.Name)
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Docker volume %q before delete: %w", desired.Name, err)
	}
	if err := verify(existing); err != nil {
		return fmt.Errorf("refuse to delete Docker volume %q: %w", desired.Name, err)
	}
	if err := d.engine.VolumeRemove(ctx, desired.Name, false); err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("remove Docker volume %q: %w", desired.Name, err)
	}
	return nil
}

func dockerProjectMemoryVolume(task *model.Task) volume.CreateOptions {
	return volume.CreateOptions{
		Name: dockerProjectMemoryVolumeName(task.ProjectID), Driver: dockerVolumeDriver,
		Labels: map[string]string{dockerProjectIDLabel: task.ProjectID, dockerUserIDLabel: task.UserID},
	}
}

func dockerTaskWorkspaceVolume(task *model.Task) volume.CreateOptions {
	return volume.CreateOptions{
		Name: dockerTaskWorkspaceVolumeName(task.ID), Driver: dockerVolumeDriver,
		Labels: map[string]string{dockerTaskIDLabel: task.ID, dockerProjectIDLabel: task.ProjectID, dockerUserIDLabel: task.UserID},
	}
}

func (d *DockerDispatcher) validateDispatch(execution *model.TaskExecution, task *model.Task) error {
	if err := d.validateExecution(execution); err != nil {
		return err
	}
	if err := validateDockerTaskIdentity(task); err != nil {
		return err
	}
	if execution.TaskID != task.ID {
		return fmt.Errorf("execution task identity mismatch: execution has %q, task has %q", execution.TaskID, task.ID)
	}
	if execution.Attempt <= 1 && strings.TrimSpace(execution.ParentExecutionID) == "" {
		configured := d.ResolveRuntime(task.Type)
		if execution.RuntimeProfile != strings.TrimSpace(configured.Profile) || execution.RuntimeImage != strings.TrimSpace(configured.Image) {
			return fmt.Errorf(
				"initial runtime identity mismatch: execution has %q %q, configured runtime is %q %q",
				execution.RuntimeProfile, execution.RuntimeImage, strings.TrimSpace(configured.Profile), strings.TrimSpace(configured.Image),
			)
		}
	}
	return nil
}

func (d *DockerDispatcher) validateExecution(execution *model.TaskExecution) error {
	if d == nil || d.engine == nil || d.tokens == nil || d.now == nil {
		return fmt.Errorf("Docker dispatcher is not configured")
	}
	if execution == nil || !isCanonicalDockerIdentity(execution.ID) || !isCanonicalDockerIdentity(execution.TaskID) {
		return fmt.Errorf("execution is required with canonical execution and task IDs")
	}
	if !isCanonicalDockerIdentity(execution.RuntimeProfile) || !isCanonicalDockerIdentity(execution.RuntimeImage) {
		return fmt.Errorf("execution runtime identity is required and must be canonical")
	}
	wantWorkload := dockerRuntimeContainerName(execution.ID)
	if execution.RuntimeScope != "" && execution.RuntimeScope != dockerRuntimeScope {
		return fmt.Errorf("execution runtime scope identity mismatch: scope is %q, want %q", execution.RuntimeScope, dockerRuntimeScope)
	}
	if execution.RuntimeWorkload != "" && execution.RuntimeWorkload != wantWorkload {
		return fmt.Errorf("execution workload identity mismatch: name is %q, want %q", execution.RuntimeWorkload, wantWorkload)
	}
	if execution.RuntimeInstanceID != "" && !isCanonicalDockerIdentity(execution.RuntimeInstanceID) {
		return fmt.Errorf("execution runtime instance identity must be canonical")
	}
	return nil
}

func (d *DockerDispatcher) validatePersistedContainerIdentity(execution *model.TaskExecution) error {
	if err := d.validateExecution(execution); err != nil {
		return err
	}
	if execution.RuntimeScope != dockerRuntimeScope ||
		execution.RuntimeWorkload != dockerRuntimeContainerName(execution.ID) ||
		execution.RuntimeInstanceID == "" {
		return fmt.Errorf("persisted Docker runtime identity is incomplete or mismatched")
	}
	return nil
}

func validateDockerTaskIdentity(task *model.Task) error {
	if task == nil || !isCanonicalDockerIdentity(task.ID) || !isCanonicalDockerIdentity(task.UserID) || !isCanonicalDockerIdentity(task.ProjectID) {
		return fmt.Errorf("task identity is required and must be canonical")
	}
	return nil
}

func isCanonicalDockerIdentity(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}

func verifyDockerExecutionContainer(inspected containertypes.InspectResponse, execution *model.TaskExecution) error {
	wantName := dockerRuntimeContainerName(execution.ID)
	if inspected.ContainerJSONBase == nil || inspected.Config == nil || inspected.ID == "" || strings.TrimPrefix(inspected.Name, "/") != wantName {
		return fmt.Errorf("Docker container %q identity mismatch", wantName)
	}
	if execution.RuntimeInstanceID != "" && inspected.ID != execution.RuntimeInstanceID {
		return fmt.Errorf("Docker container %q instance identity mismatch", wantName)
	}
	if strings.TrimSpace(inspected.Image) == "" || inspected.Config.Image != inspected.Image {
		return fmt.Errorf("Docker container %q runtime image identity mismatch", wantName)
	}
	if inspected.Config.Labels[dockerExecutionIDLabel] != execution.ID ||
		inspected.Config.Labels[dockerTaskIDLabel] != execution.TaskID ||
		!isCanonicalDockerIdentity(inspected.Config.Labels[dockerProjectIDLabel]) ||
		!isCanonicalDockerIdentity(inspected.Config.Labels[dockerUserIDLabel]) {
		return fmt.Errorf("Docker container %q ownership labels mismatch", wantName)
	}
	return nil
}
