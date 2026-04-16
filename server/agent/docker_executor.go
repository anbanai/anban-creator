package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
)

// Compile-time interface check.
var _ TaskExecutor = (*DockerExecutor)(nil)

// UserKeyProvider resolves API keys for MCP authentication.
type UserKeyProvider interface {
	EnsureUserKey(ctx context.Context, userID string) (string, error)
	EnsureSystemKey(ctx context.Context) (string, error)
}

// DockerExecutor runs agent tasks inside Docker containers.
// Each task gets an isolated container with resource limits and a bind-mounted workspace.
type DockerExecutor struct {
	logger            *zerolog.Logger
	imageAPICfg       *srvconfig.ImageAPIConfig
	claudeEnv         map[string]string
	dockerCfg         srvconfig.DockerConfig
	dockerCLI         *client.Client
	defaultModel      string // configured model; empty means use env vars
	keyProvider       UserKeyProvider
	maxTurnsOverrides map[string]int
}

// NewDockerExecutor creates a new DockerExecutor.
// It initializes a Docker API client, auto-detecting the Docker socket path.
func NewDockerExecutor(
	logger *zerolog.Logger,
	imageAPICfg *srvconfig.ImageAPIConfig,
	claudeEnv map[string]string,
	dockerCfg srvconfig.DockerConfig,
	defaultModel string,
	keyProvider UserKeyProvider,
	maxTurnsOverrides map[string]int,
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

	// Verify the Docker image exists locally before accepting any task execution.
	if _, err := cli.ImageInspect(context.Background(), dockerCfg.Image); err != nil {
		return nil, fmt.Errorf("docker image %q not found locally (run 'make docker-image' to build it): %w", dockerCfg.Image, err)
	}

	return &DockerExecutor{
		logger:            logger,
		imageAPICfg:       imageAPICfg,
		claudeEnv:         claudeEnv,
		dockerCfg:         dockerCfg,
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

// Execute runs the agent task in a Docker container.
func (e *DockerExecutor) Execute(ctx context.Context, opts *ExecutionOptions) (*ExecutionResult, error) {
	// 1. Resolve defaults.
	model := opts.Model
	if model == "" {
		model = e.defaultModel
	}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns(opts.Task.Type, e.maxTurnsOverrides)
	}

	// 2. Create workspace directory on host.
	// Use 0777 so the container's non-root user (node, uid 1000) can write to it.
	var workDir string
	if e.dockerCfg.ContainerName != "" && e.dockerCfg.WorkspaceDir != "" {
		workDir = filepath.Join(e.dockerCfg.WorkspaceDir, opts.Task.ID)
	} else {
		workDir = filepath.Join(os.TempDir(), "abwriter", opts.Task.ID)
	}
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

	// 2.5 Write task log header.
	if opts.LogWriter != nil {
		opts.LogWriter.WriteHeader(opts.Task.Type, opts.Task.Topic, model, maxTurns)
	}

	// 3. Write channel config to workspace settings.json.
	if opts.Channel != nil {
		cfg, err := BuildAppConfig(opts.Channel, e.imageAPICfg)
		if err != nil {
			return nil, fmt.Errorf("build app config: %w", err)
		}
		if err := writeSettingsJSON(workDir, cfg); err != nil {
			return nil, fmt.Errorf("write settings: %w", err)
		}

		// Download brand reference image if configured.
		if opts.Channel.ReferenceImageURL != "" {
			if err := DownloadReferenceImage(ctx, workDir, opts.Channel.ReferenceImageURL); err != nil {
				e.logger.Warn().Err(err).
					Str("task_id", opts.Task.ID).
					Str("url", opts.Channel.ReferenceImageURL).
					Msg("failed to download reference image, continuing without it")
			}
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

	// 3.5. Resolve per-user API key for MCP authentication.
	var mcpAPIKey string
	if e.keyProvider != nil && opts.Task.UserID != "" {
		rawKey, err := e.keyProvider.EnsureUserKey(ctx, opts.Task.UserID)
		if err != nil {
			return nil, fmt.Errorf("resolve user API key: %w", err)
		}
		mcpAPIKey = rawKey
		e.logger.Info().
			Str("task_id", opts.Task.ID).
			Str("user_id", opts.Task.UserID).
			Msg("using per-user API key for MCP")
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
		"--permission-mode", "bypassPermissions",
		"--output-format", "json",
		"--print", userPrompt,
	}
	// Only set model if explicitly configured; otherwise let Claude CLI use env vars.
	if model != "" {
		cmd = append(cmd, "--model", model)
	}
	e.logger.Debug().Strs("cmd", cmd).Str("task_id", opts.Task.ID).Msg("container command")

	// 5. Build environment variables.
	env := []string{
		"PATH=/app:/usr/local/bin:/usr/bin:/bin",
	}
	for k, v := range e.claudeEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Inject MCP server env vars so plugin/.mcp.json can resolve
	// ${ANBANWRITER_API_KEY} and ${ANBANWRITER_API_URL} inside the container.
	if mcpAPIKey != "" {
		env = append(env, fmt.Sprintf("ANBANWRITER_API_KEY=%s", mcpAPIKey))
	}
	if e.dockerCfg.MCPBaseURL != "" {
		env = append(env, fmt.Sprintf("ANBANWRITER_API_URL=%s", e.dockerCfg.MCPBaseURL))
	}

	// 6. Dispatch to persistent container (docker exec) or create new container.
	var execRes execResult
	if e.dockerCfg.ContainerName != "" {
		execRes = e.executeViaExec(ctx, opts, workDir, cmd, env)
	} else {
		execRes = e.executeInNewContainer(ctx, opts, workDir, cmd, env)
	}

	// Handle execution failures.
	if execRes.err != "" {
		if opts.LogWriter != nil {
			opts.LogWriter.WriteError(execRes.err)
			opts.LogWriter.WriteResult(false, 0, 0, nil, nil)
		}
		return &ExecutionResult{
			Success: false,
			Error:   execRes.err,
			WorkDir: workDir,
		}, nil
	}

	// Shared post-processing: log workspace, parse results, check files.
	if files, listErr := ListWorkDirFiles(workDir); listErr == nil {
		e.logger.Info().
			Str("task_id", opts.Task.ID).
			Int("file_count", len(files)).
			Interface("files", files).
			Msg("workspace contents after execution")
	}

	if execRes.stdout.Len() > 0 {
		logDockerConversation(execRes.stdout.String(), e.logger, opts.Task.ID, opts.LogWriter)
	}

	result := &ExecutionResult{Success: true, WorkDir: workDir}
	if execRes.stdout.Len() > 0 {
		parseDockerJSONResult(execRes.stdout.String(), result, e.logger, opts.Task.ID)
	}

	if opts.LogWriter != nil {
		opts.LogWriter.WriteResult(true, result.DurationMs, result.NumTurns, result.TotalCostUSD, result.TokenUsage)
	}

	if result.WorkDir != "" {
		fileCount := CountMeaningfulFiles(result.WorkDir)
		result.NoOutputFiles = fileCount == 0
		if result.NoOutputFiles && result.NumTurns <= 1 {
			result.AgentLikelyFailed = true
		}
	}

	if result.NumTurns <= 1 {
		stdoutSnippet := ""
		if execRes.stdout.Len() > 0 {
			stdoutSnippet = execRes.stdout.String()
			if len(stdoutSnippet) > 2000 {
				stdoutSnippet = stdoutSnippet[len(stdoutSnippet)-2000:]
			}
		}
		logLevel := e.logger.Warn()
		if result.AgentLikelyFailed {
			logLevel = e.logger.Error()
		}
		logLevel.
			Str("task_id", opts.Task.ID).
			Int("num_turns", result.NumTurns).
			Bool("no_output_files", result.NoOutputFiles).
			Bool("agent_likely_failed", result.AgentLikelyFailed).
			Str("stdout", stdoutSnippet).
			Msg("agent completed with <=1 turns; agent definition may not have loaded or MCP connection failed")
	}

	if model != "" && !isKnownClaudeModel(model) {
		e.logger.Warn().
			Str("task_id", opts.Task.ID).
			Str("model", model).
			Msg("configured model is not a Claude model; agent tool use may not work correctly")
	}

	return result, nil
}

// executeViaExec runs the claude command inside a persistent container using docker exec.
// The container must already be running and have the workspace dir bind-mounted as /workspace.
// execResult holds the output of a container execution.
type execResult struct {
	stdout bytes.Buffer
	err    string // non-empty if execution failed
}

func (e *DockerExecutor) executeViaExec(ctx context.Context, opts *ExecutionOptions, workDir string, cmd []string, env []string) execResult {
	taskID := opts.Task.ID
	containerName := e.dockerCfg.ContainerName

	// Verify the persistent container exists and is running.
	nameFilter := filters.NewArgs()
	nameFilter.Add("name", containerName)
	containers, err := e.dockerCLI.ContainerList(ctx, container.ListOptions{
		Filters: nameFilter,
	})
	if err != nil || len(containers) == 0 {
		e.logger.Error().Err(err).
			Str("task_id", taskID).
			Str("container", containerName).
			Msg("persistent container not found, falling back to create+destroy")
		return e.executeInNewContainer(ctx, opts, workDir, cmd, env)
	}

	// Use task-specific subdirectory inside the container.
	// The persistent container mounts {workspace_dir} -> /workspace,
	// so /workspace/{taskID} maps to {workspace_dir}/{taskID} on the host.
	workDirInContainer := "/workspace/" + taskID

	e.logger.Info().
		Str("task_id", taskID).
		Str("container", containerName).
		Str("cwd", workDirInContainer).
		Msg("executing via persistent container")

	// Create exec instance.
	execCreate, err := e.dockerCLI.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		WorkingDir:   workDirInContainer,
		User:         "node",
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		e.logger.Error().Err(err).
			Str("task_id", taskID).
			Msg("failed to create exec instance")
		return execResult{err: fmt.Sprintf("exec create: %v", err)}
	}

	// Attach to exec to capture stdout/stderr.
	hijacked, err := e.dockerCLI.ContainerExecAttach(ctx, execCreate.ID, container.ExecStartOptions{
		Detach: false,
		Tty:    false,
	})
	if err != nil {
		e.logger.Error().Err(err).
			Str("task_id", taskID).
			Msg("failed to attach to exec")
		return execResult{err: fmt.Sprintf("exec attach: %v", err)}
	}
	defer hijacked.Close()

	// Stream output from exec (multiplexed: same 8-byte header format as container logs).
	var stdoutBuf bytes.Buffer
	var streamWg sync.WaitGroup
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		// hijacked.Reader is a *bufio.Reader, not io.ReadCloser.
		// Wrap it so streamAndCaptureLogs can call Close().
		e.streamAndCaptureLogs(io.NopCloser(hijacked.Reader), opts, &stdoutBuf)
	}()

	// Poll for exec completion with timeout.
	timeout := time.Duration(e.dockerCfg.TimeoutSec) * time.Second
	deadline := time.Now().Add(timeout)
	var timedOut bool
	var execErr string
	for {
		select {
		case <-ctx.Done():
			execErr = "task cancelled"
			break
		default:
		}
		if execErr != "" {
			break
		}
		inspect, err := e.dockerCLI.ContainerExecInspect(ctx, execCreate.ID)
		if err != nil {
			execErr = fmt.Sprintf("exec inspect: %v", err)
			break
		}
		if !inspect.Running {
			if inspect.ExitCode != 0 {
				execErr = fmt.Sprintf("exec exited with code %d", inspect.ExitCode)
			}
			break
		}
		if time.Now().After(deadline) {
			timedOut = true
			execErr = fmt.Sprintf("exec timed out after %d seconds", e.dockerCfg.TimeoutSec)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Kill the exec process if still running (timeout or cancellation).
	if timedOut || ctx.Err() != nil {
		if inspect, err := e.dockerCLI.ContainerExecInspect(context.Background(), execCreate.ID); err == nil && inspect.Running {
			// Kill the process inside the container.
			e.dockerCLI.ContainerExecCreate(context.Background(), containerName, container.ExecOptions{
				Cmd:  []string{"kill", fmt.Sprintf("%d", inspect.Pid)},
				User: "root",
			})
		}
	}

	if execErr != "" {
		e.logger.Error().Str("task_id", taskID).Str("error", execErr).Msg("exec failed")
	}

	// Wait for stream to finish reading all buffered data.
	streamWg.Wait()
	return execResult{stdout: stdoutBuf, err: execErr}
}

// executeInNewContainer creates, runs, and destroys a container for a single task (legacy mode).
func (e *DockerExecutor) executeInNewContainer(ctx context.Context, opts *ExecutionOptions, workDir string, cmd []string, env []string) execResult {
	taskID := opts.Task.ID

	// Create container with per-task bind mount.
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

	containerName := "abwriter-" + taskID
	resp, err := e.dockerCLI.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return execResult{err: fmt.Sprintf("docker create: %v", err)}
	}

	// Ensure container cleanup.
	defer func() {
		removeCtx := context.Background()
		if err := e.dockerCLI.ContainerRemove(removeCtx, resp.ID, container.RemoveOptions{Force: true}); err != nil {
			e.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to remove container")
		}
	}()

	// Start container.
	if err := e.dockerCLI.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return execResult{err: fmt.Sprintf("docker start: %v", err)}
	}

	// Follow logs for progress streaming.
	var stdoutBuf bytes.Buffer
	var logWg sync.WaitGroup
	logsReader, err := e.dockerCLI.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		Follow:     true,
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
	})
	if err != nil {
		e.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to follow container logs")
	} else {
		logWg.Add(1)
		go func() {
			defer logWg.Done()
			e.streamAndCaptureLogs(logsReader, opts, &stdoutBuf)
		}()
	}

	// Wait for container with timeout.
	timeout := time.Duration(e.dockerCfg.TimeoutSec) * time.Second
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	statusCh, errCh := e.dockerCLI.ContainerWait(waitCtx, resp.ID, container.WaitConditionNotRunning)
	var execErr string
	select {
	case err := <-errCh:
		if waitCtx.Err() == context.DeadlineExceeded {
			e.dockerCLI.ContainerStop(context.Background(), resp.ID, container.StopOptions{})
			execErr = fmt.Sprintf("container timed out after %d seconds", e.dockerCfg.TimeoutSec)
		} else if err != nil {
			execErr = fmt.Sprintf("container wait: %v", err)
		}
	case waitResult := <-statusCh:
		if waitResult.StatusCode != 0 {
			containerLogs := e.readContainerLogs(resp.ID)
			execErr = fmt.Sprintf("container exited with code %d", waitResult.StatusCode)
			if containerLogs != "" {
				if len(containerLogs) > 2000 {
					containerLogs = containerLogs[len(containerLogs)-2000:]
				}
				execErr += "\n" + containerLogs
			}
		}
	}

	logWg.Wait()
	return execResult{stdout: stdoutBuf, err: execErr}
}

