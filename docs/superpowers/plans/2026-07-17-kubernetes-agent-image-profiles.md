# Kubernetes Agent Image Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Select immutable Agent images by task type and persist the selected runtime profile and image through execution and resume lineage.

**Architecture:** Kubernetes configuration owns a default image plus task-type mappings. `TaskService` resolves and persists the selection before dispatch; Job construction consumes the persisted image rather than mutable current configuration. Resume and pre-start replacement attempts copy the parent selection and fail closed when it is absent or inconsistent.

**Tech Stack:** Go, GORM, Kubernetes batch/v1 Jobs, YAML configuration, table-driven tests

---

### Task 1: Add image profile configuration

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config/kubernetes_config_test.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`
- Modify: `server/Deployment.yaml`

- [ ] **Step 1: Write failing resolver tests**

```go
func TestKubernetesImageForTaskUsesProfileThenDefault(t *testing.T) {
    cfg := KubernetesConfig{
        AgentImage: "registry/content@sha256:default",
        ImageProfiles: map[string]string{
            model.PlatformMontage: "registry/montage@sha256:montage",
        },
    }
    if got := cfg.ImageForTask(model.PlatformMontage); got.Profile != "montage" || got.Image != "registry/montage@sha256:montage" {
        t.Fatalf("montage runtime = %#v", got)
    }
    if got := cfg.ImageForTask(model.PlatformSeednote); got.Profile != "content" || got.Image != "registry/content@sha256:default" {
        t.Fatalf("seednote runtime = %#v", got)
    }
}
```

Add validation cases for an empty mapped image and an unsupported task key.

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./server/config -run 'TestKubernetesImageForTask|TestValidateKubernetesImageProfiles' -count=1
```

Expected: FAIL because `ImageProfiles`, `RuntimeImageSelection`, and `ImageForTask` do not exist.

- [ ] **Step 3: Implement the resolver**

```go
type KubernetesConfig struct {
    // existing fields
    AgentImage    string            `yaml:"agent_image"`
    ImageProfiles map[string]string `yaml:"image_profiles"`
}

type RuntimeImageSelection struct {
    Profile string
    Image   string
}

func (c KubernetesConfig) ImageForTask(taskType string) RuntimeImageSelection {
    taskType = strings.TrimSpace(taskType)
    if image := strings.TrimSpace(c.ImageProfiles[taskType]); image != "" {
        return RuntimeImageSelection{Profile: taskType, Image: image}
    }
    return RuntimeImageSelection{Profile: "content", Image: strings.TrimSpace(c.AgentImage)}
}
```

Validation accepts only model platform constants and requires a non-empty mapped image.

- [ ] **Step 4: Wire deployment configuration**

Document `ANBAN_AGENT_IMAGE` as the content default and add:

```yaml
image_profiles:
  montage: "${ANBAN_MONTAGE_AGENT_IMAGE}"
```

Add `ANBAN_MONTAGE_AGENT_IMAGE` to the server Deployment environment.

- [ ] **Step 5: Run tests and commit**

```bash
go test ./server/config -count=1
git add server/config/config.go server/config/kubernetes_config_test.go server/config.yaml server/config.example.yaml server/Deployment.yaml
git commit -m "feat(config): add task agent image profiles"
```

Expected: tests PASS and commit succeeds.

### Task 2: Persist runtime identity on TaskExecution

**Files:**
- Modify: `server/model/task_execution.go`
- Modify: `server/model/task_execution_test.go`
- Modify: `server/agent/kubernetes_dispatcher.go`
- Modify: `server/service/task_dispatch.go`
- Modify: `server/service/task_execution_reconcile.go`
- Modify: `server/service/task_execution_decouple_test.go`
- Modify: `server/service/task_execution_complete_test.go`

- [ ] **Step 1: Write failing lineage tests**

Assert initial execution resolution, resume reuse, and pre-start replacement copying:

```go
if execution.RuntimeProfile != "montage" || execution.RuntimeImage != "registry/montage@sha256:abc" {
    t.Fatalf("runtime identity = %q %q", execution.RuntimeProfile, execution.RuntimeImage)
}
```

