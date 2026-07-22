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
	dockerRuntimeName            = "runc"
	dockerDefaultShmSize         = 64 * 1024 * 1024
)

var (
	dockerRequiredMaskedPaths = []string{
		"/proc/interrupts", "/proc/kcore", "/proc/keys", "/proc/timer_list", "/proc/scsi", "/sys/firmware",
	}
	dockerRequiredReadonlyPaths = []string{
		"/proc/bus", "/proc/fs", "/proc/irq", "/proc/sys", "/proc/sysrq-trigger",
	}
)

type dockerRuntimeConfig struct {
	srvconfig.DockerConfig
	ServerURL   string
	ImageConfig *containertypes.Config
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
	memoryBytes := cfg.MemoryMB * 1024 * 1024
	useDockerInit := false
	oomKillDisable := false
	containerConfig := dockerContainerConfigFromImage(cfg.ImageConfig)
	containerConfig.Image = dockerExecutionRuntimeImage(execution)
	containerConfig.User = ContainerRuntimeUser
	containerConfig.Entrypoint = []string{"tini", "--", AgentBinaryName}
	containerConfig.Cmd = []string{
		"job",
		"--server-url", strings.TrimRight(cfg.ServerURL, "/"),
		"--execution-id", executionID(execution),
		"--workspace", dockerTaskWorkspaceMountPath,
		"--workload-token-file", dockerWorkloadTokenFile,
	}
	containerConfig.WorkingDir = dockerTaskWorkspaceMountPath
	containerConfig.Labels[dockerExecutionIDLabel] = executionID(execution)
	containerConfig.Labels[dockerTaskIDLabel] = taskID(task)
	containerConfig.Labels[dockerProjectIDLabel] = projectID(task)
	containerConfig.Labels[dockerUserIDLabel] = dockerTaskUserID(task)
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
		ContainerName:   dockerRuntimeContainerName(executionID(execution)),
		ProjectVolume:   projectVolume,
		TaskVolume:      taskVolume,
		ContainerConfig: containerConfig,
		HostConfig: &containertypes.HostConfig{
			NetworkMode:  containertypes.NetworkMode(cfg.Network),
			LogConfig:    dockerRuntimeLogConfig(),
			IpcMode:      containertypes.IPCModePrivate,
			CgroupnsMode: containertypes.CgroupnsModePrivate,
			Runtime:      dockerRuntimeName,
			ShmSize:      dockerDefaultShmSize,
			Init:         &useDockerInit,
			RestartPolicy: containertypes.RestartPolicy{
				Name: containertypes.RestartPolicyDisabled,
			},
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges=true"},
			Resources: containertypes.Resources{
				NanoCPUs:       cfg.CPUCores * 1_000_000_000,
				Memory:         memoryBytes,
				MemorySwap:     memoryBytes,
				OomKillDisable: &oomKillDisable,
				PidsLimit:      &pidsLimit,
				Ulimits:        dockerRuntimeUlimits(),
			},
			Mounts: []mount.Mount{
				{Type: mount.TypeVolume, Source: taskVolume.Name, Target: dockerTaskWorkspaceMountPath},
				{Type: mount.TypeVolume, Source: projectVolume.Name, Target: dockerProjectMemoryMountPath},
			},
		},
		Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
	}
}

func dockerRuntimeUlimits() []*containertypes.Ulimit {
	limits := []containertypes.Ulimit{
		{Name: "core", Soft: 0, Hard: 0},
		{Name: "cpu", Soft: -1, Hard: -1},
		{Name: "data", Soft: -1, Hard: -1},
		{Name: "fsize", Soft: -1, Hard: -1},
		{Name: "locks", Soft: -1, Hard: -1},
		{Name: "memlock", Soft: 64 * 1024, Hard: 64 * 1024},
		{Name: "msgqueue", Soft: 819200, Hard: 819200},
		{Name: "nice", Soft: 0, Hard: 0},
		{Name: "nofile", Soft: 65536, Hard: 65536},
		{Name: "nproc", Soft: 4096, Hard: 4096},
		{Name: "rss", Soft: -1, Hard: -1},
		{Name: "rtprio", Soft: 0, Hard: 0},
		{Name: "rttime", Soft: 200000, Hard: 200000},
		{Name: "sigpending", Soft: 4096, Hard: 4096},
		{Name: "stack", Soft: 8 * 1024 * 1024, Hard: 8 * 1024 * 1024},
	}
	result := make([]*containertypes.Ulimit, len(limits))
	for i := range limits {
		result[i] = &limits[i]
	}
	return result
}

func dockerRuntimeLogConfig() containertypes.LogConfig {
	return containertypes.LogConfig{Type: "json-file", Config: map[string]string{
		"max-size": "10m", "max-file": "3", "compress": "true", "labels": "", "labels-regex": "", "env": "", "env-regex": "", "tag": "{{.ID}}",
		"mode": "non-blocking", "max-buffer-size": "4m", "cache-disabled": "true", "cache-max-size": "20m", "cache-max-file": "1", "cache-compress": "false",
	}}
}

