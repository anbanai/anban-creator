package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
	validMakefile := "AGENT_IMAGE := creator-agent-article:latest\n"
	validCompose := `services:
  agent:
    image: creator-agent-article:latest
    container_name: creator-agent-article
`

	for _, tc := range []struct {
		name     string
		makefile string
		compose  string
	}{
		{
			name:     "Make variable name decoy",
			makefile: "LEGACY_AGENT_IMAGE := creator-agent-article:latest\n",
			compose:  validCompose,
		},
		{
			name:     "commented Make assignment",
			makefile: "# AGENT_IMAGE := creator-agent-article:latest\n",
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
    image: creator-agent-article:latest
    container_name: creator-agent-article
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
	if len(makeValues) != 1 || makeValues[0] != "creator-agent-article:latest" {
		return fmt.Errorf("Makefile must define AGENT_IMAGE exactly once with value creator-agent-article:latest")
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
	if agentBlockCount != 1 || len(imageValues) != 1 || imageValues[0] != "creator-agent-article:latest" || len(containerNameValues) != 1 || containerNameValues[0] != "creator-agent-article" {
		return fmt.Errorf("docker-compose.yml must define exactly one agent service with one image creator-agent-article:latest and one container_name creator-agent-article")
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

func TestAgentDockerfilesSeparateArticleAndSeednoteDependencies(t *testing.T) {
	root := repositoryRoot(t)
	articlePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-article")
	seednotePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote")
	article := readTextFile(t, articlePath)
	seednote := readTextFile(t, seednotePath)
	for _, want := range []string{
		"FROM node:22-bookworm-slim",
		"apt-get install -y --no-install-recommends ca-certificates curl git jq tini",
		"ARG CLAUDE_CODE_VERSION=2.1.208",
		`npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"`,
		"claude --version",
		"COPY plugins/",
		"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
		"claude plugin install --scope user anban@anbanai",
	} {
		for _, runtime := range []struct {
			path string
			body string
		}{{articlePath, article}, {seednotePath, seednote}} {
			if !strings.Contains(runtime.body, want) {
				t.Fatalf("%s missing %q", runtime.path, want)
			}
		}
	}
	for _, forbidden := range []string{"Agent-Reach", "python3", "mcporter", "ffmpeg", "fonts-noto-cjk", "OpenMontage"} {
		if strings.Contains(article, forbidden) {
			t.Fatalf("%s must not contain specialized dependency %q", articlePath, forbidden)
		}
	}
	for _, want := range []string{"python3 python3-venv", "ARG MCPORTER_VERSION=0.9.0", "mcporter --version", "COPY third_party/Agent-Reach/", `python3 -m venv "$AGENT_REACH_VENV"`} {
		if !strings.Contains(seednote, want) {
			t.Fatalf("%s missing %q", seednotePath, want)
		}
	}
	for _, forbidden := range []string{"COPY third_party/OpenMontage/", "ANBAN_MONTAGE_SUBMODULE_PATH"} {
		if strings.Contains(article, forbidden) || strings.Contains(seednote, forbidden) {
			t.Fatalf("Article and Seednote Dockerfiles must not contain Montage dependency %q", forbidden)
		}
	}
}

func TestDockerRuntimeProfiles(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"deploy/docker/Dockerfile.agent-article", "deploy/docker/Dockerfile.agent-seednote", "deploy/docker/Dockerfile.agent-montage"} {
		path := filepath.Join(root, name)
		body := readTextFile(t, path)
		for _, want := range []string{
			"ARG CLAUDE_CODE_VERSION=2.1.208",
			"COPY --from=builder /out/anban",
			"COPY plugins/",
			"ENTRYPOINT",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}

func TestGoDockerfilesStageLocalSDKModuleBeforeDependencyDownload(t *testing.T) {
	root := repositoryRoot(t)
	const sdkModuleCopy = "COPY third_party/claude-agent-sdk-go/go.mod ./third_party/claude-agent-sdk-go/go.mod"
	const dependencyDownload = "RUN go mod download"

	for _, name := range []string{"deploy/docker/Dockerfile.agent-article", "deploy/docker/Dockerfile.agent-seednote", "deploy/docker/Dockerfile.agent-montage", "deploy/docker/Dockerfile.server"} {
		t.Run(name, func(t *testing.T) {
			body := readTextFile(t, filepath.Join(root, name))
			copyIndex := strings.Index(body, sdkModuleCopy)
			downloadIndex := strings.Index(body, dependencyDownload)
			if copyIndex < 0 {
				t.Fatalf("%s must copy the repository-local Claude Agent SDK module before resolving the root go.mod", name)
			}
			if downloadIndex < 0 {
				t.Fatalf("%s missing %q", name, dependencyDownload)
			}
			if copyIndex > downloadIndex {
				t.Fatalf("%s copies the repository-local Claude Agent SDK module after dependency download", name)
			}
		})
	}
}

func TestMontageRuntimeImageContract(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "deploy/docker/Dockerfile.agent-montage")
	body := readTextFile(t, path)
	for _, want := range []string{
		"COPY third_party/OpenMontage/ /opt/montage-template/",
		"requirements.txt",
		"remotion-composer/package-lock.json",
		"npm ci",
		"registry.discover()",
		"load_pipeline",
		"ENV ANBAN_MONTAGE_TEMPLATE_PATH=/opt/montage-template",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}
	for _, forbidden := range []string{"Agent-Reach", "MCPORTER_VERSION", "mcporter", "gh --version", " ca-certificates curl gh git", "OPENMONTAGE_REVISION", ".anban-source-revision", "ANBAN_MONTAGE_SUBMODULE_PATH=/app"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("%s must not contain Seednote-only dependency %q", path, forbidden)
		}
	}
}

func TestServerDockerfileUsesMinimalRuntime(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "deploy/docker/Dockerfile.server")
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
		"COPY plugins/",
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

func TestArticleAgentRuntimeInstallsPackagesAsRoot(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "deploy/docker/Dockerfile.agent-article")
	body := readTextFile(t, path)
	from := strings.Index(body, "FROM node:22-bookworm-slim")
	if from < 0 {
		t.Fatalf("%s missing Article runtime", path)
	}
	apt := strings.Index(body[from:], "apt-get update")
	if apt < 0 {
		t.Fatalf("%s missing apt-get update in Article runtime", path)
	}
	beforeApt := body[from : from+apt]
	if !strings.Contains(beforeApt, "USER root") {
		t.Fatalf("%s must switch to USER root before apt-get update", path)
	}
}

func TestAgentReachIsBuildInstalledAndRuntimeReadOnly(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote"))
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
			t.Fatalf("deploy/docker/Dockerfile.agent-seednote missing Agent-Reach build contract %q", want)
		}
	}
	installAt := strings.Index(body, `python3 -m venv "$AGENT_REACH_VENV"`)
	runtimeUserAt := strings.Index(body[installAt:], "USER 1000:1000")
	if runtimeUserAt >= 0 {
		runtimeUserAt += installAt
	}
	if installAt < 0 || runtimeUserAt < installAt {
		t.Fatalf("Agent-Reach must be installed before switching to runtime user: install=%d user=%d", installAt, runtimeUserAt)
	}
	if ContainerSeednoteRuntimePath != "/opt/agent-reach-venv/bin:/usr/local/bin:/usr/bin:/bin" {
		t.Fatalf("ContainerSeednoteRuntimePath = %q, want Agent-Reach venv first", ContainerSeednoteRuntimePath)
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
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		path := filepath.Join(root, "deploy", "docker", name)
		body := readTextFile(t, path)
		for _, want := range []string{
			`getent passwd 1000 >/dev/null`,
			`getent group 1000 >/dev/null`,
			`install -d -m 0755 -o 1000 -g 1000 /home/node`,
			"ENV HOME=/home/node",
			"USER 1000:1000",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing numeric identity contract %q", path, want)
			}
		}
		for _, forbidden := range []string{"useradd", "groupadd", "USER node", "id -u node", "node:node"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must not rely on a named node account via %q", path, forbidden)
			}
		}
		homeAt := strings.Index(body, "ENV HOME=/home/node")
		userAt := strings.Index(body, "USER 1000:1000")
		installAt := strings.Index(body, "claude plugin install --scope user anban@anbanai")
		if homeAt < 0 || userAt < homeAt || installAt < userAt {
			t.Fatalf("%s HOME/numeric USER/install order is invalid: HOME=%d USER=%d install=%d", path, homeAt, userAt, installAt)
		}
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
		"dockerAgentContainerConfig(image, cmd, env, runtimeUser)",
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
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		path := filepath.Join(root, "deploy", "docker", name)
		body := readTextFile(t, path)
		for _, want := range []string{
			"ENV ANBAN_HOME_TEMPLATE=/opt/anban-home-template",
			`for file in known_marketplaces.json installed_plugins.json; do`,
			`cp -a "$HOME/.claude/plugins/$file" "$ANBAN_HOME_TEMPLATE/.claude/plugins/$file"`,
			`test -d "$HOME/.claude/plugins/cache/anbanai"`,
			`cp -a "$HOME/.claude/plugins/cache/anbanai" "$ANBAN_HOME_TEMPLATE/.claude/plugins/cache/anbanai"`,
			`find "$ANBAN_HOME_TEMPLATE" -type l -print -quit`,
			`chown -R root:root "$ANBAN_HOME_TEMPLATE"`,
			`chmod -R a=rX "$ANBAN_HOME_TEMPLATE"`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing immutable home-template contract %q", path, want)
			}
		}
		installAt := strings.Index(body, "claude plugin install --scope user anban@anbanai")
		snapshotAt := strings.Index(body, `for file in known_marketplaces.json installed_plugins.json; do`)
		if installAt < 0 || snapshotAt < installAt {
			t.Fatalf("%s home snapshot must occur after plugin installation: install=%d snapshot=%d", path, installAt, snapshotAt)
		}
		for _, forbidden := range []string{
			`for entry in .claude .agents .claude.json; do`,
			`cp -a "/home/node/.claude"`,
			`cp -a "/home/node/.agents"`,
			`cp -a "/home/node/.claude.json"`,
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must not broadly snapshot user home via %q", path, forbidden)
			}
		}
	}

	seednotePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote")
	seednote := readTextFile(t, seednotePath)
	for _, want := range []string{
		`printf '%s\n' '--js-runtimes node' > "$HOME/.config/yt-dlp/config"`,
		`install -m 0444 "$HOME/.config/yt-dlp/config" "$ANBAN_HOME_TEMPLATE/.config/yt-dlp/config"`,
	} {
		if !strings.Contains(seednote, want) {
			t.Fatalf("%s missing Seednote home-template contract %q", seednotePath, want)
		}
	}
}

