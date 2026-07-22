package agent

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestBuildDockerRuntimeSpec(t *testing.T) {
	cfg := dockerRuntimeConfig{
		DockerConfig: srvconfig.DockerConfig{
			Network: "anban", CPUCores: 2, MemoryMB: 4096, PidsLimit: 256, TimeoutSec: 3600,
		},
		ServerURL: "http://server:8080/",
	}
	execution := &model.TaskExecution{
		ID: "execution-123", TaskID: "task-456", RuntimeImage: "registry.example/creator-agent-article@sha256:persisted",
	}
	task := &model.Task{ID: "task-456", ProjectID: "project-789", UserID: "user-012"}

	spec := buildDockerRuntimeSpec(cfg, execution, task)

	if spec.ContainerName != dockerRuntimeContainerName(execution.ID) {
		t.Fatalf("container name = %q, want deterministic execution name", spec.ContainerName)
	}
	if spec.ProjectVolume.Name != dockerProjectMemoryVolumeName(task.ProjectID) {
		t.Fatalf("project volume name = %q, want deterministic project name", spec.ProjectVolume.Name)
	}
	if spec.TaskVolume.Name != dockerTaskWorkspaceVolumeName(task.ID) {
		t.Fatalf("task volume name = %q, want deterministic task name", spec.TaskVolume.Name)
	}
	if spec.ContainerConfig.Image != execution.RuntimeImage {
		t.Fatalf("image = %q, want persisted runtime image %q", spec.ContainerConfig.Image, execution.RuntimeImage)
	}
	if spec.ContainerConfig.User != ContainerRuntimeUser {
		t.Fatalf("user = %q, want %q", spec.ContainerConfig.User, ContainerRuntimeUser)
	}
	wantLabels := map[string]string{
		dockerExecutionIDLabel: execution.ID,
		dockerTaskIDLabel:      task.ID,
		dockerProjectIDLabel:   task.ProjectID,
		dockerUserIDLabel:      task.UserID,
	}
	if !maps.Equal(spec.ContainerConfig.Labels, wantLabels) {
		t.Fatalf("container labels = %#v, want %#v", spec.ContainerConfig.Labels, wantLabels)
	}
	if len(spec.HostConfig.Binds) != 0 || len(spec.HostConfig.VolumesFrom) != 0 {
		t.Fatalf("host paths or inherited volumes configured: binds=%v volumesFrom=%v", spec.HostConfig.Binds, spec.HostConfig.VolumesFrom)
	}
	wantMounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: spec.TaskVolume.Name, Target: dockerTaskWorkspaceMountPath},
		{Type: mount.TypeVolume, Source: spec.ProjectVolume.Name, Target: dockerProjectMemoryMountPath},
	}
	if !reflect.DeepEqual(spec.HostConfig.Mounts, wantMounts) {
		t.Fatalf("mounts = %#v, want exactly named workspace mounts %#v", spec.HostConfig.Mounts, wantMounts)
	}
	if spec.HostConfig.NetworkMode != containertypes.NetworkMode("anban") {
		t.Fatalf("network = %q, want anban", spec.HostConfig.NetworkMode)
	}
	if spec.HostConfig.NanoCPUs != 2_000_000_000 || spec.HostConfig.Memory != 4096*1024*1024 {
		t.Fatalf("resources = cpu %d memory %d", spec.HostConfig.NanoCPUs, spec.HostConfig.Memory)
	}
	if spec.HostConfig.PidsLimit == nil || *spec.HostConfig.PidsLimit != 256 {
		t.Fatalf("PIDs limit = %v, want 256", spec.HostConfig.PidsLimit)
	}
	if spec.Timeout != time.Hour {
		t.Fatalf("timeout = %v, want 1h", spec.Timeout)
	}
	wantEntrypoint := []string{"tini", "--", "anban"}
	wantCommand := []string{
		"job",
		"--server-url", "http://server:8080",
		"--execution-id", execution.ID,
		"--workspace", dockerTaskWorkspaceMountPath,
		"--workload-token-file", dockerWorkloadTokenFile,
	}
	if !slices.Equal(spec.ContainerConfig.Entrypoint, wantEntrypoint) || !slices.Equal(spec.ContainerConfig.Cmd, wantCommand) {
		t.Fatalf("command = %v %v, want %v %v", spec.ContainerConfig.Entrypoint, spec.ContainerConfig.Cmd, wantEntrypoint, wantCommand)
	}
	if spec.ContainerConfig.WorkingDir != dockerTaskWorkspaceMountPath {
		t.Fatalf("working directory = %q, want %q", spec.ContainerConfig.WorkingDir, dockerTaskWorkspaceMountPath)
	}
	if !maps.Equal(spec.ProjectVolume.Labels, map[string]string{
		dockerProjectIDLabel: task.ProjectID,
		dockerUserIDLabel:    task.UserID,
	}) {
		t.Fatalf("project volume labels = %#v", spec.ProjectVolume.Labels)
	}
	if !maps.Equal(spec.TaskVolume.Labels, map[string]string{
		dockerTaskIDLabel:    task.ID,
		dockerProjectIDLabel: task.ProjectID,
		dockerUserIDLabel:    task.UserID,
	}) {
		t.Fatalf("task volume labels = %#v", spec.TaskVolume.Labels)
	}
}

