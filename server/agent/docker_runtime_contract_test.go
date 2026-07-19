package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreatorAgentImageNamingContract(t *testing.T) {
	root := repositoryRoot(t)

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	if err := validateCreatorAgentImageNaming(makefile, compose); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCreatorAgentImageNamingRejectsDecoysAndLegacyValues(t *testing.T) {
	validMakefile := "AGENT_IMAGE := creator-agent:latest\n"
	validCompose := `services:
  # agent:
  #   image: creator-agent:latest
  #   container_name: creator-agent
`

	for _, tc := range []struct {
		name     string
		makefile string
		compose  string
	}{
		{
			name:     "Make variable name decoy",
			makefile: "LEGACY_AGENT_IMAGE := creator-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "commented Make assignment",
			makefile: "# AGENT_IMAGE := creator-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "legacy Make identity remains",
			makefile: validMakefile + "LEGACY_AGENT_IMAGE := anban-creator-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "retired short Make identity remains",
			makefile: validMakefile + "LEGACY_AGENT_IMAGE := anban-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "duplicate Make assignment overrides expected value",
			makefile: validMakefile + "AGENT_IMAGE := wrong-agent:latest\n",
			compose:  validCompose,
		},
		{
			name:     "Compose values outside agent block",
			makefile: validMakefile,
			compose: `services:
  # worker:
  #   image: creator-agent:latest
  #   container_name: creator-agent
`,
		},
		{
			name:     "legacy Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  # legacy-agent:
  #   image: anban-creator-agent:latest
  #   container_name: anban-creator-agent
`,
		},
		{
			name:     "retired short Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  # legacy-agent:
  #   image: anban-agent:latest
  #   container_name: anban-agent
`,
		},
		{
			name:     "duplicate Compose image overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "  #   image: wrong-agent:latest\n",
		},
		{
			name:     "duplicate Compose container name overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "  #   container_name: wrong-agent\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCreatorAgentImageNaming(tc.makefile, tc.compose); err == nil {
				t.Fatal("validateCreatorAgentImageNaming accepted an invalid runtime identity contract")
			}
		})
	}
}

func validateCreatorAgentImageNaming(makefile, compose string) error {
	if containsRetiredAgentIdentity(makefile) {
		return fmt.Errorf("Makefile must not retain anban-creator-agent or anban-agent identities")
	}
	makeValues := makeVariableAssignments(makefile, "AGENT_IMAGE")
	if len(makeValues) != 1 || makeValues[0] != "creator-agent:latest" {
		return fmt.Errorf("Makefile must define AGENT_IMAGE exactly once with value creator-agent:latest")
	}

	if containsRetiredAgentIdentity(compose) {
		return fmt.Errorf("docker-compose.yml must not retain anban-creator-agent or anban-agent identities")
	}
	lines := strings.Split(compose, "\n")
	agentBlockCount := 0
	var imageValues []string
	var containerNameValues []string
	for i, line := range lines {
		if line != "  # agent:" {
			continue
		}
		agentBlockCount++

		for _, blockLine := range lines[i+1:] {
			if !strings.HasPrefix(blockLine, "  #   ") {
				break
			}
			content := strings.TrimPrefix(blockLine, "  #   ")
			if strings.HasPrefix(content, " ") {
				continue
			}
			key, value, found := strings.Cut(content, ":")
			if !found {
				continue
			}
			switch key {
			case "image":
				imageValues = append(imageValues, strings.TrimSpace(value))
			case "container_name":
				containerNameValues = append(containerNameValues, strings.TrimSpace(value))
			}
		}
	}
	if agentBlockCount != 1 || len(imageValues) != 1 || imageValues[0] != "creator-agent:latest" || len(containerNameValues) != 1 || containerNameValues[0] != "creator-agent" {
		return fmt.Errorf("docker-compose.yml must define exactly one optional # agent: block with one image creator-agent:latest and one container_name creator-agent")
	}
	return nil
}

func containsRetiredAgentIdentity(text string) bool {
	for _, retired := range []string{"anban-creator-agent", "anban-agent"} {
		if strings.Contains(text, retired) {
			return true
		}
	}
	return false
}

func makeVariableAssignments(text, variable string) []string {
	var values []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, operator := range []string{":=", "?=", "+=", "="} {
			name, value, found := strings.Cut(line, operator)
			if !found || strings.TrimSpace(name) != variable {
				continue
			}
			values = append(values, strings.TrimSpace(value))
			break
		}
	}
	return values
}

