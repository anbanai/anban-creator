package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"gopkg.in/yaml.v3"
)

func TestDockerfileFromInstructionsIgnoreSubstringDecoys(t *testing.T) {
	body := `FROM node:24-bookworm-slim AS builder
RUN printf '%s\n' 'FROM node:24-bookworm-slim'
FROM debian:bookworm-slim
`
	want := []string{
		"FROM node:24-bookworm-slim AS builder",
		"FROM debian:bookworm-slim",
	}

	if got := dockerfileFromInstructions(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("Dockerfile FROM instructions mismatch\nwant: %v\n got: %v", want, got)
	}
}

func dockerfileFromInstructions(body string) []string {
	var instructions []string
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		instructions = append(instructions, strings.Join(fields, " "))
	}
	return instructions
}

func TestCreatorAgentImageNamingContract(t *testing.T) {
	root := repositoryRoot(t)

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	if err := validateCreatorAgentImageNaming(makefile, compose); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCreatorAgentImageNamingRejectsDecoysAndLegacyValues(t *testing.T) {
	validMakefile := "WECHAT_ARTICLE_AGENT_IMAGE ?= creator-agent-wechat-article:latest\nWECHAT_PICTURE_AGENT_IMAGE ?= creator-agent-wechat-picture:latest\n"
	validCompose := `services:
	  server:
	    environment:
	      ANBAN_AGENT_IMAGE_WECHAT_ARTICLE: "creator-agent-wechat-article:latest"
	      ANBAN_AGENT_IMAGE_WECHAT_PICTURE: "creator-agent-wechat-picture:latest"
`

	for _, tc := range []struct {
		name     string
		makefile string
		compose  string
	}{
		{
			name:     "Make variable name decoy",
			makefile: "LEGACY_AGENT_IMAGE := creator-agent-wechat-article:latest\n",
			compose:  validCompose,
		},
		{
			name:     "commented Make assignment",
			makefile: "# AGENT_IMAGE := creator-agent-wechat-article:latest\n",
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
			name:     "Compose image outside Server environment",
			makefile: validMakefile,
			compose: `services:
	  server:
	    image: creator-agent-wechat-article:latest
`,
		},
		{
			name:     "persistent Agent service remains",
			makefile: validMakefile,
			compose: validCompose + `  agent:
	    image: creator-agent-wechat-article:latest
`,
		},
		{
			name:     "legacy Compose identity remains",
			makefile: validMakefile,
			compose: validCompose + `  old-agent:
	    image: anban-agent:latest
`,
		},
		{
			name:     "duplicate Compose environment overrides expected value",
			makefile: validMakefile,
			compose:  validCompose + "      ANBAN_AGENT_IMAGE_WECHAT_ARTICLE: \"wrong-agent:latest\"\n",
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
	articleValues := makeVariableAssignments(makefile, "WECHAT_ARTICLE_AGENT_IMAGE")
	pictureValues := makeVariableAssignments(makefile, "WECHAT_PICTURE_AGENT_IMAGE")
	if len(articleValues) != 1 || articleValues[0] != "creator-agent-wechat-article:latest" || len(pictureValues) != 1 || pictureValues[0] != "creator-agent-wechat-picture:latest" {
		return fmt.Errorf("Makefile must define independent WeChat article and picture image variables")
	}

	if containsRetiredAgentIdentity(compose) {
		return fmt.Errorf("docker-compose.yml must not retain anban-creator-agent or anban-agent identities")
	}
	if strings.Contains("\n"+compose, "\n  agent:\n") {
		return fmt.Errorf("docker-compose.yml must not define a persistent agent service")
	}
	articleValues = composeEnvironmentAssignments(compose, "server", "ANBAN_AGENT_IMAGE_WECHAT_ARTICLE")
	pictureValues = composeEnvironmentAssignments(compose, "server", "ANBAN_AGENT_IMAGE_WECHAT_PICTURE")
	if len(articleValues) != 1 || articleValues[0] != `"creator-agent-wechat-article:latest"` || len(pictureValues) != 1 || pictureValues[0] != `"creator-agent-wechat-picture:latest"` {
		return fmt.Errorf("docker-compose.yml must configure independent WeChat runtime images exactly once")
	}
	return nil
}

func composeEnvironmentAssignments(text, service, variable string) []string {
	lines := strings.Split(text, "\n")
	inService := false
	inEnvironment := false
	var values []string
	for _, line := range lines {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") {
			inService = line == "  "+service+":"
			inEnvironment = false
			continue
		}
		if !inService {
			continue
		}
		if line == "    environment:" {
			inEnvironment = true
			continue
		}
		if inEnvironment && strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") {
			inEnvironment = false
		}
		if !inEnvironment {
			continue
		}
		name, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if found && name == variable {
			values = append(values, strings.TrimSpace(value))
		}
	}
	return values
}

func TestDockerRuntimeContractRemovesPersistentHostExecutors(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	productionGo := readProductionGoFiles(t, filepath.Join(root, "server"))
	for _, required := range []struct {
		path   string
		marker string
	}{
		{path: "server/main.go", marker: "func main() {"},
		{path: "server/config/config.go", marker: "type Config struct {"},
	} {
		if !strings.Contains(productionGo, required.marker) {
			t.Errorf("production runtime scan excludes %s", required.path)
		}
	}
	testOnlyMarker := "func TestConfigRejectsLegacyManagedExecutorFields("
	configTest := readTextFile(t, filepath.Join(root, "server", "config", "runtime_images_test.go"))
	if !strings.Contains(configTest, testOnlyMarker) {
		t.Fatal("runtime scan test marker is missing from server/config/runtime_images_test.go")
	}
	if strings.Contains(productionGo, testOnlyMarker) {
		t.Error("production runtime scan includes _test.go files")
	}
	for _, forbidden := range []string{
		"NewDockerExecutor(",
		"ContainerExecCreate(",
		"ANBAN_CLAUDE_DOCKER_CONTAINER_NAME",
		"ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR",
		"agent.TaskExecutor",
		"uploadMissingTaskFiles(",
		"ExtractTitleFromWorkspace(",
	} {
		if strings.Contains(productionGo, forbidden) {
			t.Errorf("removed host execution contract remains: %s", forbidden)
		}
	}
	if strings.Contains(compose, `"sleep", "infinity"`) {
		t.Error("docker-compose.yml retains a persistent agent process")
	}
	for _, obsolete := range []string{
		"server/agent/docker_executor.go",
		"server/agent/executor_interface.go",
	} {
		if _, err := os.Stat(filepath.Join(root, obsolete)); !os.IsNotExist(err) {
			t.Errorf("obsolete host executor %s still exists (stat error %v)", obsolete, err)
		}
	}
	if strings.Contains("\n"+compose, "\n  agent:\n") {
		t.Error("docker-compose.yml still defines a persistent agent service")
	}
	for _, forbidden := range []string{"depends_on.agent", "./data/workspace:/workspace", "./data/workspace:/app/data/workspace"} {
		if strings.Contains(compose, forbidden) {
			t.Errorf("docker-compose.yml retains shared host execution contract %q", forbidden)
		}
	}
}

func readProductionGoFiles(t *testing.T, root string) string {
	t.Helper()
	var contents strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			switch entry.Name() {
			case ".git", "generated", "gen", "node_modules", "vendor":
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source := readTextFile(t, path)
		if strings.Contains(source, "// Code generated ") && strings.Contains(source, " DO NOT EDIT.") {
			return nil
		}
		contents.WriteString(source)
		contents.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("scan production Go sources under %s: %v", root, err)
	}
	return contents.String()
}

func TestManagedRuntimeWiring(t *testing.T) {
	root := repositoryRoot(t)
	serverMain := readTextFile(t, filepath.Join(root, "server", "main.go"))
	for _, required := range []string{
		"NewDockerDispatcher",
		"NewKubernetesDispatcherWithClient",
		"NewRuntimeReconciler",
		"SetRuntimeDispatcher",
		"SetTaskWorkspaceLifecycle",
		"SetProjectMemoryLifecycle",
		"NewExecutionTokenService",
		"NewWorkloadTokenService",
		"NewAgentBootstrapService",
	} {
		if !strings.Contains(serverMain, required) {
			t.Errorf("server/main.go missing managed runtime wiring %s", required)
		}
	}
	taskExecution := readTextFile(t, filepath.Join(root, "server", "service", "task_execution.go"))
	if strings.Contains(taskExecution, "s.runtimeDispatcher != nil") {
		t.Error("HandleExecutionFromPayload must not retain a synchronous executor branch")
	}
	if !strings.Contains(taskExecution, "s.dispatchRuntime(ctx, task)") {
		t.Error("HandleExecutionFromPayload must always dispatch a durable managed execution")
	}
	if !strings.Contains(taskExecution, "s.dispatchPendingTask(ctx, task)") || !strings.Contains(taskExecution, "s.finalizePendingDispatchFailure(task, err)") {
		t.Error("HandleExecutionFromPayload must terminalize pre-dispatch failures instead of leaving retryable pending tasks")
	}
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
	articlePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-wechat-article")
	seednotePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote")
	article := readTextFile(t, articlePath)
	seednote := readTextFile(t, seednotePath)
	for _, want := range []string{
		"FROM node:24.22.0-bookworm-slim",
		"apt-get install -y --no-install-recommends ca-certificates curl git jq tini",
		"COPY runtime-claude/package.json runtime-claude/package-lock.json ./",
		"npm prune --omit=dev",
		"@anthropic-ai/claude-agent-sdk",
		"COPY harness/",
		"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
		"COPY deploy/docker/anban-agent-launcher /usr/local/bin/anban",
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
	for _, forbidden := range []string{"Agent-Reach", "AGENT_REACH", "python3", "python3-venv", "pip", "venv", "mcporter", "third_party/Agent-Reach", "OpenCLI", "ffmpeg", "fonts-noto-cjk", "OpenMontage"} {
		if strings.Contains(article, forbidden) || strings.Contains(seednote, forbidden) {
			t.Fatalf("Article and Seednote Dockerfiles must not contain dependency %q", forbidden)
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
	for _, name := range []string{"deploy/docker/Dockerfile.agent-wechat-article", "deploy/docker/Dockerfile.agent-wechat-picture", "deploy/docker/Dockerfile.agent-moments", "deploy/docker/Dockerfile.agent-ecommerce", "deploy/docker/Dockerfile.agent-seednote", "deploy/docker/Dockerfile.agent-montage"} {
		path := filepath.Join(root, name)
		body := readTextFile(t, path)
		for _, want := range []string{
			"COPY --from=builder /app/runtime-claude/node_modules",
			"COPY --from=builder /app/runtime-claude/dist",
			"COPY harness/",
			"ENTRYPOINT",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}

func TestServerDockerfileDoesNotReferenceRemovedGoSDK(t *testing.T) {
	root := repositoryRoot(t)
	const sdkPath = "third_party/claude-agent-sdk-go"

	for _, name := range []string{"deploy/docker/Dockerfile.server"} {
		t.Run(name, func(t *testing.T) {
			body := readTextFile(t, filepath.Join(root, name))
			if strings.Contains(body, sdkPath) {
				t.Fatalf("%s references removed Claude Agent SDK path %q", name, sdkPath)
			}
		})
	}
}

func TestMontageRuntimeImageContract(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "deploy/docker/Dockerfile.agent-montage")
	body := readTextFile(t, path)
	wantFrom := []string{
		"FROM node:24.22.0-bookworm-slim AS builder",
		"FROM node:24.22.0-bookworm-slim",
	}
	if got := dockerfileFromInstructions(body); !reflect.DeepEqual(got, wantFrom) {
		t.Fatalf("%s FROM instructions mismatch\nwant: %v\n got: %v", path, wantFrom, got)
	}
	for _, want := range []string{
		"ARG OPENMONTAGE_REPO=https://github.com/calesthio/OpenMontage.git",
		"ARG OPENMONTAGE_REF=c8066452325cb57c8fc1ba8c2a71b6e8842e6b65",
		"COPY deploy/docker/openmontage-remotion-deps.patch /tmp/openmontage-remotion-deps.patch",
		"git apply /tmp/openmontage-remotion-deps.patch",
		"npm audit --omit=dev --audit-level=moderate",
		"apt-get install -y --no-install-recommends",
		"ca-certificates curl git jq ffmpeg fontconfig fonts-noto-cjk fonts-noto-color-emoji",
		"libnss3 libdbus-1-3 libatk1.0-0 libgbm-dev libasound2",
		"libxrandr2 libxkbcommon-dev libxfixes3 libxcomposite1 libxdamage1",
		"libatk-bridge2.0-0 libpango-1.0-0 libcairo2 libcups2",
		"python3 python3-venv tini",
		"git fetch --depth 1 origin \"$OPENMONTAGE_REF\"",
		"test \"$(git rev-parse HEAD)\" = \"$OPENMONTAGE_REF\"",
		"requirements.txt",
		"remotion-composer/package-lock.json",
		"npm ci",
		"./node_modules/.bin/remotion browser ensure",
		"registry.discover()",
		"load_pipeline",
		"ENV ANBAN_MONTAGE_TEMPLATE_PATH=/opt/montage-template",
		"cp -a /opt/montage-template /workspace/openmontage",
		"chown -R 1000:1000 /workspace/openmontage",
		"chmod -R u+rwX /workspace/openmontage",
		"install -d -m 0750 -o 1000 -g 1000 /workspace/openmontage/.claude",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q", path, want)
		}
	}
	for _, forbidden := range []string{
		"COPY third_party/OpenMontage/",
		"git submodule",
		"OPENMONTAGE_RUNTIME_IMAGE",
		"creator-openmontage-runtime",
		"ghcr.io/openhands",
		"OpenHands",
		"deb.nodesource.com",
		"normalize-debian-apt-sources.sh",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("%s retains obsolete runtime dependency %q", path, forbidden)
		}
	}
}

func TestServerDockerfileUsesMinimalRuntime(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "deploy/docker/Dockerfile.server")
	body := readTextFile(t, path)
	if strings.Contains(body, "test -x node_modules/node-pty/build/Release/spawn-helper") {
		t.Fatal("server Dockerfile must not require node-pty's macOS-only spawn-helper in the Alpine/Linux image")
	}
	for _, want := range []string{
		"FROM node:24.22.0-alpine3.24",
		"apk add --no-cache ca-certificates docker-cli ffmpeg tzdata",
		"WORKDIR /build/server",
		"COPY server/go.mod server/go.sum ./",
		`RUN test "$(node --version)" = "v24.22.0"`,
		"go build -ldflags=\"-s -w\" -o /anban-creator-server .",
		"COPY --from=builder /anban-creator-server /app/anban-creator-server",
		"COPY --from=builder /build/server/billing/economics.yaml /app/conf/billing/economics.yaml",
		"COPY --from=builder /build/server/billing/products.yaml /app/conf/billing/products.yaml",
		"COPY --from=builder /build/server/billing/costs.yaml /app/conf/billing/costs.yaml",
		"COPY --from=builder /build/server/billing/promotions.yaml /app/conf/billing/promotions.yaml",
		"FROM node:24.22.0-alpine3.24",
		"USER 1000:1000",
		"/app/data/project-memory/projects",
		"chown -R 1000:1000 /app/data",
		`CMD ["/app/anban-creator-server", "-config", "/app/conf/config.yaml"]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing minimal server runtime contract %q", path, want)
		}
	}
	for _, forbidden := range []string{
		"addgroup -S -g 1000 anban",
		"adduser -S -D -u 1000 -G anban -h /home/anban anban",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("server Dockerfile must reuse the Node image's pre-created UID/GID 1000 account; found %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"ghcr.io/openhands/agent-server",
		"./agent",
		"/usr/local/bin/anban",
		"COPY harness/",
		"COPY third_party/OpenMontage/",
		"COPY third_party/Agent-Reach/",
		"/build/server/billing/policy.yaml",
		"AGENT_REACH_VENV",
		"CLAUDE_PLUGIN_ROOT",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"npm install",
		"mcporter",
		"npx -y skills",
		"claude plugin",
		"runtime-claude/",
		"runtime-dsh/",
		"@anthropic-ai/claude-agent-sdk",
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
	path := filepath.Join(repositoryRoot(t), "deploy/docker/Dockerfile.agent-wechat-article")
	body := readTextFile(t, path)
	from := strings.Index(body, "FROM node:24.22.0-bookworm-slim")
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

func TestSeednoteRuntimeUsesContentPATHWithoutExternalRouter(t *testing.T) {
	root := repositoryRoot(t)
	if got := containerRuntimePath(model.PlatformSeednote); got != ContainerContentRuntimePath {
		t.Fatalf("seednote runtime PATH = %q, want %q", got, ContainerContentRuntimePath)
	}
	for _, name := range []string{"Dockerfile.agent-seednote"} {
		body := readTextFile(t, filepath.Join(root, "deploy", "docker", name))
		for _, forbidden := range []string{"Agent-Reach", "AGENT_REACH", "python3", "pip", "venv", "mcporter"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains removed dependency %q", name, forbidden)
			}
		}
	}
}

func TestAgentReachSubmoduleIsRemoved(t *testing.T) {
	gitmodules := readTextFile(t, filepath.Join(repositoryRoot(t), ".gitmodules"))
	if strings.Contains(gitmodules, "Agent-Reach") {
		t.Fatal(".gitmodules still declares Agent Reach")
	}
}

func TestDockerRuntimeAgentImageAndKubernetesJobAgreeOnNumericIdentity(t *testing.T) {
	if kubernetesAgentUID != 1000 || kubernetesAgentGID != 1000 {
		t.Fatalf("Job identity = %d:%d, want numeric 1000:1000", kubernetesAgentUID, kubernetesAgentGID)
	}
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-wechat-article", "Dockerfile.agent-wechat-picture", "Dockerfile.agent-moments", "Dockerfile.agent-ecommerce", "Dockerfile.agent-seednote", "Dockerfile.agent-montage", "Dockerfile.agent-hypit"} {
		path := filepath.Join(root, "deploy", "docker", name)
		body := readTextFile(t, path)
		for _, want := range []string{
			`getent passwd 1000 >/dev/null`,
			`getent group 1000 >/dev/null`,
			`install -d -m 0755 -o 1000 -g 1000 /home/node`,
			`install -d -m 0770 -o 1000 -g 1000 /workspace`,
			`install -d -m 0750 -o 1000 -g 1000 /workspace/.claude`,
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
		userAt := strings.LastIndex(body, "USER 1000:1000")
		if homeAt < 0 || userAt < homeAt {
			t.Fatalf("%s HOME/numeric USER order is invalid: HOME=%d USER=%d", path, homeAt, userAt)
		}
	}
}

func TestDockerRuntimeAgentSourceUsesFixedOneShotIdentity(t *testing.T) {
	root := repositoryRoot(t)
	dockerRuntime := readTextFile(t, filepath.Join(root, "server", "agent", "docker_runtime.go"))
	if !strings.Contains(dockerRuntime, "containerConfig.User = ContainerRuntimeUser") {
		t.Fatal("Docker one-shot runtime must use the fixed image identity")
	}
	if _, err := os.Stat(filepath.Join(root, "server", "agent", "docker_executor.go")); !os.IsNotExist(err) {
		t.Fatalf("legacy Docker executor must be deleted, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "server", "agent", "kubernetes_executor.go")); !os.IsNotExist(err) {
		t.Fatalf("legacy Kubernetes executor must be deleted, stat error = %v", err)
	}
}

func TestDockerRuntimeAgentLoadsImmutablePluginDirectly(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-wechat-article", "Dockerfile.agent-wechat-picture", "Dockerfile.agent-moments", "Dockerfile.agent-ecommerce", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		path := filepath.Join(root, "deploy", "docker", name)
		body := readTextFile(t, path)
		for _, want := range []string{
			"COPY harness/ /anbanai/",
			"ENV CLAUDE_PLUGIN_ROOT=/anbanai",
			`find /anbanai -type l -print -quit`,
			`chown -R root:root /anbanai /app/runtime-claude`,
			`chmod -R a=rX /anbanai /app/runtime-claude`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing immutable home-template contract %q", path, want)
			}
		}
		for _, forbidden := range []string{
			"ANBAN_HOME_TEMPLATE",
			"claude plugin install",
			"@anthropic-ai/claude-code",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s must not broadly snapshot user home via %q", path, forbidden)
			}
		}
	}

}

func TestProjectCreatorPatchAnchorsLocalPluginsInTopLevelGroup(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "harness", "dsh", "project-creator.patch.yml"))
	var patch []map[string]any
	if err := yaml.Unmarshal([]byte(body), &patch); err != nil {
		t.Fatalf("parse project-creator.patch.yml: %v", err)
	}

	var topLevelGroup map[string]any
	for _, entry := range patch {
		rows, ok := entry["insert"].([]any)
		if !ok {
			continue
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if ok && row["id"] == "project-creator-plugins" {
				topLevelGroup = row
				break
			}
		}
		if topLevelGroup != nil {
			break
		}
	}
	if topLevelGroup == nil {
		t.Fatal("project-creator.patch.yml must declare a top-level project-creator-plugins group")
	}
	if group, ok := topLevelGroup["group"].(bool); !ok || !group {
		t.Fatal("project-creator-plugins must be a nested loader group")
	}
	config, ok := topLevelGroup["config"].([]any)
	if !ok {
		t.Fatalf("project-creator-plugins config = %T, want list", topLevelGroup["config"])
	}
	want := map[string]bool{
		"./plugins/project-creator-restrictions.js": false,
		"./plugins/project-creator-context.js":      false,
	}
	for _, raw := range config {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		if _, exists := want[name]; exists {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("top-level project-creator-plugins group missing %q", name)
		}
	}

	for _, entry := range patch {
		if entry["insert"] == nil {
			continue
		}
		rows, ok := entry["insert"].([]any)
		if !ok {
			continue
		}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok || row["id"] != "preset-project-creator" {
				continue
			}
			presetConfig, _ := row["config"].(map[string]any)
			plugins, _ := presetConfig["plugins"].([]any)
			for _, plugin := range plugins {
				pluginRow, _ := plugin.(map[string]any)
				name, _ := pluginRow["name"].(string)
				if _, exists := want[name]; exists {
					t.Errorf("local plugin %q must not be nested in preset-project-creator config", name)
				}
			}
		}
	}
}

func TestDockerRuntimeAgentImageIsImmutableOneShotJobRuntime(t *testing.T) {
	body := readTextFile(t, filepath.Join(repositoryRoot(t), "deploy/docker/Dockerfile.agent-wechat-article"))
	for _, want := range []string{
		`find /anbanai -type l -print -quit`,
		`chown -R root:root /anbanai`,
		`chmod -R a=rX /anbanai`,
		`ENTRYPOINT ["tini", "--", "anban"]`,
		`CMD ["job"]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("deploy/docker/Dockerfile.agent-wechat-article missing immutable one-shot contract %q", want)
		}
	}
	for _, forbidden := range []string{`CMD ["sleep", "infinity"]`, `USER root\nWORKDIR /workspace`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("deploy/docker/Dockerfile.agent-wechat-article retains reusable runtime contract %q", forbidden)
		}
	}
}

