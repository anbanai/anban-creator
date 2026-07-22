package agent

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const (
	dockerTaskWorkspaceMountPath = "/workspace"
	dockerProjectMemoryMountPath = "/workspace/.claude/memory"
	dockerWorkloadTokenFile      = "/run/secrets/anban/token"
	dockerVolumeDriver           = "local"
)

type dockerRuntimeConfig struct {
	srvconfig.DockerConfig
	ServerURL string
}

type dockerRuntimeSpec struct {
	ContainerName   string
	ProjectVolume   volume.CreateOptions
	TaskVolume      volume.CreateOptions
	ContainerConfig *containertypes.Config
	HostConfig      *containertypes.HostConfig
	Timeout         time.Duration
}

func buildDockerRuntimeSpec(cfg dockerRuntimeConfig, execution *model.TaskExecution, task *model.Task) dockerRuntimeSpec {
	pidsLimit := cfg.PidsLimit
	labels := map[string]string{
		dockerExecutionIDLabel: executionID(execution),
		dockerTaskIDLabel:      taskID(task),
		dockerProjectIDLabel:   projectID(task),
		dockerUserIDLabel:      dockerTaskUserID(task),
	}
	projectVolume := volume.CreateOptions{
		Name:   dockerProjectMemoryVolumeName(projectID(task)),
		Driver: dockerVolumeDriver,
		Labels: map[string]string{
			dockerProjectIDLabel: projectID(task),
			dockerUserIDLabel:    dockerTaskUserID(task),
		},
	}
	taskVolume := volume.CreateOptions{
		Name:   dockerTaskWorkspaceVolumeName(taskID(task)),
		Driver: dockerVolumeDriver,
		Labels: map[string]string{
			dockerTaskIDLabel:    taskID(task),
			dockerProjectIDLabel: projectID(task),
			dockerUserIDLabel:    dockerTaskUserID(task),
		},
	}
	return dockerRuntimeSpec{
		ContainerName: dockerRuntimeContainerName(executionID(execution)),
		ProjectVolume: projectVolume,
		TaskVolume:    taskVolume,
		ContainerConfig: &containertypes.Config{
			Image:      dockerExecutionRuntimeImage(execution),
			User:       ContainerRuntimeUser,
			Entrypoint: []string{"tini", "--", AgentBinaryName},
			Cmd: []string{
				"job",
				"--server-url", strings.TrimRight(cfg.ServerURL, "/"),
				"--execution-id", executionID(execution),
				"--workspace", dockerTaskWorkspaceMountPath,
				"--workload-token-file", dockerWorkloadTokenFile,
			},
			WorkingDir: dockerTaskWorkspaceMountPath,
			Labels:     labels,
		},
		HostConfig: &containertypes.HostConfig{
			NetworkMode: containertypes.NetworkMode(cfg.Network),
			RestartPolicy: containertypes.RestartPolicy{
				Name: containertypes.RestartPolicyDisabled,
			},
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges:true"},
			Resources: containertypes.Resources{
				NanoCPUs:  cfg.CPUCores * 1_000_000_000,
				Memory:    cfg.MemoryMB * 1024 * 1024,
				PidsLimit: &pidsLimit,
			},
			Mounts: []mount.Mount{
				{Type: mount.TypeVolume, Source: taskVolume.Name, Target: dockerTaskWorkspaceMountPath},
				{Type: mount.TypeVolume, Source: projectVolume.Name, Target: dockerProjectMemoryMountPath},
			},
		},
		Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
	}
}