func TestDockerRuntimeAgentImageIsImmutableOneShotJobRuntime(t *testing.T) {
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "deploy/docker/Dockerfile.agent-article"))
	for _, want := range []string{
		`find /anbanai -type l -print -quit`,
		`chown -R root:root /anbanai`,
		`chmod -R a=rX /anbanai`,
		`ENTRYPOINT ["tini", "--", "anban"]`,
		`CMD ["job"]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deploy/docker/Dockerfile.agent-article missing immutable one-shot contract %q", want)
		}
	}
	for _, forbidden := range []string{`CMD ["sleep", "infinity"]`, `USER root\nWORKDIR /workspace`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("deploy/docker/Dockerfile.agent-article retains reusable runtime contract %q", forbidden)
		}
	}
}

func TestDockerignoreExcludesLargeNonRuntimeTrees(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, ".dockerignore"))
	for _, want := range []string{
		"desktop/",
		"miniapp/",
		"**/.git",
		"**/node_modules/",
		"**/dist/",
		"**/.cache/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf(".dockerignore missing %q", want)
		}
	}
	if strings.Contains(body, "/plugins/") {
		t.Fatal(".dockerignore must keep the unified plugin available to Agent Docker builds")
	}
}

func TestAgentDockerfilesAreCentralized(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-article", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		if _, err := os.Stat(filepath.Join(root, "deploy", "docker", name)); err != nil {
			t.Fatalf("deploy/docker/%s must exist: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "agent", "Dockerfile")); err == nil {
		t.Fatal("agent/Dockerfile must not exist; use deploy/docker Agent Dockerfiles")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat agent/Dockerfile: %v", err)
	}
}

func TestServerDockerfileIsCentralized(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "deploy/docker/Dockerfile.server")); err != nil {
		t.Fatalf("deploy/docker/Dockerfile.server must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "server", "Dockerfile")); err == nil {
		t.Fatal("server/Dockerfile must not exist; use deploy/docker/Dockerfile.server")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat server/Dockerfile: %v", err)
	}
}

func TestStudioDockerfileIsCentralized(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := os.Stat(filepath.Join(root, "deploy/docker/Dockerfile.studio")); err != nil {
		t.Fatalf("deploy/docker/Dockerfile.studio must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "studio", "Dockerfile")); err == nil {
		t.Fatal("studio/Dockerfile must not exist; use deploy/docker/Dockerfile.studio")
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
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-article"),
			want: []string{"FROM golang:alpine AS builder", "FROM node:22-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote"),
			want: []string{"FROM golang:alpine AS builder", "FROM node:22-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-montage"),
			want: []string{"FROM golang:alpine AS builder", "FROM ghcr.io/openhands/agent-server:latest-python"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.server"),
			want: []string{"FROM golang:alpine AS builder", "FROM alpine:latest"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.studio"),
			want: []string{"FROM oven/bun:latest AS build", "FROM nginx:alpine"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.wcflink"),
			want: []string{"FROM golang:alpine AS builder", "FROM alpine:latest"},
		},
		{
			path: filepath.Join(root, "docker-compose.yml"),
			want: []string{
				"image: mysql:latest",
				"image: redis:alpine",
				"image: xpzouying/xiaohongshu-mcp:latest",
				"dockerfile: deploy/docker/Dockerfile.wcflink",
				"dockerfile: deploy/docker/Dockerfile.studio",
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

func TestDockerfileInventoryIsCentralized(t *testing.T) {
	root := repositoryRoot(t)
	got := trackedDockerfiles(t, root)
	want := []string{
		"deploy/docker/Dockerfile.agent-article",
		"deploy/docker/Dockerfile.agent-montage",
		"deploy/docker/Dockerfile.agent-seednote",
		"deploy/docker/Dockerfile.server",
		"deploy/docker/Dockerfile.studio",
		"deploy/docker/Dockerfile.wcflink",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("repository-owned Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestCollectOwnedDockerfilesDetectsUnexpectedEntrypoints(t *testing.T) {
	got := collectOwnedDockerfilePaths([]string{
		"deploy/docker/Dockerfile.server",
		"nested/Dockerfile.extra",
		"plugins/Dockerfile.plugin",
		"third_party/tool/Dockerfile",
		"web/node_modules/Dockerfile",
		"web/dist/Dockerfile.generated",
		".worktrees/other/Dockerfile",
	})
	want := []string{"deploy/docker/Dockerfile.server", "nested/Dockerfile.extra"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fixture Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestStudioDockerfileUsesRootBuildContext(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.studio"))
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
			t.Fatalf("deploy/docker/Dockerfile.studio missing root-context Studio contract %q", want)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "studio", ".dockerignore")); err == nil {
		t.Fatal("studio/.dockerignore must not exist; root-context builds use deploy/docker/Dockerfile.studio.dockerignore")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat studio/.dockerignore: %v", err)
	}

	rules := dockerignoreRules(readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.studio.dockerignore")))
	if len(rules) == 0 || rules[0] != "**" {
		t.Fatalf("deploy/docker/Dockerfile.studio.dockerignore must begin its non-comment rules with broad exclusion **; got %v", rules)
	}
	studioDir := dockerignoreRuleIndex(t, rules, "!studio/")
	studioTree := dockerignoreRuleIndex(t, rules, "!studio/**")
	if studioDir <= 0 || studioTree <= studioDir {
		t.Fatalf("deploy/docker/Dockerfile.studio.dockerignore must unignore studio/ then studio/** after **; got %v", rules)
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
			t.Fatalf("deploy/docker/Dockerfile.studio.dockerignore must exclude %q after unignoring Studio source; got %v", generated, rules)
		}
	}
}

func TestWcfLinkDockerfileRetainsPinnedBuildContract(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.wcflink"))
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
			t.Fatalf("deploy/docker/Dockerfile.wcflink missing pinned runtime contract %q", want)
		}
	}
}

func TestComposeAndMakefileUseCentralizedDockerfileBuilds(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"agent:\n    build:\n      context: .\n      dockerfile: deploy/docker/Dockerfile.agent-article",
		"wcflink:\n    build:\n      context: .\n      dockerfile: deploy/docker/Dockerfile.wcflink",
		"server:\n    build:\n      context: .\n      dockerfile: deploy/docker/Dockerfile.server",
		"studio:\n    build:\n      context: .\n      dockerfile: deploy/docker/Dockerfile.studio",
		"studio:\n    build:\n      context: .\n      dockerfile: deploy/docker/Dockerfile.studio\n      args:\n        VITE_API_BASE_URL: /api/v1",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing centralized Docker build contract %q", want)
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"WCFLINK_IMAGE := anban-creator-wcflink:latest",
		"STUDIO_IMAGE := anban-creator-studio:latest",
		"docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image docker-images",
		"docker-seednote-agent-image:",
		"docker-montage-agent-image:",
		"docker-wcflink-image:",
		"docker-studio-image:",
		"docker-images: docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image",
		"docker build -f deploy/docker/Dockerfile.agent-article -t $(AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-seednote -t $(SEEDNOTE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-montage",
		"docker build -f deploy/docker/Dockerfile.wcflink -t $(WCFLINK_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.studio -t $(STUDIO_IMAGE) .",
		"docker-image: docker-agent-image",
		"make docker-agent-image",
		"make docker-server-image",
		"make docker-wcflink-image",
		"make docker-studio-image",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing centralized Docker build contract %q", want)
		}
	}
}

func TestComposeUsesPersistentDockerExecutorRuntime(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"pull_policy: build\n    container_name: " + dockerPersistentAgentContainerName,
		"entrypoint: [\"tini\", \"--\"]\n    command: [\"sleep\", \"infinity\"]",
		"- ./data/workspace:" + dockerContainerWorkspaceRoot,
		"agent:\n        condition: service_started",
		"ANBAN_AGENT_EXECUTOR: \"docker\"",
		"ANBAN_CLAUDE_AGENT_SERVER_URL: \"http://server:8080\"",
		"ANBAN_AGENT_IMAGE_ARTICLE: \"creator-agent-article:latest\"",
		"ANBAN_AGENT_IMAGE_SEEDNOTE: \"creator-agent-seednote:latest\"",
		"ANBAN_AGENT_IMAGE_MONTAGE: \"creator-agent-montage:latest\"",
		"- ./data/workspace:" + DockerServerWorkspaceRoot,
		"- /var/run/docker.sock:/var/run/docker.sock",
		"group_add:\n      - \"${DOCKER_GID:-0}\"",
		"anban-creator-network:\n    name: anban-creator-network\n    driver: bridge",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing persistent Docker executor contract %q", want)
		}
	}
	for _, legacy := range []string{
		"ANBAN_CLAUDE_EXECUTOR",
		"ANBAN_CLAUDE_DOCKER_ARTICLE_IMAGE",
		"ANBAN_CLAUDE_DOCKER_SEEDNOTE_IMAGE",
		"ANBAN_CLAUDE_DOCKER_MONTAGE_IMAGE",
		"ANBAN_CLAUDE_DOCKER_CONTAINER_NAME",
		"ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR",
	} {
		if strings.Contains(compose, legacy) {
			t.Fatalf("docker-compose.yml retains legacy managed runtime env %q", legacy)
		}
	}

	for _, configPath := range []string{"server/config.yaml", "server/config.example.yaml"} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(configPath)))
		for _, want := range []string{
			"executor: \"${ANBAN_AGENT_EXECUTOR}\"",
			"article: \"${ANBAN_AGENT_IMAGE_ARTICLE}\"",
			"seednote: \"${ANBAN_AGENT_IMAGE_SEEDNOTE}\"",
			"montage: \"${ANBAN_AGENT_IMAGE_MONTAGE}\"",
			"network: \"${ANBAN_AGENT_DOCKER_NETWORK:-anban-creator-network}\"",
			"pids_limit: 512",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing Docker executor config contract %q", configPath, want)
			}
		}
		for _, legacy := range []string{"article_image:", "image_profiles:", "ANBAN_CLAUDE_DOCKER_CONTAINER_NAME", "ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR"} {
			if strings.Contains(body, legacy) {
				t.Fatalf("%s retains legacy Docker executor config %q", configPath, legacy)
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
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("list repository Dockerfiles: %v", err)
	}
	var existing []string
	for _, path := range strings.Split(strings.TrimRight(string(output), "\x00"), "\x00") {
		if path == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			existing = append(existing, path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat repository path %s: %v", path, err)
		}
	}
	return collectOwnedDockerfilePaths(existing)
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
	t.Fatalf("deploy/docker/Dockerfile.studio.dockerignore missing %q; got %v", want, rules)
	return -1
}

func ownedDockerContractPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		switch segment {
		case ".git", ".worktrees", "plugins", "third_party", "vendor", "node_modules", "dist", "build", "coverage", ".cache", ".vite", ".next", "bin", "data", "release":
			return false
		}
	}
	return true
}
