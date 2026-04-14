package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
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
	logger       *zerolog.Logger
	imageAPICfg  *srvconfig.ImageAPIConfig
	claudeEnv    map[string]string
	dockerCfg    srvconfig.DockerConfig
	dockerCLI    *client.Client
	defaultModel string // configured model; empty means use env vars
}

// NewDockerExecutor creates a new DockerExecutor.
// It initializes a Docker API client, auto-detecting the Docker socket path.
func NewDockerExecutor(
	logger *zerolog.Logger,
	imageAPICfg *srvconfig.ImageAPIConfig,
	claudeEnv map[string]string,
	dockerCfg srvconfig.DockerConfig,
	defaultModel string,
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
		dockerCLI:    cli,
		defaultModel: defaultModel,
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
		model = e.defaultModel
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type)
	}

	// 2. Create workspace directory on host.
	// Use 0777 so the container's non-root user (node, uid 1000) can write to it.
	workDir := filepath.Join(os.TempDir(), "anbanwriter", opts.Task.ID)
	if err := os.MkdirAll(workDir, 0777); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	logEvt := e.logger.Info().
		Str("task_id", opts.Task.ID).
		Str("type", opts.Task.Type).
		Str("topic", opts.Task.Topic).
		Int("max_turns", maxTurns).
		Str("work_dir", workDir).
		Str("image", e.dockerCfg.Image)
	if model != "" {
		logEvt = logEvt.Str("model", model)
	}
	logEvt.Msg("starting docker agent execution")

	// 3. Write channel config to workspace settings.json.
	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}

		// Validate image API config — warn if keys are missing.
		if e.imageAPICfg != nil {
			hasCover := e.imageAPICfg.Cover != nil && e.imageAPICfg.Cover.Key != ""
			hasContent := e.imageAPICfg.Content != nil && e.imageAPICfg.Content.Key != ""
			if !hasCover || !hasContent {
				e.logger.Warn().
					Bool("cover_key_set", hasCover).
					Bool("content_key_set", hasContent).
					Msg("image API key not configured; image generation may fail")
			} else {
				e.logger.Info().
					Bool("cover_key_set", hasCover).
					Bool("content_key_set", hasContent).
					Msg("image API config validated")
			}
		}
	}

	// 3.5. Write MCP config so the agent can connect to the server's MCP endpoint.
	if e.dockerCfg.MCPBaseURL != "" && e.dockerCfg.MCPAPIKey != "" {
		if err := writeMCPConfig(workDir, e.dockerCfg.MCPBaseURL, e.dockerCfg.MCPAPIKey); err != nil {
			return nil, fmt.Errorf("write mcp config: %w", err)
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
		"--plugin-dir", "/app",
		"--agent", agentFlag,
		"--max-turns", fmt.Sprintf("%d", maxTurns),
		"--permission-mode", "bypassPermissions",
		"--print", userPrompt,
	}
	// Only set model if explicitly configured; otherwise let Claude CLI use env vars.
	if model != "" {
		cmd = append(cmd, "--model", model)
	}

	// 5. Build environment variables.
	env := []string{
		"PATH=/app:/usr/local/bin:/usr/bin:/bin",
	}
	for k, v := range e.claudeEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// 6. Create container.
	// Run as "node" user (uid 1000) — Claude CLI refuses --permission-mode
	// bypassPermissions when running as root. The node:22-slim image includes this user.
	containerConfig := &container.Config{
		Image:      e.dockerCfg.Image,
		Cmd:        cmd,
		Env:        env,
		WorkingDir: "/workspace",
		User:       "node",
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
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
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
			// Capture container logs for diagnostics.
			containerLogs := e.readContainerLogs(resp.ID)
			errMsg := fmt.Sprintf("container exited with code %d", result.StatusCode)
			if containerLogs != "" {
				truncated := containerLogs
				if len(truncated) > 2000 {
					truncated = truncated[len(truncated)-2000:]
				}
				errMsg += "\n" + truncated
			}
			return &ExecutionResult{
				Success: false,
				Error:   errMsg,
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

// readContainerLogs reads the full log output of a container (stdout + stderr)
// and returns it as a plain string. Used for diagnostics when the container
// exits with a non-zero status code.
func (e *DockerExecutor) readContainerLogs(containerID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reader, err := e.dockerCLI.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
	})
	if err != nil {
		return ""
	}
	defer reader.Close()

	// Docker multiplexed stream: 8-byte header + payload per frame.
	var buf []byte
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(reader, hdr); err != nil {
			break
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		if size == 0 {
			continue
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(reader, frame); err != nil {
			break
		}
		buf = append(buf, frame...)
	}
	return string(buf)
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

// writeMCPConfig writes a .mcp.json file inside the .claude/ subdirectory so the
// Claude CLI running inside the container can connect to the server's MCP endpoint.
// Placing it inside .claude/ prevents it from being uploaded as a task file (the
// .claude directory is already skipped during file upload).
func writeMCPConfig(workDir, baseURL, apiKey string) error {
	claudeDir := filepath.Join(workDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	mcpConfig := map[string]any{
		"mcpServers": map[string]any{
			"rednote": map[string]any{
				"type": "http",
				"url":  baseURL + "/mcp",
				"headers": map[string]string{
					"X-API-Key": apiKey,
				},
			},
		},
	}
	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(claudeDir, ".mcp.json"), data, 0644)
}
