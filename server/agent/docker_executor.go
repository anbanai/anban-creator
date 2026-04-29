package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/rs/zerolog"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
)

var _ TaskExecutor = (*DockerExecutor)(nil)

// UserKeyProvider resolves API keys for MCP and agent authentication.
type UserKeyProvider interface {
	EnsureUserKey(ctx context.Context, userID string) (string, error)
	EnsureSystemKey(ctx context.Context) (string, error)
}

// DockerExecutor runs the standalone abwriter-agent inside Docker containers.
type DockerExecutor struct {
	logger            *zerolog.Logger
	imageAPICfg       *srvconfig.ImageAPIConfig
	claudeEnv         map[string]string
	dockerCfg         srvconfig.DockerConfig
	serverURL         string
	dockerCLI         *client.Client
	defaultModel      string
	keyProvider       UserKeyProvider
	maxTurnsOverrides map[string]int
}

// NewDockerExecutor creates a new DockerExecutor.
func NewDockerExecutor(
	logger *zerolog.Logger,
	imageAPICfg *srvconfig.ImageAPIConfig,
	claudeEnv map[string]string,
	dockerCfg srvconfig.DockerConfig,
	serverURL string,
	defaultModel string,
	keyProvider UserKeyProvider,
	maxTurnsOverrides map[string]int,
) (*DockerExecutor, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if os.Getenv("DOCKER_HOST") == "" {
		if host := detectDockerHost(); host != "" {
			opts = append(opts, client.WithHost(host))
		}
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client init: %w", err)
	}
	if _, err := cli.ImageInspect(context.Background(), dockerCfg.Image); err != nil {
		return nil, fmt.Errorf("docker image %q not found locally (run 'make docker-image' to build it): %w", dockerCfg.Image, err)
	}

	return &DockerExecutor{
		logger:            logger,
		imageAPICfg:       imageAPICfg,
		claudeEnv:         claudeEnv,
		dockerCfg:         dockerCfg,
			serverURL:         serverURL,
		dockerCLI:         cli,
		defaultModel:      defaultModel,
		keyProvider:       keyProvider,
		maxTurnsOverrides: maxTurnsOverrides,
	}, nil
}

// Close releases the Docker client connection.
func (e *DockerExecutor) Close() error {
	if e.dockerCLI != nil {
		return e.dockerCLI.Close()
	}
	return nil
}

// CleanupOrphanedContainers removes stopped ephemeral task containers
// (abwriter-task-*) left behind by previous runs. Safe to call at startup.
func (e *DockerExecutor) CleanupOrphanedContainers() {
	containers, err := e.dockerCLI.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.KeyValuePair{Key: "name", Value: "^/abwriter-task-"}),
	})
	if err != nil {
		e.logger.Warn().Err(err).Msg("failed to list containers for orphan cleanup")
		return
	}

	removed := 0
	for _, c := range containers {
		if err := e.dockerCLI.ContainerRemove(context.Background(), c.ID, container.RemoveOptions{Force: true}); err != nil {
			e.logger.Warn().Err(err).Str("container", c.ID).Msg("failed to remove orphan container")
			continue
		}
		removed++
	}
	if removed > 0 {
		e.logger.Info().Int("removed", removed).Msg("cleaned up orphaned abwriter containers")
	}
}