func TestAgentImagesPrecreateWorkloadSecretDirectory(t *testing.T) {
	const contract = "install -d -m 0700 -o 1000 -g 1000 /run/secrets/anban"
	for _, name := range []string{"article", "seednote", "montage"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "deploy", "docker", "Dockerfile.agent-"+name)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			contents := string(raw)
			contractAt := strings.LastIndex(contents, contract)
			finalUserAt := strings.LastIndex(contents, "USER 1000:1000")
			if contractAt < 0 || contractAt > finalUserAt {
				t.Fatalf("%s must contain %q before final USER 1000:1000", path, contract)
			}
			if rootAt := strings.LastIndex(contents[:contractAt], "USER root"); rootAt < 0 || strings.LastIndex(contents[:contractAt], "USER 1000:1000") > rootAt {
				t.Fatalf("%s must precreate the secret directory while running as root", path)
			}
		})
	}
}

func TestVerifyDockerContainerAcceptsExactSpecAndRejectsDrift(t *testing.T) {
	spec := testDockerRuntimeSpec()
	if err := verifyDockerContainer(testDockerInspect(spec), spec); err != nil {
		t.Fatalf("exact container spec rejected: %v", err)
	}
	withImageMetadata := testDockerInspect(spec)
	withImageMetadata.Config.Labels["org.opencontainers.image.authors"] = "upstream image author"
	if err := verifyDockerContainer(withImageMetadata, spec); err != nil {
		t.Fatalf("container with inherited image metadata rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*containertypes.InspectResponse)
	}{
		{name: "container name", mutate: func(got *containertypes.InspectResponse) { got.Name = "/replacement" }},
		{name: "labels", mutate: func(got *containertypes.InspectResponse) { got.Config.Labels[dockerTaskIDLabel] = "replacement" }},
		{name: "unknown ownership label", mutate: func(got *containertypes.InspectResponse) { got.Config.Labels["anban.ai/replacement"] = "unexpected" }},
		{name: "image", mutate: func(got *containertypes.InspectResponse) { got.Config.Image = "replacement:latest" }},
		{name: "user", mutate: func(got *containertypes.InspectResponse) { got.Config.User = "0:0" }},
		{name: "mounts", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Mounts[0].Target = "/replacement" }},
		{name: "command", mutate: func(got *containertypes.InspectResponse) { got.Config.Cmd[0] = "run" }},
		{name: "network", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.NetworkMode = "bridge" }},
		{name: "memory", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Memory++ }},
		{name: "CPU", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.NanoCPUs++ }},
		{name: "PID limit", mutate: func(got *containertypes.InspectResponse) { *got.HostConfig.PidsLimit++ }},
		{name: "binds", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Binds = []string{"/host:/workspace"} }},
		{name: "VolumesFrom", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.VolumesFrom = []string{"other"} }},
		{name: "privileged", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Privileged = true }},
		{name: "capabilities added", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CapAdd = []string{"SYS_ADMIN"} }},
		{name: "capability drop removed", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CapDrop = nil }},
		{name: "device", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.Devices = []containertypes.DeviceMapping{{PathOnHost: "/dev/null"}}
		}},
		{name: "restart", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.RestartPolicy.Name = containertypes.RestartPolicyAlways
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := testDockerInspect(spec)
			tc.mutate(&got)
			if err := verifyDockerContainer(got, spec); err == nil {
				t.Fatal("drifted container spec accepted")
			}
		})
	}
}

