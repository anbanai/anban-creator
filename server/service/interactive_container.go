package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

// ContainerRuntimeFactory starts one short-lived DSH workload per Assistant
// Workspace. The Server process owns the Docker CLI connection; the DSH
// workload never receives the Docker socket.
type ContainerRuntimeFactory struct {
	Config            srvconfig.DshConfig
	ProjectMemoryRoot string
	ServerInstanceID  string
	Provider          string
	API               string
	BaseURL           string
	APIKey            string
	Model             string
	MCPURL            string
}

// CleanupOrphans removes DSH workloads left by a previous Server process. A
// session token is never reused after restart, so retaining these containers
// would only leak resources and expose stale control channels.
func (f ContainerRuntimeFactory) CleanupOrphans(ctx context.Context) error {
	docker := strings.TrimSpace(f.Config.DockerBinary)
	if docker == "" {
		docker = "docker"
	}
	list := exec.CommandContext(ctx, docker, "ps", "-aq", "--filter", "label=com.anban.runtime=dsh-interactive")
	output, err := list.Output()
	if err != nil {
		return fmt.Errorf("list orphaned DSH containers: %w", err)
	}
	for _, id := range strings.Fields(string(output)) {
		if err := exec.CommandContext(ctx, docker, "rm", "-f", id).Run(); err != nil {
			return fmt.Errorf("remove orphaned DSH container: %w", err)
		}
	}
	return nil
}

var _ RuntimeFactory = (*ContainerRuntimeFactory)(nil)
var _ RuntimeFactoryWithSession = (*ContainerRuntimeFactory)(nil)

func (f ContainerRuntimeFactory) Start(ctx context.Context, sessionID, agentID, runtimeToken string, emit func(string, any)) (Runtime, error) {
	return f.StartSession(ctx, InteractiveRuntimeSpec{SessionID: sessionID, AgentPackID: agentID, RuntimeToken: runtimeToken}, emit)
}

