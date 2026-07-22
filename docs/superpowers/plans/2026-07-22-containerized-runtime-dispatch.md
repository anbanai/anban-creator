# Containerized Runtime Dispatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Server-local and persistent-Docker execution with one managed one-shot runtime protocol for Docker and Kubernetes, while moving workspace ownership out of Agents and Skills.

**Architecture:** `TaskService` persists a runtime-neutral execution identity and delegates to a common `RuntimeDispatcher`. Kubernetes keeps Jobs/PVCs; Docker creates one labeled container per execution plus task/project volumes, and both run `anban job` with the same bootstrap, output, upload, completion, reconciliation, and resume contracts.

**Tech Stack:** Go 1.24, Fiber v3, GORM/MySQL, Docker Engine API v28, Kubernetes client-go, Claude Agent SDK Go, MCP Go SDK, Docker Compose.

---

## File Map

New files have one responsibility each:

- `server/agent/runtime_dispatcher.go`: neutral dispatcher, identity, and state contracts.
- `server/agent/runtime_reconciler.go`: neutral reconciliation and cleanup loop.
- `server/agent/docker_runtime.go`: Docker names, labels, specs, and verification.
- `server/agent/docker_dispatcher.go`: Docker dispatch, inspect, delete, and volume lifecycle.
- `server/agent/docker_identity.go`: verify Docker bootstrap identity against a live container.
- `server/auth/workload_token.go`: issue and validate short-lived Docker workload JWTs.
- `server/migrations/20260722_runtime_dispatch_identity.sql`: rename Kubernetes-specific execution identity columns.
- `server/service/task_artifact_stream.go`: bounded provider-neutral artifact ingestion.
- `server/handler/agent_artifact_stream.go`: execution-authenticated artifact stream endpoint.
- `server/agent/runtime_plugin_workspace_contract_test.go`: prohibit workflow-owned directory management.
- `deploy/docker/runtime-smoke.sh`: real dispatch/bootstrap/upload/resume smoke test.

Removed files:

- `server/agent/docker_executor.go`
- `server/agent/docker_executor_naming_test.go`
- `server/mcp/workspace_tools.go`
- `server/mcp/workspace_tools_test.go`
- `server/service/workspace.go`
- `server/service/workspace_test.go`
- `docs/superpowers/specs/2026-04-15-persistent-docker-executor-design.md`

`server/agent/executor.go` retains only result and prompt helpers used by the standalone Agent. Delete `LocalExecutor`, `TaskExecutor`, and host-workspace execution code after moving retained helpers into focused files.

### Task 1: Replace Executor-Specific Image Configuration

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/kubernetes_config_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`
- Modify: `server/agent/runtime_names.go`

- [ ] **Step 1: Write failing configuration and selection tests**

```go
func TestRuntimeImageForTaskUsesCanonicalProfileMap(t *testing.T) {
    cfg := RuntimeImages{
        model.PlatformArticle: "registry/article:v1",
        model.PlatformSeednote: "registry/seednote:v1",
        model.PlatformMontage: "registry/montage:v1",
    }
    tests := []struct{ taskType, profile, image string }{
        {model.PlatformArticle, model.PlatformArticle, cfg[model.PlatformArticle]},
        {model.PlatformMoments, model.PlatformArticle, cfg[model.PlatformArticle]},
        {model.PlatformEcommerce, model.PlatformArticle, cfg[model.PlatformArticle]},
        {model.PlatformSeednote, model.PlatformSeednote, cfg[model.PlatformSeednote]},
        {model.PlatformMontage, model.PlatformMontage, cfg[model.PlatformMontage]},
    }
    for _, tt := range tests {
        got := cfg.ForTask(tt.taskType)
        if got.Profile != tt.profile || got.Image != tt.image {
            t.Fatalf("ForTask(%q) = %#v", tt.taskType, got)
        }
    }
}

func TestClaudeConfigRejectsRemovedManagedExecutorConfig(t *testing.T) {
    for _, body := range []string{
        "claude:\n  executor: local\n",
        "claude:\n  docker:\n    article_image: old\n",
        "claude:\n  docker:\n    image_profiles:\n      seednote: old\n",
        "claude:\n  docker:\n    container_name: old\n",
        "claude:\n  docker:\n    workspace_dir: /tmp/old\n",
    } {
        if _, err := loadConfigYAMLForTest(body); err == nil { t.Fatalf("accepted: %s", body) }
    }
}
```

- [ ] **Step 2: Run the tests and verify failure**

Run: `go test ./server/config -run 'TestRuntimeImageForTask|TestClaudeConfigRejectsRemoved' -count=1`

Expected: FAIL because `RuntimeImages` is absent and removed keys are accepted.

- [ ] **Step 3: Implement the shared image contract**

```go
type RuntimeImages map[string]string

func runtimeProfileForTask(taskType string) string {
    switch strings.TrimSpace(taskType) {
    case model.PlatformSeednote:
        return model.PlatformSeednote
    case model.PlatformMontage:
        return model.PlatformMontage
    default:
        return model.PlatformArticle
    }
}

func (images RuntimeImages) ForTask(taskType string) RuntimeImageSelection {
    profile := runtimeProfileForTask(taskType)
    return RuntimeImageSelection{Profile: profile, Image: strings.TrimSpace(images[profile])}
}
```

Add to `ClaudeConfig`:

```go
RuntimeImages        RuntimeImages `yaml:"runtime_images"`
ExecutionTokenSecret string        `yaml:"execution_token_secret"`
```

Reduce `DockerConfig` to `Network`, `CPUCores`, `MemoryMB`, `PidsLimit`, and `TimeoutSec`. Accept only `docker` or `kubernetes`; require exactly `article`, `seednote`, and `montage`; reject removed keys in YAML unmarshalling. Update both YAML files to the approved shape.

- [ ] **Step 4: Run focused tests**

Run: `go test ./server/config ./server/agent -run 'TestRuntimeImageForTask|TestClaudeConfigRejectsRemoved|TestDocker.*Default' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/config/config.go server/config/kubernetes_config_test.go server/config.yaml server/config.example.yaml server/agent/runtime_names.go
git commit -m "refactor(config): unify managed runtime images"
```

### Task 2: Generalize Persisted Runtime Identity

**Files:**
- Create: `server/migrations/20260722_runtime_dispatch_identity.sql`
- Modify: `server/migrations/migrations_test.go`
- Modify: `server/model/task_execution.go`
- Modify: `server/model/task_execution_test.go`
- Modify: `server/repository/repository.go`
- Modify: `server/repository/task_execution.go`
- Modify: `server/repository/task_execution_test.go`
- Modify: `server/service/task_execution_reconcile.go`

- [ ] **Step 1: Write failing schema and repository tests**

```go
func TestRuntimeDispatchIdentityMigration(t *testing.T) {
    raw, err := os.ReadFile("20260722_runtime_dispatch_identity.sql")
    if err != nil { t.Fatal(err) }
    for _, fragment := range []string{
        "CHANGE COLUMN `namespace` `runtime_scope`",
        "CHANGE COLUMN `job_name` `runtime_workload`",
        "CHANGE COLUMN `pod_uid` `runtime_instance_id`",
        "idx_task_executions_runtime_workload",
    } {
        if !strings.Contains(string(raw), fragment) { t.Errorf("missing %q", fragment) }
    }
}

