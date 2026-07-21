package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
  agent:
    image: creator-agent:latest
    container_name: creator-agent
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
  worker:
    image: creator-agent:latest
    container_name: creator-agent
`,
		},
		{
			name:     "legacy Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  legacy-agent:
    image: anban-creator-agent:latest
    container_name: anban-creator-agent
`,
		},
		{
			name:     "retired short Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  legacy-agent:
    image: anban-agent:latest
    container_name: anban-agent
`,
		},
		{
			name:     "duplicate Compose image overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "    image: wrong-agent:latest\n",
		},
		{
			name:     "duplicate Compose container name overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "    container_name: wrong-agent\n",
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
		if line != "  agent:" {
			continue
		}
		agentBlockCount++

		for _, blockLine := range lines[i+1:] {
			if !strings.HasPrefix(blockLine, "    ") {
				break
			}
			content := strings.TrimPrefix(blockLine, "    ")
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
		return fmt.Errorf("docker-compose.yml must define exactly one agent service with one image creator-agent:latest and one container_name creator-agent")
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
			path: filepath.Join(root, "Dockerfile.wcflink"),
			want: []string{"FROM golang:alpine AS builder", "FROM alpine:latest"},
		},
		{
			path: filepath.Join(root, "docker-compose.yml"),
			want: []string{
				"image: mysql:latest",
				"image: redis:alpine",
				"image: xpzouying/xiaohongshu-mcp:latest",
				"dockerfile: Dockerfile.wcflink",
				"dockerfile: Dockerfile.studio",
				"VITE_API_BASE_URL: /api/v1",
			},
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

func TestDockerfileInventoryIsRootOnly(t *testing.T) {
	root := repositoryRoot(t)
	got := trackedDockerfiles(t, root)
	want := []string{
		"Dockerfile.agent",
		"Dockerfile.agent-montage",
		"Dockerfile.server",
		"Dockerfile.studio",
		"Dockerfile.wcflink",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("repository-owned Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestCollectOwnedDockerfilesDetectsUnexpectedEntrypoints(t *testing.T) {
	got := collectOwnedDockerfilePaths([]string{
		"Dockerfile.server",
		"nested/Dockerfile.extra",
		"claudecode/Dockerfile.plugin",
		"third_party/tool/Dockerfile",
		"web/node_modules/Dockerfile",
		"web/dist/Dockerfile.generated",
		".worktrees/other/Dockerfile",
	})
	want := []string{"Dockerfile.server", "nested/Dockerfile.extra"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fixture Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestStudioDockerfileUsesRootBuildContext(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "Dockerfile.studio"))
	for _, want := range []string{
		"# syntax=docker/dockerfile:1.7",
		"FROM oven/bun:latest AS build",
		"WORKDIR /app",
		"COPY studio/package.json studio/bun.lock ./",
		"COPY studio/ .",
		"COPY studio/default.conf.template /etc/nginx/templates/default.conf.template",
		"RUN bun install --frozen-lockfile",
		"ARG VITE_API_BASE_URL=/api/v1",
		"ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}",
		"RUN bun run build",
		"FROM nginx:alpine",
		"ENV BACKEND_HOST=server",
		"COPY --from=build /app/dist /usr/share/nginx/html",
		"EXPOSE 80",
		"CMD [\"nginx\", \"-g\", \"daemon off;\"]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.studio missing root-context Studio contract %q", want)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "studio", ".dockerignore")); err == nil {
		t.Fatal("studio/.dockerignore must not exist; root-context builds use Dockerfile.studio.dockerignore")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat studio/.dockerignore: %v", err)
	}

	rules := dockerignoreRules(readTextFile(t, filepath.Join(root, "Dockerfile.studio.dockerignore")))
	if len(rules) == 0 || rules[0] != "**" {
		t.Fatalf("Dockerfile.studio.dockerignore must begin its non-comment rules with broad exclusion **; got %v", rules)
	}
	studioDir := dockerignoreRuleIndex(t, rules, "!studio/")
	studioTree := dockerignoreRuleIndex(t, rules, "!studio/**")
	if studioDir <= 0 || studioTree <= studioDir {
		t.Fatalf("Dockerfile.studio.dockerignore must unignore studio/ then studio/** after **; got %v", rules)
	}
	for _, generated := range []string{
		"studio/node_modules/",
		"studio/dist/",
		"studio/build/",
		"studio/.cache/",
		"studio/.vite/",
		"studio/.next/",
		"studio/coverage/",
		"studio/.env*",
		"studio/**/.env*",
		"studio/*.pem",
		"studio/**/*.pem",
		"studio/.vercel/",
		"studio/out/",
		"studio/*.tsbuildinfo",
		"studio/**/*.tsbuildinfo",
		"studio/next-env.d.ts",
		"studio/**/next-env.d.ts",
		"studio/.pnp.*",
		"studio/.yarn/",
		"studio/.yarnrc*",
		"studio/npm-debug.log*",
		"studio/**/npm-debug.log*",
		"studio/yarn-debug.log*",
		"studio/**/yarn-debug.log*",
		"studio/yarn-error.log*",
		"studio/**/yarn-error.log*",
		"studio/pnpm-debug.log*",
		"studio/**/pnpm-debug.log*",
	} {
		if index := dockerignoreRuleIndex(t, rules, generated); index <= studioTree {
			t.Fatalf("Dockerfile.studio.dockerignore must exclude %q after unignoring Studio source; got %v", generated, rules)
		}
	}
}

func TestWcfLinkDockerfileRetainsPinnedBuildContract(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "Dockerfile.wcflink"))
	for _, want := range []string{
		"ARG WCFLINK_REPO=https://github.com/lich0821/wcfLink.git",
		"ARG WCFLINK_REF=refs/tags/v0.1.0",
		"ARG WCFLINK_COMMIT=fb0999b81043c91e8fddb780eb2ecf03f1f8588f",
		"git fetch --depth 1 origin \"$WCFLINK_REF\"",
		"test \"$(git rev-parse HEAD)\" = \"$WCFLINK_COMMIT\"",
		"CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags=\"-s -w\" -o /out/wcfLink ./cmd/wcfLink",
		"FROM alpine:latest",
		"COPY --from=builder /out/wcfLink /app/wcfLink",
		"ENV WCFLINK_LISTEN_ADDR=:18070 \\\n+    WCFLINK_STATE_DIR=/app/state \\\n+    WCFLINK_DB_PATH=/app/state/wcf.db \\\n+    WCFLINK_LOG_LEVEL=info",
		"EXPOSE 18070",
		"VOLUME [\"/app/state\"]",
		"ENTRYPOINT [\"/app/wcfLink\"]",
	} {
		want = strings.ReplaceAll(want, "\n+", "\n")
		if !strings.Contains(body, want) {
			t.Fatalf("Dockerfile.wcflink missing pinned runtime contract %q", want)
		}
	}
}

func TestComposeAndMakefileUseRootDockerfileBuilds(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"agent:\n    build:\n      context: .\n      dockerfile: Dockerfile.agent",
		"wcflink:\n    build:\n      context: .\n      dockerfile: Dockerfile.wcflink",
		"server:\n    build:\n      context: .\n      dockerfile: Dockerfile.server",
		"studio:\n    build:\n      context: .\n      dockerfile: Dockerfile.studio",
		"studio:\n    build:\n      context: .\n      dockerfile: Dockerfile.studio\n      args:\n        VITE_API_BASE_URL: /api/v1",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing root Docker build contract %q", want)
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"WCFLINK_IMAGE := anban-creator-wcflink:latest",
		"STUDIO_IMAGE := anban-creator-studio:latest",
		"docker-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image docker-images",
		"docker-montage-agent-image:",
		"docker-wcflink-image:",
		"docker-studio-image:",
		"docker-images: docker-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image",
		"docker build -f Dockerfile.agent-montage",
		"docker build -f Dockerfile.wcflink -t $(WCFLINK_IMAGE) .",
		"docker build -f Dockerfile.studio -t $(STUDIO_IMAGE) .",
		"docker-image: docker-agent-image",
		"make docker-agent-image",
		"make docker-server-image",
		"make docker-wcflink-image",
		"make docker-studio-image",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing root Docker build contract %q", want)
		}
	}
}

func TestComposeUsesPersistentDockerExecutorRuntime(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"pull_policy: build\n    container_name: creator-agent",
		"entrypoint: [\"tini\", \"--\"]\n    command: [\"sleep\", \"infinity\"]",
		"- ./data/workspace:/workspace",
		"agent:\n        condition: service_started",
		"ANBAN_CLAUDE_EXECUTOR: \"docker\"",
		"ANBAN_CLAUDE_AGENT_SERVER_URL: \"http://server:8080\"",
		"ANBAN_CLAUDE_DOCKER_IMAGE: \"creator-agent:latest\"",
		"ANBAN_CLAUDE_DOCKER_CONTAINER_NAME: \"creator-agent\"",
		"ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR: \"/app/data/workspace\"",
		"- ./data/workspace:/app/data/workspace",
		"- /var/run/docker.sock:/var/run/docker.sock",
		"group_add:\n      - \"${DOCKER_GID:-0}\"",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing persistent Docker executor contract %q", want)
		}
	}

	for _, configPath := range []string{"server/config.yaml", "server/config.example.yaml"} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(configPath)))
		for _, want := range []string{
			"image: \"${ANBAN_CLAUDE_DOCKER_IMAGE:-creator-agent:latest}\"",
			"container_name: \"${ANBAN_CLAUDE_DOCKER_CONTAINER_NAME}\"",
			"workspace_dir: \"${ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR}\"",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing Docker executor config contract %q", configPath, want)
			}
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"DOCKER_SOCKET_GID := $(shell stat -L -c '%g' /var/run/docker.sock 2>/dev/null || stat -L -f '%g' /var/run/docker.sock 2>/dev/null || echo 0)",
		"DOCKER_GID=\"$(DOCKER_SOCKET_GID)\" docker compose up -d",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing Docker socket group contract %q", want)
		}
	}
}

func TestNoStaleDockerfileReferences(t *testing.T) {
	root := repositoryRoot(t)
	paths := staleDockerfileReferencePaths(t, root)
	if len(paths) > 0 {
		t.Fatalf("stale Dockerfile references remain in repository-owned files: %s", strings.Join(paths, ", "))
	}
}

func trackedDockerfiles(t *testing.T, root string) []string {
	t.Helper()
	return collectOwnedDockerfilePaths(trackedFiles(t, root))
}

func collectOwnedDockerfilePaths(paths []string) []string {
	var dockerfiles []string
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if !ownedDockerContractPath(path) {
			continue
		}
		name := filepath.Base(path)
		if strings.HasSuffix(name, ".dockerignore") {
			continue
		}
		if !strings.HasPrefix(name, "Dockerfile") && !strings.HasSuffix(name, ".Dockerfile") {
			continue
		}
		dockerfiles = append(dockerfiles, path)
	}
	sort.Strings(dockerfiles)
	return dockerfiles
}

func staleDockerfileReferencePaths(t *testing.T, root string) []string {
	t.Helper()
	const contractTestPath = "server/agent/docker_runtime_contract_test.go"
	var paths []string
	for _, rel := range trackedFiles(t, root) {
		rel = filepath.ToSlash(rel)
		if !ownedDockerContractPath(rel) {
			continue
		}
		if rel == contractTestPath {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read tracked file %s: %v", rel, err)
		}
		if bytes.IndexByte(body, 0) >= 0 {
			continue
		}
		if bytes.Contains(body, []byte("studio/Dockerfile")) || bytes.Contains(body, []byte("docker/wcflink.Dockerfile")) {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	return paths
}

func dockerignoreRules(body string) []string {
	var rules []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, line)
		}
	}
	return rules
}

func dockerignoreRuleIndex(t *testing.T, rules []string, want string) int {
	t.Helper()
	for index, rule := range rules {
		if rule == want {
			return index
		}
	}
	t.Fatalf("Dockerfile.studio.dockerignore missing %q; got %v", want, rules)
	return -1
}

func ownedDockerContractPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		switch segment {
		case ".git", ".worktrees", "claudecode", "codex", "openclaw", "third_party", "vendor", "node_modules", "dist", "build", "coverage", ".cache", ".vite", ".next", "bin", "data", "release":
			return false
		}
	}
	return true
}