func (f ContainerRuntimeFactory) StartSession(ctx context.Context, spec InteractiveRuntimeSpec, emit func(string, any)) (Runtime, error) {
	patchPath, err := interactivePresetPatchPath(spec.AgentPackID)
	if err != nil {
		return nil, err
	}
	for _, presetID := range spec.PresetIDs {
		_, pathErr := interactivePresetPatchPath(presetID)
		if pathErr != nil {
			return nil, pathErr
		}
	}
	if strings.TrimSpace(f.Config.Image) == "" || strings.TrimSpace(f.Config.Network) == "" {
		return nil, fmt.Errorf("interactive container image and network are required")
	}
	if strings.TrimSpace(spec.SessionID) == "" || strings.TrimSpace(spec.RuntimeToken) == "" {
		return nil, fmt.Errorf("interactive session id and runtime token are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	docker := strings.TrimSpace(f.Config.DockerBinary)
	if docker == "" {
		docker = "docker"
	}
	image, err := resolveInteractiveImage(ctx, docker, f.Config.Image)
	if err != nil {
		return nil, err
	}
	name := "anban-dsh-" + safeContainerPart(spec.SessionID)
	contextDir := ""
	contextCleanup := func() {}
	if strings.TrimSpace(spec.InitialContext) != "" {
		contextDir, err = os.MkdirTemp("", "anban-dsh-context-")
		if err != nil {
			return nil, fmt.Errorf("create DSH initial context directory: %w", err)
		}
		contextPath := filepath.Join(contextDir, "initial-context")
		if err := os.WriteFile(contextPath, []byte(spec.InitialContext), 0o644); err != nil {
			_ = os.RemoveAll(contextDir)
			return nil, fmt.Errorf("write DSH initial context: %w", err)
		}
		contextCleanup = func() { _ = os.RemoveAll(contextDir) }
	}
	args := []string{"run", "--rm", "--init", "-i", "--name", name,
		"--label", "com.anban.runtime=dsh-interactive",
		"--label", "com.anban.session_id=" + spec.SessionID,
		"--label", "com.anban.preset_id=" + spec.AgentPackID,
		"--label", "com.anban.server_instance=" + strings.TrimSpace(f.ServerInstanceID),
		"--network", f.Config.Network,
		"--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--tmpfs", "/workspace/project/.claude/agent-memory/" + spec.AgentPackID + ":rw,noexec,nosuid,size=16m",
		"--workdir", "/workspace/project",
		"--memory", strconv.FormatInt(f.Config.MemoryMB, 10) + "m",
		"--cpus", strconv.FormatInt(f.Config.CPUCores, 10),
		"--pids-limit", strconv.FormatInt(f.Config.PidsLimit, 10),
		"-e", "HOME=/tmp/dsh-home", "-e", "DSH_HOME=/tmp/dsh-home",
		"-e", "ANBAN_DSH_BRIDGE_SESSION_ID",
		"-e", "ANBAN_DSH_BRIDGE_INTERACTION_ID",
		"-e", "ANBAN_DSH_BRIDGE_GENERATION",
		"-e", "ANBAN_DSH_BRIDGE_WORKSPACE_ID",
		"-e", "ANBAN_DSH_BRIDGE_PRESETS",
		"-e", "ANBAN_DSH_BRIDGE_PATCHES",
		"-e", "ANBAN_DSH_BRIDGE_RUNTIME_TOKEN",
		"-e", "ANBAN_DSH_BRIDGE_HOME=/tmp/dsh-home",
		"-e", "ANBAN_DSH_BRIDGE_PRESET_ID=" + spec.AgentPackID,
		"-e", "ANBAN_DSH_BRIDGE_PATCH=" + patchPath,
		"-e", "ANBAN_DSH_BRIDGE_MCP_URL",
		"-e", "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY",
	}
	if contextDir != "" {
		args = append(args,
			"--mount", "type=bind,source="+filepath.Join(contextDir, "initial-context")+",destination=/tmp/anban-dsh-initial-context,readonly",
			"-e", "ANBAN_DSH_BRIDGE_INITIAL_CONTEXT_FILE=/tmp/anban-dsh-initial-context",
		)
	}
	if strings.TrimSpace(f.Config.ProjectMemoryVolume) != "" && strings.TrimSpace(spec.ProjectID) != "" {
		args = append(args, "--mount", "type=volume,source="+f.Config.ProjectMemoryVolume+",destination="+f.Config.ProjectMemoryMount+",volume-subpath=projects/"+safeContainerPart(spec.ProjectID)+",readonly")
	} else if strings.TrimSpace(f.ProjectMemoryRoot) != "" && strings.TrimSpace(spec.ProjectID) != "" {
		projectDir := filepath.Join(f.ProjectMemoryRoot, "projects", filepath.Base(spec.ProjectID))
		args = append(args, "--mount", "type=bind,source="+projectDir+",destination="+f.Config.ProjectMemoryMount+",readonly")
	}
	args = append(args, image, "--provider", f.Provider, "--model", f.Model, "--route-api", f.API, "--base-url", f.BaseURL)
	var cancel context.CancelFunc
	if f.Config.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(f.Config.TimeoutSec)*time.Second)
	}
	runtime, startErr := f.startCommand(ctx, cancel, docker, args, spec, emit, contextCleanup)
	if startErr != nil {
		contextCleanup()
	}
	return runtime, startErr
}

func interactivePresetPatchPath(presetID string) (string, error) {
	switch presetID {
	case "project-creator", "creation-planner", "postmortem-review":
		return "/app/harness/dsh/" + presetID + ".patch.yml", nil
	default:
		return "", fmt.Errorf("unsupported interactive DSH preset %q", strings.TrimSpace(presetID))
	}
}

func assistantPresetAllowed(presetID string) bool {
	switch strings.TrimSpace(presetID) {
	case "project-creator", "creation-planner", "postmortem-review":
		return true
	default:
		return false
	}
}

func interactiveExecPresetPatchPath(configuredPatch, presetID string) (string, error) {
	if !assistantPresetAllowed(presetID) {
		return "", fmt.Errorf("unsupported interactive DSH preset %q", strings.TrimSpace(presetID))
	}
	configuredPatch = strings.TrimSpace(configuredPatch)
	if configuredPatch == "" || filepath.Base(configuredPatch) != "project-creator.patch.yml" {
		return "", errors.New("dsh.dsh_patch must name project-creator.patch.yml for the process executor")
	}
	return filepath.Join(filepath.Dir(configuredPatch), presetID+".patch.yml"), nil
}