// streamAndCaptureLogs reads multiplexed Docker log frames, forwards them to reads multiplexed Docker log frames, forwards them to
// OnProgress, and captures stdout frames into buf for post-execution JSON parsing.
// Docker multiplexed stream: [1 byte stream type (1=stdout, 2=stderr)][3 bytes padding][4 bytes size].
func (e *DockerExecutor) streamAndCaptureLogs(reader io.ReadCloser, opts *ExecutionOptions, stdoutBuf *bytes.Buffer) {
	defer reader.Close()
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(reader, hdr); err != nil {
			return
		}
		streamType := hdr[0]
		size := binary.BigEndian.Uint32(hdr[4:])
		if size == 0 {
			continue
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return
		}
		frame := string(buf)
		if streamType == 1 {
			// Capture stdout frames for JSON result parsing.
			stdoutBuf.Write(buf)
			// Also forward stdout to progress log for visibility.
			if opts.OnProgress != nil {
				opts.OnProgress(opts.Task.ID, frame)
			}
			// Write raw stdout to task log for real-time visibility.
			if opts.LogWriter != nil {
				opts.LogWriter.WriteRawStdout(frame)
			}
		} else {
			// stderr — forward to progress log AND server log for diagnostics.
			if opts.OnProgress != nil {
				opts.OnProgress(opts.Task.ID, frame)
			}
			e.logger.Debug().
				Str("task_id", opts.Task.ID).
				Str("container_stderr", frame).
				Msg("claude code stderr")
			if opts.LogWriter != nil {
				opts.LogWriter.WriteStderr(frame)
			}
		}
	}
}