func TestDockerignoreExcludesLargeNonRuntimeTrees(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, ".dockerignore"))
	for _, want := range []string{
		"**/.git",
		"**/node_modules/",
		"**/dist/",
		"**/.cache/",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf(".dockerignore missing %q", want)
		}
	}
	if strings.Contains(body, "/harness/") {
		t.Fatal(".dockerignore must keep the unified plugin available to Agent Docker builds")
	}
}

func TestAgentDockerfilesAreCentralized(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"Dockerfile.agent-wechat-article", "Dockerfile.agent-wechat-picture", "Dockerfile.agent-moments", "Dockerfile.agent-ecommerce", "Dockerfile.agent-seednote", "Dockerfile.agent-montage"} {
		if _, err := os.Stat(filepath.Join(root, "deploy", "docker", name)); err != nil {
			t.Fatalf("deploy/docker/%s must exist: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "agent")); err == nil {
		t.Fatal("legacy agent directory must not exist; use runtime-claude and deploy/docker Agent Dockerfiles")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat legacy agent directory: %v", err)
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

func TestDockerBuildInputsUseDeclaredImageTags(t *testing.T) {
	root := repositoryRoot(t)
	for _, tc := range []struct {
		path     string
		from     []string
		contains []string
	}{
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-wechat-article"),
			from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-wechat-picture"),
			from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-moments"),
			from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-ecommerce"),
			from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote"),
			from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-montage"),
			from: []string{"FROM node:24.22.0-bookworm-slim AS builder", "FROM node:24.22.0-bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.server"),
			from: []string{"FROM golang:alpine AS builder", "FROM node:24.22.0-alpine3.24"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.runtime-dsh"),
			from: []string{"FROM node:24.22.0-alpine3.24 AS builder", "FROM node:24.22.0-alpine3.24"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.studio"),
			from: []string{"FROM ${STUDIO_PREVIOUS_IMAGE} AS previous", "FROM oven/bun:latest AS build", "FROM nginx:alpine"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.sidecar-ilink"),
			from: []string{"FROM golang:alpine AS builder", "FROM alpine:latest"},
		},
		{
			path: filepath.Join(root, "docker-compose.yml"),
			contains: []string{
				"image: mysql:latest",
				"image: redis:alpine",
				"image: anban-creator-sidecar-ilink:latest",
				"dockerfile: deploy/docker/Dockerfile.sidecar-ilink",
				"dockerfile: deploy/docker/Dockerfile.studio",
				"VITE_API_BASE_URL: /api/v1",
			},
		},
	} {
		body := readTextFile(t, tc.path)
		if tc.from != nil {
			if got := dockerfileFromInstructions(body); !reflect.DeepEqual(got, tc.from) {
				t.Errorf("%s FROM instructions mismatch\nwant: %v\n got: %v", tc.path, tc.from, got)
			}
		}
		for _, want := range tc.contains {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing rolling image/build contract %q", tc.path, want)
			}
		}
	}
}

func TestHypitRuntimePreparesProgramsWithExplicitProjectRoot(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "deploy", "docker", "Dockerfile.agent-hypit")
	body := readTextFile(t, path)
	for _, want := range []string{
		"ARG HYPIT_REF=dc2b7a8f31b1ef1443a79ca448548d69e3ceb6a0",
		"ARG PNPM_VERSION=10.33.0",
		"ARG UV_VERSION=0.13.1",
		"hypit programs prepare --project /opt/hypit --runtime /tmp/runtime-profile.json --endpoint html.local",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing current Hypit runtime contract %q", path, want)
		}
	}
	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	smoke := readTextFile(t, filepath.Join(root, "deploy", "docker", "hypit-smoke.sh"))
	for name, content := range map[string]string{"Makefile": makefile, "Hypit smoke script": smoke} {
		if !strings.Contains(content, "dc2b7a8f31b1ef1443a79ca448548d69e3ceb6a0") {
			t.Fatalf("%s does not use the Dockerfile's pinned Hypit revision", name)
		}
	}
}

func TestCatalogDigestIsCheckedAtRuntimeAndRecordedForRelease(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{
		"Dockerfile.agent-ecommerce",
		"Dockerfile.agent-feedback-analysis",
		"Dockerfile.agent-hypit",
		"Dockerfile.agent-moments",
		"Dockerfile.agent-montage",
		"Dockerfile.agent-profile-analysis",
		"Dockerfile.agent-seednote",
		"Dockerfile.agent-wechat-article",
		"Dockerfile.agent-wechat-picture",
		"Dockerfile.agent-whiteboard-animation",
	} {
		body := readTextFile(t, filepath.Join(root, "deploy/docker", name))
		for _, forbidden := range []string{
			"ARG ANBAN_AGENT_PACK_CATALOG_DIGEST",
			"LABEL org.anban.agent.pack.catalog-digest=",
			"ANBAN_AGENT_PACK_CATALOG_DIGEST",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("deploy/docker/%s retains build-time Catalog digest dependency %q", name, forbidden)
			}
		}
		if !strings.Contains(body, "COPY harness/ /anbanai/") {
			t.Fatalf("deploy/docker/%s must package the runtime catalog for startup verification", name)
		}
		if !strings.Contains(body, "X-Anban-Agent-Pack-Catalog-Digest") {
			t.Fatalf("deploy/docker/%s must fail the image build when bootstrap Catalog digest support is missing", name)
		}
	}

	server := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.server"))
	for _, forbidden := range []string{
		"ARG ANBAN_AGENT_PACK_CATALOG_DIGEST",
		"LABEL org.anban.agent.pack.catalog-digest=",
		"ANBAN_AGENT_PACK_CATALOG_DIGEST",
	} {
		if strings.Contains(server, forbidden) {
			t.Fatalf("deploy/docker/Dockerfile.server retains build-time Catalog digest dependency %q", forbidden)
		}
	}
	dsh := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.runtime-dsh"))
	for _, want := range []string{
		"harness/dsh/project-creator.patch.yml /app/harness/dsh/project-creator.patch.yml",
		"harness/dsh/plugins/project-creator-restrictions.js /app/harness/dsh/plugins/project-creator-restrictions.js",
		"harness/dsh/plugins/project-creator-context.js /app/harness/dsh/plugins/project-creator-context.js",
		"harness/dsh/plugins/project-creator-skills-provider.js /app/harness/dsh/plugins/project-creator-skills-provider.js",
		"--from=builder --chown=1000:1000 /build/runtime-dsh/harness-dsh/lib /app/harness/dsh/lib",
		"harness/dsh/presets/project-creator/skills/ /app/harness/dsh/presets/project-creator/skills/",
	} {
		if !strings.Contains(dsh, want) {
			t.Fatalf("deploy/docker/Dockerfile.runtime-dsh missing plugin asset %q", want)
		}
	}

	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, forbidden := range []string{
		"ANBAN_AGENT_PACK_CATALOG_DIGEST",
		"AGENT_PACK_CATALOG_DIGEST",
	} {
		if strings.Contains(compose, forbidden) {
			t.Fatalf("docker-compose.yml retains Catalog digest configuration dependency %q", forbidden)
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	if !strings.Contains(makefile, "docker-agent-build-check: agent-pack-check") {
		t.Fatal("Makefile must run agent-pack-check before managed image builds")
	}
	for _, forbidden := range []string{"AGENT_PACK_CATALOG_DIGEST", "--build-arg ANBAN_AGENT_PACK_CATALOG_DIGEST"} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("Makefile retains manual Catalog digest build dependency %q", forbidden)
		}
	}

	smoke := readTextFile(t, filepath.Join(root, "deploy/docker/runtime-smoke.sh"))
	if !strings.Contains(smoke, "make -C \"$REPO_ROOT\" agent-pack-check") {
		t.Fatal("runtime smoke must validate the generated Catalog before building images")
	}
	if strings.Contains(smoke, "ANBAN_AGENT_PACK_CATALOG_DIGEST") || strings.Contains(smoke, "AGENT_PACK_CATALOG_DIGEST") {
		t.Fatal("runtime smoke builds must not need a Catalog digest build argument")
	}

	release := readTextFile(t, filepath.Join(root, "scripts/agent-runtime-release.sh"))
	for _, want := range []string{"catalog_digest", "Agent Pack Catalog files differ", "DSH_IMAGE", `"dsh=${DSH_IMAGE:?DSH_IMAGE must be set}"`} {
		if !strings.Contains(release, want) {
			t.Fatalf("release manifest script missing Catalog audit contract %q", want)
		}
	}
	if strings.Contains(release, "org.anban.agent.pack.catalog-digest") || strings.Contains(release, "Catalog label mismatch") {
		t.Fatal("release manifest must not depend on image metadata labels for runtime verification")
	}

	deployment := readTextFile(t, filepath.Join(root, "server/Deployment.yaml"))
	if strings.Contains(deployment, "${catalog_digest}") || strings.Contains(deployment, "agent-pack-catalog-digest") {
		t.Fatal("cloud-rendered Server Deployment must not require an undeclared Catalog digest variable")
	}
}

func TestDshRuntimeMakesBundledDependenciesVisibleToHarnessPlugins(t *testing.T) {
	root := repositoryRoot(t)
	dsh := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.runtime-dsh"))
	if !strings.Contains(dsh, "RUN ln -s /app/runtime-dsh/node_modules /app/node_modules") {
		t.Fatal("DSH runtime must expose its bundled node_modules at /app so harness plugins can resolve bare imports")
	}
}

func TestDockerfileInventoryIsCentralized(t *testing.T) {
	root := repositoryRoot(t)
	got := trackedDockerfiles(t, root)
	want := []string{
		"deploy/docker/Dockerfile.agent-content-planning",
		"deploy/docker/Dockerfile.agent-ecommerce",
		"deploy/docker/Dockerfile.agent-feedback-analysis",
		"deploy/docker/Dockerfile.agent-hypit",
		"deploy/docker/Dockerfile.agent-local-life",
		"deploy/docker/Dockerfile.agent-moments",
		"deploy/docker/Dockerfile.agent-montage",
		"deploy/docker/Dockerfile.agent-profile-analysis",
		"deploy/docker/Dockerfile.agent-seednote",
		"deploy/docker/Dockerfile.agent-talking-head-editor",
		"deploy/docker/Dockerfile.agent-video-edit",
		"deploy/docker/Dockerfile.agent-viral-ip-video-editor",
		"deploy/docker/Dockerfile.agent-wechat-article",
		"deploy/docker/Dockerfile.agent-wechat-picture",
		"deploy/docker/Dockerfile.agent-whiteboard-animation",
		"deploy/docker/Dockerfile.runtime-dsh",
		"deploy/docker/Dockerfile.server",
		"deploy/docker/Dockerfile.sidecar-ilink",
		"deploy/docker/Dockerfile.studio",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("repository-owned Dockerfile inventory mismatch\nwant: %v\n got: %v", want, got)
	}
}

func TestVideoEditDockerfilePinsTheUnmodifiedVideoUseRuntime(t *testing.T) {
	root := repositoryRoot(t)
	dockerfile := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.agent-video-edit"))
	for _, want := range []string{
		"https://github.com/browser-use/video-use.git",
		"43cfc566833548093455c9d4a46bd69913b7e552",
		"/opt/video-use/.anban-source-revision",
		"python3 -m venv /opt/video-use-venv",
		"pip install --no-cache-dir -e /opt/video-use",
		"ffmpeg",
		"ffprobe",
		"for helper in transcribe.py transcribe_batch.py pack_transcripts.py timeline_view.py grade.py render.py",
		"/opt/video-use/helpers/$helper\" --help",
		"COPY harness/ /anbanai/",
		"ANBAN_VIDEO_USE_ROOT=/opt/video-use",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("video-edit Dockerfile missing pinned runtime contract %q", want)
		}
	}
	if strings.Contains(dockerfile, "openmontage") || strings.Contains(dockerfile, "git apply") {
		t.Fatal("video-edit Dockerfile must use the unmodified video-use source, without OpenMontage or source patches")
	}
}