func TestVerifyDockerVolumeAcceptsExactSpecAndRejectsDrift(t *testing.T) {
	spec := testDockerRuntimeSpec()
	for _, desired := range []volume.CreateOptions{spec.ProjectVolume, spec.TaskVolume} {
		if err := verifyDockerVolume(testDockerVolume(desired), desired); err != nil {
			t.Fatalf("exact volume spec rejected: %v", err)
		}
		for _, tc := range []struct {
			name   string
			mutate func(*volume.Volume)
		}{
			{name: "name", mutate: func(got *volume.Volume) { got.Name = "replacement" }},
			{name: "driver", mutate: func(got *volume.Volume) { got.Driver = "replacement" }},
			{name: "labels", mutate: func(got *volume.Volume) { got.Labels[dockerUserIDLabel] = "replacement" }},
			{name: "options", mutate: func(got *volume.Volume) { got.Options = map[string]string{"type": "nfs"} }},
		} {
			t.Run(desired.Name+"/"+tc.name, func(t *testing.T) {
				got := testDockerVolume(desired)
				tc.mutate(&got)
				if err := verifyDockerVolume(got, desired); err == nil {
					t.Fatal("drifted volume spec accepted")
				}
			})
		}
	}
}

func TestDockerRuntimeNamesAreStableCollisionResistantAndLengthSafe(t *testing.T) {
	builders := []struct {
		name  string
		build func(string) string
	}{
		{name: "container", build: dockerRuntimeContainerName},
		{name: "project volume", build: dockerProjectMemoryVolumeName},
		{name: "task volume", build: dockerTaskWorkspaceVolumeName},
	}
	dockerName := regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]+$`)
	for _, builder := range builders {
		t.Run(builder.name, func(t *testing.T) {
			idA := "Same/Unsafe Value" + strings.Repeat("-very-long", 100)
			idB := "same-unsafe-value" + strings.Repeat("-very-long", 100)
			got := builder.build(idA)
			if got != builder.build(idA) {
				t.Fatalf("name is not stable: %q", got)
			}
			if got == builder.build(idB) {
				t.Fatalf("distinct identities collided: %q", got)
			}
			if len(got) > dockerRuntimeNameMaxLength || !dockerName.MatchString(got) {
				t.Fatalf("name is not Docker-safe: %q (length %d)", got, len(got))
			}
			if strings.Contains(got, "/workspace/") || strings.Contains(got, string(filepath.Separator)) {
				t.Fatalf("name depends on a workflow directory: %q", got)
			}
		})
	}
	if dockerRuntimeContainerName("execution-a") == dockerRuntimeContainerName("execution-b") ||
		dockerProjectMemoryVolumeName("project-a") == dockerProjectMemoryVolumeName("project-b") ||
		dockerTaskWorkspaceVolumeName("task-a") == dockerTaskWorkspaceVolumeName("task-b") {
		t.Fatal("different runtime identities must produce distinct names")
	}
}

func testDockerRuntimeSpec() dockerRuntimeSpec {
	return buildDockerRuntimeSpec(dockerRuntimeConfig{
		DockerConfig: srvconfig.DockerConfig{Network: "anban", CPUCores: 2, MemoryMB: 4096, PidsLimit: 256, TimeoutSec: 3600},
		ServerURL:    "http://server:8080",
	}, &model.TaskExecution{ID: "execution-1", TaskID: "task-1", RuntimeImage: "registry/agent@sha256:persisted"},
		&model.Task{ID: "task-1", ProjectID: "project-1", UserID: "user-1"})
}

func testDockerInspect(spec dockerRuntimeSpec) containertypes.InspectResponse {
	config := *spec.ContainerConfig
	config.Labels = maps.Clone(config.Labels)
	config.Cmd = slices.Clone(config.Cmd)
	config.Entrypoint = slices.Clone(config.Entrypoint)
	hostConfig := *spec.HostConfig
	hostConfig.Binds = slices.Clone(hostConfig.Binds)
	hostConfig.VolumesFrom = slices.Clone(hostConfig.VolumesFrom)
	hostConfig.Mounts = slices.Clone(hostConfig.Mounts)
	hostConfig.CapAdd = slices.Clone(hostConfig.CapAdd)
	hostConfig.CapDrop = slices.Clone(hostConfig.CapDrop)
	hostConfig.Devices = slices.Clone(hostConfig.Devices)
	if hostConfig.PidsLimit != nil {
		limit := *hostConfig.PidsLimit
		hostConfig.PidsLimit = &limit
	}
	return containertypes.InspectResponse{
		ContainerJSONBase: &containertypes.ContainerJSONBase{Name: "/" + spec.ContainerName, HostConfig: &hostConfig},
		Config:            &config,
	}
}

func testDockerVolume(desired volume.CreateOptions) volume.Volume {
	return volume.Volume{
		Name: desired.Name, Driver: desired.Driver, Labels: maps.Clone(desired.Labels), Options: maps.Clone(desired.DriverOpts),
	}
}