// logDockerConversation parses all JSON lines from Claude CLI stdout and logs
// each assistant message, tool call, and tool result at debug level.
func logDockerConversation(stdout string, logger *zerolog.Logger, taskID string, lw *TaskLogWriter) {
	type assistantContent struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Name  string `json:"name"`
		ID    string `json:"id"`
		Input any    `json:"input"`
	}
	type assistantMsg struct {
		Type    string `json:"type"`
		Message struct {
			Content []assistantContent `json:"content"`
		} `json:"message"`
	}

	lines := bytes.Split([]byte(stdout), []byte("\n"))
	turnNum := 0
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		msgType, _ := raw["type"].(string)
		switch msgType {
		case "assistant":
			var msg assistantMsg
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			turnNum++
			for _, block := range msg.Message.Content {
				switch block.Type {
				case "text":
					text := block.Text
					if len(text) > 500 {
						text = text[:500] + "...(truncated)"
					}
					logger.Debug().
						Str("task_id", taskID).
						Int("turn", turnNum).
						Str("text", text).
						Msg("claude assistant text")
					if lw != nil {
						lw.WriteAssistantText(turnNum, block.Text)
					}
				case "tool_use":
					inputJSON, _ := json.Marshal(block.Input)
					inputStr := string(inputJSON)
					if len(inputStr) > 1000 {
						inputStr = inputStr[:1000] + "...(truncated)"
					}
					logger.Debug().
						Str("task_id", taskID).
						Int("turn", turnNum).
						Str("tool", block.Name).
						Str("input", inputStr).
						Msg("claude tool use")
					if lw != nil {
						lw.WriteToolUse(turnNum, block.Name, string(inputJSON))
					}
				case "tool_result":
					// Tool results come as part of user messages in some formats,
					// but can also appear in assistant messages.
					contentJSON, _ := json.Marshal(block)
					contentStr := string(contentJSON)
					if len(contentStr) > 1000 {
						contentStr = contentStr[:1000] + "...(truncated)"
					}
					logger.Debug().
						Str("task_id", taskID).
						Int("turn", turnNum).
						Str("content", contentStr).
						Msg("claude tool result")
					if lw != nil {
						lw.WriteToolResult(block.ID, string(contentJSON))
					}
				}
			}
		case "user":
			// User messages may contain tool results embedded in content.
			if rawContent, ok := raw["message"].(map[string]any); ok {
				if arr, ok := rawContent["content"].([]any); ok {
					for _, item := range arr {
						b, err := json.Marshal(item)
						if err != nil {
							continue
						}
						var block assistantContent
						if json.Unmarshal(b, &block) == nil && block.Type == "tool_result" {
							contentJSON := string(b)
							if len(contentJSON) > 1000 {
								contentJSON = contentJSON[:1000] + "...(truncated)"
							}
							logger.Debug().
								Str("task_id", taskID).
								Str("tool_use_id", block.ID).
								Str("content", contentJSON).
								Msg("claude tool result")
							if lw != nil {
								lw.WriteToolResult(block.ID, string(contentJSON))
							}
						}
					}
				}
			}
		}
	}
}