func TestTalkingHeadEditorDockerfileValidatesErduoMotionContract(t *testing.T) {
	root := repositoryRoot(t)
	dockerfile := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.agent-talking-head-editor"))
	for _, want := range []string{
		"FROM node:24.22.0-trixie-slim",
		"ARG AUTO_EDITOR_VERSION=29.3.1",
		"ARG HYPERFRAMES_VERSION=0.8.214",
		"/opt/video-use-venv/bin/pip install --no-cache-dir \"auto-editor==${AUTO_EDITOR_VERSION}\"",
		"test \"$(uname -m)\" = x86_64",
		"auto-editor-linux-x86_64",
		"Path(auto_editor.__file__).parent / \"bin\" / \"auto-editor\"",
		"chmod 0755 \"$auto_editor_binary\"",
		"curl --fail --location --retry 5 --retry-all-errors",
		"importlib.metadata as m; print(m.version(\"auto-editor\"))",
		"/opt/video-use-venv/bin/auto-editor --version",
		"npm install --global --omit=dev \"hyperframes@${HYPERFRAMES_VERSION}\"",
		"test \"$(hyperframes --version)\" = \"$HYPERFRAMES_VERSION\"",
		"for helper in transcribe.py transcribe_batch.py pack_transcripts.py timeline_view.py grade.py render.py",
		"/opt/video-use/helpers/$helper\" --help",
		"43cfc566833548093455c9d4a46bd69913b7e552",
		"/anbanai/skills/erduo-broll-loop-engineering/SKILL.md",
		"HyperFrames lint",
		"aesthetic review",
		"/anbanai/skills/talking-head-sound-design/SKILL.md",
		"/anbanai/skills/talking-head-sound-design/assets/sfx/manifest.json",
		"sound_pass.py --help",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("talking-head-editor Dockerfile missing runtime contract %q", want)
		}
	}
	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"docker build --platform linux/amd64 -f deploy/docker/Dockerfile.agent-talking-head-editor",
		"--build-arg AUTO_EDITOR_VERSION=\"$(AUTO_EDITOR_VERSION)\"",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("talking-head-editor Makefile contract missing %q", want)
		}
	}
}