func TestCompleteDispatchPersistsRuntimeWorkload(t *testing.T) {
    identity := model.RuntimeIdentity{Scope: "docker", Workload: "creator-agent-exec-1"}
    won, err := repo.TaskExecutions().CompleteDispatch(ctx, execution.ID, token, identity)
    if err != nil || !won { t.Fatalf("CompleteDispatch = %v, %v", won, err) }
    got, _ := repo.TaskExecutions().FindByID(ctx, execution.ID)
    if got.RuntimeScope != identity.Scope || got.RuntimeWorkload != identity.Workload { t.Fatalf("got %#v", got) }
}
```

- [ ] **Step 2: Verify failures**

Run: `go test ./server/migrations ./server/model ./server/repository -run 'TestRuntimeDispatchIdentity|TestCompleteDispatchPersistsRuntimeWorkload' -count=1`

Expected: FAIL because neutral identity fields do not exist.

- [ ] **Step 3: Implement the forward-only migration and types**

```go
type RuntimeIdentity struct {
    Scope      string
    Workload   string
    InstanceID string
}

type ExecutionTransition struct {
    Started            bool
    RuntimeInstanceID  string
    ManifestStatus     string
    TerminalReason     string
    Diagnostics        datatypes.JSON
    Result             datatypes.JSON
    FinalizationStatus string
    CleanupStatus      string
}
```

Rename model fields and repository columns to `RuntimeScope`, `RuntimeWorkload`, and `RuntimeInstanceID`. Make `CompleteDispatch` and `SetRuntimeIdentity` accept `model.RuntimeIdentity`. The SQL renames all three columns, replaces the job-name index, and leaves no compatibility columns.

- [ ] **Step 4: Run package tests**

Run: `go test ./server/migrations ./server/model ./server/repository -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/migrations/20260722_runtime_dispatch_identity.sql server/migrations/migrations_test.go server/model/task_execution.go server/model/task_execution_test.go server/repository/repository.go server/repository/task_execution.go server/repository/task_execution_test.go server/service/task_execution_reconcile.go
git commit -m "refactor(runtime): persist generic workload identity"
```

### Task 3: Introduce The Common Runtime Dispatcher

**Files:**
- Create: `server/agent/runtime_dispatcher.go`
- Create: `server/agent/runtime_dispatcher_test.go`
- Modify: `server/agent/kubernetes_dispatcher.go`
- Modify: `server/agent/kubernetes_executor_test.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_dispatch.go`
- Modify: `server/service/task_dispatch_test.go`
- Modify: `server/service/task_execution_complete.go`
- Modify: `server/service/task_execution_complete_test.go`

- [ ] **Step 1: Write failing neutral-dispatch tests**

```go
type dispatchTestRuntime struct {
    selection config.RuntimeImageSelection
    identity  *model.RuntimeIdentity
}
func (d *dispatchTestRuntime) Scope() string { return "docker" }
func (d *dispatchTestRuntime) ResolveRuntime(string) config.RuntimeImageSelection { return d.selection }
func (d *dispatchTestRuntime) Dispatch(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error) { return d.identity, nil }
func (d *dispatchTestRuntime) Inspect(context.Context, *model.TaskExecution) (*agent.RuntimeExecutionState, error) { return nil, nil }
func (d *dispatchTestRuntime) Delete(context.Context, *model.TaskExecution) error { return nil }
```

Assert `SetRuntimeDispatcher` dispatches a pending task, persists `Target="docker"`, and records `RuntimeScope/RuntimeWorkload` without Kubernetes names.

- [ ] **Step 2: Verify failure**

Run: `go test ./server/service ./server/agent -run 'TestManagedDispatch|TestKubernetesImageForTask' -count=1`

Expected: FAIL because neutral contracts are absent.

- [ ] **Step 3: Implement neutral contracts and adapt Kubernetes**

```go
type RuntimeDispatcher interface {
    Scope() string
    ResolveRuntime(string) config.RuntimeImageSelection
    Dispatch(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error)
    Inspect(context.Context, *model.TaskExecution) (*RuntimeExecutionState, error)
    Delete(context.Context, *model.TaskExecution) error
}

type RuntimeExecutionState struct {
    Phase string; InstanceID string; Reason string; Message string
    ExitCode *int32; CompletedAt *time.Time
}
```

Define phases `pending`, `running`, `succeeded`, and `failed`. Rename TaskService fields/methods and error text from Kubernetes to runtime-neutral terms. `createCurrentExecution` must set the required `TaskExecution.Target` from `runtimeDispatcher.Scope()` before persisting the execution. Implement `Scope()` as `"docker"` and `"kubernetes"` on the concrete dispatchers and all fakes. Adapt Kubernetes dispatcher return values without changing its Job/PVC checks.

- [ ] **Step 4: Run focused tests**

Run: `go test ./server/service ./server/agent -run 'TestManagedDispatch|TestCreateCurrentExecution|TestResumeExecution|TestBuildKubernetesJob|TestKubernetes' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/runtime_dispatcher.go server/agent/runtime_dispatcher_test.go server/agent/kubernetes_dispatcher.go server/agent/kubernetes_executor_test.go server/service/task.go server/service/task_dispatch.go server/service/task_dispatch_test.go server/service/task_execution_complete.go server/service/task_execution_complete_test.go
git commit -m "refactor(runtime): generalize managed dispatch"
```

### Task 4: Generalize Workload Authentication And Bootstrap

**Files:**
- Create: `server/auth/workload_token.go`
- Create: `server/auth/workload_token_test.go`
- Create: `server/agent/docker_identity.go`
- Create: `server/agent/docker_identity_test.go`
- Modify: `server/agent/kubernetes_identity.go`
- Modify: `server/agent/kubernetes_identity_test.go`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_test.go`
- Modify: `server/handler/agent.go`
- Modify: `server/handler/agent_test.go`

