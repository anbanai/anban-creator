# Creator Agent Runtime Naming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename all deployment-facing Agent workload, container, and image identities from `anban-agent` or `anban-creator-agent` to `creator-agent` without renaming stable Agent configuration interfaces.

**Architecture:** Keep the existing executors and naming helpers. Change the runtime identity values used by Kubernetes and Docker, then synchronize build defaults, Compose examples, tests, and operational documentation. Preserve `ANBAN_AGENT_*`, `agent_image`, `Dockerfile.agent`, and `docker-agent-image` as stable functional interfaces.

**Tech Stack:** Go 1.26, Kubernetes client-go/core API types, Docker Compose YAML, GNU Make, Go tests.

---

### Task 1: Rename the Kubernetes workload identity

**Files:**
- Modify: `server/agent/kubernetes_executor_test.go:15-245`
- Modify: `server/agent/kubernetes_naming.go:14-20`
- Modify: `server/agent/kubernetes_executor.go:39-44`

- [ ] **Step 1: Change Pod prefix and label expectations**

Use these assertions in `server/agent/kubernetes_executor_test.go`:

```go
if !strings.HasPrefix(first, "creator-agent-") {
	t.Fatalf("pod name = %q, want creator-agent prefix", first)
}
if labels["app.kubernetes.io/name"] != "creator-agent" {
	t.Fatalf("app label = %q, want creator-agent", labels["app.kubernetes.io/name"])
}
```

- [ ] **Step 2: Verify RED**

Run `go test ./server/agent -run 'TestKubernetes(PodName|Labels)' -count=1`.

Expected: FAIL because the implementation still returns `anban-agent`.

- [ ] **Step 3: Change the Kubernetes name and label constants**

```go
const (
	kubernetesAgentAppName       = "creator-agent"
	kubernetesAgentNamePrefix    = kubernetesAgentAppName
	kubernetesUserIDLabel        = "anban.ai/user-id"
	kubernetesProjectIDLabel     = "anban.ai/project-id"
	kubernetesTaskIDLabel        = "anban.ai/task-id"
	kubernetesWorkspaceMountName = "workspace"
)
```

- [ ] **Step 4: Verify GREEN**

Run `go test ./server/agent -run 'TestKubernetes(PodName|Labels)' -count=1`.

Expected: PASS.

- [ ] **Step 5: Add the primary container identity assertion**

In `TestKubernetesPodSpecUsesConfiguredImageAndPVC`, change the fixture and add:

```go
AgentImage: "registry.example.com/creator-agent:latest",
```

```go
agentContainer := pod.Spec.Containers[0]
if agentContainer.Name != "creator-agent" {
	t.Fatalf("container name = %q, want creator-agent", agentContainer.Name)
}
```

Also change the two drift-test image fixtures to `registry.example.com/creator-agent:v1`.

- [ ] **Step 6: Verify RED**

Run `go test ./server/agent -run TestKubernetesPodSpecUsesConfiguredImageAndPVC -count=1`.

Expected: FAIL with `container name = "agent", want creator-agent`.

- [ ] **Step 7: Rename the Kubernetes container constant**

```go
const (
	kubernetesAgentContainerName      = kubernetesAgentAppName
	kubernetesPodTTLAnnotation        = "anban.ai/pod-ttl-seconds"
	kubernetesPodRevisionAnnotation   = "anban.ai/pod-revision"
	kubernetesPodConfigHashAnnotation = "anban.ai/pod-config-hash"
)
```

- [ ] **Step 8: Verify GREEN and commit**

Run `go test ./server/agent -run TestKubernetes -count=1`; expected: PASS.

```bash
git add server/agent/kubernetes_naming.go server/agent/kubernetes_executor.go server/agent/kubernetes_executor_test.go
git commit -m "refactor(agent): rename kubernetes runtime identity"
```

### Task 2: Rename Docker image and ephemeral container defaults