func dockerContainerConfigFromImage(imageConfig *containertypes.Config) *containertypes.Config {
	config := &containertypes.Config{Labels: map[string]string{}}
	if imageConfig == nil {
		return config
	}
	config.Env = slices.Clone(imageConfig.Env)
	config.ExposedPorts = maps.Clone(imageConfig.ExposedPorts)
	config.Volumes = maps.Clone(imageConfig.Volumes)
	config.Labels = maps.Clone(imageConfig.Labels)
	if config.Labels == nil {
		config.Labels = map[string]string{}
	}
	if imageConfig.Healthcheck != nil {
		healthcheck := *imageConfig.Healthcheck
		healthcheck.Test = slices.Clone(imageConfig.Healthcheck.Test)
		config.Healthcheck = &healthcheck
	}
	config.StopSignal = imageConfig.StopSignal
	config.Shell = slices.Clone(imageConfig.Shell)
	config.ArgsEscaped = imageConfig.ArgsEscaped
	return config
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
		{name: "hostname", equal: dockerHostnameEqual(existing.Config.Hostname, existing.ID, desired.ContainerConfig.Hostname)},
		{name: "domain name", equal: existing.Config.Domainname == desired.ContainerConfig.Domainname},
		{name: "environment", equal: slices.Equal(existing.Config.Env, desired.ContainerConfig.Env)},
		{name: "exposed ports", equal: maps.Equal(existing.Config.ExposedPorts, desired.ContainerConfig.ExposedPorts)},
		{name: "declared volumes", equal: maps.Equal(existing.Config.Volumes, desired.ContainerConfig.Volumes)},
		{name: "healthcheck", equal: reflect.DeepEqual(existing.Config.Healthcheck, desired.ContainerConfig.Healthcheck)},
		{name: "escaped arguments", equal: existing.Config.ArgsEscaped == desired.ContainerConfig.ArgsEscaped},
		{name: "shell", equal: slices.Equal(existing.Config.Shell, desired.ContainerConfig.Shell)},
		{name: "MAC address", equal: existing.Config.MacAddress == desired.ContainerConfig.MacAddress},
		{name: "on-build command", equal: slices.Equal(existing.Config.OnBuild, desired.ContainerConfig.OnBuild)},
		{name: "entrypoint", equal: slices.Equal(existing.Config.Entrypoint, desired.ContainerConfig.Entrypoint)},
		{name: "command", equal: slices.Equal(existing.Config.Cmd, desired.ContainerConfig.Cmd)},
		{name: "working directory", equal: existing.Config.WorkingDir == desired.ContainerConfig.WorkingDir},
		{name: "network disabled", equal: existing.Config.NetworkDisabled == desired.ContainerConfig.NetworkDisabled},
		{name: "terminal", equal: existing.Config.Tty == desired.ContainerConfig.Tty},
		{name: "stdin", equal: existing.Config.OpenStdin == desired.ContainerConfig.OpenStdin && existing.Config.StdinOnce == desired.ContainerConfig.StdinOnce && existing.Config.AttachStdin == desired.ContainerConfig.AttachStdin},
		{name: "stream attachment", equal: existing.Config.AttachStdout == desired.ContainerConfig.AttachStdout && existing.Config.AttachStderr == desired.ContainerConfig.AttachStderr},
		{name: "stop signal", equal: existing.Config.StopSignal == desired.ContainerConfig.StopSignal},
		{name: "stop timeout", equal: equalIntPointer(existing.Config.StopTimeout, desired.ContainerConfig.StopTimeout)},
		{name: "mounts", equal: dockerSliceDeepEqual(existing.HostConfig.Mounts, desired.HostConfig.Mounts)},
		{name: "binds", equal: slices.Equal(existing.HostConfig.Binds, desired.HostConfig.Binds)},
		{name: "volumes-from", equal: slices.Equal(existing.HostConfig.VolumesFrom, desired.HostConfig.VolumesFrom)},
		{name: "network", equal: existing.HostConfig.NetworkMode == desired.HostConfig.NetworkMode},
		{name: "log configuration", equal: dockerLogConfigEqual(existing.HostConfig.LogConfig, desired.HostConfig.LogConfig)},
		{name: "init process", equal: equalBoolPointer(existing.HostConfig.Init, desired.HostConfig.Init)},
		{name: "isolation", equal: existing.HostConfig.Isolation == desired.HostConfig.Isolation},
		{name: "annotations", equal: maps.Equal(existing.HostConfig.Annotations, desired.HostConfig.Annotations)},
		{name: "console size", equal: existing.HostConfig.ConsoleSize == desired.HostConfig.ConsoleSize},
		{name: "container ID file", equal: existing.HostConfig.ContainerIDFile == desired.HostConfig.ContainerIDFile},
		{name: "volume driver", equal: existing.HostConfig.VolumeDriver == desired.HostConfig.VolumeDriver},
		{name: "port bindings", equal: dockerMapDeepEqual(existing.HostConfig.PortBindings, desired.HostConfig.PortBindings)},
		{name: "PID namespace", equal: existing.HostConfig.PidMode == desired.HostConfig.PidMode},
		{name: "IPC namespace", equal: existing.HostConfig.IpcMode == desired.HostConfig.IpcMode},
		{name: "UTS namespace", equal: existing.HostConfig.UTSMode == desired.HostConfig.UTSMode},
		{name: "user namespace", equal: existing.HostConfig.UsernsMode == desired.HostConfig.UsernsMode},
		{name: "cgroup namespace", equal: existing.HostConfig.CgroupnsMode == desired.HostConfig.CgroupnsMode},
		{name: "container cgroup", equal: existing.HostConfig.Cgroup == desired.HostConfig.Cgroup},
		{name: "cgroup parent", equal: existing.HostConfig.CgroupParent == desired.HostConfig.CgroupParent},
		{name: "additional groups", equal: slices.Equal(existing.HostConfig.GroupAdd, desired.HostConfig.GroupAdd)},
		{name: "runtime", equal: existing.HostConfig.Runtime == desired.HostConfig.Runtime},
		{name: "tmpfs", equal: maps.Equal(existing.HostConfig.Tmpfs, desired.HostConfig.Tmpfs)},
		{name: "sysctls", equal: maps.Equal(existing.HostConfig.Sysctls, desired.HostConfig.Sysctls)},
		{name: "extra hosts", equal: slices.Equal(existing.HostConfig.ExtraHosts, desired.HostConfig.ExtraHosts)},
		{name: "links", equal: slices.Equal(existing.HostConfig.Links, desired.HostConfig.Links)},
		{name: "DNS", equal: slices.Equal(existing.HostConfig.DNS, desired.HostConfig.DNS)},
		{name: "DNS options", equal: slices.Equal(existing.HostConfig.DNSOptions, desired.HostConfig.DNSOptions)},
		{name: "DNS search", equal: slices.Equal(existing.HostConfig.DNSSearch, desired.HostConfig.DNSSearch)},
		{name: "memory", equal: existing.HostConfig.Memory == desired.HostConfig.Memory},
		{name: "memory reservation", equal: existing.HostConfig.MemoryReservation == desired.HostConfig.MemoryReservation},
		{name: "memory swap", equal: existing.HostConfig.MemorySwap == desired.HostConfig.MemorySwap},
		{name: "memory swappiness", equal: equalInt64Pointer(existing.HostConfig.MemorySwappiness, desired.HostConfig.MemorySwappiness)},
		{name: "OOM killer", equal: equalBoolPointer(existing.HostConfig.OomKillDisable, desired.HostConfig.OomKillDisable)},
		{name: "OOM score", equal: existing.HostConfig.OomScoreAdj == desired.HostConfig.OomScoreAdj},
		{name: "shared memory", equal: existing.HostConfig.ShmSize == desired.HostConfig.ShmSize},
		{name: "CPU", equal: existing.HostConfig.NanoCPUs == desired.HostConfig.NanoCPUs},
		{name: "CPU shares", equal: existing.HostConfig.CPUShares == desired.HostConfig.CPUShares},
		{name: "CPU period", equal: existing.HostConfig.CPUPeriod == desired.HostConfig.CPUPeriod},
		{name: "CPU quota", equal: existing.HostConfig.CPUQuota == desired.HostConfig.CPUQuota},
		{name: "CPU realtime period", equal: existing.HostConfig.CPURealtimePeriod == desired.HostConfig.CPURealtimePeriod},
		{name: "CPU realtime runtime", equal: existing.HostConfig.CPURealtimeRuntime == desired.HostConfig.CPURealtimeRuntime},
		{name: "CPU set", equal: existing.HostConfig.CpusetCpus == desired.HostConfig.CpusetCpus && existing.HostConfig.CpusetMems == desired.HostConfig.CpusetMems},
		{name: "block IO weight", equal: existing.HostConfig.BlkioWeight == desired.HostConfig.BlkioWeight},
		{name: "block IO weight devices", equal: dockerSliceDeepEqual(existing.HostConfig.BlkioWeightDevice, desired.HostConfig.BlkioWeightDevice)},
		{name: "block IO read BPS", equal: dockerSliceDeepEqual(existing.HostConfig.BlkioDeviceReadBps, desired.HostConfig.BlkioDeviceReadBps)},
		{name: "block IO write BPS", equal: dockerSliceDeepEqual(existing.HostConfig.BlkioDeviceWriteBps, desired.HostConfig.BlkioDeviceWriteBps)},
		{name: "block IO read IOPS", equal: dockerSliceDeepEqual(existing.HostConfig.BlkioDeviceReadIOps, desired.HostConfig.BlkioDeviceReadIOps)},
		{name: "block IO write IOPS", equal: dockerSliceDeepEqual(existing.HostConfig.BlkioDeviceWriteIOps, desired.HostConfig.BlkioDeviceWriteIOps)},
		{name: "kernel memory", equal: existing.HostConfig.KernelMemory == desired.HostConfig.KernelMemory && existing.HostConfig.KernelMemoryTCP == desired.HostConfig.KernelMemoryTCP},
		{name: "PID limit", equal: equalInt64Pointer(existing.HostConfig.PidsLimit, desired.HostConfig.PidsLimit)},
		{name: "ulimits", equal: dockerSliceDeepEqual(existing.HostConfig.Ulimits, desired.HostConfig.Ulimits)},
		{name: "platform CPU", equal: existing.HostConfig.CPUCount == desired.HostConfig.CPUCount && existing.HostConfig.CPUPercent == desired.HostConfig.CPUPercent},
		{name: "platform IO", equal: existing.HostConfig.IOMaximumIOps == desired.HostConfig.IOMaximumIOps && existing.HostConfig.IOMaximumBandwidth == desired.HostConfig.IOMaximumBandwidth},
		{name: "privileged mode", equal: existing.HostConfig.Privileged == desired.HostConfig.Privileged},
		{name: "added capabilities", equal: slices.Equal(existing.HostConfig.CapAdd, desired.HostConfig.CapAdd)},
		{name: "dropped capabilities", equal: slices.Equal(existing.HostConfig.CapDrop, desired.HostConfig.CapDrop)},
		{name: "security options", equal: dockerSecurityOptionsEqual(existing.HostConfig.SecurityOpt, desired.HostConfig.SecurityOpt)},
		{name: "devices", equal: dockerSliceDeepEqual(existing.HostConfig.Devices, desired.HostConfig.Devices)},
		{name: "device requests", equal: dockerSliceDeepEqual(existing.HostConfig.DeviceRequests, desired.HostConfig.DeviceRequests)},
		{name: "device cgroup rules", equal: slices.Equal(existing.HostConfig.DeviceCgroupRules, desired.HostConfig.DeviceCgroupRules)},
		{name: "restart policy", equal: existing.HostConfig.RestartPolicy == desired.HostConfig.RestartPolicy},
		{name: "auto-remove", equal: existing.HostConfig.AutoRemove == desired.HostConfig.AutoRemove},
		{name: "published ports", equal: existing.HostConfig.PublishAllPorts == desired.HostConfig.PublishAllPorts},
		{name: "read-only root", equal: existing.HostConfig.ReadonlyRootfs == desired.HostConfig.ReadonlyRootfs},
		{name: "storage options", equal: maps.Equal(existing.HostConfig.StorageOpt, desired.HostConfig.StorageOpt)},
		{name: "masked paths", equal: dockerContainsAll(existing.HostConfig.MaskedPaths, dockerRequiredMaskedPaths)},
		{name: "read-only paths", equal: dockerContainsAll(existing.HostConfig.ReadonlyPaths, dockerRequiredReadonlyPaths)},
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
	return maps.Equal(existing, desired)
}