For resume, mutate current configuration after the parent completes and assert the child still stores the parent's values.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
go test ./server/service -run 'TestCreateCurrentExecutionPersistsRuntimeImage|TestResumeExecutionReusesParentRuntimeImage|TestReplacePreStartExecutionPreservesRuntimeImage' -count=1
```

Expected: compile failure because model fields and the resolver interface are missing.

- [ ] **Step 3: Add model fields**

```go
RuntimeProfile string `gorm:"type:varchar(40)" json:"runtime_profile,omitempty"`
RuntimeImage   string `gorm:"type:varchar(512)" json:"runtime_image,omitempty"`
```

GORM AutoMigrate adds nullable-compatible columns for existing rows.

- [ ] **Step 4: Expose runtime selection through the dispatcher**

Extend `agent.KubernetesDispatcher`:

```go
ResolveRuntime(taskType string) srvconfig.RuntimeImageSelection
```

Implement it on `kubernetesJobDispatcher` by delegating to `d.config.ImageForTask(taskType)`. Update dispatcher fakes in service tests with deterministic selections.

- [ ] **Step 5: Resolve or inherit in createCurrentExecution**

Change `resumeExecutionLineage` to return the parent execution as well as SessionID. Initial executions call `ResolveRuntime`; resume executions copy non-empty `RuntimeProfile` and `RuntimeImage` from the terminal parent. Old parent rows without runtime identity fail with an explicit resume error rather than using current configuration.

- [ ] **Step 6: Preserve fields in pre-start replacement**

```go
replacement = &model.TaskExecution{
    // existing identity
    RuntimeProfile: current.RuntimeProfile,
    RuntimeImage:   current.RuntimeImage,
}
```

- [ ] **Step 7: Run tests and commit**

```bash
go test ./server/model ./server/service ./server/agent -count=1
git add server/model/task_execution.go server/model/task_execution_test.go server/agent/kubernetes_dispatcher.go server/service/task_dispatch.go server/service/task_execution_reconcile.go server/service/task_execution_decouple_test.go server/service/task_execution_complete_test.go
git commit -m "feat(agent): persist execution runtime images"
```

Expected: all selected packages PASS.

### Task 3: Build Jobs from persisted runtime identity

**Files:**
- Modify: `server/agent/kubernetes_job.go`
- Modify: `server/agent/kubernetes_dispatcher.go`
- Modify: `server/agent/kubernetes_executor_test.go`

- [ ] **Step 1: Write the failing Job test**

```go
execution := testExecution()
execution.RuntimeImage = "registry.example.com/montage@sha256:run"
job := buildKubernetesJob(testJobConfig(), execution, testTask())
if job.Spec.Template.Spec.InitContainers[0].Image != execution.RuntimeImage ||
    job.Spec.Template.Spec.Containers[0].Image != execution.RuntimeImage {
    t.Fatalf("job did not use persisted runtime image")
}
```

- [ ] **Step 2: Run and verify failure**

```bash
go test ./server/agent -run TestBuildKubernetesJobUsesPersistedRuntimeImage -count=1
```

Expected: FAIL because both containers use `cfg.AgentImage`.

- [ ] **Step 3: Use and validate the persisted image**

`buildKubernetesJob` assigns `execution.RuntimeImage` to init and main containers. Dispatcher validation rejects blank runtime identity. Initial attempts must match the current configured selection; resume attempts are allowed to use their persisted parent digest.

- [ ] **Step 4: Verify config hashing**

Construct otherwise identical Jobs with two runtime images and assert different `anban.ai/config-hash` annotations.

- [ ] **Step 5: Run verification and commit**

```bash
go test ./server/agent ./server/service ./server/config ./server/model -count=1
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
git diff --check
git add server/agent/kubernetes_job.go server/agent/kubernetes_dispatcher.go server/agent/kubernetes_executor_test.go
git commit -m "feat(agent): route jobs by runtime image"
```

Expected: all commands exit 0.