- [ ] **Step 1: Write failing identity tests**

```go
func TestWorkloadTokenRoundTrip(t *testing.T) {
    svc, _ := NewWorkloadTokenService(strings.Repeat("s", 32))
    claims := WorkloadClaims{RuntimeScope: "docker", RuntimeWorkload: "exec-1",
        RuntimeInstanceID: strings.Repeat("a", 64), UserID: "u", ProjectID: "p", TaskID: "t", ExecutionID: "e"}
    raw, err := svc.Issue(claims, time.Now().Add(5*time.Minute))
    if err != nil { t.Fatal(err) }
    got, err := svc.Validate(raw)
    if err != nil || got.ExecutionID != claims.ExecutionID { t.Fatalf("got %#v, %v", got, err) }
}

func TestBootstrapAcceptsGenericDockerIdentity(t *testing.T) {
    identity := &agent.WorkloadIdentity{RuntimeIdentity: model.RuntimeIdentity{Scope: "docker", Workload: "exec-1", InstanceID: "container-id"}, ExecutionID: execution.ID, TaskID: task.ID, ProjectID: task.ProjectID, UserID: task.UserID, Deadline: time.Now().Add(10*time.Minute)}
    response, err := svc.Bootstrap(ctx, identity)
    if err != nil || response.ExecutionToken == "" { t.Fatalf("response %#v, %v", response, err) }
}
```

- [ ] **Step 2: Verify failures**

Run: `go test ./server/auth ./server/agent ./server/service ./server/handler -run 'TestWorkloadToken|TestBootstrapAcceptsGenericDocker' -count=1`

Expected: FAIL because bootstrap accepts only Kubernetes identity.

- [ ] **Step 3: Implement generic identity and verifiers**

```go
type WorkloadIdentity struct {
    model.RuntimeIdentity
    ExecutionID string; TaskID string; ProjectID string; UserID string
    Deadline time.Time
}
type WorkloadVerifier interface {
    Verify(context.Context, string, string) (*WorkloadIdentity, error)
}
```

Use a distinct workload-token issuer/audience. Docker verification validates JWT, inspects the claimed live container, and checks execution/task/project/user labels. Kubernetes returns the common type after its existing TokenReview/Pod/Job verification. Bootstrap compares generic persisted identity and stores `RuntimeInstanceID` during starting-to-running transition.

- [ ] **Step 4: Run identity and bootstrap tests**

Run: `go test ./server/auth ./server/agent ./server/service ./server/handler -run 'TestWorkload|TestBootstrap|TestAgentHandler' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/auth/workload_token.go server/auth/workload_token_test.go server/agent/docker_identity.go server/agent/docker_identity_test.go server/agent/kubernetes_identity.go server/agent/kubernetes_identity_test.go server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go server/handler/agent.go server/handler/agent_test.go
git commit -m "feat(runtime): authenticate Docker workloads"
```

### Task 5: Build Docker Resource Specifications

**Files:**
- Create: `server/agent/docker_runtime.go`
- Create: `server/agent/docker_runtime_test.go`
- Modify: `server/agent/runtime_names.go`
- Modify: `deploy/docker/Dockerfile.agent-article`
- Modify: `deploy/docker/Dockerfile.agent-seednote`
- Modify: `deploy/docker/Dockerfile.agent-montage`

- [ ] **Step 1: Write failing resource tests**

```go
func TestBuildDockerRuntimeSpec(t *testing.T) {
    spec := buildDockerRuntimeSpec(dockerRuntimeConfig{DockerConfig: config.DockerConfig{Network: "anban", CPUCores: 2, MemoryMB: 4096, PidsLimit: 256, TimeoutSec: 3600}, ServerURL: "http://server:8080"}, testExecution(), testTask())
    if spec.Container.Config.Image != testExecution().RuntimeImage { t.Fatalf("image = %q", spec.Container.Config.Image) }
    if spec.Container.Config.User != "1000:1000" { t.Fatalf("user = %q", spec.Container.Config.User) }
    if len(spec.Container.HostConfig.Binds) != 0 { t.Fatal("host binds forbidden") }
    if len(spec.Container.HostConfig.Mounts) != 2 { t.Fatalf("mounts = %#v", spec.Container.HostConfig.Mounts) }
    if spec.Container.Config.Labels[dockerExecutionIDLabel] != testExecution().ID { t.Fatalf("labels = %#v", spec.Container.Config.Labels) }
}

func TestAgentImagesPrecreateWorkloadSecretDirectory(t *testing.T) {
    for _, path := range []string{
        "../../deploy/docker/Dockerfile.agent-article",
        "../../deploy/docker/Dockerfile.agent-seednote",
        "../../deploy/docker/Dockerfile.agent-montage",
    } {
        body, err := os.ReadFile(path)
        if err != nil { t.Fatal(err) }
        if !strings.Contains(string(body), "install -d -m 0700 -o 1000 -g 1000 /run/secrets/anban") {
            t.Errorf("%s does not precreate the workload secret directory", path)
        }
    }
}
```

- [ ] **Step 2: Verify failure**

Run: `go test ./server/agent -run 'TestBuildDockerRuntimeSpec|TestAgentImagesPrecreateWorkloadSecretDirectory|TestDockerRuntimeName' -count=1`

Expected: FAIL because managed Docker specs are absent.

- [ ] **Step 3: Implement deterministic builders and verifiers**

Build hashed names and ownership labels. Create a `1000:1000` container using the persisted image, `/workspace`, task/project named volumes, configured network/resources/PID limit, and `anban job --workload-token-file /run/secrets/anban/token`. In all three profile Dockerfiles, create `/run/secrets/anban` while still root with owner `1000:1000` and mode `0700`, before the final `USER 1000:1000`, so `CopyToContainer` has a restrictive existing destination. Add pure checks for label, image, mount, command, network, and resource drift. Do not use bind mounts or `VolumesFrom`.

- [ ] **Step 4: Run builder tests**

