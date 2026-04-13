package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
)

// Compile-time interface check.
var _ TaskExecutor = (*DockerExecutor)(nil)

// DockerExecutor runs agent tasks inside Docker containers.
// Each task gets an isolated container with resource limits and a bind-mounted workspace.
type DockerExecutor struct {
	logger      *zerolog.Logger
	imageAPICfg *srvconfig.ImageAPIConfig
	claudeEnv   map[string]string
	dockerCfg   srvconfig.DockerConfig
	dockerCLI   *client.Client
}

// NewDockerExecutor creates a new DockerExecutor.
// It initializes a Docker API client, auto-detecting the Docker socket path.
func NewDockerExecutor(
	logger *zerolog.Logger,
	imageAPICfg *srvconfig.ImageAPIConfig,
	claudeEnv map[string]string,
	dockerCfg srvconfig.DockerConfig,
) (*DockerExecutor, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}

	// Auto-detect Docker socket if DOCKER_HOST is not set.
	// Docker Desktop on macOS uses ~/.docker/run/docker.sock instead of
	// /var/run/docker.sock, and client.FromEnv alone won't find it.
	if os.Getenv("DOCKER_HOST") == "" {
		if host := detectDockerHost(); host != "" {
			opts = append(opts, client.WithHost(host))
		}
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client init: %w", err)
	}
	return &DockerExecutor{
		logger:      logger,
		imageAPICfg: imageAPICfg,
		claudeEnv:   claudeEnv,
		dockerCfg:   dockerCfg,
		dockerCLI:   cli,
	}, nil
}

// Close releases the Docker client connection.
func (e *DockerExecutor) Close() error {
	if e.dockerCLI != nil {
		return e.dockerCLI.Close()
	}
	return nil
}

// Execute runs the agent task in a Docker container.
func (e *DockerExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	// 1. Resolve defaults.
	model := opts.Model
	if model == "" {
		model = DefaultModel()
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type)
	}

	// 2. Create workspace directory on host.
	workDir := filepath.Join(os.TempDir(), "anbanwriter", opts.Task.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	e.logger.Info().
		Str("task_id", opts.Task.ID).
		Str("type", opts.Task.Type).
		Str("topic", opts.Task.Topic).
		Str("model", model).
		Int("max_turns", maxTurns).
		Str("work_dir", workDir).
		Str("image", e.dockerCfg.Image).
		Msg("starting docker agent execution")

	// 3. Write channel config to workspace settings.json.
	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}
	}

	// 4. Build container command.
	userPrompt := opts.Task.Topic
	if userPrompt == "" {
		userPrompt = fmt.Sprintf("Generate a %s content.", opts.Task.Type)
	}
	agentName := taskTypeToAgent(opts.Task.Type)
	agentFlag := "anbanwriter:" + agentName

	cmd := []string{
		"claude",
		"--agent", agentFlag,
		"--max-turns", fmt.Sprintf("%d", maxTurns),
		"--model", model,
		"--permission-mode", "bypassPermissions",
		"--setting-sources", "user",
		"--print", userPrompt,
	}

	// 5. Build environment variables.
	env := []string{
		"CLAUDE_PLUGIN_ROOT=/app",
		"PATH=/app:/usr/local/bin:/usr/bin:/bin",
	}
	for k, v := range e.claudeEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// 6. Create container.
	containerConfig := &container.Config{
		Image:      e.dockerCfg.Image,
		Cmd:        cmd,
		Env:        env,
		WorkingDir: "/workspace",
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: workDir,
				Target: "/workspace",
			},
		},
		Resources: container.Resources{
			NanoCPUs: e.dockerCfg.CPUCores * 1e9,
			Memory:   e.dockerCfg.MemoryMB * 1024 * 1024,
		},
	}

	containerName := "anbanwriter-" + opts.Task.ID
	resp, err := e.dockerCLI.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("docker create: %w", err)
	}

	// Ensure container cleanup.
	defer func() {
		removeCtx := context.Background()
		if err := e.dockerCLI.ContainerRemove(removeCtx, resp.ID, container.RemoveOptions{Force: true}); err != nil {
			e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to remove container")
		}
	}()

	// 7. Start container.
	if err := e.dockerCLI.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("docker start: %w", err)
	}

	// 8. Follow logs for progress streaming.
	logsReader, err := e.dockerCLI.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		Follow:     true,
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
	})
	if err != nil {
		e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to follow container logs")
	} else {
		go e.streamLogs(logsReader, opts)
	}

	// 9. Wait for container with timeout.
	timeout := time.Duration(e.dockerCfg.TimeoutSec) * time.Second
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	statusCh, errCh := e.dockerCLI.ContainerWait(waitCtx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if waitCtx.Err() == context.DeadlineExceeded {
			// Timeout — kill the container.
			e.dockerCLI.ContainerStop(context.Background(), resp.ID, container.StopOptions{})
			return &ExecutionResult{
				Success: false,
				Error:   fmt.Sprintf("task timed out after %d seconds", e.dockerCfg.TimeoutSec),
				WorkDir: workDir,
			}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("docker wait: %w", err)
		}
		// errCh sent nil — container exited normally, read status below.
	case result := <-statusCh:
		if result.StatusCode != 0 {
			return &ExecutionResult{
				Success: false,
				Error:   fmt.Sprintf("container exited with code %d", result.StatusCode),
				WorkDir: workDir,
			}, nil
		}
	}

	// 10. Log workspace contents.
	if files, listErr := ListWorkDirFiles(workDir); listErr == nil {
		e.logger.Info().
			Str("task_id", opts.Task.ID).
			Int("file_count", len(files)).
			Interface("files", files).
			Msg("workspace contents after execution")
	}

	return &ExecutionResult{Success: true, WorkDir: workDir}, nil
}

// streamLogs reads multiplexed Docker log frames and forwards to OnProgress.
// Docker uses 8-byte headers: [1 byte stream type][3 bytes padding][4 bytes size].
func (e *DockerExecutor) streamLogs(reader io.ReadCloser, opts *ExecutionOptions) {
	defer reader.Close()
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(reader, hdr); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		if size == 0 {
			continue
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return
		}
		if opts.OnProgress != nil {
			opts.OnProgress(opts.Task.ID, string(buf))
		}
	}
}

// detectDockerHost searches for the Docker daemon socket on the filesystem.
// Returns the host URL (e.g. "unix:///Users/.../.docker/run/docker.sock") or empty string.
func detectDockerHost() string {
	homeDir, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		"/run/docker.sock",
	}
	if homeDir != "" {
		candidates = append(candidates,
			filepath.Join(homeDir, ".docker", "run", "docker.sock"),
		)
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.Mode()&os.ModeSocket != 0 {
			return "unix://" + p
		}
	}
	return ""
}