func TestViralIPVideoEditorRuntimeProfileIsPackagedAndRegistered(t *testing.T) {
	root := repositoryRoot(t)
	for path, expected := range map[string][]string{
		"deploy/docker/Dockerfile.agent-viral-ip-video-editor": {
			"ARG AUTO_EDITOR_VERSION=29.3.1",
			"auto-editor==${AUTO_EDITOR_VERSION}",
			"/bin/auto-editor\" --version",
			"ARG OPENMONTAGE_REF=c8066452325cb57c8fc1ba8c2a71b6e8842e6b65",
			"COPY harness/ /anbanai/",
			"ANBAN_MONTAGE_TEMPLATE_PATH=/opt/montage-template",
		},
		"Makefile": {
			"VIRAL_IP_VIDEO_EDITOR_AGENT_IMAGE ?= creator-agent-viral-ip-video-editor:latest",
			"docker-viral-ip-video-editor-agent-image:",
			"docker build --platform linux/amd64 -f deploy/docker/Dockerfile.agent-viral-ip-video-editor",
			"AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR",
		},
		"docker-compose.yml": {
			"ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR: \"${ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR:-creator-agent-viral-ip-video-editor:latest}\"",
		},
		"server/config.example.yaml": {
			"viral-ip-video-editor: \"${ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR:-creator-agent-viral-ip-video-editor:latest}\"",
		},
		"server/Deployment.yaml": {
			"name: ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR",
		},
		"scripts/agent-runtime-release.sh": {
			"viral-ip-video-editor=${AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR:?AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR must be set}",
		},
		"scripts/render-ack-server-deployment.sh": {
			"viral_ip_video_editor_agent_image_repo",
			".images[\"viral-ip-video-editor\"]",
		},
	} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(path)))
		for _, want := range expected {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing viral IP runtime contract %q", path, want)
			}
		}
	}
}