// Execute runs the standalone abwriter-agent in Docker and returns its final JSON result.
func (e *DockerExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	model := opts.Model
	if model == "" {
		model = e.defaultModel
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type, e.maxTurnsOverrides)
	}

	var workDir string
	if e.dockerCfg.ContainerName != "" && e.dockerCfg.WorkspaceDir != "" {
		workDir = filepath.Join(e.dockerCfg.WorkspaceDir, opts.Task.ID)
	} else {
		workDir = filepath.Join(os.TempDir(), "abwriter", opts.Task.ID)
	}
	if err := os.MkdirAll(workDir, 0o777); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	if opts.LogWriter != nil {
		opts.LogWriter.WriteHeader(opts.Task.Type, opts.Task.Prompt, model, maxTurns)
	}

	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg, opts.Task.ImageRatio)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}
		if opts.Channel.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, workDir, opts.Channel.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to download reference image")
			}
		}
	}

	apiKey, err := e.resolveAgentAPIKey(ctx, opts)
	if err != nil {
		return nil, err
	}

	var cmd []string
	var workDirInContainer string
	if e.dockerCfg.ContainerName != "" {
		workDirInContainer = filepath.ToSlash(filepath.Join("/workspace", opts.Task.ID))
	} else {
		workDirInContainer = "/workspace"
	}
	cmd = e.buildAgentCommand(opts, model, maxTurns, workDirInContainer, apiKey)
	env := e.buildAgentEnv(opts)

	var execRes execResult
	if e.dockerCfg.ContainerName != "" {
		execRes = e.executeViaExec(ctx, opts.Task.ID, workDirInContainer, cmd, env, opts.HeartbeatFunc)
	} else {
		execRes = e.executeInNewContainer(ctx, opts.Task.ID, workDir, cmd, env)
	}

	if opts.LogWriter != nil {
		if execRes.stderr.Len() > 0 {
			opts.LogWriter.WriteStderr(strings.TrimSpace(execRes.stderr.String()))
		}
	}

	result := &ExecutionResult{Success: false, WorkDir: workDir}
	if parsed, err := parseAgentResult(execRes.stdout.String()); err == nil {
		result = parsed
	} else if execRes.err == nil {
		execRes.err = fmt.Errorf("parse agent result: %w", err)
	}

	if execRes.err != nil {
		if result.Error == "" {
			result.Error = execRes.err.Error()
		}
		if opts.LogWriter != nil {
			opts.LogWriter.WriteError(result.Error)
			opts.LogWriter.WriteResult(false, result.DurationMs, result.NumTurns, result.TotalCostUSD, result.TokenUsage)
		}
		return result, nil
	}

	// Always override WorkDir with the host-side path. The agent binary
	// runs inside the container and sets WorkDir to the container path
	// (e.g. /workspace/{taskID}), but all host-side consumers
	// (uploadMissingTaskFiles, CountMeaningfulFiles, cleanup) need the
	// host filesystem path.
	result.WorkDir = workDir

	if opts.LogWriter != nil {
		opts.LogWriter.WriteResult(result.Success, result.DurationMs, result.NumTurns, result.TotalCostUSD, result.TokenUsage)
	}

	return result, nil
}

func (e *DockerExecutor) resolveAgentAPIKey(ctx context.Context, opts *ExecutionOptions) (string, error) {
	if e.keyProvider == nil {
		return "", fmt.Errorf("agent API key provider is not configured")
	}
	if opts.Task.UserID != "" {
		if rawKey, err := e.keyProvider.EnsureUserKey(ctx, opts.Task.UserID); err == nil {
			return rawKey, nil
		}
	}
	rawKey, err := e.keyProvider.EnsureSystemKey(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve system API key: %w", err)
	}
	return rawKey, nil
}

func (e *DockerExecutor) buildAgentCommand(opts *ExecutionOptions, model string, maxTurns int, workspace, apiKey string) []string {
	cmd := []string{
		"abwriter-agent",
		"--server-url", strings.TrimRight(e.serverURL, "/"),
		"--api-key", apiKey,
		"--task-id", opts.Task.ID,
		"--task-type", opts.Task.Type,
		"--topic", opts.Task.Prompt,
		"--max-turns", fmt.Sprintf("%d", maxTurns),
		"--workspace", workspace,
		"--agent-flag", "anbanwriter:" + TaskTypeToAgent(opts.Task.Type),
	}
	if model != "" {
		cmd = append(cmd, "--model", model)
	}
	return cmd
}

func (e *DockerExecutor) buildAgentEnv(opts *ExecutionOptions) []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	for k, v := range e.claudeEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	// Additional per-user env var overrides.
	for k, v := range opts.UserEnvOverrides {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

type execResult struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
	err    error
}