func TestAgentDockerfileUsesOpenHandsAgentRuntime(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "Dockerfile.agent")
	body := readTextFile(t, path)
	for _, want := range []string{
		"FROM ghcr.io/openhands/agent-server:latest-python",
		"apt-get install -y --no-install-recommends ca-certificates curl gh git jq fontconfig fonts-noto-cjk python3 python3-venv",
		"https://deb.nodesource.com/setup_22.x",
		"if ! command -v ffmpeg >/dev/null 2>&1 || ! command -v ffprobe >/dev/null 2>&1; then",
		"apt-get install -y --no-install-recommends ffmpeg",
		"ARG CLAUDE_CODE_VERSION=2.1.208",
		"ARG MCPORTER_VERSION=0.9.0",
		`npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "mcporter@${MCPORTER_VERSION}"`,
		"claude --version",
		"mcporter --version",
		"gh --version",
		"COPY claudecode/",
		"COPY third_party/Agent-Reach/ /app/third_party/Agent-Reach/",
		"ENV AGENT_REACH_VENV=/opt/agent-reach-venv",
		`python3 -m venv "$AGENT_REACH_VENV"`,
		`--constraint /app/third_party/Agent-Reach/constraints.txt`,
		`/app/third_party/Agent-Reach`,
		`"$AGENT_REACH_VENV/bin/agent-reach" --version`,
		`ENV PATH="${AGENT_REACH_VENV}/bin:${PATH}"`,
		"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
		"npx -y skills@latest add heygen-com/hyperframes",
		"--skill music-to-video",
		"--skill slideshow",
		"npx -y skills@latest add remotion-dev/skills",
		"--skill remotion-best-practices",
		"claude plugin install --scope user anban@anbanai",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}
	for _, forbidden := range []string{"COPY third_party/OpenMontage/", "ANBAN_MONTAGE_SUBMODULE_PATH"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("%s content runtime must not contain Montage dependency %q", path, forbidden)
		}
	}
}

func TestDockerRuntimeProfiles(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent", "Dockerfile.agent-montage"} {
		path := filepath.Join(root, name)
		body := readTextFile(t, path)
		for _, want := range []string{
			"ARG CLAUDE_CODE_VERSION=2.1.208",
			"COPY --from=builder /out/anban",
			"COPY claudecode/",
			"ENTRYPOINT",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}

func TestMontageRuntimeImageContract(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "Dockerfile.agent-montage")
	body := readTextFile(t, path)
	for _, want := range []string{
		"ARG OPENMONTAGE_REVISION",
		"COPY third_party/OpenMontage/ /app/third_party/OpenMontage/",
		"requirements.txt",
		"remotion-composer/package-lock.json",
		"npm ci",
		"registry.discover()",
		"load_pipeline",
		".anban-source-revision",
		"ENV ANBAN_MONTAGE_SUBMODULE_PATH=/app/third_party/OpenMontage",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}
}