func verifyDockerContainer(existing containertypes.InspectResponse, desired dockerRuntimeSpec) error {
	if existing.ContainerJSONBase == nil || existing.Config == nil || existing.HostConfig == nil || desired.ContainerConfig == nil || desired.HostConfig == nil {
		return fmt.Errorf("Docker container specification is incomplete")
	}
	checks := []struct {
		name  string
		equal bool
	}{
		{name: "name", equal: strings.TrimPrefix(existing.Name, "/") == desired.ContainerName},
		{name: "labels", equal: dockerOwnershipLabelsEqual(existing.Config.Labels, desired.ContainerConfig.Labels)},
		{name: "image", equal: existing.Config.Image == desired.ContainerConfig.Image},
		{name: "user", equal: existing.Config.User == desired.ContainerConfig.User},
		{name: "entrypoint", equal: slices.Equal(existing.Config.Entrypoint, desired.ContainerConfig.Entrypoint)},
		{name: "command", equal: slices.Equal(existing.Config.Cmd, desired.ContainerConfig.Cmd)},
		{name: "working directory", equal: existing.Config.WorkingDir == desired.ContainerConfig.WorkingDir},
		{name: "mounts", equal: reflect.DeepEqual(existing.HostConfig.Mounts, desired.HostConfig.Mounts)},
		{name: "binds", equal: slices.Equal(existing.HostConfig.Binds, desired.HostConfig.Binds)},
		{name: "volumes-from", equal: slices.Equal(existing.HostConfig.VolumesFrom, desired.HostConfig.VolumesFrom)},
		{name: "network", equal: existing.HostConfig.NetworkMode == desired.HostConfig.NetworkMode},
		{name: "memory", equal: existing.HostConfig.Memory == desired.HostConfig.Memory},
		{name: "CPU", equal: existing.HostConfig.NanoCPUs == desired.HostConfig.NanoCPUs},
		{name: "PID limit", equal: equalInt64Pointer(existing.HostConfig.PidsLimit, desired.HostConfig.PidsLimit)},
		{name: "privileged mode", equal: existing.HostConfig.Privileged == desired.HostConfig.Privileged},
		{name: "added capabilities", equal: slices.Equal(existing.HostConfig.CapAdd, desired.HostConfig.CapAdd)},
		{name: "dropped capabilities", equal: slices.Equal(existing.HostConfig.CapDrop, desired.HostConfig.CapDrop)},
		{name: "security options", equal: slices.Equal(existing.HostConfig.SecurityOpt, desired.HostConfig.SecurityOpt)},
		{name: "devices", equal: reflect.DeepEqual(existing.HostConfig.Devices, desired.HostConfig.Devices)},
		{name: "device requests", equal: reflect.DeepEqual(existing.HostConfig.DeviceRequests, desired.HostConfig.DeviceRequests)},
		{name: "device cgroup rules", equal: slices.Equal(existing.HostConfig.DeviceCgroupRules, desired.HostConfig.DeviceCgroupRules)},
		{name: "restart policy", equal: existing.HostConfig.RestartPolicy == desired.HostConfig.RestartPolicy},
		{name: "auto-remove", equal: existing.HostConfig.AutoRemove == desired.HostConfig.AutoRemove},
		{name: "published ports", equal: existing.HostConfig.PublishAllPorts == desired.HostConfig.PublishAllPorts},
	}
	for _, check := range checks {
		if !check.equal {
			return fmt.Errorf("Docker container %s drift", check.name)
		}
	}
	return nil
}

func verifyDockerVolume(existing volume.Volume, desired volume.CreateOptions) error {
	checks := []struct {
		name  string
		equal bool
	}{
		{name: "name", equal: existing.Name == desired.Name},
		{name: "driver", equal: existing.Driver == desired.Driver},
		{name: "labels", equal: maps.Equal(existing.Labels, desired.Labels)},
		{name: "driver options", equal: maps.Equal(existing.Options, desired.DriverOpts)},
	}
	for _, check := range checks {
		if !check.equal {
			return fmt.Errorf("Docker volume %s drift", check.name)
		}
	}
	return nil
}

func dockerOwnershipLabelsEqual(existing, desired map[string]string) bool {
	for label, value := range desired {
		if existing[label] != value {
			return false
		}
	}
	for label := range existing {
		if strings.HasPrefix(label, "anban.ai/") {
			if _, expected := desired[label]; !expected {
				return false
			}
		}
	}
	return true
}

func dockerTaskUserID(task *model.Task) string {
	if task == nil {
		return ""
	}
	return task.UserID
}

func dockerExecutionRuntimeImage(execution *model.TaskExecution) string {
	if execution == nil {
		return ""
	}
	return execution.RuntimeImage
}

func equalInt64Pointer(left, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