Run: `go test ./server/agent -run 'TestBuildDockerRuntimeSpec|TestAgentImagesPrecreateWorkloadSecretDirectory|TestVerifyDocker|TestDockerRuntimeName' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/docker_runtime.go server/agent/docker_runtime_test.go server/agent/runtime_names.go deploy/docker/Dockerfile.agent-article deploy/docker/Dockerfile.agent-seednote deploy/docker/Dockerfile.agent-montage
git commit -m "feat(runtime): define Docker job resources"
```

### Task 6: Implement Docker Dispatch And Volume Lifecycle

**Files:**
- Create: `server/agent/docker_dispatcher.go`
- Create: `server/agent/docker_dispatcher_test.go`
- Modify: `server/service/task_workspace_test.go`

- [ ] **Step 1: Write failing lifecycle tests against a fake Docker Engine**

```go
func TestDockerDispatcherCreatesVolumeContainerSecretAndStarts(t *testing.T) {
    engine := newFakeDockerEngine(t)
    dispatcher := newDockerDispatcherForTest(t, engine.Client())
    identity, err := dispatcher.Dispatch(t.Context(), testExecution(), testTask())
    if err != nil { t.Fatal(err) }
    if identity.Scope != "docker" || identity.InstanceID != engine.ContainerID { t.Fatalf("identity %#v", identity) }
    engine.AssertCalls(t, "volume-create:project", "volume-create:task", "container-create", "archive-copy", "container-start")
}

func TestDockerResumeRequiresExistingTaskVolume(t *testing.T) {
    execution := testExecution(); execution.ParentExecutionID = "parent"
    _, err := newMissingVolumeDispatcher(t).Dispatch(t.Context(), execution, testTask())
    if !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "original task workspace") { t.Fatalf("error %v", err) }
}
```

- [ ] **Step 2: Verify failure**

Run: `go test ./server/agent -run 'TestDockerDispatcher|TestDockerResume' -count=1`

Expected: FAIL because the dispatcher does not exist.

- [ ] **Step 3: Implement dispatch, inspect, delete, and volume ownership**

`NewDockerDispatcher` receives runtime images, Docker config, Server URL, workload-token service, API client, and clock. `Dispatch` validates identities; creates or verifies project/task volumes; creates or verifies the deterministic container; issues a workload token containing the returned container ID; copies a tarred mode-`0400`, UID/GID-`1000` token into `/run/secrets/anban`; starts the container; and returns `model.RuntimeIdentity`.

`Inspect` maps created/running/exited/dead plus exit code into common runtime state. `Delete` identity-checks then stops/removes only the execution container. `DeleteTaskWorkspace` and `DeleteProjectMemory` verify labels before volume removal. Terminal cleanup never removes either volume.

- [ ] **Step 4: Run lifecycle tests**

Run: `go test ./server/agent ./server/service -run 'TestDockerDispatcher|TestDockerResume|TestDockerInspect|TestDockerDelete|TestDeleteTaskWorkspace' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/docker_dispatcher.go server/agent/docker_dispatcher_test.go server/service/task_workspace_test.go
git commit -m "feat(runtime): dispatch one-shot Docker jobs"
```

### Task 7: Generalize Runtime Reconciliation

**Files:**
- Create: `server/agent/runtime_reconciler.go`
- Create: `server/agent/runtime_reconciler_test.go`
- Remove: `server/agent/kubernetes_reconciler.go`
- Remove: `server/agent/kubernetes_reconciler_test.go`
- Modify: `server/service/task_execution_reconcile.go`
- Modify: `server/service/task_execution_complete.go`

- [ ] **Step 1: Port tests to neutral state**

```go
func TestRuntimeReconcilerFinalizesExitedContainer(t *testing.T) {
    dispatcher := &fakeRuntimeDispatcher{states: map[string]*RuntimeExecutionState{
        "execution-1": {Phase: RuntimePhaseFailed, InstanceID: "container-id", Reason: "Error", ExitCode: int32Ptr(1)},
    }}
    svc := newFakeRuntimeReconcileService(testExecution())
    r := NewRuntimeReconciler(dispatcher, svc, RuntimeReconcilerConfig{}, zerolog.Nop())
    if err := r.ReconcileOnce(t.Context()); err != nil { t.Fatal(err) }
    if svc.failureReason != "runtime_failed" || svc.deletedExecution != "execution-1" { t.Fatalf("svc %#v", svc) }
}
```

- [ ] **Step 2: Verify failure**

Run: `go test ./server/agent -run 'TestRuntimeReconciler' -count=1`

Expected: FAIL because only `KubernetesReconciler` exists.

- [ ] **Step 3: Implement common reconciliation**

Move dispatch recovery, heartbeat timeout, completion grace, pre-start replacement, terminal diagnostics, cleanup leases, and cleanup retry into `RuntimeReconciler`. Dispatchers return `ErrRuntimeWorkloadNotFound`; remove Kubernetes API error inspection from the common loop. Rename `RecordExecutionPod` to `RecordExecutionInstance` and persist `RuntimeInstanceID`.

Map deadline and exit 137 to `deadline_exceeded` and `oom_killed`; map any other failed runtime to `runtime_failed`. Keep finalization and billing idempotence unchanged.

- [ ] **Step 4: Run reconciler/finalization tests**

Run: `go test ./server/agent ./server/service -run 'TestRuntimeReconciler|TestReconcileExecution|TestCompleteCloudExecution|TestCancelCloud' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/runtime_reconciler.go server/agent/runtime_reconciler_test.go server/agent/kubernetes_reconciler.go server/agent/kubernetes_reconciler_test.go server/service/task_execution_reconcile.go server/service/task_execution_complete.go
git commit -m "refactor(runtime): reconcile managed workloads uniformly"
```

### Task 8: Wire Managed Runtimes And Remove Host Executors

**Files:**
- Modify: `server/main.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_execution.go`
- Modify: `server/service/local_executor_claim.go`
- Modify: `server/agent/executor.go`
- Remove: `server/agent/docker_executor.go`
- Remove: `server/agent/docker_executor_naming_test.go`
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `server/agent/claude_runtime_env_test.go`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write failing source/wiring contract tests**

```go
for _, forbidden := range []string{
    "NewLocalExecutor(", "NewDockerExecutor(", "ContainerExecCreate(",
    "ANBAN_CLAUDE_DOCKER_CONTAINER_NAME", "ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR", "sleep\", \"infinity",
} {
    if strings.Contains(repositoryText, forbidden) { t.Errorf("removed contract remains: %s", forbidden) }
}
for _, required := range []string{"NewDockerDispatcher", "NewRuntimeReconciler", "SetRuntimeDispatcher"} {
    if !strings.Contains(serverMain, required) { t.Errorf("missing %s", required) }
}
```