func TestServerDockerfileUsesMinimalRuntime(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "Dockerfile.server")
	body := readTextFile(t, path)
	for _, want := range []string{
		"FROM alpine:latest",
		"apk add --no-cache ca-certificates ffmpeg tzdata",
		"go build -ldflags=\"-s -w\" -o /anban-creator-server ./server/",
		"COPY --from=builder /anban-creator-server /app/anban-creator-server",
		"COPY --from=builder /build/server/billing/policy.yaml /app/conf/billing/policy.yaml",
		"COPY --from=builder /build/server/billing/products.yaml /app/conf/billing/products.yaml",
		"COPY --from=builder /build/server/billing/costs.yaml /app/conf/billing/costs.yaml",
		"COPY --from=builder /build/server/billing/promotions.yaml /app/conf/billing/promotions.yaml",
		"addgroup -S -g 1000 anban",
		"adduser -S -D -u 1000 -G anban -h /home/anban anban",
		"chown -R 1000:1000 /app/data",
		"USER 1000:1000",
		`CMD ["/app/anban-creator-server", "-config", "/app/conf/config.yaml"]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing minimal server runtime contract %q", path, want)
		}
	}
	for _, forbidden := range []string{
		"ghcr.io/openhands/agent-server",
		"./agent",
		"/usr/local/bin/anban",
		"COPY claudecode/",
		"COPY third_party/OpenMontage/",
		"COPY third_party/Agent-Reach/",
		"AGENT_REACH_VENV",
		"CLAUDE_PLUGIN_ROOT",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"npm install",
		"mcporter",
		"npx -y skills",
		"claude plugin",
		"COPY --from=builder /build/server/billing /app/conf/billing",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("%s must not contain Agent runtime dependency %q", path, forbidden)
		}
	}
}

func TestServerComposeInjectsBillingAdminKeyAndDocumentsIt(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	if !strings.Contains(compose, `ANBAN_BILLING_ADMIN_API_KEY: "${ANBAN_BILLING_ADMIN_API_KEY:?ANBAN_BILLING_ADMIN_API_KEY is required}"`) {
		t.Fatal("docker-compose server must require and inject ANBAN_BILLING_ADMIN_API_KEY")
	}
	envExample := readTextFile(t, filepath.Join(root, ".env.example"))
	if !strings.Contains(envExample, "ANBAN_BILLING_ADMIN_API_KEY=") {
		t.Fatal(".env.example must declare ANBAN_BILLING_ADMIN_API_KEY without a secret value")
	}
	readme := readTextFile(t, filepath.Join(root, "README.md"))
	if !strings.Contains(readme, "ANBAN_BILLING_ADMIN_API_KEY") {
		t.Fatal("README must document the required billing admin key for Docker Compose")
	}
	for _, path := range []string{filepath.Join(root, ".gitignore"), filepath.Join(root, ".dockerignore")} {
		body := readTextFile(t, path)
		if !strings.Contains("\n"+body+"\n", "\n.env\n") {
			t.Fatalf("%s must exclude the root .env secret file", path)
		}
	}
}

func TestOpenHandsAgentRuntimeInstallsPackagesAsRoot(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "Dockerfile.agent")
	body := readTextFile(t, path)
	from := strings.Index(body, "FROM ghcr.io/openhands/agent-server:latest-python")
	if from < 0 {
		t.Fatalf("%s missing OpenHands runtime stage", path)
	}
	apt := strings.Index(body[from:], "apt-get update")
	if apt < 0 {
		t.Fatalf("%s missing apt-get update in OpenHands runtime stage", path)
	}
	beforeApt := body[from : from+apt]
	if !strings.Contains(beforeApt, "USER root") {
		t.Fatalf("%s must switch to USER root before apt-get update because the OpenHands base image may default to a non-root user", path)
	}
}

func TestAgentReachIsBuildInstalledAndRuntimeReadOnly(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "Dockerfile.agent"))
	for _, want := range []string{
		`python3 -m venv "$AGENT_REACH_VENV"`,
		`"$AGENT_REACH_VENV/bin/pip" install`,
		`--constraint /app/third_party/Agent-Reach/constraints.txt`,
		`"$AGENT_REACH_VENV/bin/agent-reach" --version`,
		`"$AGENT_REACH_VENV/bin/agent-reach" doctor --json`,
		`{"status", "active_backend", "message"}`,
		`chown -R root:root /app/third_party/Agent-Reach "$AGENT_REACH_VENV"`,
		`chmod -R a=rX /app/third_party/Agent-Reach "$AGENT_REACH_VENV"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent missing Agent-Reach build contract %q", want)
		}
	}
	installAt := strings.Index(body, `python3 -m venv "$AGENT_REACH_VENV"`)
	runtimeUserAt := strings.Index(body, "USER 1000:1000")
	if installAt < 0 || runtimeUserAt < installAt {
		t.Fatalf("Agent-Reach must be installed before switching to runtime user: install=%d user=%d", installAt, runtimeUserAt)
	}
	if ContainerRuntimePath != "/opt/agent-reach-venv/bin:/usr/local/bin:/usr/bin:/bin" {
		t.Fatalf("ContainerRuntimePath = %q, want Agent-Reach venv first", ContainerRuntimePath)
	}
}

func TestAgentReachSubmodulePathIsDeclared(t *testing.T) {
	gitmodules := readTextFile(t, filepath.Join(repositoryRoot(t), ".gitmodules"))
	want := "[submodule \"third_party/Agent-Reach\"]\n" +
		"\tpath = third_party/Agent-Reach\n" +
		"\turl = https://github.com/Panniantong/Agent-Reach.git\n" +
		"\tbranch = main\n" +
		"\tshallow = true\n"
	if !strings.Contains(gitmodules, want) {
		t.Fatalf(".gitmodules missing exact Agent-Reach submodule contract:\n%s", want)
	}
}