**Files:**
- Modify: `server/agent/docker_executor_naming_test.go:11-32`
- Modify: `server/agent/runtime_names.go:8-16`
- Modify: `server/config/kubernetes_config_test.go:9-55`
- Modify: `server/config/config.go:1096-1104,1573-1575`

- [ ] **Step 1: Change Docker runtime expectations**

Rename the test to `TestDockerExecutorUsesCreatorAgentRuntimeNames`, then use:

```go
if got, want := EphemeralContainerName(task.ID), "creator-agent-task-task-1"; got != want {
	t.Fatalf("container name = %q, want %q", got, want)
}
if got, want := DockerAgentImageDefault, "creator-agent:latest"; got != want {
	t.Fatalf("default agent image = %q, want %q", got, want)
}
```

- [ ] **Step 2: Verify RED**

Run `go test ./server/agent -run TestDockerExecutorUsesCreatorAgentRuntimeNames -count=1`.

Expected: FAIL on the old container prefix or image default.

- [ ] **Step 3: Update centralized Docker runtime names**

Keep `AgentBinaryName` and `DefaultWorkspaceBaseName` unchanged, and set:

```go
EphemeralContainerNamePrefix = "creator-agent-task-"
DockerAgentImageDefault      = "creator-agent:latest"
```

- [ ] **Step 4: Verify GREEN**

Run `go test ./server/agent -run TestDockerExecutorUsesCreatorAgentRuntimeNames -count=1`.

Expected: PASS.

- [ ] **Step 5: Change the config default expectation and verify RED**

In `server/config/kubernetes_config_test.go`, use `registry.example.com/creator-agent:latest` for the Kubernetes fixture and assert:

```go
if cfg.Claude.Docker.Image != "creator-agent:latest" {
	t.Fatalf("docker image default = %q, want creator-agent runtime identity", cfg.Claude.Docker.Image)
}
```

Run `go test ./server/config -run 'Test(KubernetesAgentImageMustBeExplicit|ValidateAcceptsKubernetesExecutor)' -count=1`.

Expected: FAIL because the Docker default is still `anban-creator-agent:latest`.

- [ ] **Step 6: Update the Docker config default**

Change the `DockerConfig.Image` comment to name `creator-agent:latest`, then use:

```go
if c.Claude.Docker.Image == "" {
	c.Claude.Docker.Image = "creator-agent:latest"
}
```

- [ ] **Step 7: Verify GREEN and commit**

Run `go test ./server/config -run 'Test(KubernetesAgentImageMustBeExplicit|ValidateAcceptsKubernetesExecutor)' -count=1`; expected: PASS.

```bash
git add server/agent/docker_executor_naming_test.go server/agent/runtime_names.go server/config/kubernetes_config_test.go server/config/config.go
git commit -m "refactor(agent): rename docker runtime identity"
```

### Task 3: Synchronize build, Compose, and operational documentation

**Files:**
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `Makefile:6-10`
- Modify: `docker-compose.yml:109-123`
- Modify: `docs/superpowers/specs/2026-04-15-persistent-docker-executor-design.md`
- Modify: `docs/superpowers/specs/2026-07-09-ack-agent-pod-executor-design.md`
- Modify: `docs/superpowers/specs/2026-07-12-kubernetes-agent-job-runtime-design.md`
- Modify: `docs/superpowers/plans/2026-07-09-ack-agent-pod-executor.md`
- Modify: `docs/superpowers/plans/2026-07-12-kubernetes-agent-job-runtime.md`

- [ ] **Step 1: Add a failing repository contract test**

Add to `server/agent/docker_runtime_contract_test.go`:

```go
func TestCreatorAgentImageNamingContract(t *testing.T) {
	root := repositoryRoot(t)
	for _, tc := range []struct {
		path string
		want []string
	}{
		{path: filepath.Join(root, "Makefile"), want: []string{"AGENT_IMAGE := creator-agent:latest"}},
		{
			path: filepath.Join(root, "docker-compose.yml"),
			want: []string{"image: creator-agent:latest", "container_name: creator-agent"},
		},
	} {
		body := readTextFile(t, tc.path)
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", tc.path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Verify RED**

Run `go test ./server/agent -run TestCreatorAgentImageNamingContract -count=1`.

Expected: FAIL because Make and Compose still use `anban-creator-agent`.

- [ ] **Step 3: Update build and Compose identities**

Set `AGENT_IMAGE := creator-agent:latest` in `Makefile`. In the optional Agent service block use:

```yaml
#   image: creator-agent:latest
#   container_name: creator-agent
```

- [ ] **Step 4: Verify GREEN**

Run `go test ./server/agent -run TestCreatorAgentImageNamingContract -count=1`.

Expected: PASS.

- [ ] **Step 5: Update operational examples**

Apply only these runtime-identity replacements in the listed docs:

```text
anban-creator-agent:latest -> creator-agent:latest
registry.example.com/anban-agent:* -> registry.example.com/creator-agent:*
anban-agent-runner -> creator-agent-runner
anban-agent-nas -> creator-agent-nas
app.kubernetes.io/name=anban-agent -> app.kubernetes.io/name=creator-agent
anban-agent-u-<short_user_hash>-p-<short_project_hash> -> creator-agent-u-<short_user_hash>-p-<short_project_hash>
anban-agent-execution -> creator-agent-execution
```

Do not change `ANBAN_AGENT_*`, `agent_image`, `Dockerfile.agent`, `docker-agent-image`, `anban.ai/*`, or the `anban` binary/plugin identity.

- [ ] **Step 6: Inspect old-name matches**

```bash
rg -n 'anban-agent|anban-creator-agent' --hidden --glob '!.git' --glob '!third_party/**' --glob '!docs/superpowers/specs/2026-07-12-creator-agent-runtime-naming-design.md' --glob '!docs/superpowers/plans/2026-07-12-creator-agent-runtime-naming.md' .
```

Expected: remaining matches, if any, are explicit negative regression guards; no active runtime value or operational example uses an old identity.

- [ ] **Step 7: Commit synchronized contracts**

```bash
git add Makefile docker-compose.yml server/agent/docker_runtime_contract_test.go docs/superpowers/specs/2026-04-15-persistent-docker-executor-design.md docs/superpowers/specs/2026-07-09-ack-agent-pod-executor-design.md docs/superpowers/specs/2026-07-12-kubernetes-agent-job-runtime-design.md docs/superpowers/plans/2026-07-09-ack-agent-pod-executor.md docs/superpowers/plans/2026-07-12-kubernetes-agent-job-runtime.md
git commit -m "docs: align creator agent runtime naming"
```

### Task 4: Run full verification

**Files:**
- Verify all files changed in Tasks 1-3.

- [ ] **Step 1: Format and inspect**

```bash
gofmt -w server/agent/kubernetes_executor_test.go server/agent/kubernetes_naming.go server/agent/kubernetes_executor.go server/agent/docker_executor_naming_test.go server/agent/runtime_names.go server/config/kubernetes_config_test.go server/config/config.go server/agent/docker_runtime_contract_test.go
git diff --check HEAD~3..HEAD
```

Expected: `git diff --check` prints nothing.

- [ ] **Step 2: Run affected package tests**

Run `go test ./server/agent ./server/config -count=1`.

Expected: PASS.

- [ ] **Step 3: Run all Go tests**

Run `go test ./... -count=1`.

Expected: PASS with zero failing packages.

- [ ] **Step 4: Build both runtime binaries**

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: both commands exit 0.

- [ ] **Step 5: Preserve unrelated work**

Run `git status --short`.

Expected: the pre-existing `third_party/OpenMontage` modification remains and no unrelated file is staged or modified.