func (e *DockerExecutor) executeViaExec(ctx context.Context, taskID, workDirInContainer string, cmd []string, env []string, heartbeatFunc func(string)) execResult {
	containerName := e.dockerCfg.ContainerName
	if containerName == "" {
		return execResult{err: fmt.Errorf("persistent container name is empty")}
	}

	mkdirExec, err := e.dockerCLI.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:  []string{"mkdir", "-p", workDirInContainer},
		User: "root",
	})
	if err == nil {
		_ = e.dockerCLI.ContainerExecStart(ctx, mkdirExec.ID, container.ExecStartOptions{})
	}

	execCreate, err := e.dockerCLI.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		WorkingDir:   workDirInContainer,
		User:         "node",
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return execResult{err: fmt.Errorf("exec create: %w", err)}
	}

	attachResp, err := e.dockerCLI.ContainerExecAttach(ctx, execCreate.ID, container.ExecStartOptions{})
	if err != nil {
		return execResult{err: fmt.Errorf("exec attach: %w", err)}
	}
	defer attachResp.Close()

	res := execResult{}
	done := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(&res.stdout, &res.stderr, attachResp.Reader)
		done <- err
	}()

	timeout := time.Duration(e.dockerCfg.TimeoutSec) * time.Second
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			res.err = fmt.Errorf("task cancelled")
			e.killExecProcess(execCreate.ID, containerName)
			e.waitForDone(done, execCreate.ID, containerName)
			return res
		default:
		}

		// Update heartbeat each poll iteration for stuck-task detection.
		if heartbeatFunc != nil {
			heartbeatFunc(taskID)
		}

		inspect, err := e.dockerCLI.ContainerExecInspect(ctx, execCreate.ID)
		if err != nil {
			res.err = fmt.Errorf("exec inspect: %w", err)
			<-done
			return res
		}
		if !inspect.Running {
			if err := <-done; err != nil && err != io.EOF {
				res.err = fmt.Errorf("read exec output: %w", err)
				return res
			}
			if inspect.ExitCode != 0 {
				res.err = fmt.Errorf("exec exited with code %d: %s", inspect.ExitCode, strings.TrimSpace(res.stderr.String()))
			}
			return res
		}
		if time.Now().After(deadline) {
			res.err = fmt.Errorf("exec timed out after %d seconds", e.dockerCfg.TimeoutSec)
			e.killExecProcess(execCreate.ID, containerName)
			e.waitForDone(done, execCreate.ID, containerName)
			return res
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (e *DockerExecutor) killExecProcess(execID, containerName string) {
	inspect, err := e.dockerCLI.ContainerExecInspect(context.Background(), execID)
	if err != nil || !inspect.Running || inspect.Pid == 0 {
		return
	}

	killExec, err := e.dockerCLI.ContainerExecCreate(context.Background(), containerName, container.ExecOptions{
		Cmd:  []string{"kill", "-TERM", fmt.Sprintf("%d", inspect.Pid)},
		User: "root",
	})
	if err != nil {
		return
	}
	_ = e.dockerCLI.ContainerExecStart(context.Background(), killExec.ID, container.ExecStartOptions{})
}

// forceKillExecProcess sends SIGKILL (-9) to the exec process.
// Used as a fallback when SIGTERM fails to terminate the process.
func (e *DockerExecutor) forceKillExecProcess(execID, containerName string) {
	inspect, err := e.dockerCLI.ContainerExecInspect(context.Background(), execID)
	if err != nil || !inspect.Running || inspect.Pid == 0 {
		return
	}

	e.logger.Warn().Int("pid", inspect.Pid).Msg("exec process did not respond to SIGTERM, sending SIGKILL")

	killExec, err := e.dockerCLI.ContainerExecCreate(context.Background(), containerName, container.ExecOptions{
		Cmd:  []string{"kill", "-9", fmt.Sprintf("%d", inspect.Pid)},
		User: "root",
	})
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to create SIGKILL exec")
		return
	}
	_ = e.dockerCLI.ContainerExecStart(context.Background(), killExec.ID, container.ExecStartOptions{})
}