func TestCollectOwnedDockerfilesDetectsUnexpectedEntrypoints(t *testing.T) {
	got := collectOwnedDockerfilePaths([]string{
		"deploy/docker/Dockerfile.server",
		"nested/Dockerfile.extra",
		"harness/Dockerfile.plugin",
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

func TestIlinkDockerfileBuildsLatestUpstream(t *testing.T) {
	root := repositoryRoot(t)
	body := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.sidecar-ilink"))
	for _, want := range []string{
		"ARG ILINK_REPO=https://github.com/lich0821/wcfLink.git",
		"ARG ILINK_REF=master",
		"git fetch --depth 1 origin \"$ILINK_REF\"",
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
			t.Fatalf("deploy/docker/Dockerfile.sidecar-ilink missing latest runtime contract %q", want)
		}
	}
}

func TestComposeAndMakefileUseCentralizedDockerfileBuilds(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"sidecar-ilink:\n    build:\n      context: .\n      dockerfile: deploy/docker/Dockerfile.sidecar-ilink",
		"ANBAN_ILINK_BASE_URL: \"http://sidecar-ilink:18070\"",
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
		"OPENMONTAGE_SOURCE_REPO ?= https://github.com/calesthio/OpenMontage.git",
		"OPENMONTAGE_SOURCE_REF ?= c8066452325cb57c8fc1ba8c2a71b6e8842e6b65",
		"SIDECAR_ILINK_IMAGE ?= anban-creator-sidecar-ilink:latest",
		"STUDIO_IMAGE := anban-creator-studio:latest",
		"agent-test:",
		"agent-build:",
		"docker-seednote-agent-image:",
		"docker-montage-agent-image:",
		"docker-profile-analysis-agent-image:",
		"docker-feedback-analysis-agent-image:",
		"docker-sidecar-ilink-image:",
		"docker-studio-image:",
		"docker-moments-agent-image:",
		"docker-ecommerce-agent-image:",
		"docker-local-life-agent-image:",
		"docker-wechat-article-agent-image:",
		"docker-wechat-picture-agent-image:",
		"docker build -f deploy/docker/Dockerfile.agent-wechat-article -t $(WECHAT_ARTICLE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-wechat-picture -t $(WECHAT_PICTURE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-moments -t $(MOMENTS_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-ecommerce -t $(ECOMMERCE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-seednote -t $(SEEDNOTE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-montage",
		"--build-arg OPENMONTAGE_REPO=\"$(OPENMONTAGE_SOURCE_REPO)\"",
		"--build-arg OPENMONTAGE_REF=\"$(OPENMONTAGE_SOURCE_REF)\"",
		"docker build --pull --no-cache -f deploy/docker/Dockerfile.sidecar-ilink",
		"docker build -f deploy/docker/Dockerfile.studio -t $(STUDIO_IMAGE) .",
		"docker-image:",
		"docker-images: docker-wechat-article-agent-image docker-wechat-picture-agent-image docker-seednote-agent-image docker-montage-agent-image docker-viral-ip-video-editor-agent-image docker-video-edit-agent-image docker-talking-head-editor-agent-image docker-moments-agent-image docker-ecommerce-agent-image docker-profile-analysis-agent-image docker-feedback-analysis-agent-image docker-whiteboard-animation-agent-image docker-hypit-agent-image docker-local-life-agent-image docker-dsh-runtime-image docker-server-image",
		"docker-dsh-runtime-image:",
		"docker-wechat-article-agent-image",
		"make docker-server-image",
		"make docker-dsh-runtime-image",
		"make docker-sidecar-ilink-image",
		"make docker-studio-image",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing centralized Docker build contract %q", want)
		}
	}
	for _, forbidden := range []string{"OPENMONTAGE_RUNTIME_IMAGE", "docker-openmontage-runtime-image", "Dockerfile.runtime-openmontage"} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("Makefile retains obsolete Montage build dependency %q", forbidden)
		}
	}
}

