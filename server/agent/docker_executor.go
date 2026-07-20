package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resolver"
	"github.com/anbanai/anban-creator/server/storage"
)

var _ TaskExecutor = (*DockerExecutor)(nil)

// UserKeyProvider resolves API keys for MCP and agent authentication.
type UserKeyProvider interface {
	EnsureUserKey(ctx context.Context, userID string) (string, error)
	EnsureSystemKey(ctx context.Context) (string, error)
}

// DockerExecutor runs the standalone Anban Creator agent inside Docker containers.
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
	store             storage.Provider
	memoryMgr         *projectmemory.ProjectMemoryManager
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
	store storage.Provider,
	memoryMgr *projectmemory.ProjectMemoryManager,
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
		claudeEnv:         filterAgentEnv(claudeEnv),
		dockerCfg:         dockerCfg,
		serverURL:         serverURL,
		dockerCLI:         cli,
		defaultModel:      defaultModel,
		keyProvider:       keyProvider,
		maxTurnsOverrides: maxTurnsOverrides,
		store:             store,
		memoryMgr:         memoryMgr,
	}, nil
}

// Close releases the Docker client connection.
func (e *DockerExecutor) Close() error {
	if e.dockerCLI != nil {
		return e.dockerCLI.Close()
	}
	return nil
}

// CleanupOrphanedContainers removes stopped or unused Agent task containers
// left behind by previous runs. Safe to call at startup.
func (e *DockerExecutor) CleanupOrphanedContainers() {
	nameFilters := filters.NewArgs()
	for _, name := range OrphanedContainerNameFilters() {
		nameFilters.Add("name", name)
	}
	containers, err := e.dockerCLI.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: nameFilters,
	})
	if err != nil {
		e.logger.Warn().Err(err).Msg("failed to list Agent task containers for orphan cleanup")
		return
	}

	removed := 0
	for _, c := range containers {
		if !isRemovableOrphanContainerState(c.State) {
			continue
		}
		if err := e.dockerCLI.ContainerRemove(context.Background(), c.ID, container.RemoveOptions{Force: true}); err != nil {
			e.logger.Warn().Err(err).Str("container", c.ID).Msg("failed to remove orphaned Agent task container")
			continue
		}
		removed++
	}
	if removed > 0 {
		e.logger.Info().Int("removed", removed).Msg("cleaned up stopped or unused Agent task containers")
	}
}

func isRemovableOrphanContainerState(state container.ContainerState) bool {
	switch state {
	case container.StateCreated, container.StateExited, container.StateDead:
		return true
	default:
		return false
	}
}