func dockerHostnameEqual(existingHostname, existingID, desiredHostname string) bool {
	if desiredHostname != "" {
		return existingHostname == desiredHostname
	}
	if len(existingID) < 12 {
		return false
	}
	return existingHostname == existingID[:12]
}

func dockerLogConfigEqual(existing, desired containertypes.LogConfig) bool {
	return existing.Type == desired.Type && maps.Equal(existing.Config, desired.Config)
}

func dockerSecurityOptionsEqual(existing, desired []string) bool {
	normalize := func(values []string) []string {
		result := slices.Clone(values)
		for i, value := range result {
			if value == "no-new-privileges" || value == "no-new-privileges:true" {
				result[i] = "no-new-privileges=true"
			}
		}
		slices.Sort(result)
		return result
	}
	return slices.Equal(normalize(existing), normalize(desired))
}

func dockerContainsAll(existing, required []string) bool {
	available := make(map[string]struct{}, len(existing))
	for _, value := range existing {
		available[value] = struct{}{}
	}
	for _, value := range required {
		if _, ok := available[value]; !ok {
			return false
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

func equalIntPointer(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func equalBoolPointer(left, right *bool) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func dockerSliceDeepEqual[T any](left, right []T) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	return reflect.DeepEqual(left, right)
}

func dockerMapDeepEqual[K comparable, V any](left, right map[K]V) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	return reflect.DeepEqual(left, right)
}