func TestDockerRuntimeAgentImageAndKubernetesJobAgreeOnNumericIdentity(t *testing.T) {
	if kubernetesAgentUID != 1000 || kubernetesAgentGID != 1000 {
		t.Fatalf("Job identity = %d:%d, want numeric 1000:1000", kubernetesAgentUID, kubernetesAgentGID)
	}
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "Dockerfile.agent"))
	for _, want := range []string{
		`getent passwd 1000 >/dev/null`,
		`getent group 1000 >/dev/null`,
		`install -d -m 0755 -o 1000 -g 1000 /home/node`,
		"ENV HOME=/home/node",
		"USER 1000:1000",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent missing numeric identity contract %q", want)
		}
	}
	for _, forbidden := range []string{"useradd", "groupadd", "USER node", "id -u node", "node:node"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Dockerfile.agent must not rely on node account via %q", forbidden)
		}
	}
	homeAt := strings.Index(body, "ENV HOME=/home/node")
	userAt := strings.Index(body, "USER 1000:1000")
	installAt := strings.Index(body, "npx -y skills@latest add")
	if homeAt < 0 || userAt < homeAt || installAt < userAt {
		t.Fatalf("HOME/numeric USER/install order is invalid: HOME=%d USER=%d install=%d", homeAt, userAt, installAt)
	}
}

func TestDockerRuntimeAgentSourceUsesResolvedLocalIdentity(t *testing.T) {
	root := repositoryRoot(t)
	dockerExecutor := readTextFile(t, filepath.Join(root, "server", "agent", "docker_executor.go"))
	if strings.Contains(dockerExecutor, `User:         "node"`) || strings.Contains(dockerExecutor, `User:       "node"`) {
		t.Fatal("Docker executor must use numeric runtime identity")
	}
	for _, want := range []string{
		"currentDockerRuntimeUser()",
		"dockerWorkspacePreparationExecOptions(workDirInContainer, runtimeUser)",
		"dockerAgentExecOptions(cmd, env, workDirInContainer, runtimeUser)",
		"dockerAgentContainerConfig(e.dockerCfg.Image, cmd, env, runtimeUser)",
	} {
		if !strings.Contains(dockerExecutor, want) {
			t.Fatalf("Docker executor missing resolved local identity flow %q", want)
		}
	}
	if strings.Contains(dockerExecutor, "chown -R") || strings.Contains(dockerExecutor, "os.Chmod") {
		t.Fatal("Docker executor must not recursively change bind-mounted host permissions")
	}
	for _, want := range []string{
		"prepare persistent container workspace exec",
		"start persistent container workspace preparation",
		"inspect persistent container workspace preparation",
		"persistent container workspace preparation exited with code",
	} {
		if !strings.Contains(dockerExecutor, want) {
			t.Fatalf("Docker executor missing explicit workspace preparation error %q", want)
		}
	}
	if strings.Contains(dockerExecutor, "if err == nil {\n\t\t\t_ = e.dockerCLI.ContainerExecStart") {
		t.Fatal("Docker executor must not ignore persistent workspace preparation failures")
	}
	if _, err := os.Stat(filepath.Join(root, "server", "agent", "kubernetes_executor.go")); !os.IsNotExist(err) {
		t.Fatalf("legacy Kubernetes executor must be deleted, stat error = %v", err)
	}
}

func TestDockerRuntimeAgentImageSnapshotsInstalledHomeState(t *testing.T) {
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "Dockerfile.agent"))
	for _, want := range []string{
		"ENV ANBAN_HOME_TEMPLATE=/opt/anban-home-template",
		`printf '%s\n' '--js-runtimes node' > "$HOME/.config/yt-dlp/config"`,
		`install -m 0444 "$HOME/.config/yt-dlp/config" "$ANBAN_HOME_TEMPLATE/.config/yt-dlp/config"`,
		`for skill in music-to-video slideshow remotion-best-practices; do`,
		`test -f "$HOME/.claude/skills/$skill/SKILL.md"`,
		`cp -a "$HOME/.claude/skills/$skill/." "$ANBAN_HOME_TEMPLATE/.claude/skills/$skill/"`,
		`for file in known_marketplaces.json installed_plugins.json; do`,
		`cp -a "$HOME/.claude/plugins/$file" "$ANBAN_HOME_TEMPLATE/.claude/plugins/$file"`,
		`test -d "$HOME/.claude/plugins/cache/anbanai"`,
		`cp -a "$HOME/.claude/plugins/cache/anbanai" "$ANBAN_HOME_TEMPLATE/.claude/plugins/cache/anbanai"`,
		`find "$ANBAN_HOME_TEMPLATE" -type l -print -quit`,
		`chown -R root:root "$ANBAN_HOME_TEMPLATE"`,
		`chmod -R a=rX "$ANBAN_HOME_TEMPLATE"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent missing immutable home-template contract %q", want)
		}
	}
	installAt := strings.Index(body, "claude plugin install --scope user anban@anbanai")
	snapshotAt := strings.Index(body, `for skill in music-to-video slideshow remotion-best-practices; do`)
	if installAt < 0 || snapshotAt < installAt {
		t.Fatalf("home snapshot must occur after all node-user plugin installation: install=%d snapshot=%d", installAt, snapshotAt)
	}
	for _, forbidden := range []string{
		`for entry in .claude .agents .claude.json; do`,
		`cp -a "/home/node/.claude"`,
		`cp -a "/home/node/.agents"`,
		`cp -a "/home/node/.claude.json"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Dockerfile.agent must not broadly snapshot user home via %q", forbidden)
		}
	}
}