- [ ] **Step 2: Verify failure**

Run: `go test ./server/agent ./server/service -run 'TestDockerRuntimeContract|TestManagedRuntimeWiring' -count=1`

Expected: FAIL while old executors remain.

- [ ] **Step 3: Replace wiring and delete host execution**

Always create execution tokens, workload tokens, bootstrap service, one selected dispatcher, common reconciler, task-workspace lifecycle, and project-memory lifecycle. `HandleExecutionFromPayload` always dispatches a managed execution. Preserve desktop claim/complete APIs, but remove Server `LocalExecutor`, `DockerExecutor`, `TaskExecutor`, host `WorkDir` scanning, and unreachable host workspace handling.

Docker Compose removes the Agent service, `depends_on.agent`, workspace mounts, and persistent environment variables. Server keeps only the Docker socket and Agent network.

- [ ] **Step 4: Run Server and Agent package tests**

Run: `go test ./server/... ./agent/... -run 'TestDockerRuntimeContract|TestManagedRuntimeWiring|TestLocalExecutionClaim|TestTask' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/main.go server/service/task.go server/service/task_execution.go server/service/local_executor_claim.go server/agent/executor.go server/agent/docker_executor.go server/agent/docker_executor_naming_test.go server/agent/docker_runtime_contract_test.go server/agent/claude_runtime_env_test.go docker-compose.yml
git commit -m "refactor(runtime): remove host-managed agent execution"
```

### Task 9: Make Runtime Own `output/`

**Files:**
- Create: `agent/main_test.go`
- Modify: `agent/bootstrap.go`
- Modify: `agent/bootstrap_test.go`
- Modify: `agent/bootstrap_unix_test.go`
- Modify: `agent/job.go`
- Modify: `agent/job_test.go`
- Modify: `agent/runner.go`
- Modify: `agent/runner_contract_test.go`
- Modify: `agent/montage_runtime.go`
- Modify: `agent/montage_runtime_test.go`
- Modify: `agent/artifact_upload.go`
- Modify: `agent/artifact_upload_test.go`

- [ ] **Step 1: Write failing output-safety tests**

```go
func TestBootstrapCreatesRuntimeOwnedOutput(t *testing.T) {
    workspace := t.TempDir()
    if err := prepareRuntimeWorkspace(workspace, "article"); err != nil { t.Fatal(err) }
    info, err := os.Lstat(filepath.Join(workspace, "output"))
    if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 { t.Fatalf("output %#v, %v", info, err) }
}

func TestMontageRuntimeLinksCanonicalOutput(t *testing.T) {
    workspace := t.TempDir(); runtime := materializeTestMontageRuntime(t, workspace)
    link, err := os.Readlink(filepath.Join(runtime, "output"))
    if err != nil || link != filepath.Join(workspace, "output") { t.Fatalf("link %q, %v", link, err) }
}

func TestJobArtifactUploaderRejectsSymlinkOutput(t *testing.T) {
    workspace := t.TempDir(); _ = os.Symlink(t.TempDir(), filepath.Join(workspace, "output"))
    _, err := scanWorkspaceArtifacts(t.Context(), workspace, "article")
    if err == nil || !strings.Contains(err.Error(), "real directory") { t.Fatalf("error %v", err) }
}

func TestRunAgentPreparesDesktopWorkspaceOutput(t *testing.T) {
    t.Setenv(homeTemplateEnv, "")
    workspace := t.TempDir()
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
    }))
    defer server.Close()
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    cfg := &Config{ServerURL: server.URL, APIKey: "key", TaskID: "task-1",
        TaskType: "article", Topic: "write", Workspace: workspace, MaxTurns: 1}
    _ = runAgent(ctx, cfg, io.Discard, io.Discard)
    info, err := os.Lstat(filepath.Join(workspace, "output"))
    if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o750 {
        t.Fatalf("desktop output %#v, %v", info, err)
    }
}
```

- [ ] **Step 2: Verify failures**

Run: `go test ./agent -run 'TestBootstrapCreatesRuntimeOwnedOutput|TestMontageRuntimeLinksCanonicalOutput|TestJobArtifactUploaderRejectsSymlinkOutput|TestRunAgentPreparesDesktopWorkspaceOutput' -count=1`

Expected: FAIL because runtime output ownership is incomplete.

- [ ] **Step 3: Implement output preparation**

After bootstrap materialization and before execution, reject a symlink/non-directory at `/workspace/output`, create it with `0750`, and create Montage's `openmontage/output` link to the canonical output. Both managed `BootstrapJob` and standalone/desktop `runAgent` must call the same `prepareRuntimeWorkspace` helper before the runner starts. Normal CWD remains `/workspace`; Montage remains `/workspace/openmontage`. Managed artifact scanning always uses canonical output and never falls back to workspace root.

- [ ] **Step 4: Run Agent tests**

Run: `go test ./agent -run 'TestBootstrap|TestRunAgent|TestRunner|TestMontage|Test.*Artifact' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agent/main_test.go agent/bootstrap.go agent/bootstrap_test.go agent/bootstrap_unix_test.go agent/job.go agent/job_test.go agent/runner.go agent/runner_contract_test.go agent/montage_runtime.go agent/montage_runtime_test.go agent/artifact_upload.go agent/artifact_upload_test.go
git commit -m "refactor(agent): make runtime own task output"
```

### Task 10: Add Provider-Neutral Streaming Artifact Upload

**Files:**
- Create: `server/service/task_artifact_stream.go`
- Create: `server/service/task_artifact_stream_test.go`
- Create: `server/handler/agent_artifact_stream.go`
- Create: `server/handler/agent_artifact_stream_test.go`
- Modify: `server/handler/agent.go`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/main.go`
- Modify: `server/router/router.go`
- Modify: `server/router/router_test.go`
- Modify: `agent/artifact_upload.go`
- Modify: `agent/artifact_upload_test.go`
- Modify: `agent/reporter.go`
- Modify: `agent/reporter_test.go`

- [ ] **Step 1: Write failing local-provider tests**

```go
func TestStreamTaskArtifactUploadsWithoutOSS(t *testing.T) {
    body := strings.NewReader("artifact-body"); sum := sha256.Sum256([]byte("artifact-body"))
    got, err := svc.StreamTaskArtifact(ctx, task.ID, task.UserID, execution.ID, TaskArtifactStreamRequest{
        RelativePath: "final.md", ContentType: "text/markdown", Size: int64(body.Len()), SHA256: hex.EncodeToString(sum[:]), Body: body,
    })
    if err != nil || got.ObjectKey == "" { t.Fatalf("got %#v, %v", got, err) }
}