// parseDockerJSONResult attempts to extract execution metrics from the JSON
// output produced by `claude --print --output-format json`.
// The output is a series of JSON objects (one per line); the last "result" type
// message contains cost and usage data.
func parseDockerJSONResult(stdout string, result *ExecutionResult, logger *zerolog.Logger, taskID string) {
	type jsonMsg struct {
		Type         string          `json:"type"`
		Subtype      string          `json:"subtype"`
		DurationMs   int             `json:"duration_ms"`
		DurationAPMs int             `json:"duration_api_ms"`
		NumTurns     int             `json:"num_turns"`
		SessionID    string          `json:"session_id"`
		TotalCostUSD *float64        `json:"total_cost_usd"`
		Usage        json.RawMessage `json:"usage"`
		Result       *string         `json:"result"`
		IsError      bool            `json:"is_error"`
	}

	// Find the last "result" message by scanning backwards through lines.
	lines := bytes.Split([]byte(stdout), []byte("\n"))
	var lastResult []byte
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		// Quick check: result messages contain "result" type.
		if bytes.Contains(line, []byte(`"type":"result"`)) || bytes.Contains(line, []byte(`"type": "result"`)) {
			lastResult = line
			break
		}
	}

	if lastResult == nil {
		logger.Debug().Str("task_id", taskID).Msg("no result message found in docker stdout")
		return
	}

	var msg jsonMsg
	if err := json.Unmarshal(lastResult, &msg); err != nil {
		logger.Debug().Err(err).Str("task_id", taskID).Msg("failed to parse docker result JSON")
		return
	}

	result.DurationMs = msg.DurationMs
	result.DurationAPIMs = msg.DurationAPMs
	result.NumTurns = msg.NumTurns
	result.SessionID = msg.SessionID
	result.TotalCostUSD = msg.TotalCostUSD

	if len(msg.Usage) > 0 {
		var usageMap map[string]any
		if err := json.Unmarshal(msg.Usage, &usageMap); err == nil {
			result.TokenUsage = ParseTokenUsage(usageMap)
		}
	}

	logEvt := logger.Info().
		Str("task_id", taskID).
		Int("duration_ms", msg.DurationMs).
		Int("num_turns", msg.NumTurns).
		Int("duration_api_ms", msg.DurationAPMs)
	if msg.TotalCostUSD != nil {
		logEvt = logEvt.Float64("cost_usd", *msg.TotalCostUSD)
	}
	if result.TokenUsage != nil {
		logEvt = logEvt.
			Int("input_tokens", result.TokenUsage.InputTokens).
			Int("output_tokens", result.TokenUsage.OutputTokens)
	}
	logEvt.Msg("docker execution metrics")
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

// isKnownClaudeModel returns true for Claude model identifiers.
func isKnownClaudeModel(model string) bool {
	return strings.HasPrefix(model, "claude-") ||
		strings.HasPrefix(model, "anthropic:")
}
