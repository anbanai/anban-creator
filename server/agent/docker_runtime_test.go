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
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestBuildDockerRuntimeSpec(t *testing.T) {
	imageConfig := &containertypes.Config{
		User:         "image-user",
		Env:          []string{"PATH=/image/bin:/usr/bin", "HOME=/home/node"},
		Entrypoint:   []string{"image-entrypoint"},
		Cmd:          []string{"image-command"},
		WorkingDir:   "/image-workdir",
		Labels:       map[string]string{"org.opencontainers.image.authors": "trusted author"},
		ExposedPorts: nat.PortSet{"9090/tcp": {}},
		Volumes:      map[string]struct{}{"/image-data": {}},
		Healthcheck:  &containertypes.HealthConfig{Test: []string{"CMD", "image-health"}, Retries: 2},
		StopSignal:   "SIGTERM",
		Shell:        []string{"/bin/sh", "-c"},
	}
	cfg := dockerRuntimeConfig{
		DockerConfig: srvconfig.DockerConfig{
			Network: " anban ", CPUCores: 2, MemoryMB: 4096, PidsLimit: 256, TimeoutSec: 3600,
		},
		ServerURL:   "http://server:8080/",
		ImageID:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ImageConfig: imageConfig,
	}
	execution := &model.TaskExecution{
		ID: "execution-123", TaskID: "task-456", RuntimeImage: "registry.example/creator-agent-article@sha256:persisted",
	}
	task := &model.Task{ID: "task-456", ProjectID: "project-789", UserID: "user-012"}

	spec := buildDockerRuntimeSpec(cfg, execution, task)

	if spec.ContainerName != dockerRuntimeContainerName(execution.ID) {
		t.Fatalf("container name = %q, want deterministic execution name", spec.ContainerName)
	}
	if spec.NetworkName != "anban" {
		t.Fatalf("network name = %q, want trimmed configured network identity", spec.NetworkName)
	}
	if spec.ProjectVolume.Name != dockerProjectMemoryVolumeName(task.ProjectID) {
		t.Fatalf("project volume name = %q, want deterministic project name", spec.ProjectVolume.Name)
	}
	if spec.TaskVolume.Name != dockerTaskWorkspaceVolumeName(task.ID) {
		t.Fatalf("task volume name = %q, want deterministic task name", spec.TaskVolume.Name)
	}
	if spec.ContainerConfig.Image != cfg.ImageID {
		t.Fatalf("create image = %q, want trusted inspected image ID %q", spec.ContainerConfig.Image, cfg.ImageID)
	}
	if spec.ImageID != cfg.ImageID {
		t.Fatalf("image ID = %q, want trusted inspected image ID %q", spec.ImageID, cfg.ImageID)
	}
	if spec.ContainerConfig.User != ContainerRuntimeUser {
		t.Fatalf("user = %q, want %q", spec.ContainerConfig.User, ContainerRuntimeUser)
	}
	wantLabels := map[string]string{
		"org.opencontainers.image.authors": "trusted author",
		dockerExecutionIDLabel:             execution.ID,
		dockerTaskIDLabel:                  task.ID,
		dockerProjectIDLabel:               task.ProjectID,
		dockerUserIDLabel:                  task.UserID,
	}
	if !maps.Equal(spec.ContainerConfig.Labels, wantLabels) {
		t.Fatalf("container labels = %#v, want %#v", spec.ContainerConfig.Labels, wantLabels)
	}
	if !slices.Equal(spec.ContainerConfig.Env, imageConfig.Env) ||
		!maps.Equal(spec.ContainerConfig.ExposedPorts, imageConfig.ExposedPorts) ||
		!maps.Equal(spec.ContainerConfig.Volumes, imageConfig.Volumes) ||
		!reflect.DeepEqual(spec.ContainerConfig.Healthcheck, imageConfig.Healthcheck) ||
		spec.ContainerConfig.StopSignal != imageConfig.StopSignal ||
		!slices.Equal(spec.ContainerConfig.Shell, imageConfig.Shell) {
		t.Fatalf("trusted image config was not preserved: %#v", spec.ContainerConfig)
	}
	if _, mutated := imageConfig.Labels[dockerExecutionIDLabel]; mutated {
		t.Fatal("buildDockerRuntimeSpec mutated the trusted image config")
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
	if spec.HostConfig.IpcMode != containertypes.IPCModePrivate || spec.HostConfig.CgroupnsMode != containertypes.CgroupnsModePrivate || spec.HostConfig.Runtime != "runc" {
		t.Fatalf("safe runtime defaults = ipc %q cgroupns %q runtime %q", spec.HostConfig.IpcMode, spec.HostConfig.CgroupnsMode, spec.HostConfig.Runtime)
	}
	wantLogOptions := map[string]string{
		"max-size": "10m", "max-file": "3", "compress": "true", "labels": "", "labels-regex": "", "env": "", "env-regex": "", "tag": "{{.ID}}",
		"mode": "non-blocking", "max-buffer-size": "4m", "cache-disabled": "true", "cache-max-size": "20m", "cache-max-file": "1", "cache-compress": "false",
	}
	if spec.HostConfig.LogConfig.Type != "json-file" || !maps.Equal(spec.HostConfig.LogConfig.Config, wantLogOptions) {
		t.Fatalf("log policy = %#v, want complete json-file policy %#v", spec.HostConfig.LogConfig, wantLogOptions)
	}
	if spec.HostConfig.NanoCPUs != 2_000_000_000 || spec.HostConfig.Memory != 4096*1024*1024 {
		t.Fatalf("resources = cpu %d memory %d", spec.HostConfig.NanoCPUs, spec.HostConfig.Memory)
	}
	if spec.HostConfig.MemorySwap != spec.HostConfig.Memory {
		t.Fatalf("memory swap = %d, want pinned to memory %d", spec.HostConfig.MemorySwap, spec.HostConfig.Memory)
	}
	if spec.HostConfig.OomKillDisable == nil || *spec.HostConfig.OomKillDisable {
		t.Fatalf("OOM killer setting = %v, want explicit false", spec.HostConfig.OomKillDisable)
	}
	wantUlimits := map[string]struct{}{
		"core": {}, "cpu": {}, "data": {}, "fsize": {}, "locks": {}, "memlock": {}, "msgqueue": {}, "nice": {},
		"nofile": {}, "nproc": {}, "rss": {}, "rtprio": {}, "rttime": {}, "sigpending": {}, "stack": {},
	}
	if len(spec.HostConfig.Ulimits) != len(wantUlimits) {
		t.Fatalf("ulimit policy has %d entries, want complete %d-name policy", len(spec.HostConfig.Ulimits), len(wantUlimits))
	}
	for _, limit := range spec.HostConfig.Ulimits {
		if _, ok := wantUlimits[limit.Name]; !ok {
			t.Fatalf("unexpected ulimit policy entry %q", limit.Name)
		}
		delete(wantUlimits, limit.Name)
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
		"--allow-http-server",
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

func TestDockerRuntimeSpecEmitsExplicitHTTPBootstrapTrustOnlyForHTTP(t *testing.T) {
	for _, test := range []struct {
		name       string
		serverURL  string
		wantPolicy bool
	}{
		{name: "operator HTTP", serverURL: "http://creator-server:8080", wantPolicy: true},
		{name: "HTTPS", serverURL: "https://creator-server:8443"},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := buildDockerRuntimeSpec(dockerRuntimeConfig{
				DockerConfig: dockerDispatcherTestConfig(), ServerURL: test.serverURL,
				ImageID: dockerDispatcherTestImageID, ImageConfig: &containertypes.Config{},
			}, dockerDispatcherTestExecution(), dockerDispatcherTestTask())
			got := slices.Contains(spec.ContainerConfig.Cmd, "--allow-http-server")
			if got != test.wantPolicy {
				t.Fatalf("command = %v, allow HTTP flag=%v want %v", spec.ContainerConfig.Cmd, got, test.wantPolicy)
			}
		})
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
	if spec.ContainerConfig.Labels["org.opencontainers.image.authors"] != "upstream image author" {
		t.Fatal("trusted image metadata missing from canonical container config")
	}
	missingImageID := spec
	missingImageID.ImageID = ""
	if err := verifyDockerContainer(testDockerInspect(missingImageID), missingImageID); err == nil {
		t.Fatal("container spec without a trusted image ID accepted")
	}
	missingImageID.ImageID = "   "
	if err := verifyDockerContainer(testDockerInspect(missingImageID), missingImageID); err == nil {
		t.Fatal("container spec with a blank trusted image ID accepted")
	}
	normalizedEmptyDefaults := testDockerInspect(spec)
	normalizedEmptyDefaults.HostConfig.Binds = []string{}
	normalizedEmptyDefaults.HostConfig.VolumesFrom = []string{}
	normalizedEmptyDefaults.HostConfig.PortBindings = nat.PortMap{}
	normalizedEmptyDefaults.HostConfig.GroupAdd = []string{}
	normalizedEmptyDefaults.HostConfig.Tmpfs = map[string]string{}
	normalizedEmptyDefaults.HostConfig.Sysctls = map[string]string{}
	normalizedEmptyDefaults.HostConfig.ExtraHosts = []string{}
	normalizedEmptyDefaults.HostConfig.Links = []string{}
	normalizedEmptyDefaults.HostConfig.DNS = []string{}
	normalizedEmptyDefaults.HostConfig.DNSOptions = []string{}
	normalizedEmptyDefaults.HostConfig.DNSSearch = []string{}
	normalizedEmptyDefaults.HostConfig.Devices = []containertypes.DeviceMapping{}
	normalizedEmptyDefaults.HostConfig.DeviceRequests = []containertypes.DeviceRequest{}
	normalizedEmptyDefaults.HostConfig.DeviceCgroupRules = []string{}
	if err := verifyDockerContainer(normalizedEmptyDefaults, spec); err != nil {
		t.Fatalf("safe daemon-normalized empty defaults rejected: %v", err)
	}
	daemonOomDefault := testDockerInspect(spec)
	oomKillDisabled := false
	daemonOomDefault.HostConfig.OomKillDisable = &oomKillDisabled
	if err := verifyDockerContainer(daemonOomDefault, spec); err != nil {
		t.Fatalf("daemon-normalized OOM killer default rejected: %v", err)
	}
	daemonLogDefaults := testDockerInspect(spec)
	daemonLogDefaults.HostConfig.LogConfig.Config = maps.Clone(spec.HostConfig.LogConfig.Config)
	for key, value := range map[string]string{"max-size": "100m", "max-file": "5", "compress": "false"} {
		if _, explicitlyPinned := daemonLogDefaults.HostConfig.LogConfig.Config[key]; !explicitlyPinned {
			daemonLogDefaults.HostConfig.LogConfig.Config[key] = value
		}
	}
	if err := verifyDockerContainer(daemonLogDefaults, spec); err != nil {
		t.Fatalf("container changed by daemon log defaults: %v", err)
	}
	legacyNoNewPrivileges := testDockerInspect(spec)
	legacyNoNewPrivileges.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	if err := verifyDockerContainer(legacyNoNewPrivileges, spec); err != nil {
		t.Fatalf("semantically equivalent no-new-privileges form rejected: %v", err)
	}
	legacyNetworkAliases := testDockerInspect(spec)
	legacyNetworkAliases.NetworkSettings.Networks[spec.NetworkName].Aliases = []string{legacyNetworkAliases.ID[:12]}
	if err := verifyDockerContainer(legacyNetworkAliases, spec); err != nil {
		t.Fatalf("Docker API <1.45 generated network aliases rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*containertypes.InspectResponse)
	}{
		{name: "container name", mutate: func(got *containertypes.InspectResponse) { got.Name = "/replacement" }},
		{name: "labels", mutate: func(got *containertypes.InspectResponse) { got.Config.Labels[dockerTaskIDLabel] = "replacement" }},
		{name: "unknown ownership label", mutate: func(got *containertypes.InspectResponse) { got.Config.Labels["anban.ai/replacement"] = "unexpected" }},
		{name: "unknown image metadata label", mutate: func(got *containertypes.InspectResponse) {
			got.Config.Labels["org.opencontainers.image.authors"] = "injected"
		}},
		{name: "image", mutate: func(got *containertypes.InspectResponse) { got.Config.Image = "replacement:latest" }},
		{name: "resolved image ID", mutate: func(got *containertypes.InspectResponse) {
			got.Image = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{name: "user", mutate: func(got *containertypes.InspectResponse) { got.Config.User = "0:0" }},
		{name: "hostname", mutate: func(got *containertypes.InspectResponse) { got.Config.Hostname = "attacker" }},
		{name: "domain name", mutate: func(got *containertypes.InspectResponse) { got.Config.Domainname = "attacker.invalid" }},
		{name: "environment", mutate: func(got *containertypes.InspectResponse) {
			got.Config.Env = []string{"LD_PRELOAD=/workspace/injected.so"}
		}},
		{name: "exposed ports", mutate: func(got *containertypes.InspectResponse) { got.Config.ExposedPorts = nat.PortSet{"8080/tcp": {}} }},
		{name: "declared volumes", mutate: func(got *containertypes.InspectResponse) { got.Config.Volumes = map[string]struct{}{"/host-data": {}} }},
		{name: "healthcheck", mutate: func(got *containertypes.InspectResponse) {
			got.Config.Healthcheck = &containertypes.HealthConfig{Test: []string{"CMD", "injected"}}
		}},
		{name: "network disabled", mutate: func(got *containertypes.InspectResponse) { got.Config.NetworkDisabled = true }},
		{name: "TTY", mutate: func(got *containertypes.InspectResponse) { got.Config.Tty = true }},
		{name: "stdin", mutate: func(got *containertypes.InspectResponse) { got.Config.OpenStdin = true }},
		{name: "stream attachment", mutate: func(got *containertypes.InspectResponse) { got.Config.AttachStdout = true }},
		{name: "escaped arguments", mutate: func(got *containertypes.InspectResponse) { got.Config.ArgsEscaped = !got.Config.ArgsEscaped }},
		{name: "shell", mutate: func(got *containertypes.InspectResponse) { got.Config.Shell = []string{"/bin/bash", "-c"} }},
		{name: "MAC address", mutate: func(got *containertypes.InspectResponse) { got.Config.MacAddress = "02:42:ac:11:00:02" }},
		{name: "on-build command", mutate: func(got *containertypes.InspectResponse) { got.Config.OnBuild = []string{"RUN injected"} }},
		{name: "stop signal", mutate: func(got *containertypes.InspectResponse) { got.Config.StopSignal = "SIGKILL" }},
		{name: "mounts", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Mounts[0].Target = "/replacement" }},
		{name: "command", mutate: func(got *containertypes.InspectResponse) { got.Config.Cmd[0] = "run" }},
		{name: "network", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.NetworkMode = "bridge" }},
		{name: "missing network settings", mutate: func(got *containertypes.InspectResponse) { got.NetworkSettings = nil }},
		{name: "missing network attachment", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks = nil
		}},
		{name: "wrong network attachment", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks = map[string]*networktypes.EndpointSettings{"other": testDockerEndpoint(got.ID, spec.ContainerName)}
		}},
		{name: "extra network attachment", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks["other"] = testDockerEndpoint(got.ID, spec.ContainerName)
		}},
		{name: "missing network endpoint", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)] = nil
		}},
		{name: "static endpoint IPAM", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].IPAMConfig = &networktypes.EndpointIPAMConfig{IPv4Address: "172.20.0.10"}
		}},
		{name: "endpoint alias", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].Aliases = []string{"database"}
		}},
		{name: "endpoint alias mixed with legacy generated alias", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].Aliases = []string{got.ID[:12], "database"}
		}},
		{name: "endpoint link", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].Links = []string{"database:database"}
		}},
		{name: "endpoint driver option", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].DriverOpts = map[string]string{"com.docker.network.endpoint.sysctls": "net.ipv4.conf.IFNAME.forwarding=1"}
		}},
		{name: "endpoint gateway priority", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].GwPriority = 1
		}},
		{name: "endpoint DNS name", mutate: func(got *containertypes.InspectResponse) {
			got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].DNSNames = append(
				got.NetworkSettings.Networks[string(spec.HostConfig.NetworkMode)].DNSNames,
				"database",
			)
		}},
		{name: "log driver", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.LogConfig.Type = "syslog" }},
		{name: "log option", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.LogConfig.Config["max-size"] = "unlimited" }},
		{name: "init process", mutate: func(got *containertypes.InspectResponse) { value := true; got.HostConfig.Init = &value }},
		{name: "isolation", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Isolation = "hyperv" }},
		{name: "annotations", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.Annotations = map[string]string{"run.oci.handler": "other"}
		}},
		{name: "console size", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.ConsoleSize = [2]uint{80, 24} }},
		{name: "masked paths", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.MaskedPaths = nil }},
		{name: "read-only paths", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.ReadonlyPaths = nil }},
		{name: "port bindings", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.PortBindings = nat.PortMap{"8080/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}}}
		}},
		{name: "PID namespace", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.PidMode = "host" }},
		{name: "IPC namespace", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.IpcMode = containertypes.IPCModeHost }},
		{name: "UTS namespace", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.UTSMode = "host" }},
		{name: "user namespace", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.UsernsMode = "host" }},
		{name: "cgroup namespace", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.CgroupnsMode = containertypes.CgroupnsModeHost
		}},
		{name: "additional groups", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.GroupAdd = []string{"0"} }},
		{name: "container cgroup", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Cgroup = "container:other" }},
		{name: "cgroup parent", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CgroupParent = "/host" }},
		{name: "runtime", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Runtime = "kata" }},
		{name: "tmpfs", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Tmpfs = map[string]string{"/host": "rw"} }},
		{name: "sysctls", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.Sysctls = map[string]string{"net.ipv4.ip_forward": "1"}
		}},
		{name: "extra hosts", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.ExtraHosts = []string{"server:127.0.0.1"} }},
		{name: "links", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Links = []string{"other:other"} }},
		{name: "DNS", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.DNS = []string{"8.8.8.8"} }},
		{name: "DNS options", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.DNSOptions = []string{"use-vc"} }},
		{name: "DNS search", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.DNSSearch = []string{"attacker.invalid"} }},
		{name: "volume driver", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.VolumeDriver = "other" }},
		{name: "container ID file", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.ContainerIDFile = "/host/container.id" }},
		{name: "shared memory", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.ShmSize++ }},
		{name: "OOM score", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.OomScoreAdj++ }},
		{name: "OOM killer disabled", mutate: func(got *containertypes.InspectResponse) { value := true; got.HostConfig.OomKillDisable = &value }},
		{name: "OOM killer setting removed", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.OomKillDisable = nil }},
		{name: "read-only root", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.ReadonlyRootfs = !got.HostConfig.ReadonlyRootfs
		}},
		{name: "storage options", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.StorageOpt = map[string]string{"size": "1G"} }},
		{name: "memory", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Memory++ }},
		{name: "memory swap", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.MemorySwap++ }},
		{name: "CPU", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.NanoCPUs++ }},
		{name: "CPU shares", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CPUShares++ }},
		{name: "CPU set", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CpusetCpus = "0" }},
		{name: "block IO weight", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.BlkioWeight++ }},
		{name: "kernel memory", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.KernelMemory++ }},
		{name: "platform CPU count", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CPUCount++ }},
		{name: "platform IO limit", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.IOMaximumIOps++ }},
		{name: "ulimit", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Ulimits[0].Hard++ }},
		{name: "PID limit", mutate: func(got *containertypes.InspectResponse) { *got.HostConfig.PidsLimit++ }},
		{name: "binds", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Binds = []string{"/host:/workspace"} }},
		{name: "VolumesFrom", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.VolumesFrom = []string{"other"} }},
		{name: "privileged", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.Privileged = true }},
		{name: "capabilities added", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CapAdd = []string{"SYS_ADMIN"} }},
		{name: "capability drop removed", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.CapDrop = nil }},
		{name: "security option removed", mutate: func(got *containertypes.InspectResponse) { got.HostConfig.SecurityOpt = nil }},
		{name: "security option added", mutate: func(got *containertypes.InspectResponse) {
			got.HostConfig.SecurityOpt = append(got.HostConfig.SecurityOpt, "seccomp=unconfined")
		}},
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
		ImageID:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ImageConfig: &containertypes.Config{
			Env:          []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/node"},
			Labels:       map[string]string{"org.opencontainers.image.authors": "upstream image author"},
			ExposedPorts: nat.PortSet{"9090/tcp": {}},
			Volumes:      map[string]struct{}{"/image-data": {}},
			Healthcheck:  &containertypes.HealthConfig{Test: []string{"CMD", "image-health"}},
			StopSignal:   "SIGTERM",
			Shell:        []string{"/bin/sh", "-c"},
		},
	}, &model.TaskExecution{ID: "execution-1", TaskID: "task-1", RuntimeImage: "registry/agent@sha256:persisted"},
		&model.Task{ID: "task-1", ProjectID: "project-1", UserID: "user-1"})
}