func TestRouterEnablesRequestBodyStreaming(t *testing.T) {
    app := NewRouter(&Services{Config: &config.Config{}})
    if !app.Config().StreamRequestBody { t.Fatal("StreamRequestBody must be enabled") }
}
```

Add table cases for `../secret`, excess size, short body, long body, and digest mismatch; all must fail and delete any staged object.

- [ ] **Step 2: Verify failures**

Run: `go test ./server/service ./server/handler ./server/router ./agent -run 'TestStreamTaskArtifact|TestRouterEnablesRequestBodyStreaming|TestReporterStreamsArtifact|TestArtifactUploaderUsesProviderMode' -count=1`

Expected: FAIL because the stream endpoint/client are absent.

- [ ] **Step 3: Implement bounded streaming and transport selection**

Enable `StreamRequestBody: true` in the main `fiber.Config` in `server/router/router.go`. Add execution-authenticated `POST /api/v1/agent/artifacts/content`; its handler passes `c.Request().BodyStream()` directly into the service and must not call `c.Body()`. Validate normalized relative path, declared size, SHA-256, task, and execution. Stream through `io.LimitReader` plus `io.TeeReader` into `storage.Provider.Upload`; never buffer the entire file. Delete a stored object on final validation failure.

Bootstrap returns `artifact_transport.mode` as `direct` for OSS and `stream` otherwise. Agent uses existing direct upload for `direct`, HTTP streaming for `stream`, then submits the same atomic manifest.

- [ ] **Step 4: Run artifact tests**

Run: `go test ./server/service ./server/handler ./server/router ./agent -run 'Test.*Artifact|TestStreamTaskArtifact|TestRouterEnablesRequestBodyStreaming|TestReporter' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/task_artifact_stream.go server/service/task_artifact_stream_test.go server/handler/agent_artifact_stream.go server/handler/agent_artifact_stream_test.go server/handler/agent.go server/service/agent_bootstrap.go server/main.go server/router/router.go server/router/router_test.go agent/artifact_upload.go agent/artifact_upload_test.go agent/reporter.go agent/reporter_test.go
git commit -m "feat(runtime): stream artifacts through any storage provider"
```

### Task 11: Remove Workspace MCP And Add The Prohibition Contract

**Files:**
- Remove: `server/mcp/workspace_tools.go`
- Remove: `server/mcp/workspace_tools_test.go`
- Remove: `server/service/workspace.go`
- Remove: `server/service/workspace_test.go`
- Modify: `server/mcp/tools.go`
- Modify: `server/main.go`
- Modify: `server/agent/runtime_policy.go`
- Modify: `server/agent/runtime_policy_test.go`
- Modify: `server/agent/server_workspace_contract_test.go`
- Create: `server/agent/runtime_plugin_workspace_contract_test.go`

- [ ] **Step 1: Replace positive workspace tests with a failing prohibition test**

```go
func TestPluginAssetsDoNotControlManagedWorkspaceDirectories(t *testing.T) {
    root := filepath.Join(repoRoot(t), "plugins")
    forbidden := []*regexp.Regexp{
        regexp.MustCompile(`\bprepare_workspace\b`),
        regexp.MustCompile(`\$DIR\b`),
        regexp.MustCompile(`mkdir\s+-p[^\n]*(output|DIR)`),
        regexp.MustCompile(`\.task-context`),
        regexp.MustCompile(`CWD[^\n]*(TASK_ID|任务 ID)`),
    }
    err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
        if err != nil || entry.IsDir() || !ownedPluginWorkflowFile(path) { return err }
        body, readErr := os.ReadFile(path); if readErr != nil { return readErr }
        for _, pattern := range forbidden { if pattern.Match(body) { t.Errorf("%s contains %s", path, pattern) } }
        return nil
    })
    if err != nil { t.Fatal(err) }
}
```

Also assert the MCP tool list does not contain `prepare_workspace` and runtime policy does not name it.

- [ ] **Step 2: Verify failures**

Run: `go test ./server/mcp ./server/service ./server/agent -run 'TestPluginAssetsDoNotControlManagedWorkspaceDirectories|Test.*Workspace|TestRuntimePolicy' -count=1`

Expected: FAIL on tool registration and plugin content.

- [ ] **Step 3: Delete Server workspace APIs and invert old contracts**

Delete tool/service files and wiring. Replace positive `$DIR` expectations in `server_workspace_contract_test.go` with explicit `output/<filename>` requirements for Article, Seednote, Moments, Ecommerce, Designer, Montage, and Live Slicer. Keep the repository-wide prohibition test failing only on plugin submodule content for the next task.

- [ ] **Step 4: Run Server-side tests**

Run: `go test ./server/mcp ./server/service ./server/agent -run 'TestPluginAssetsDoNotControlManagedWorkspaceDirectories|TestShippedWorkflows|TestRuntimePolicy' -count=1`

Expected: compilation succeeds; the prohibition test still reports plugin files.

- [ ] **Step 5: Commit the parent-side contract**

```bash
git add server/mcp/workspace_tools.go server/mcp/workspace_tools_test.go server/service/workspace.go server/service/workspace_test.go server/mcp/tools.go server/main.go server/agent/runtime_policy.go server/agent/runtime_policy_test.go server/agent/server_workspace_contract_test.go server/agent/runtime_plugin_workspace_contract_test.go
git commit -m "refactor(mcp): remove workspace directory tool"
```

### Task 12: Rewrite Plugin Workflows Around `output/`

**Files:**
- Modify: `plugins/agents/article.md`
- Modify: `plugins/agents/article.toml`
- Modify: `plugins/agents/seednote.md`
- Modify: `plugins/agents/seednote.toml`
- Modify: `plugins/agents/ecommerce.md`
- Modify: `plugins/agents/ecommerce.toml`
- Modify: `plugins/agents/moments.md`
- Modify: `plugins/agents/moments.toml`
- Modify: `plugins/agents/designer.md`
- Modify: `plugins/agents/designer.toml`
- Modify: `plugins/agents/montage.md`
- Modify: `plugins/agents/montage.toml`
- Modify: `plugins/agents/live-slicer.md`
- Modify: `plugins/agents/live-slicer.toml`
- Modify: the exact Skill/reference paths in Appendix A
- Modify: `plugins/hooks/seednote-quality-gate.sh`
- Modify: `plugins/CODEX.md`
- Modify: `plugins/README.md`
- Modify: `plugins/docs/plugin-development.md`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `plugins/CHANGELOG.md`

- [ ] **Step 1: Capture the exact failing plugin surface**

Run:

```bash
rg -l '\$DIR|prepare_workspace|\.task-context|mkdir\s+-p[^\n]*(output|DIR)' \
  plugins/agents plugins/skills plugins/hooks \
  plugins/CODEX.md plugins/README.md plugins/docs/plugin-development.md \
  --glob '!skills/humanizer/**' | sort
