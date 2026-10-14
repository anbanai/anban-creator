package service

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	srvconfig "github.com/anbanai/anban-creator/server/config"
)

// ContainerRuntimeFactory starts one short-lived DSH workload per interactive
// session. The Server process owns the Docker CLI connection; the DSH workload
// never receives the Docker socket.
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

var _ RuntimeFactory = (*ContainerRuntimeFactory)(nil)
var _ RuntimeFactoryWithSession = (*ContainerRuntimeFactory)(nil)

func (f ContainerRuntimeFactory) Start(ctx context.Context, sessionID, agentID, runtimeToken string, emit func(string, any)) (Runtime, error) {
	return f.StartSession(ctx, InteractiveRuntimeSpec{SessionID: sessionID, AgentPackID: agentID, RuntimeToken: runtimeToken}, emit)
}

func (f ContainerRuntimeFactory) StartSession(ctx context.Context, spec InteractiveRuntimeSpec, emit func(string, any)) (Runtime, error) {
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
	name := "anban-dsh-" + safeContainerPart(spec.SessionID)
	args := []string{"run", "--rm", "--init", "-i", "--name", name,
		"--label", "com.anban.runtime=dsh-interactive",
		"--label", "com.anban.session_id=" + spec.SessionID,
		"--label", "com.anban.server_instance=" + strings.TrimSpace(f.ServerInstanceID),
		"--network", f.Config.Network,
		"--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
		"--tmpfs", "/workspace/project/.claude/agent-memory/project-creator:rw,noexec,nosuid,size=16m",
		"--workdir", "/workspace/project",
		"--memory", strconv.FormatInt(f.Config.MemoryMB, 10) + "m",
		"--cpus", strconv.FormatInt(f.Config.CPUCores, 10),
		"--pids-limit", strconv.FormatInt(f.Config.PidsLimit, 10),
		"-e", "HOME=/tmp/dsh-home", "-e", "DSH_HOME=/tmp/dsh-home",
		"-e", "ANBAN_DSH_BRIDGE_SESSION_ID=" + spec.SessionID,
		"-e", "ANBAN_DSH_BRIDGE_RUNTIME_TOKEN=" + spec.RuntimeToken,
		"-e", "ANBAN_DSH_BRIDGE_HOME=/tmp/dsh-home",
		"-e", "ANBAN_DSH_BRIDGE_PATCH=/app/harness/dsh/project-creator.patch.yml",
		"-e", "ANBAN_DSH_BRIDGE_MCP_URL=" + strings.TrimRight(dshFirstNonEmpty(f.MCPURL, f.Config.ServerURL+"/api/mcp"), "/"),
		"-e", "ANBAN_DSH_BRIDGE_PROVIDER_API_KEY=" + f.APIKey,
	}
	if strings.TrimSpace(f.Config.ProjectMemoryVolume) != "" {
		if strings.TrimSpace(spec.ProjectID) == "" {
			return nil, fmt.Errorf("project memory volume requires a project id")
		}
		args = append(args, "--mount", "type=volume,source="+f.Config.ProjectMemoryVolume+",destination="+f.Config.ProjectMemoryMount+",volume-subpath=projects/"+safeContainerPart(spec.ProjectID)+",readonly")
	} else if strings.TrimSpace(f.ProjectMemoryRoot) != "" && strings.TrimSpace(spec.ProjectID) != "" {
		projectDir := filepath.Join(f.ProjectMemoryRoot, "projects", filepath.Base(spec.ProjectID))
		args = append(args, "--mount", "type=bind,source="+projectDir+",destination="+f.Config.ProjectMemoryMount+",readonly")
	}
	args = append(args, f.Config.Image, "--provider", f.Provider, "--model", f.Model, "--route-api", f.API, "--base-url", f.BaseURL)
	cmd := exec.CommandContext(ctx, docker, args...)
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
		return nil, fmt.Errorf("start DSH container: %w", err)
	}
	// Route values and the initial context are injected by the deployment
	// secret/config layer; never log the resulting command or environment.
	return startContainerExecRuntime(cmd, stdin, stdout, stderr, emit, []string{spec.RuntimeToken, f.APIKey}), nil
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

func startContainerExecRuntime(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, stderr io.ReadCloser, emit func(string, any), redactions []string) Runtime {
	if emit == nil {
		emit = func(string, any) {}
	}
	r := &execRuntime{cmd: cmd, in: stdin, done: make(chan struct{}), pending: map[uint64]chan error{}, cleanup: func() { _ = stdin.Close() }, redactions: redactions}
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