func TestSidecarDockerfilesBuildLatestUpstream(t *testing.T) {
	root := repositoryRoot(t)
	ilinkDockerfile := readTextFile(t, filepath.Join(root, "deploy/docker/Dockerfile.sidecar-ilink"))
	for _, want := range []string{
		"ILINK_REPO=https://github.com/lich0821/wcfLink.git",
		"ILINK_REF=master",
		"git fetch --depth 1 origin \"$ILINK_REF\"",
	} {
		if !strings.Contains(ilinkDockerfile, want) {
			t.Fatalf("iLink Dockerfile missing latest-upstream build contract %q", want)
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"docker-sidecar-ilink-image:",
		"--build-arg ILINK_REPO=\"$(SIDECAR_ILINK_REPO)\"",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing iLink sidecar build contract %q", want)
		}
	}
}

func TestComposeUsesOneShotManagedDockerRuntime(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"ANBAN_AGENT_EXECUTOR: \"docker\"",
		"ANBAN_CLAUDE_AGENT_SERVER_URL: \"http://server:8080\"",
		"ANBAN_AGENT_EXECUTION_TOKEN_SECRET:",
		"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE: \"creator-agent-wechat-article:latest\"",
		"ANBAN_AGENT_IMAGE_WECHAT_PICTURE: \"creator-agent-wechat-picture:latest\"",
		"ANBAN_AGENT_IMAGE_SEEDNOTE: \"creator-agent-seednote:latest\"",
		"ANBAN_AGENT_IMAGE_MONTAGE: \"creator-agent-montage:latest\"",
		"ANBAN_AGENT_IMAGE_VIDEO_EDIT: \"${ANBAN_AGENT_IMAGE_VIDEO_EDIT:-creator-agent-video-edit:latest}\"",
		"ANBAN_AGENT_IMAGE_TALKING_HEAD_EDITOR: \"${ANBAN_AGENT_IMAGE_TALKING_HEAD_EDITOR:-creator-agent-talking-head-editor:latest}\"",
		"ANBAN_AGENT_IMAGE_MOMENTS: \"creator-agent-moments:latest\"",
		"ANBAN_AGENT_IMAGE_ECOMMERCE: \"creator-agent-ecommerce:latest\"",
		"ANBAN_AGENT_DOCKER_NETWORK: \"creator-runtime-network\"",
		"- /var/run/docker.sock:/var/run/docker.sock",
		"group_add:\n      - \"${DOCKER_GID:-0}\"",
		"anban-creator-network:\n    name: anban-creator-network\n    driver: bridge",
		"creator-runtime-network:\n    name: creator-runtime-network\n    driver: bridge",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing managed Docker runtime contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"\n  agent:\n",
		`["sleep", "infinity"]`,
		"./data/workspace:/workspace",
		"./data/workspace:/app/data/workspace",
		"ANBAN_CLAUDE_EXECUTOR",
		"ANBAN_CLAUDE_DOCKER_ARTICLE_IMAGE",
		"ANBAN_CLAUDE_DOCKER_SEEDNOTE_IMAGE",
		"ANBAN_CLAUDE_DOCKER_MONTAGE_IMAGE",
		"ANBAN_CLAUDE_DOCKER_CONTAINER_NAME",
		"ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR",
	} {
		if strings.Contains("\n"+compose, forbidden) {
			t.Fatalf("docker-compose.yml retains obsolete managed runtime contract %q", forbidden)
		}
	}

	for _, configPath := range []string{"server/config.example.yaml"} {
		body := readTextFile(t, filepath.Join(root, filepath.FromSlash(configPath)))
		for _, want := range []string{
			"executor: \"${ANBAN_AGENT_EXECUTOR}\"",
			"wechat-article: \"${ANBAN_AGENT_IMAGE_WECHAT_ARTICLE:-creator-agent-wechat-article:latest}\"",
			"seednote: \"${ANBAN_AGENT_IMAGE_SEEDNOTE:-creator-agent-seednote:latest}\"",
			"montage: \"${ANBAN_AGENT_IMAGE_MONTAGE:-creator-agent-montage:latest}\"",
			"video-edit: \"${ANBAN_AGENT_IMAGE_VIDEO_EDIT:-creator-agent-video-edit:latest}\"",
			"network: \"${ANBAN_AGENT_DOCKER_NETWORK:-creator-runtime-network}\"",
			"pids_limit: 512",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing Docker runtime config contract %q", configPath, want)
			}
		}
		for _, legacy := range []string{"article_image:", "image_profiles:", "ANBAN_CLAUDE_DOCKER_CONTAINER_NAME", "ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR"} {
			if strings.Contains(body, legacy) {
				t.Fatalf("%s retains legacy Docker runtime config %q", configPath, legacy)
			}
		}
	}

	makefile := readTextFile(t, filepath.Join(root, "Makefile"))
	for _, want := range []string{
		"docker-up: docker-server-image docker-dsh-runtime-image docker-wechat-article-agent-image docker-wechat-picture-agent-image docker-seednote-agent-image docker-montage-agent-image docker-viral-ip-video-editor-agent-image docker-video-edit-agent-image docker-talking-head-editor-agent-image docker-moments-agent-image docker-ecommerce-agent-image",
		"DOCKER_SOCKET_GID := $(shell stat -L -c '%g' /var/run/docker.sock 2>/dev/null || stat -L -f '%g' /var/run/docker.sock 2>/dev/null || echo 0)",
		"DOCKER_GID=\"$(DOCKER_SOCKET_GID)\" docker compose up -d",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing Docker socket group contract %q", want)
		}
	}
}