go test ./server/agent -run 'TestPluginAssetsDoNotControlManagedWorkspaceDirectories|TestShippedWorkflows' -count=1
```

Expected: the scan exactly matches Appendix A and tests fail on each forbidden contract. Do not modify `plugins/skills/humanizer/README.md`: it belongs to the nested upstream submodule and its generic installation `mkdir -p` is outside the managed task-output contract.

- [ ] **Step 2: Rewrite every Claude/Codex Agent pair**

Use this exact invariant:

```text
The managed runtime provides a task-private workspace and a pre-created output/
directory. Write final and resume-critical artifacts to the explicit
output/<filename> paths below. Do not create, discover, move, or rename the
output directory. TASK_ID is supplied by structured runtime context.
```

Replace `$DIR/name` with `output/name`; remove workspace preparation stages and task-ID inference; retain all MCP, content, quality, failure-state, and delivery validation behavior. Montage keeps its OpenMontage project-root CWD and writes through the runtime-provided `output` link.

- [ ] **Step 3: Rewrite Skills/hooks/docs and release the plugin contract**

Skills retain artifact filenames and validation but no directory lifecycle. Seednote hook names missing `output` files. Docs state runtime ownership. Bump both manifests from `3.0.1` to `3.1.0` and add a changelog entry for removing `prepare_workspace`.

- [ ] **Step 4: Run plugin and parent contract tests**

Run:

```bash
git -C plugins diff --check
rg -n '\$DIR|prepare_workspace|\.task-context|mkdir\s+-p[^\n]*(output|DIR)' \
  plugins/agents plugins/skills plugins/hooks \
  plugins/CODEX.md plugins/README.md plugins/docs/plugin-development.md \
  --glob '!skills/humanizer/**'
go test ./server/agent ./server/mcp -run 'TestPluginAssetsDoNotControlManagedWorkspaceDirectories|TestShippedWorkflows|Test.*Skill|Test.*Plugin' -count=1
```

Expected: `rg` returns no matches and tests PASS.

- [ ] **Step 5: Commit/publish plugin, then parent pointer**

```bash
git -C plugins add agents skills hooks CODEX.md README.md docs/plugin-development.md .claude-plugin/plugin.json .codex-plugin/plugin.json CHANGELOG.md
git -C plugins commit -m "refactor!: make runtime own workflow output"
git -C plugins push origin HEAD:main
git add plugins
git commit -m "build: update runtime-owned plugin workflows"
```

The plugin push must succeed before the parent repository is pushed.

### Task 13: Update Deployment Contracts And Documentation

**Files:**
- Modify: `server/Deployment.yaml`
- Modify: `server/k8s_agent_runtime_test.go`
- Modify: `docker-compose.yml`
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/montage-upgrade.md`
- Remove: `docs/superpowers/specs/2026-04-15-persistent-docker-executor-design.md`

- [ ] **Step 1: Write failing deployment assertions**

```go
required := []string{"ANBAN_AGENT_EXECUTOR", "ANBAN_AGENT_IMAGE_ARTICLE", "ANBAN_AGENT_IMAGE_SEEDNOTE", "ANBAN_AGENT_IMAGE_MONTAGE", "ANBAN_AGENT_EXECUTION_TOKEN_SECRET"}
forbidden := []string{"ANBAN_CLAUDE_DOCKER_CONTAINER_NAME", "ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR", "ANBAN_CLAUDE_DOCKER_ARTICLE_IMAGE", "ANBAN_ARTICLE_AGENT_IMAGE", "article_image:", "image_profiles:"}
```

Assert every required term exists in its relevant deployment and every forbidden term is absent from current configs/docs.

- [ ] **Step 2: Verify failure**

Run: `go test ./server ./server/agent -run 'TestKubernetesAgentRuntime|TestDockerRuntimeContract' -count=1`

Expected: FAIL on old environment/YAML contracts.

- [ ] **Step 3: Update templates and docs**

Docker Compose sets shared runtime images and Docker scheduler config with no Agent service/workspace mount. Kubernetes maps its three image parameters into the shared names while retaining namespace/PVC/ServiceAccount config. Update `docs/montage-upgrade.md` to the shared `ANBAN_AGENT_IMAGE_*` names and `/workspace/openmontage` runtime-owned output contract. Document that Server never builds a missing image and every execution receives a new container. Remove the superseded persistent-executor design document. Do not rewrite historical plans or the 2026-07-17 design merely because they record the superseded configuration at that point in history.

- [ ] **Step 4: Run deployment tests**