// waitForDone waits for the stdout copy goroutine with a secondary timeout.
// If the goroutine doesn't finish within killWaitTimeout, it sends SIGKILL
// to force-terminate the exec process.
const killWaitTimeout = 10 * time.Second

func (e *DockerExecutor) waitForDone(done chan error, execID, containerName string) error {
	select {
	case err := <-done:
		return err
	case <-time.After(killWaitTimeout):
		e.forceKillExecProcess(execID, containerName)
		return <-done
	}
}

func (e *DockerExecutor) executeInNewContainer(ctx context.Context, taskID, workDir string, cmd []string, env []string) execResult {
	containerConfig := &container.Config{
		Image:      e.dockerCfg.Image,
		Cmd:        cmd,
		Env:        env,
		WorkingDir: "/workspace",
		User:       "node",
	}
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: workDir,
			Target: "/workspace",
		}},
		Resources: container.Resources{
			NanoCPUs: e.dockerCfg.CPUCores * 1e9,
			Memory:   e.dockerCfg.MemoryMB * 1024 * 1024,
		},
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	}

	containerName := "abwriter-task-" + taskID
	resp, err := e.dockerCLI.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return execResult{err: fmt.Errorf("docker create: %w", err)}
	}
	defer func() {
		timeout := 5
		_ = e.dockerCLI.ContainerStop(context.Background(), resp.ID, container.StopOptions{Timeout: &timeout})
		_ = e.dockerCLI.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	}()

	if err := e.dockerCLI.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return execResult{err: fmt.Errorf("docker start: %w", err)}
	}

	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(e.dockerCfg.TimeoutSec)*time.Second)
	defer cancel()

	// Stop the container if the parent context is cancelled (e.g. user cancels task).
	go func() {
		<-ctx.Done()
		timeout := 5
		_ = e.dockerCLI.ContainerStop(context.Background(), resp.ID, container.StopOptions{Timeout: &timeout})
	}()

	statusCh, errCh := e.dockerCLI.ContainerWait(waitCtx, resp.ID, container.WaitConditionNotRunning)
	res := execResult{}
	select {
	case err := <-errCh:
		if err != nil {
			res.err = fmt.Errorf("container wait: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			res.err = fmt.Errorf("container exited with code %d", status.StatusCode)
		}
	case <-waitCtx.Done():
		if ctx.Err() != nil {
			res.err = fmt.Errorf("task cancelled")
		} else {
			res.err = fmt.Errorf("container timed out after %d seconds", e.dockerCfg.TimeoutSec)
		}
	}

	logsReader, err := e.dockerCLI.ContainerLogs(context.Background(), resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err == nil {
		defer logsReader.Close()
		if _, copyErr := stdcopy.StdCopy(&res.stdout, &res.stderr, logsReader); copyErr != nil && res.err == nil {
			res.err = fmt.Errorf("read container logs: %w", copyErr)
		}
	}
	if res.err != nil && strings.TrimSpace(res.stderr.String()) != "" {
		res.err = fmt.Errorf("%w: %s", res.err, strings.TrimSpace(res.stderr.String()))
	}
	return res
}

func parseAgentResult(stdout string) (*ExecutionResult, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var result ExecutionResult
		if err := json.Unmarshal([]byte(line), &result); err == nil {
			return &result, nil
		}
	}
	return nil, fmt.Errorf("no execution result JSON found in stdout")
}

// detectDockerHost searches for the Docker daemon socket on the filesystem.
func detectDockerHost() string {
	homeDir, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		"/run/docker.sock",
	}
	if homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".docker", "run", "docker.sock"))
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.Mode()&os.ModeSocket != 0 {
			return "unix://" + p
		}
	}
	return ""
}