func TestDockerRuntimeContract(t *testing.T) {
	root := repositoryRoot(t)

	t.Run("Compose configures the scheduler without a persistent Agent", func(t *testing.T) {
		body := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
		var compose struct {
			Services map[string]struct {
				Environment map[string]string `yaml:"environment"`
				EnvFile     []string          `yaml:"env_file"`
				Volumes     []string          `yaml:"volumes"`
				GroupAdd    []string          `yaml:"group_add"`
				Networks    []string          `yaml:"networks"`
				Ports       []string          `yaml:"ports"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal([]byte(body), &compose); err != nil {
			t.Fatalf("parse docker-compose.yml: %v", err)
		}
		server, ok := compose.Services["server"]
		if !ok {
			t.Fatal("docker-compose.yml has no server service")
		}
		for name, want := range map[string]string{
			"ANBAN_AGENT_EXECUTOR":                   "docker",
			"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE":       "creator-agent-wechat-article:latest",
			"ANBAN_AGENT_IMAGE_SEEDNOTE":             "creator-agent-seednote:latest",
			"ANBAN_AGENT_IMAGE_MONTAGE":              "creator-agent-montage:latest",
			"ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR": "${ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR:-creator-agent-viral-ip-video-editor:latest}",
			"ANBAN_AGENT_IMAGE_VIDEO_EDIT":           "${ANBAN_AGENT_IMAGE_VIDEO_EDIT:-creator-agent-video-edit:latest}",
			"ANBAN_AGENT_IMAGE_TALKING_HEAD_EDITOR":  "${ANBAN_AGENT_IMAGE_TALKING_HEAD_EDITOR:-creator-agent-talking-head-editor:latest}",
			"VIDEO_EDIT_ELEVENLABS_API_KEY":          "${VIDEO_EDIT_ELEVENLABS_API_KEY:-}",
			"VIDEO_USE_API_KEY":                      "${VIDEO_USE_API_KEY:-}",
			"ANBAN_AGENT_IMAGE_MOMENTS":              "creator-agent-moments:latest",
			"ANBAN_AGENT_IMAGE_ECOMMERCE":            "creator-agent-ecommerce:latest",
			"ANBAN_AGENT_IMAGE_PROFILE_ANALYSIS":     "${ANBAN_AGENT_IMAGE_PROFILE_ANALYSIS:-creator-agent-profile-analysis:latest}",
			"ANBAN_AGENT_IMAGE_FEEDBACK_ANALYSIS":    "${ANBAN_AGENT_IMAGE_FEEDBACK_ANALYSIS:-creator-agent-feedback-analysis:latest}",
			"ANBAN_AGENT_IMAGE_WHITEBOARD_ANIMATION": "${ANBAN_AGENT_IMAGE_WHITEBOARD_ANIMATION:-creator-agent-whiteboard-animation:latest}",
			"ANBAN_AGENT_IMAGE_HYPIT":                "${ANBAN_AGENT_IMAGE_HYPIT:-creator-agent-hypit:latest}",
			"ANBAN_AGENT_DOCKER_NETWORK":             "creator-runtime-network",
			"ANBAN_AGENT_EXECUTION_TOKEN_SECRET":     "${ANBAN_AGENT_EXECUTION_TOKEN_SECRET:?ANBAN_AGENT_EXECUTION_TOKEN_SECRET is required}",
			"ANBAN_BILLING_ADMIN_API_KEY":            "${ANBAN_BILLING_ADMIN_API_KEY:?ANBAN_BILLING_ADMIN_API_KEY is required}",
			"ANBAN_JWT_SECRET_KEY":                   "${ANBAN_JWT_SECRET_KEY:?ANBAN_JWT_SECRET_KEY is required}",
			"ANBAN_OSS_ENDPOINT":                     "${ANBAN_OSS_ENDPOINT:?ANBAN_OSS_ENDPOINT is required}",
			"ANBAN_OSS_ACCESS_KEY_ID":                "${ANBAN_OSS_ACCESS_KEY_ID:?ANBAN_OSS_ACCESS_KEY_ID is required}",
			"ANBAN_OSS_ACCESS_KEY_SECRET":            "${ANBAN_OSS_ACCESS_KEY_SECRET:?ANBAN_OSS_ACCESS_KEY_SECRET is required}",
			"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL":      "${ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL:-}",
			"ANBAN_DEEPSEEK_API_KEY":                 "${ANBAN_DEEPSEEK_API_KEY:?ANBAN_DEEPSEEK_API_KEY is required}",
			"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL":      "${ANBAN_MOONSHOT_ANTHROPIC_BASE_URL:-https://api.moonshot.cn/anthropic}",
			"ANBAN_MOONSHOT_API_KEY":                 "${ANBAN_MOONSHOT_API_KEY:?ANBAN_MOONSHOT_API_KEY is required}",
			"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":         "${ANBAN_ZHIPU_ANTHROPIC_BASE_URL:-https://open.bigmodel.cn/api/anthropic}",
			"ANBAN_ZHIPU_API_KEY":                    "${ANBAN_ZHIPU_API_KEY:?ANBAN_ZHIPU_API_KEY is required}",
			"MOONSHOT_API_KEY":                       "${MOONSHOT_API_KEY:?MOONSHOT_API_KEY is required}",
		} {
			if got := server.Environment[name]; got != want {
				t.Errorf("server environment %s = %q, want %q", name, got, want)
			}
		}
		for _, obsolete := range []string{"ANBAN_KIMI_API_KEY", "ANBAN_DOUBAO_AGENT_BASE_URL", "ANBAN_DOUBAO_AGENT_API_KEY"} {
			if _, exists := server.Environment[obsolete]; exists {
				t.Fatalf("docker-compose.yml retains removed %s", obsolete)
			}
		}
		if _, exists := compose.Services["agent"]; exists {
			t.Fatal("docker-compose.yml must not define a persistent agent service")
		}
		if len(server.EnvFile) != 1 || server.EnvFile[0] != ".env" {
			t.Fatalf("server env_file = %v, want root .env", server.EnvFile)
		}
		if !containsString(server.Volumes, "/var/run/docker.sock:/var/run/docker.sock") {
			t.Fatalf("server volumes = %v, want Docker daemon socket", server.Volumes)
		}
		if !containsString(server.GroupAdd, "${DOCKER_GID:-0}") {
			t.Fatalf("server group_add = %v, want Docker socket group", server.GroupAdd)
		}
		if !reflect.DeepEqual(server.Networks, []string{"anban-creator-network", "creator-runtime-network"}) {
			t.Fatalf("server networks = %v, want sidecar and isolated Agent networks", server.Networks)
		}
		for _, volume := range server.Volumes {
			if strings.Contains(volume, ":/workspace") || strings.Contains(volume, ":/app/data/workspace") {
				t.Fatalf("server retains persistent Agent workspace mount %q", volume)
			}
		}

		dotenv := dotenvAssignments(readTextFile(t, filepath.Join(root, ".env.example")))
		for _, name := range []string{
			"ANBAN_BILLING_ADMIN_API_KEY",
			"ANBAN_AGENT_EXECUTION_TOKEN_SECRET",
			"ANBAN_AGENT_EXECUTOR",
			"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE",
			"ANBAN_AGENT_IMAGE_WECHAT_PICTURE",
			"ANBAN_AGENT_IMAGE_SEEDNOTE",
			"ANBAN_AGENT_IMAGE_MONTAGE",
			"ANBAN_AGENT_IMAGE_VIRAL_IP_VIDEO_EDITOR",
			"ANBAN_AGENT_IMAGE_VIDEO_EDIT",
			"ANBAN_AGENT_IMAGE_TALKING_HEAD_EDITOR",
			"VIDEO_EDIT_ELEVENLABS_API_KEY",
			"ANBAN_AGENT_IMAGE_MOMENTS",
			"ANBAN_AGENT_IMAGE_ECOMMERCE",
			"ANBAN_JWT_SECRET_KEY",
			"ANBAN_OSS_ENDPOINT",
			"ANBAN_OSS_ACCESS_KEY_ID",
			"ANBAN_OSS_ACCESS_KEY_SECRET",
			"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL",
			"ANBAN_DEEPSEEK_API_KEY",
			"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL",
			"ANBAN_MOONSHOT_API_KEY",
			"ANBAN_ZHIPU_ANTHROPIC_BASE_URL",
			"ANBAN_ZHIPU_API_KEY",
			"MOONSHOT_API_KEY",
			"VOLCENGINE_ARK_API_KEY",
			"WANGCAI_OPENAI_BASE_URL",
			"WANGCAI_OPENAI_API_KEY",
		} {
			if _, ok := dotenv[name]; !ok {
				t.Errorf(".env.example missing required Server variable %s", name)
			}
		}

		makefile := readTextFile(t, filepath.Join(root, "Makefile"))
		for _, target := range []string{"server-run:", "server-dev:"} {
			start := strings.Index(makefile, target)
			if start < 0 {
				t.Fatalf("Makefile missing %s", target)
			}
			body := makefile[start:]
			if next := strings.Index(body[len(target):], "\n\n"); next >= 0 {
				body = body[:len(target)+next]
			}
			if !strings.Contains(body, ". ./.env") {
				t.Errorf("Makefile %s does not load the documented root .env", target)
			}
		}
	})

	t.Run("repository config loads with the documented Compose environment", func(t *testing.T) {
		configPath := filepath.Join(root, "server", "config.example.yaml")
		configBody := readTextFile(t, configPath)
		envReference := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)
		for _, match := range envReference.FindAllStringSubmatch(configBody, -1) {
			t.Setenv(match[1], "")
		}
		for name, value := range map[string]string{
			"ANBAN_DATABASE_DSN":                  "root:dev@tcp(mysql:3306)/anban_creator?charset=utf8mb4&parseTime=True&loc=Local",
			"ANBAN_REDIS_ADDR":                    "redis:6379",
			"ANBAN_BILLING_ADMIN_API_KEY":         "test-billing-admin-key",
			"ANBAN_AGENT_EXECUTOR":                "docker",
			"ANBAN_CLAUDE_AGENT_SERVER_URL":       "http://server:8080",
			"ANBAN_AGENT_EXECUTION_TOKEN_SECRET":  "0123456789abcdef0123456789abcdef",
			"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE":    "creator-agent-wechat-article:latest",
			"ANBAN_AGENT_IMAGE_SEEDNOTE":          "creator-agent-seednote:latest",
			"ANBAN_AGENT_IMAGE_MONTAGE":           "creator-agent-montage:latest",
			"ANBAN_AGENT_IMAGE_VIDEO_EDIT":        "creator-agent-video-edit:latest",
			"VIDEO_EDIT_ELEVENLABS_API_KEY":       "test-video-edit-scribe-key",
			"ANBAN_AGENT_IMAGE_PROFILE_ANALYSIS":  "creator-agent-profile-analysis:latest",
			"ANBAN_AGENT_IMAGE_FEEDBACK_ANALYSIS": "creator-agent-feedback-analysis:latest",
			"ANBAN_JWT_SECRET_KEY":                "0123456789abcdef0123456789abcdef",
			"ANBAN_OSS_ENDPOINT":                  "oss-cn-test.aliyuncs.com",
			"ANBAN_OSS_ACCESS_KEY_ID":             "test-access-key-id",
			"ANBAN_OSS_ACCESS_KEY_SECRET":         "test-access-key-secret",
			"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL":   "https://deepseek.example.com/anthropic",
			"ANBAN_DEEPSEEK_API_KEY":              "test-deepseek-api-key",
			"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL":   "https://api.moonshot.cn/anthropic",
			"ANBAN_MOONSHOT_API_KEY":              "test-moonshot-agent-api-key",
			"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":      "https://open.bigmodel.cn/api/anthropic",
			"ANBAN_ZHIPU_API_KEY":                 "test-zhipu-api-key",
			"MOONSHOT_API_KEY":                    "test-moonshot-api-key",
			"VOLCENGINE_ARK_API_KEY":              "test-volcengine-api-key",
			"WANGCAI_OPENAI_BASE_URL":             "https://images.example.com/v1",
			"WANGCAI_OPENAI_API_KEY":              "test-openai-api-key",
		} {
			t.Setenv(name, value)
		}

		cfg, err := srvconfig.NewConfig(configPath)
		if err != nil {
			t.Fatalf("load repository config with documented Compose environment: %v", err)
		}
		if cfg.Claude.Executor != "docker" || cfg.Storage.Provider != "oss" || cfg.Storage.Endpoint != "oss-cn-test.aliyuncs.com" {
			t.Fatalf("loaded Compose config = executor %q storage %q/%q", cfg.Claude.Executor, cfg.Storage.Provider, cfg.Storage.Endpoint)
		}
		for _, profileID := range []string{"effective", "balanced", "quality"} {
			if cfg.Claude.ExecutionProfiles[profileID].Envs[model.ClaudeEnvAuthToken] == "" {
				t.Fatalf("Compose environment did not configure Agent profile %q", profileID)
			}
		}
	})

	t.Run("current deployment documentation describes live managed dispatch", func(t *testing.T) {
		docs := map[string]string{
			"README.md":               readTextFile(t, filepath.Join(root, "README.md")),
			"AGENTS.md":               readTextFile(t, filepath.Join(root, "AGENTS.md")),
			"docs/montage-upgrade.md": readTextFile(t, filepath.Join(root, "docs", "montage-upgrade.md")),
		}
		normalizedDocs := make(map[string]string, len(docs))
		for path, body := range docs {
			normalizedDocs[path] = strings.Join(strings.Fields(body), " ")
			for _, forbidden := range []string{
				"ANBAN_CLAUDE_DOCKER_CONTAINER_NAME",
				"ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR",
				"ANBAN_CLAUDE_DOCKER_ARTICLE_IMAGE",
				"ANBAN_ARTICLE_AGENT_IMAGE",
				"ANBAN_KIMI_API_KEY",
				"ANBAN_DOUBAO_AGENT_BASE_URL",
				"ANBAN_DOUBAO_AGENT_API_KEY",
			} {
				if strings.Contains(body, forbidden) {
					t.Errorf("%s retains obsolete variable %s", path, forbidden)
				}
			}
			if regexp.MustCompile(`(?m)^\s*(article_image|image_profiles):`).MatchString(body) {
				t.Errorf("%s retains an obsolete literal YAML key", path)
			}
		}

		for _, want := range []string{
			"ANBAN_AGENT_EXECUTOR",
			"ANBAN_AGENT_IMAGE_WECHAT_ARTICLE",
			"ANBAN_AGENT_IMAGE_SEEDNOTE",
			"ANBAN_AGENT_IMAGE_MONTAGE",
			"ANBAN_AGENT_EXECUTION_TOKEN_SECRET",
			"never builds missing runtime images",
			"fresh container",
			"runtime owns the task workspace and output",
			"/var/run/docker.sock",
			"immutable image digests",
			"VOLCENGINE_ARK_API_KEY",
			"WANGCAI_OPENAI_API_KEY",
			"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL",
			"ANBAN_MOONSHOT_API_KEY",
			"ANBAN_ZHIPU_ANTHROPIC_BASE_URL",
			"ANBAN_ZHIPU_API_KEY",
		} {
			if !strings.Contains(normalizedDocs["README.md"], want) {
				t.Errorf("README.md missing managed dispatch contract %q", want)
			}
		}
		operatorDocs := normalizedDocs["AGENTS.md"] + " " + normalizedDocs["docs/montage-upgrade.md"]
		for _, want := range []string{
			"server never builds a missing runtime image",
			"fresh container",
			"runtime owns its workspace and output",
			"/workspace/openmontage",
		} {
			if !strings.Contains(strings.ToLower(operatorDocs), strings.ToLower(want)) {
				t.Errorf("operator documentation missing managed dispatch rule %q", want)
			}
		}

	})
}

func dotenvAssignments(body string) map[string]string {
	assignments := make(map[string]string)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if ok {
			assignments[strings.TrimSpace(name)] = value
		}
	}
	return assignments
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
		if bytes.Contains(body, []byte("studio/Dockerfile")) || bytes.Contains(body, []byte("docker/wcflink.Dockerfile")) || bytes.Contains(body, []byte("Dockerfile.wcflink")) {
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
		case ".git", ".worktrees", "harness", "third_party", "vendor", "node_modules", "dist", "build", "coverage", ".cache", ".vite", ".next", "bin", "data", "release":
			return false
		}
	}
	return true
}