Run: `go test ./server ./server/agent -run 'TestKubernetesAgentRuntime|TestDockerRuntimeContract' -count=1 && git diff --check`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/Deployment.yaml server/k8s_agent_runtime_test.go docker-compose.yml server/agent/docker_runtime_contract_test.go Makefile README.md AGENTS.md docs/montage-upgrade.md docs/superpowers/specs/2026-04-15-persistent-docker-executor-design.md
git commit -m "docs(runtime): document live managed dispatch"
```

### Task 14: Add A Real Docker Runtime Smoke Test

**Files:**
- Create: `deploy/docker/runtime-smoke.sh`
- Create: `deploy/docker/runtime-smoke_test.go`
- Modify: `Makefile`
- Modify: `server/agent/docker_dispatcher_test.go`

- [ ] **Step 1: Write the failing script contract test**

```go
func TestRuntimeSmokeCoversDispatchAndResume(t *testing.T) {
    body, err := os.ReadFile("runtime-smoke.sh"); if err != nil { t.Fatal(err) }
    for _, term := range []string{"docker compose up", "creator-agent-article", "creator-agent-seednote", "creator-agent-montage", "runtime_workload", "output/", "resume", "docker volume inspect", "docker container inspect"} {
        if !strings.Contains(string(body), term) { t.Errorf("missing %q", term) }
    }
}
```

- [ ] **Step 2: Verify failure**

Run: `go test ./deploy/docker -run TestRuntimeSmokeCoversDispatchAndResume -count=1`

Expected: FAIL because the script is absent.

- [ ] **Step 3: Implement bounded smoke workflow**

The `set -euo pipefail` script uses a temporary Compose project, builds all runtime images, starts infrastructure, submits one controlled task per profile, polls execution with a bounded deadline, verifies persisted runtime identity/output, resumes one task, verifies volume reuse/inherited image, confirms terminal containers are removed, and cleans only its labeled resources.

Add:

```make
.PHONY: docker-runtime-smoke
docker-runtime-smoke: docker-agent-image docker-seednote-agent-image docker-montage-agent-image
	@bash deploy/docker/runtime-smoke.sh
```

Only unavailable Docker may produce an explicit exit-0 skip. Missing configuration or failed runtime behavior is an error.

- [ ] **Step 4: Run contract and smoke tests**

Run:

```bash
go test ./deploy/docker ./server/agent -run 'TestRuntimeSmoke|TestDockerDispatcher' -count=1
make docker-runtime-smoke
```

Expected: unit tests PASS; smoke PASS or the single Docker-unavailable skip.

- [ ] **Step 5: Commit**

```bash
git add deploy/docker/runtime-smoke.sh deploy/docker/runtime-smoke_test.go Makefile server/agent/docker_dispatcher_test.go
git commit -m "test(runtime): cover Docker dispatch end to end"
```

### Task 15: Full Verification And Release Readiness

**Files:**
- Modify: only files required to fix failures caused by Tasks 1-14.

- [ ] **Step 1: Scan forbidden compatibility remnants**

Run:

```bash
rg -n 'NewLocalExecutor|NewDockerExecutor|ContainerExecCreate|prepare_workspace|\$DIR|ANBAN_CLAUDE_DOCKER_CONTAINER_NAME|ANBAN_CLAUDE_DOCKER_WORKSPACE_DIR|article_image:|image_profiles:' server agent plugins docker-compose.yml README.md AGENTS.md deploy
```

Expected: no live-code/current-document matches. Migration or historical assertions must be explicitly scoped by their test.

- [ ] **Step 2: Format and statically verify**

Run:

```bash
gofmt -w agent server deploy/docker/*.go
go vet ./...
git diff --check
```

Expected: PASS.

- [ ] **Step 3: Run all tests and builds**

Run:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: PASS and both binaries exist.

- [ ] **Step 4: Build all images and run smoke**

Run:

```bash
make docker-agent-image
make docker-seednote-agent-image
make docker-montage-agent-image
make docker-runtime-smoke
```

Expected: images build and smoke verifies dispatch, bootstrap, artifact publication, cleanup, and resume-volume reuse.

- [ ] **Step 5: Prove repository and submodule state**

Run:

```bash
git status --short
git -C plugins status --short
git log --oneline --decorate -15
```

Expected: both worktrees clean, parent records the published plugin commit, and planned commits are present. If a scoped verification correction was required, stage its explicit files and commit with `fix(runtime): resolve final dispatch verification`. Do not push parent until the plugin commit is remotely reachable and the managed-submodule pre-push check passes.

## Appendix A: Exact Plugin Workflow Files Currently Affected

The Task 12 pre-change scan currently returns these 53 paths. Re-run the scan before editing; any added match must be reviewed against the same managed-output rule, while the nested `plugins/skills/humanizer` submodule remains excluded.

```text
plugins/CODEX.md
plugins/README.md
plugins/agents/article.md
plugins/agents/article.toml
plugins/agents/designer.md
plugins/agents/designer.toml
plugins/agents/ecommerce.md
plugins/agents/ecommerce.toml
plugins/agents/live-slicer.md
plugins/agents/live-slicer.toml
plugins/agents/moments.md
plugins/agents/moments.toml
plugins/agents/montage.md
plugins/agents/montage.toml
plugins/agents/seednote.md
plugins/agents/seednote.toml
plugins/docs/plugin-development.md
plugins/hooks/seednote-quality-gate.sh
plugins/skills/article-cover-design/SKILL.md
plugins/skills/article-publishing/SKILL.md
plugins/skills/article-viral-strategy/SKILL.md
plugins/skills/article-viral-strategy/references/viral-audit.md
plugins/skills/article-visual-design/SKILL.md
plugins/skills/article-visual-design/references/content.md
plugins/skills/article-visual-design/references/cover.md
plugins/skills/article/SKILL.md
plugins/skills/content-writing/SKILL.md
plugins/skills/content-writing/references/writing-guide.md
plugins/skills/ecommerce-copywriting/SKILL.md
plugins/skills/ecommerce-platform-specs/SKILL.md
plugins/skills/ecommerce-product-analysis/SKILL.md
plugins/skills/ecommerce-visual-design/SKILL.md
plugins/skills/ecommerce/SKILL.md
plugins/skills/line-art-coloring/SKILL.md
plugins/skills/line-art-coloring/references/verification.md
plugins/skills/live-slice/SKILL.md
plugins/skills/moments/SKILL.md
plugins/skills/portrait-pose-variants/SKILL.md
plugins/skills/portrait-pose-variants/references/consistency-audit.md
plugins/skills/portrait-pose-variants/references/identity-lock-template.md
plugins/skills/portrait-pose-variants/references/pose-templates.md
plugins/skills/seednote-research/SKILL.md
plugins/skills/seednote-viral-analysis/SKILL.md
plugins/skills/seednote-visual-design/SKILL.md
plugins/skills/seednote-visual-design/references/content.md
plugins/skills/seednote-writing/SKILL.md
plugins/skills/seo-optimization/SKILL.md
plugins/skills/short-video-cover/SKILL.md
plugins/skills/short-video-cover/references/analysis-template.md
plugins/skills/short-video-cover/references/optimization-checklist.md
plugins/skills/topic-research/SKILL.md
plugins/skills/topic-research/references/scoring-guide.md
plugins/skills/writers/references/writer-style-schema.md
```