// Execute runs the standalone Anban Creator agent in Docker and returns its final JSON result.
func (e *DockerExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	// Claude Code agent model comes only from config.yaml.
	agentModel := e.defaultModel
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type, e.maxTurnsOverrides)
	}
	runtimeUser, err := currentDockerRuntimeUser()
	if err != nil {
		return nil, err
	}

	var workDir string
	if e.dockerCfg.ContainerName != "" && e.dockerCfg.WorkspaceDir != "" {
		workDir = filepath.Join(e.dockerCfg.WorkspaceDir, opts.Task.ID)
	} else {
		workDir = DefaultWorkspaceDir(opts.Task.ID)
	}
	if err := os.MkdirAll(workDir, 0o777); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	if opts.LogWriter != nil {
		opts.LogWriter.WriteHeader(opts.Task.Type, opts.Task.Prompt, agentModel, maxTurns)
	}

	if opts.Project != nil {
		// Snapshot resolution feeds settings.json so retries and historical tasks
		// keep the original project configuration. Old rows without a snapshot may
		// still resolve through legacy task overrides.
		effectiveProject := EffectiveProject(opts.Project, opts.Task)
		resolved := resolver.ResolveStyle(effectiveProject, opts.Task)
		cfg, err := BuildAppConfig(effectiveProject, resolved, e.imageAPICfg, opts.Task.ImageRatio, opts.Task.SkipReferenceImage, opts.Task.ReferenceImageURL)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}
		// Write project positioning as CLAUDE.md so Claude Code loads it as
		// persistent project memory. Non-fatal: missing the file should not abort
		// a task; the agent can still rely on its default agent definition.
		if err := writeProjectCLAUDEMD(workDir, effectiveProject); err != nil {
			e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to write project CLAUDE.md, continuing")
		}
		if e.memoryMgr != nil && e.memoryMgr.Enabled() {
			runtimeDir, err := e.memoryMgr.Stage(ctx, effectiveProject.ID, opts.Task.ID, workDir)
			if err != nil {
				return nil, fmt.Errorf("stage project memory: %w", err)
			}
			opts.AutoMemoryDirectory = runtimeDir
		}
		// Download effective reference image.
		// Task-level image takes priority over project brand image.
		if opts.Task.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, workDir, opts.Task.UserID, opts.Task.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to download task reference image")
			}
		} else if opts.Project.ReferenceImageURL != "" && !opts.Task.SkipReferenceImage {
			if err := DownloadReferenceImage(ctx, e.store, e.logger, workDir, opts.Task.UserID, opts.Project.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).Str("task_id", opts.Task.ID).Msg("failed to download reference image")
			}
		}
	}

	// E-commerce: materialize the task's product photos into the workspace so the
	// agent can reference local paths (analyze_image / generate_image ref).
	if opts.Task.Type == model.PlatformEcommerce {
		photos := opts.Task.Ecommerce.Data().ProductPhotos
		if n := DownloadProductImages(ctx, e.store, e.logger, workDir, opts.Task.UserID, photos); n == 0 && len(photos) > 0 {
			// E-commerce output is a consistency contract on the uploaded product
			// photos. Fail fast (task error → refund) when none materialized rather
			// than letting the agent hallucinate inconsistent assets. See executor.go
			// for the matching guard in the non-docker path.
			e.logger.Error().Str("task_id", opts.Task.ID).Int("provided", len(photos)).Msg("failed to download any product photos, aborting")
			return nil, fmt.Errorf("ecommerce task: %d product photo(s) provided but none could be downloaded to the workspace; aborting to avoid inconsistent output", len(photos))
		}
	}
	if attachments := opts.Task.InputAttachments.Data(); len(attachments) > 0 {
		if _, err := MaterializeResumeInputs(ctx, e.store, e.logger, workDir, opts.Task.UserID, attachments); err != nil {
			return nil, fmt.Errorf("materialize resume inputs: %w", err)
		}
		if n := DownloadInputAttachments(ctx, e.store, e.logger, workDir, opts.Task.UserID, attachments); n == 0 && hasNonResumeInputAttachments(attachments) {
			e.logger.Warn().Str("task_id", opts.Task.ID).Int("provided", len(attachments)).Msg("no AI entry input attachments could be materialized")
		}
	}
	if err := writeMontageInputJSON(workDir, opts.Task); err != nil {
		return nil, err
	}
	if err := writeMontageRuntimeFiles(workDir, opts); err != nil {
		return nil, err
	}
	if err := validateDockerHostWorkspace(workDir); err != nil {
		return nil, fmt.Errorf("validate Docker host workspace: %w", err)
	}

	apiKey, err := e.resolveAgentAPIKey(ctx, opts)
	if err != nil {
		return nil, err
	}
	e.logger.Info().
		Str("task_id", opts.Task.ID).
		Str("key_prefix", truncateKey(apiKey)).
		Str("server_url", e.serverURL).
		Str("model", agentModel).
		Bool("model_from_config", agentModel != "").
		Msg("Docker executor: starting agent execution")

	var cmd []string
	var workDirInContainer string
	if e.dockerCfg.ContainerName != "" {
		workDirInContainer = filepath.ToSlash(filepath.Join("/workspace", opts.Task.ID))
	} else {
		workDirInContainer = "/workspace"
	}
	if opts.AutoMemoryDirectory != "" {
		opts.AutoMemoryDirectory = containerMemoryDir(workDir, workDirInContainer, opts.AutoMemoryDirectory)
	}
	cmd = e.buildAgentCommand(opts, agentModel, maxTurns, workDirInContainer, apiKey)
	env := e.buildAgentEnv(opts, dockerRuntimeHome(workDirInContainer))

	var execRes execResult
	if e.dockerCfg.ContainerName != "" {
		execRes = e.executeViaExec(ctx, opts.Task.ID, workDirInContainer, runtimeUser, cmd, env, opts.HeartbeatFunc)
	} else {
		execRes = e.executeInNewContainer(ctx, opts.Task.ID, workDir, runtimeUser, cmd, env)
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
		result.Model = agentModel
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
	result.Model = agentModel

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
		} else {
			e.logger.Error().Err(err).
				Str("task_id", opts.Task.ID).
				Str("user_id", opts.Task.UserID).
				Msg("failed to resolve user API key for Docker agent, falling back to system key")
		}
	}
	rawKey, err := e.keyProvider.EnsureSystemKey(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve system API key: %w", err)
	}
	return rawKey, nil
}

func (e *DockerExecutor) buildAgentCommand(opts *ExecutionOptions, agentModel string, maxTurns int, workspace, apiKey string) []string {
	cmd := []string{
		AgentBinaryName,
		"run",
		"--server-url", strings.TrimRight(e.serverURL, "/"),
		"--api-key", apiKey,
		"--task-id", opts.Task.ID,
		"--task-type", opts.Task.Type,
		"--topic", opts.Task.Prompt,
		"--max-turns", fmt.Sprintf("%d", maxTurns),
		"--workspace", workspace,
		"--agent-flag", "anban:" + TaskToAgent(opts.Task),
	}
	// Note: visual style is NOT passed as a CLI flag — it reaches the agent
	// solely via get_project_profile(task_id) (MCP), resolved from the task
	// snapshot by the server. It never enters the prompt.
	if strings.TrimSpace(opts.Task.Goal) != "" {
		cmd = append(cmd, "--goal", opts.Task.Goal)
	}
	// Seednote image composition is the only task type that consumes these flags;
	// passing them for other types would be noise. Agent CLI defaults already
	// match the task model column defaults (content on, tail off).
	if opts.Task.Type == model.PlatformSeednote {
		cmd = append(cmd,
			"--has-content-image="+strconv.FormatBool(opts.Task.HasContentImage),
			"--has-tail-image="+strconv.FormatBool(opts.Task.HasTailImage),
		)
	}
	if opts.Task.Type == model.PlatformArticle {
		cmd = append(cmd,
			"--article-with-cover="+strconv.FormatBool(opts.Task.ArticleWithCover == nil || *opts.Task.ArticleWithCover),
			"--article-with-content-images="+strconv.FormatBool(opts.Task.ArticleWithContentImages == nil || *opts.Task.ArticleWithContentImages),
		)
	}
	if strings.TrimSpace(opts.AutoMemoryDirectory) != "" {
		cmd = append(cmd, "--auto-memory-directory", opts.AutoMemoryDirectory)
	}
	if agentModel != "" {
		cmd = append(cmd, "--model", agentModel)
	}
	return cmd
}