func TestDockerRuntimeAgentImageIsImmutableOneShotJobRuntime(t *testing.T) {
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "Dockerfile.agent"))
	for _, want := range []string{
		`find /anbanai -type l -print -quit`,
		`chown -R root:root /anbanai`,
		`chmod -R a=rX /anbanai`,
		`ENTRYPOINT ["tini", "--", "anban"]`,
		`CMD ["job"]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.agent missing immutable one-shot contract %q", want)
		}
	}
	for _, forbidden := range []string{`CMD ["sleep", "infinity"]`, `USER root\nWORKDIR /workspace`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Dockerfile.agent retains reusable runtime contract %q", forbidden)
		}
	}
}

func TestDockerignoreExcludesLargeNonRuntimeTrees(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, ".dockerignore"))
	for _, want := range []string{
		"desktop/",
		"miniapp/",
		"openclaw/",
		"codex/",
		"**/node_modules/",
		"**/dist/",
		"**/.cache/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf(".dockerignore missing %q", want)
		}
	}
}

func TestAgentDockerfileIsRootEntrypoint(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile.agent")); err != nil {
		t.Fatalf("Dockerfile.agent must exist at repository root for CI builds with root context: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "agent", "Dockerfile")); err == nil {
		t.Fatalf("agent/Dockerfile must not exist; use root Dockerfile.agent so CI context is unambiguous")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat agent/Dockerfile: %v", err)
	}
}

func TestServerDockerfileIsRootEntrypoint(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile.server")); err != nil {
		t.Fatalf("Dockerfile.server must exist at repository root for CI builds with root context: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "server", "Dockerfile")); err == nil {
		t.Fatalf("server/Dockerfile must not exist; use root Dockerfile.server so CI context is unambiguous")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat server/Dockerfile: %v", err)
	}
}

func TestStudioDockerfileIsRootEntrypoint(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "Dockerfile.studio")); err != nil {
		t.Fatalf("Dockerfile.studio must exist at repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "studio", "Dockerfile")); err == nil {
		t.Fatalf("studio/Dockerfile must not exist; use root Dockerfile.studio so application Dockerfiles share one entrypoint convention")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat studio/Dockerfile: %v", err)
	}
}

func TestDockerBuildInputsUseRollingImageTags(t *testing.T) {
	root := repositoryRoot(t)
	for _, tc := range []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(root, "Dockerfile.agent"),
			want: []string{"FROM golang:alpine AS builder", "FROM ghcr.io/openhands/agent-server:latest-python"},
		},
		{
			path: filepath.Join(root, "Dockerfile.server"),
			want: []string{"FROM golang:alpine AS builder", "FROM alpine:latest"},
		},
		{
			path: filepath.Join(root, "Dockerfile.studio"),
			want: []string{"FROM oven/bun:latest AS build", "FROM nginx:alpine"},
		},
		{
			path: filepath.Join(root, "docker", "wcflink.Dockerfile"),
			want: []string{"FROM golang:alpine AS builder", "FROM alpine:latest"},
		},
		{
			path: filepath.Join(root, "docker-compose.yml"),
			want: []string{"image: mysql:latest", "image: redis:alpine", "image: xpzouying/xiaohongshu-mcp:latest", "dockerfile: ../Dockerfile.studio"},
		},
	} {
		body := readTextFile(t, tc.path)
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing rolling image/build contract %q", tc.path, want)
			}
		}
	}
}
