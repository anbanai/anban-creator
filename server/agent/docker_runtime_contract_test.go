package agent

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"gopkg.in/yaml.v3"
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
	  server:
	    environment:
	      ANBAN_AGENT_IMAGE_ARTICLE: "creator-agent-article:latest"
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
			name:     "Compose image outside Server environment",
			makefile: validMakefile,
			compose: `services:
	  server:
	    image: creator-agent-article:latest
`,
		},
		{
			name:     "persistent Agent service remains",
			makefile: validMakefile,
			compose: validCompose + `  agent:
	    image: creator-agent-article:latest
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
			compose:  validCompose + "      ANBAN_AGENT_IMAGE_ARTICLE: \"wrong-agent:latest\"\n",
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
	if strings.Contains("\n"+compose, "\n  agent:\n") {
		return fmt.Errorf("docker-compose.yml must not define a persistent agent service")
	}
	values := composeEnvironmentAssignments(compose, "server", "ANBAN_AGENT_IMAGE_ARTICLE")
	if len(values) != 1 || values[0] != `"creator-agent-article:latest"` {
		return fmt.Errorf("docker-compose.yml must configure the Server Article runtime image exactly once")
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
		"NewLocalExecutor(",
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
	articlePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-article")
	seednotePath := filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote")
	article := readTextFile(t, articlePath)
	seednote := readTextFile(t, seednotePath)
	for _, want := range []string{
		"FROM node:bookworm-slim",
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
		"COPY --from=builder /build/server/billing/economics.yaml /app/conf/billing/economics.yaml",
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
		"/build/server/billing/policy.yaml",
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
	from := strings.Index(body, "FROM node:bookworm-slim")
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
			want: []string{"FROM golang:alpine AS builder", "FROM node:bookworm-slim"},
		},
		{
			path: filepath.Join(root, "deploy/docker/Dockerfile.agent-seednote"),
			want: []string{"FROM golang:alpine AS builder", "FROM node:bookworm-slim"},
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
		"deploy/docker/Dockerfile.agent-article-ts",
		"deploy/docker/Dockerfile.agent-montage",
		"deploy/docker/Dockerfile.agent-montage-ts",
		"deploy/docker/Dockerfile.agent-seednote",
		"deploy/docker/Dockerfile.agent-seednote-ts",
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
		"TS_AGENT_IMAGE ?= creator-agent-article-ts:latest",
		"TS_SEEDNOTE_AGENT_IMAGE ?= creator-agent-seednote-ts:latest",
		"TS_MONTAGE_AGENT_IMAGE ?= creator-agent-montage-ts:latest",
		"docker-agent-ts-image:",
		"docker-seednote-agent-ts-image:",
		"docker-montage-agent-ts-image:",
		"docker-seednote-agent-image:",
		"docker-montage-agent-image:",
		"docker-wcflink-image:",
		"docker-studio-image:",
		"docker-images: docker-agent-image docker-seednote-agent-image docker-montage-agent-image docker-server-image docker-wcflink-image docker-studio-image",
		"docker build -f deploy/docker/Dockerfile.agent-article -t $(AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-seednote -t $(SEEDNOTE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-montage",
		"docker build -f deploy/docker/Dockerfile.agent-article-ts -t $(TS_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-seednote-ts -t $(TS_SEEDNOTE_AGENT_IMAGE) .",
		"docker build -f deploy/docker/Dockerfile.agent-montage-ts -t $(TS_MONTAGE_AGENT_IMAGE) .",
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

func TestComposeUsesOneShotManagedDockerRuntime(t *testing.T) {
	root := repositoryRoot(t)
	compose := readTextFile(t, filepath.Join(root, "docker-compose.yml"))
	for _, want := range []string{
		"ANBAN_AGENT_EXECUTOR: \"docker\"",
		"ANBAN_CLAUDE_AGENT_SERVER_URL: \"http://server:8080\"",
		"ANBAN_AGENT_EXECUTION_TOKEN_SECRET:",
		"ANBAN_AGENT_IMAGE_ARTICLE: \"creator-agent-article:latest\"",
		"ANBAN_AGENT_IMAGE_SEEDNOTE: \"creator-agent-seednote:latest\"",
		"ANBAN_AGENT_IMAGE_MONTAGE: \"creator-agent-montage:latest\"",
		"- /var/run/docker.sock:/var/run/docker.sock",
		"group_add:\n      - \"${DOCKER_GID:-0}\"",
		"anban-creator-network:\n    name: anban-creator-network\n    driver: bridge",
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
		"docker-up: docker-agent-image docker-seednote-agent-image docker-montage-agent-image",
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
			"ANBAN_AGENT_EXECUTOR":               "docker",
			"ANBAN_AGENT_IMAGE_ARTICLE":          "creator-agent-article:latest",
			"ANBAN_AGENT_IMAGE_SEEDNOTE":         "creator-agent-seednote:latest",
			"ANBAN_AGENT_IMAGE_MONTAGE":          "creator-agent-montage:latest",
			"ANBAN_AGENT_EXECUTION_TOKEN_SECRET": "${ANBAN_AGENT_EXECUTION_TOKEN_SECRET:?ANBAN_AGENT_EXECUTION_TOKEN_SECRET is required}",
			"ANBAN_BILLING_ADMIN_API_KEY":        "${ANBAN_BILLING_ADMIN_API_KEY:?ANBAN_BILLING_ADMIN_API_KEY is required}",
			"ANBAN_JWT_SECRET_KEY":               "${ANBAN_JWT_SECRET_KEY:?ANBAN_JWT_SECRET_KEY is required}",
			"ANBAN_OSS_ENDPOINT":                 "${ANBAN_OSS_ENDPOINT:?ANBAN_OSS_ENDPOINT is required}",
			"ANBAN_OSS_ACCESS_KEY_ID":            "${ANBAN_OSS_ACCESS_KEY_ID:?ANBAN_OSS_ACCESS_KEY_ID is required}",
			"ANBAN_OSS_ACCESS_KEY_SECRET":        "${ANBAN_OSS_ACCESS_KEY_SECRET:?ANBAN_OSS_ACCESS_KEY_SECRET is required}",
			"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL":  "${ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL:-}",
			"ANBAN_DEEPSEEK_API_KEY":             "${ANBAN_DEEPSEEK_API_KEY:?ANBAN_DEEPSEEK_API_KEY is required}",
			"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL":  "${ANBAN_MOONSHOT_ANTHROPIC_BASE_URL:-https://api.moonshot.cn/anthropic}",
			"ANBAN_MOONSHOT_API_KEY":             "${ANBAN_MOONSHOT_API_KEY:?ANBAN_MOONSHOT_API_KEY is required}",
			"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":     "${ANBAN_ZHIPU_ANTHROPIC_BASE_URL:-https://open.bigmodel.cn/api/anthropic}",
			"ANBAN_ZHIPU_API_KEY":                "${ANBAN_ZHIPU_API_KEY:?ANBAN_ZHIPU_API_KEY is required}",
			"MOONSHOT_API_KEY":                   "${MOONSHOT_API_KEY:?MOONSHOT_API_KEY is required}",
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
			"ANBAN_AGENT_IMAGE_ARTICLE",
			"ANBAN_AGENT_IMAGE_SEEDNOTE",
			"ANBAN_AGENT_IMAGE_MONTAGE",
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
		configPath := filepath.Join(root, "server", "config.yaml")
		configBody := readTextFile(t, configPath)
		envReference := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)`)
		for _, match := range envReference.FindAllStringSubmatch(configBody, -1) {
			t.Setenv(match[1], "")
		}
		for name, value := range map[string]string{
			"ANBAN_DATABASE_DSN":                 "root:dev@tcp(mysql:3306)/anban_creator?charset=utf8mb4&parseTime=True&loc=Local",
			"ANBAN_REDIS_ADDR":                   "redis:6379",
			"ANBAN_BILLING_ADMIN_API_KEY":        "test-billing-admin-key",
			"ANBAN_AGENT_EXECUTOR":               "docker",
			"ANBAN_CLAUDE_AGENT_SERVER_URL":      "http://server:8080",
			"ANBAN_AGENT_EXECUTION_TOKEN_SECRET": "0123456789abcdef0123456789abcdef",
			"ANBAN_AGENT_IMAGE_ARTICLE":          "creator-agent-article:latest",
			"ANBAN_AGENT_IMAGE_SEEDNOTE":         "creator-agent-seednote:latest",
			"ANBAN_AGENT_IMAGE_MONTAGE":          "creator-agent-montage:latest",
			"ANBAN_JWT_SECRET_KEY":               "0123456789abcdef0123456789abcdef",
			"ANBAN_OSS_ENDPOINT":                 "oss-cn-test.aliyuncs.com",
			"ANBAN_OSS_ACCESS_KEY_ID":            "test-access-key-id",
			"ANBAN_OSS_ACCESS_KEY_SECRET":        "test-access-key-secret",
			"ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL":  "https://deepseek.example.com/anthropic",
			"ANBAN_DEEPSEEK_API_KEY":             "test-deepseek-api-key",
			"ANBAN_MOONSHOT_ANTHROPIC_BASE_URL":  "https://api.moonshot.cn/anthropic",
			"ANBAN_MOONSHOT_API_KEY":             "test-moonshot-agent-api-key",
			"ANBAN_ZHIPU_ANTHROPIC_BASE_URL":     "https://open.bigmodel.cn/api/anthropic",
			"ANBAN_ZHIPU_API_KEY":                "test-zhipu-api-key",
			"MOONSHOT_API_KEY":                   "test-moonshot-api-key",
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
			"ANBAN_AGENT_IMAGE_ARTICLE",
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

		obsolete := filepath.Join(root, "docs", "superpowers", "specs", "2026-04-15-persistent-docker-executor-design.md")
		if _, err := os.Stat(obsolete); !os.IsNotExist(err) {
			t.Errorf("superseded persistent executor design must be removed; stat error = %v", err)
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