func (e *DockerExecutor) buildAgentEnv(opts *ExecutionOptions, runtimeHome string) []string {
	env := make([]string, 0, len(e.claudeEnv)+len(opts.MontageEnv)+5)
	for key, value := range montageEnvForTask(opts) {
		env = upsertContainerEnv(env, key, value)
	}
	for k, v := range e.claudeEnv {
		if isManagedContainerEnv(k) {
			continue
		}
		env = upsertContainerEnv(env, k, v)
	}
	env = upsertContainerEnv(env, "PATH", ContainerRuntimePath)
	env = upsertContainerEnv(env, "HOME", runtimeHome)
	if opts.Project != nil {
		env = upsertContainerEnv(env, "ANBAN_DEFAULT_PROJECT", opts.Project.ID)
	}
	env = upsertContainerEnv(env, "ANBAN_API_URL", e.serverURL)
	env = upsertContainerEnv(env, MontageSubmoduleEnvName, ContainerMontageSubmodulePath)
	return env
}

func upsertContainerEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

type execResult struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
	err    error
}

func (e *DockerExecutor) executeViaExec(ctx context.Context, taskID, workDirInContainer, runtimeUser string, cmd []string, env []string, heartbeatFunc func(string)) execResult {
	containerName := e.dockerCfg.ContainerName
	if containerName == "" {
		return execResult{err: fmt.Errorf("persistent container name is empty")}
	}

	mkdirExec, err := e.dockerCLI.ContainerExecCreate(ctx, containerName, dockerWorkspacePreparationExecOptions(workDirInContainer, runtimeUser))
	if err != nil {
		return execResult{err: fmt.Errorf("prepare persistent container workspace exec: %w", err)}
	}
	if err := e.dockerCLI.ContainerExecStart(ctx, mkdirExec.ID, container.ExecStartOptions{}); err != nil {
		return execResult{err: fmt.Errorf("start persistent container workspace preparation: %w", err)}
	}
	workspaceInspect, err := e.dockerCLI.ContainerExecInspect(ctx, mkdirExec.ID)
	if err != nil {
		return execResult{err: fmt.Errorf("inspect persistent container workspace preparation: %w", err)}
	}
	if workspaceInspect.ExitCode != 0 {
		return execResult{err: fmt.Errorf("persistent container workspace preparation exited with code %d", workspaceInspect.ExitCode)}
	}

	execCreate, err := e.dockerCLI.ContainerExecCreate(ctx, containerName, dockerAgentExecOptions(cmd, env, workDirInContainer, runtimeUser))
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

func validateDockerHostWorkspace(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("Docker workspace root %q must be a real directory: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("Docker workspace root %q must be a real directory", root)
	}
	return nil
}

func dockerWorkspacePreparationExecOptions(workDirInContainer, runtimeUser string) container.ExecOptions {
	return container.ExecOptions{
		Cmd:  []string{"mkdir", "-p", workDirInContainer},
		User: runtimeUser,
	}
}

func dockerAgentExecOptions(cmd, env []string, workDirInContainer, runtimeUser string) container.ExecOptions {
	return container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		WorkingDir:   workDirInContainer,
		User:         runtimeUser,
		AttachStdout: true,
		AttachStderr: true,
	}
}

func dockerAgentContainerConfig(image string, cmd, env []string, runtimeUser string) *container.Config {
	return &container.Config{
		Image:      image,
		Cmd:        cmd,
		Env:        env,
		WorkingDir: "/workspace",
		User:       runtimeUser,
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

func (e *DockerExecutor) executeInNewContainer(ctx context.Context, taskID, workDir, runtimeUser string, cmd []string, env []string) execResult {
	containerConfig := dockerAgentContainerConfig(e.dockerCfg.Image, cmd, env, runtimeUser)
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

	containerName := EphemeralContainerName(taskID)
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

	// Read logs with a bounded, parent-independent context: by now the wait ctx
	// may be done (cancel/timeout), so reusing it would silently drop the logs.
	// A fresh timeout keeps a dead Docker daemon from hanging log retrieval.
	logsCtx, cancelLogs := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelLogs()

	logsReader, err := e.dockerCLI.ContainerLogs(logsCtx, resp.ID, container.LogsOptions{
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