func testDockerInspect(spec dockerRuntimeSpec) containertypes.InspectResponse {
	const containerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	config := *spec.ContainerConfig
	config.Hostname = containerID[:12]
	config.Labels = maps.Clone(config.Labels)
	config.Cmd = slices.Clone(config.Cmd)
	config.Entrypoint = slices.Clone(config.Entrypoint)
	hostConfig := *spec.HostConfig
	hostConfig.LogConfig.Config = maps.Clone(spec.HostConfig.LogConfig.Config)
	hostConfig.Binds = slices.Clone(hostConfig.Binds)
	hostConfig.VolumesFrom = slices.Clone(hostConfig.VolumesFrom)
	hostConfig.Mounts = slices.Clone(hostConfig.Mounts)
	hostConfig.CapAdd = slices.Clone(hostConfig.CapAdd)
	hostConfig.CapDrop = slices.Clone(hostConfig.CapDrop)
	hostConfig.Devices = slices.Clone(hostConfig.Devices)
	hostConfig.Ulimits = make([]*containertypes.Ulimit, len(spec.HostConfig.Ulimits))
	for i, limit := range spec.HostConfig.Ulimits {
		copy := *limit
		hostConfig.Ulimits[i] = &copy
	}
	hostConfig.MaskedPaths = []string{"/proc/interrupts", "/proc/kcore", "/proc/keys", "/proc/timer_list", "/proc/scsi", "/sys/firmware"}
	hostConfig.ReadonlyPaths = []string{"/proc/bus", "/proc/fs", "/proc/irq", "/proc/sys", "/proc/sysrq-trigger"}
	if hostConfig.PidsLimit != nil {
		limit := *hostConfig.PidsLimit
		hostConfig.PidsLimit = &limit
	}
	return containertypes.InspectResponse{
		ContainerJSONBase: &containertypes.ContainerJSONBase{ID: containerID, Image: spec.ImageID, Name: "/" + spec.ContainerName, HostConfig: &hostConfig},
		Config:            &config,
		NetworkSettings: &containertypes.NetworkSettings{Networks: map[string]*networktypes.EndpointSettings{
			string(spec.HostConfig.NetworkMode): testDockerEndpoint(containerID, spec.ContainerName),
		}},
	}
}

func testDockerEndpoint(containerID, containerName string) *networktypes.EndpointSettings {
	return &networktypes.EndpointSettings{
		NetworkID:           "network-id",
		EndpointID:          "endpoint-id",
		Gateway:             "172.20.0.1",
		IPAddress:           "172.20.0.2",
		IPPrefixLen:         16,
		IPv6Gateway:         "fd00::1",
		GlobalIPv6Address:   "fd00::2",
		GlobalIPv6PrefixLen: 64,
		MacAddress:          "02:42:ac:14:00:02",
		DNSNames:            []string{containerName, containerID[:12]},
	}
}

func testDockerVolume(desired volume.CreateOptions) volume.Volume {
	return volume.Volume{
		Name: desired.Name, Driver: desired.Driver, Labels: maps.Clone(desired.Labels), Options: maps.Clone(desired.DriverOpts),
	}
}