func resolveInteractiveImage(ctx context.Context, docker, image string) (string, error) {
	image = strings.TrimSpace(image)
	if strings.Contains(image, "@sha256:") {
		return image, nil
	}
	output, err := exec.CommandContext(ctx, docker, "image", "inspect", "--format", "{{.Id}}", image).Output()
	if err != nil {
		return "", fmt.Errorf("inspect DSH runtime image %q: %w", image, err)
	}
	resolved := strings.TrimSpace(string(output))
	if !strings.HasPrefix(resolved, "sha256:") {
		return "", fmt.Errorf("DSH runtime image %q did not resolve to an immutable digest", image)
	}
	return resolved, nil
}

func (f ContainerRuntimeFactory) startCommand(ctx context.Context, cancel context.CancelFunc, docker string, args []string, spec InteractiveRuntimeSpec, emit func(string, any), contextCleanup func()) (Runtime, error) {
	cmd := exec.CommandContext(ctx, docker, args...)
	cmd.Env = append(os.Environ(),
		"ANBAN_DSH_BRIDGE_SESSION_ID="+spec.SessionID,
		"ANBAN_DSH_BRIDGE_INTERACTION_ID="+spec.InteractionID,
		"ANBAN_DSH_BRIDGE_GENERATION="+strconv.FormatInt(spec.Generation, 10),
		"ANBAN_DSH_BRIDGE_WORKSPACE_ID="+spec.WorkspaceID,
		"ANBAN_DSH_BRIDGE_PRESETS="+strings.Join(spec.PresetIDs, ","),
		"ANBAN_DSH_BRIDGE_PATCHES="+strings.Join(patchesForSpec(spec), ","),
		"ANBAN_DSH_BRIDGE_RUNTIME_TOKEN="+spec.RuntimeToken,
		"ANBAN_DSH_BRIDGE_MCP_URL="+strings.TrimRight(dshFirstNonEmpty(f.MCPURL, f.Config.ServerURL+"/mcp"), "/"),
		"ANBAN_DSH_BRIDGE_PROVIDER_API_KEY="+f.APIKey,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, fmt.Errorf("start DSH container: %w", err)
	}
	// Route values and the initial context are injected by the deployment
	// secret/config layer; never log the resulting command or environment.
	return startContainerExecRuntime(cmd, stdin, stdout, stderr, emit, []string{spec.RuntimeToken, f.APIKey}, cancel, contextCleanup), nil
}

func patchesForSpec(spec InteractiveRuntimeSpec) []string {
	patches := []string{}
	for _, presetID := range spec.PresetIDs {
		if path, err := interactivePresetPatchPath(presetID); err == nil {
			patches = appendInteractiveUniqueString(patches, path)
		}
	}
	if len(patches) == 0 {
		if path, err := interactivePresetPatchPath(spec.AgentPackID); err == nil {
			patches = append(patches, path)
		}
	}
	return patches
}

func appendInteractiveUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func dshFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func safeContainerPart(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "session"
	}
	return b.String()
}

func startContainerExecRuntime(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr io.ReadCloser, emit func(string, any), redactions []string, cancel context.CancelFunc, contextCleanup func()) Runtime {
	if emit == nil {
		emit = func(string, any) {}
	}
	r := &execRuntime{cmd: cmd, in: stdin, done: make(chan struct{}), pending: map[uint64]chan error{}, cleanup: func() {
		_ = stdin.Close()
		if cancel != nil {
			cancel()
		}
		if contextCleanup != nil {
			contextCleanup()
		}
	}, redactions: redactions}
	stderrTail := &interactiveTailBuffer{limit: 16 * 1024}
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	go func() { _, e := io.Copy(stderrTail, stderr); stderrDone <- e }()
	go func() { stdoutDone <- r.read(stdout, emit) }()
	go func() {
		waitErr := cmd.Wait()
		stdoutErr := <-stdoutDone
		stderrErr := <-stderrDone
		r.finish(interactiveRuntimeExitError(waitErr, stdoutErr, stderrErr, stderrTail.String(), redactions...))
		emit("runtime.exit", interactiveRuntimeExitDetails(cmd, waitErr, stdoutErr, stderrErr, stderrTail.String(), redactions...))
	}()
	return r
}
