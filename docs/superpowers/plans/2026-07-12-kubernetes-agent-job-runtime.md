# Kubernetes Agent Job Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the reusable Kubernetes Agent Pod and `pods/exec` runtime with one isolated Job per execution attempt, project-scoped NAS memory, ephemeral workspaces, and attempt-scoped private OSS results.

**Architecture:** Keep local and Docker execution on the synchronous `TaskExecutor` interface, and introduce a separate asynchronous Kubernetes dispatcher backed by durable `TaskExecution` rows. Agent Jobs bootstrap through workload identity, use a project memory PVC plus `emptyDir`, upload attempt-scoped artifacts directly to OSS, and finalize through an idempotent execution callback while a Kubernetes reconciler handles infrastructure failures.

**Tech Stack:** Go 1.26, Fiber v3, GORM, MySQL/SQLite tests, client-go v0.36, Kubernetes `batch/v1` Jobs and PVCs, Kubernetes TokenReview, JWT v5, OSS STS direct upload, Asynq, Redis, table-driven Go tests.

---

## File Structure

Create focused files instead of expanding the existing 700-line Kubernetes executor:

- `server/model/task_execution.go`: attempt lifecycle and terminal diagnostics.
- `server/repository/task_execution.go`: execution persistence and guarded transitions.
- `server/agent/kubernetes_job.go`: pure Job/PVC construction and deterministic names.
- `server/agent/kubernetes_dispatcher.go`: Kubernetes create/delete/inspect operations.
- `server/agent/kubernetes_identity.go`: TokenReview and bound Pod/Job verification.
- `server/auth/execution_token.go`: short-lived attempt-scoped JWTs.
- `server/service/task_dispatch.go`: database attempt creation plus Job dispatch.
- `server/service/agent_bootstrap.go`: bootstrap contract and signed inputs.
- `server/service/task_execution_complete.go`: cloud completion and terminal CAS.
- `server/agent/kubernetes_reconciler.go`: Job-to-execution reconciliation.
- `agent/bootstrap.go`: Agent-side bootstrap client and safe workspace materialization.
- `agent/job.go`: one-shot Job command.

Delete `server/agent/kubernetes_executor.go` after its useful pure builders have
been replaced. Keep `TaskExecutor` unchanged for local and Docker paths.

### Task 1: Add Durable Execution Attempts

**Files:**
- Create: `server/model/task_execution.go`
- Create: `server/repository/task_execution.go`
- Modify: `server/model/task.go`
- Modify: `server/model/model.go`
- Modify: `server/repository/repository.go`
- Test: `server/model/task_execution_test.go`
- Test: `server/repository/task_execution_test.go`

- [ ] **Step 1: Write failing model and repository tests**

```go
func TestTaskExecutionMigrationAndCurrentAttempt(t *testing.T) {
	db := openTaskExecutionTestDB(t)
	if err := AutoMigrate(db); err != nil { t.Fatal(err) }
	if !db.Migrator().HasTable(&TaskExecution{}) { t.Fatal("task_executions missing") }
	if !db.Migrator().HasColumn(&Task{}, "CurrentExecutionID") { t.Fatal("current_execution_id missing") }
}

func TestTaskExecutionRepositoryTransitionIsCAS(t *testing.T) {
	repo := setupTaskExecutionRepository(t)
	exec := seedTaskExecution(t, repo, model.TaskExecutionStarting)
	won, err := repo.TaskExecutions().Transition(context.Background(), exec.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionRunning,
		model.ExecutionTransition{Started: true})
	if err != nil || !won { t.Fatalf("first transition = %v, %v", won, err) }
	won, err = repo.TaskExecutions().Transition(context.Background(), exec.ID,
		[]string{model.TaskExecutionStarting}, model.TaskExecutionFailed,
		model.ExecutionTransition{TerminalReason: "late"})
	if err != nil || won { t.Fatalf("stale transition = %v, %v", won, err) }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/model ./server/repository -run 'TaskExecution' -count=1`

Expected: FAIL because `TaskExecution`, `CurrentExecutionID`, and `TaskExecutions()` do not exist.

- [ ] **Step 3: Add the execution model and statuses**

```go
type TaskExecution struct {
	ID             string         `gorm:"type:char(36);primaryKey" json:"id"`
	TaskID         string         `gorm:"type:char(36);uniqueIndex:idx_task_attempt,priority:1;index;not null" json:"task_id"`
	Attempt        int            `gorm:"uniqueIndex:idx_task_attempt,priority:2;not null" json:"attempt"`
	Target         string         `gorm:"type:varchar(20);not null" json:"target"`
	Status         string         `gorm:"type:varchar(20);index;not null" json:"status"`
	Namespace      string         `gorm:"type:varchar(63)" json:"namespace,omitempty"`
	JobName        string         `gorm:"type:varchar(63);index" json:"job_name,omitempty"`
	PodUID         string         `gorm:"type:varchar(64)" json:"pod_uid,omitempty"`
	Started        bool           `gorm:"default:false;not null" json:"started"`
	ManifestStatus string         `gorm:"type:varchar(20);default:''" json:"manifest_status,omitempty"`
	TerminalReason string         `gorm:"type:varchar(80);default:''" json:"terminal_reason,omitempty"`
	Diagnostics    datatypes.JSON `gorm:"type:json" json:"diagnostics,omitempty"`
	LastHeartbeatAt *time.Time    `json:"last_heartbeat_at,omitempty"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

const (
	TaskExecutionCreated = "created"
	TaskExecutionDispatching = "dispatching"
	TaskExecutionStarting = "starting"
	TaskExecutionRunning = "running"
	TaskExecutionSucceeded = "succeeded"
	TaskExecutionFailed = "failed"
	TaskExecutionCancelled = "cancelled"
	TaskExecutionTimedOut = "timed_out"
)

type ExecutionTransition struct {
	Started bool
	PodUID string
	ManifestStatus string
	TerminalReason string
	Diagnostics datatypes.JSON
}
```

Add `CurrentExecutionID *string` with an index to `Task`, and include
`TaskExecution` in `AutoMigrate`.

- [ ] **Step 4: Add repository operations**

```go
type TaskExecutionRepository interface {
	Create(ctx context.Context, execution *model.TaskExecution) error
	FindByID(ctx context.Context, id string) (*model.TaskExecution, error)
	FindCurrentByTaskID(ctx context.Context, taskID string) (*model.TaskExecution, error)
	FindReconcilable(ctx context.Context, before time.Time, limit int) ([]*model.TaskExecution, error)
	SetRuntimeIdentity(ctx context.Context, id, namespace, jobName, podUID string) error
	UpdateHeartbeat(ctx context.Context, id string, now time.Time) error
	Transition(ctx context.Context, id string, from []string, to string, change model.ExecutionTransition) (bool, error)
}
```

Implement `Transition` as one `UPDATE ... WHERE id=? AND status IN ?`; terminal
changes set `completed_at` in the same statement. Wire the repository into both
normal and transaction-backed repository aggregates.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./server/model ./server/repository -run 'TaskExecution' -count=1`

Expected: PASS.

```bash
git add server/model/task.go server/model/task_execution.go server/model/model.go server/model/task_execution_test.go server/repository/repository.go server/repository/task_execution.go server/repository/task_execution_test.go
git commit -m "feat(server): add durable task execution attempts"
```

### Task 2: Add Kubernetes Job Configuration Alongside Pod Configuration

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`
- Modify: `server/config/kubernetes_config_test.go`
- Modify: `docs/superpowers/plans/2026-07-12-kubernetes-agent-job-runtime.md`

- [ ] **Step 1: Add Job config default and validation assertions**

```go
func TestKubernetesJobRuntimeDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()
	if cfg.Claude.Kubernetes.Namespace != "default" { t.Fatal("namespace") }
	if cfg.Claude.Kubernetes.MemorySize != "1Gi" { t.Fatal("memory size") }
	if cfg.Claude.Kubernetes.ActiveDeadlineSeconds != 3600 { t.Fatal("active deadline") }
	if cfg.Claude.Kubernetes.CompletionGraceSeconds != 30 { t.Fatal("completion grace") }
	if cfg.Claude.Kubernetes.TTLSecondsAfterFinished != 600 { t.Fatal("job ttl") }
	if cfg.Claude.Kubernetes.PreStartRetryLimit != 1 { t.Fatal("pre-start retry limit") }
}
```

Add these cases to `TestValidateKubernetesJobRuntimeRequirements`:

```go
{
	name: "zero memory size",
	mutate: func(cfg *Config) { cfg.Claude.Kubernetes.MemorySize = "0" },
	wantErr: "claude.kubernetes.memory_size must be positive",
},
{
	name: "negative memory size",
	mutate: func(cfg *Config) { cfg.Claude.Kubernetes.MemorySize = "-1Gi" },
	wantErr: "claude.kubernetes.memory_size must be positive",
},
{
	name: "negative completion grace",
	mutate: func(cfg *Config) { cfg.Claude.Kubernetes.CompletionGraceSeconds = -1 },
	wantErr: "claude.kubernetes.completion_grace_seconds must not be negative",
},
{
	name: "negative pre-start retry limit",
	mutate: func(cfg *Config) { cfg.Claude.Kubernetes.PreStartRetryLimit = -1 },
	wantErr: "claude.kubernetes.pre_start_retry_limit must not be negative",
},
```

Keep the existing invalid-quantity case and add an explicit valid case with
both completion grace and pre-start retries set to zero. Add a real `NewConfig`
test with both YAML keys explicitly set to `0` and assert they remain zero after
loading and defaults. Keep
`memory_storage_class` explicit in YAML and valid fixtures, and prove omission
survives defaults:

```go
func TestValidateKubernetesRequiresExplicitMemoryStorageClass(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.MemoryStorageClass = ""
	cfg.applyDefaults()
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes.memory_storage_class is required") {
		t.Fatalf("Validate() error = %v", err)
	}
}
```

- [ ] **Step 2: Run config tests and verify RED**

Run: `go test ./server/config -run 'Kubernetes' -count=1`

Expected: FAIL because the new fields and validation are absent.

- [ ] **Step 3: Extend `KubernetesConfig` additively**

```go
type KubernetesConfig struct {
	Namespace                 string                   `yaml:"namespace"`
	AgentImage                string                   `yaml:"agent_image"`
	ServiceAccount            string                   `yaml:"service_account"`
	ImagePullSecret           string                   `yaml:"image_pull_secret"`
	WorkspaceMountPath        string                   `yaml:"workspace_mount_path"`
	WorkspacePVCName          string                   `yaml:"workspace_pvc_name"`
	PodRevision               string                   `yaml:"pod_revision"`
	PodTTLSeconds             int                      `yaml:"pod_ttl_seconds"`
	ExecTimeoutSec            int                      `yaml:"exec_timeout_seconds"`
	MemoryStorageClass        string                   `yaml:"memory_storage_class"`
	MemorySize                string                   `yaml:"memory_size"`
	ActiveDeadlineSeconds     int64                    `yaml:"active_deadline_seconds"`
	CompletionGraceSeconds    int                      `yaml:"completion_grace_seconds"`
	completionGraceSet        bool                     `yaml:"-"`
	TTLSecondsAfterFinished   int32                    `yaml:"ttl_seconds_after_finished"`
	PreStartRetryLimit        int                      `yaml:"pre_start_retry_limit"`
	preStartRetryLimitSet     bool                     `yaml:"-"`
	Resources                 KubernetesResourceConfig `yaml:"resources"`
}
```

Implement `KubernetesConfig.UnmarshalYAML` using a non-recursive alias and scan
the mapping node for `completion_grace_seconds` and `pre_start_retry_limit`.
Decode into a fresh alias before assigning it back so repeated unmarshalling
resets presence state. In `applyDefaults`, default zero values only when the
corresponding presence flag is false. Keep the exported values as integers.

Defaults: namespace `default`, memory size `1Gi`, active deadline `3600`,
completion grace `30`, Job TTL `600`, pre-start retries `1`. Completion grace
and pre-start retry defaults apply only when their YAML keys are omitted;
explicit YAML zero remains zero. Validation requires OSS, STS role, Agent
Server URL, image, service account, storage class, a valid positive quantity for
memory size, positive deadline/TTL values, non-negative completion grace, and a
non-negative pre-start retry limit. Storage class has no Go default and must be
explicit.

Retain the legacy reusable-Pod fields, defaults, validation, and YAML keys in
this task solely because `server/agent/kubernetes_executor.go` and the current
`server/main.go` wiring still compile and run. This is an intermediate build
contract, not a compatibility adapter. Task 9 deletes those fields and keys in
the same commit that deletes the old executor and changes main wiring.

- [ ] **Step 4: Update YAML and run tests**

Run: `go test ./server/config -run 'Kubernetes' -count=1`

Expected: PASS. Then verify the additive checkpoint remains buildable:

```bash
go test ./...
go build -o /tmp/anban-creator-server-task2 ./server
go build -o /tmp/anban-task2 ./agent
```

Expected: all commands PASS. Do not assert absence of legacy fields or YAML
keys in Task 2; those removal assertions belong to Task 9.

- [ ] **Step 5: Commit**

```bash
git add server/config/config.go server/config.yaml server/config.example.yaml server/config/kubernetes_config_test.go docs/superpowers/plans/2026-07-12-kubernetes-agent-job-runtime.md
git commit -m "refactor(config): define kubernetes job runtime"
```

### Task 3: Build Secure Jobs and Project Memory PVCs

**Files:**
- Create: `server/agent/kubernetes_job.go`
- Create: `server/agent/kubernetes_dispatcher.go`
- Modify: `server/agent/kubernetes_naming.go`
- Replace tests in: `server/agent/kubernetes_executor_test.go`

- [ ] **Step 1: Write failing pure Job/PVC tests**

```go
func TestBuildKubernetesJobIsOneShotAndHardened(t *testing.T) {
	job := buildKubernetesJob(testJobConfig(), testExecution(), testTask())
	spec := job.Spec.Template.Spec
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 { t.Fatal("backoff") }
	if spec.RestartPolicy != corev1.RestartPolicyNever { t.Fatal("restart policy") }
	if len(spec.Containers) != 1 || len(spec.InitContainers) != 0 { t.Fatal("one main container, no root init") }
	c := spec.Containers[0]
	if c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem { t.Fatal("read-only root") }
	assertMount(t, c, "workspace", "/workspace", false)
	assertMount(t, c, "memory", "/workspace/.claude/memory", false)
	assertProjectedAudience(t, spec.Volumes, "anban-server")
}

func TestBuildProjectMemoryPVCUsesNASStorageClass(t *testing.T) {
	pvc := buildProjectMemoryPVC(testJobConfig(), "project-1")
	if *pvc.Spec.StorageClassName != "alicloud-nas" { t.Fatal("storage class") }
	if !slices.Contains(pvc.Spec.AccessModes, corev1.ReadWriteMany) { t.Fatal("RWX") }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/agent -run 'Kubernetes(Job|ProjectMemoryPVC)' -count=1`

Expected: FAIL because Job/PVC builders do not exist.

- [ ] **Step 3: Define the async dispatcher contract and pure builders**

```go
type KubernetesDispatcher interface {
	Dispatch(ctx context.Context, execution *model.TaskExecution, task *model.Task) error
	Delete(ctx context.Context, execution *model.TaskExecution) error
	DeleteProjectMemory(ctx context.Context, projectID string) error
	Inspect(ctx context.Context, execution *model.TaskExecution) (*KubernetesExecutionState, error)
}

type KubernetesExecutionState struct {
	Phase string
	PodUID string
	Reason string
	Message string
	ExitCode *int32
}
```

Use deterministic DNS-safe names based on execution ID for Jobs and project ID
for PVCs. Job labels must include the unambiguous execution ID; hashed label
values may be used only when Kubernetes length rules require them.

- [ ] **Step 4: Implement idempotent PVC and Job creation**

`Dispatch` must create-or-verify the PVC, create-or-verify the Job, and reject an
existing object whose task/execution labels do not match. `Delete` uses
foreground propagation. `DeleteProjectMemory` deletes only the deterministic
PVC whose project label matches the requested project. `Inspect` maps Job
conditions and Pod termination state without using remote command APIs.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./server/agent -run 'Kubernetes' -count=1`

Expected: PASS.

```bash
git add server/agent/kubernetes_job.go server/agent/kubernetes_dispatcher.go server/agent/kubernetes_naming.go server/agent/kubernetes_executor_test.go
git commit -m "feat(agent): build one-shot kubernetes jobs"
```

### Task 4: Dispatch Kubernetes Attempts Asynchronously

**Files:**
- Create: `server/service/task_dispatch.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_execution.go`
- Modify: `server/repository/task.go`
- Modify: `server/repository/repository.go`
- Test: `server/service/task_dispatch_test.go`

- [ ] **Step 1: Write failing dispatch idempotency tests**

```go
func TestDispatchCloudTaskCreatesOneAttemptAndReturnsAfterJobAccepted(t *testing.T) {
	svc, repo, dispatcher, task := setupDispatchTest(t)
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil { t.Fatal(err) }
	if dispatcher.calls != 1 { t.Fatalf("dispatch calls=%d", dispatcher.calls) }
	current := mustCurrentExecution(t, repo, task.ID)
	if current.Status != model.TaskExecutionStarting { t.Fatalf("status=%s", current.Status) }
	if err := svc.HandleExecutionFromPayload(context.Background(), task.ID, task.UserID); err != nil { t.Fatal(err) }
	if dispatcher.calls != 1 { t.Fatal("duplicate dispatch") }
}
```

- [ ] **Step 2: Run test and verify RED**

Run: `go test ./server/service -run 'DispatchCloudTask' -count=1`

Expected: FAIL because `TaskService` still calls synchronous `Execute`.

- [ ] **Step 3: Add dispatcher wiring and transactional attempt creation**

```go
func (s *TaskService) SetKubernetesDispatcher(d agent.KubernetesDispatcher) {
	s.kubernetesDispatcher = d
}

func (s *TaskService) dispatchKubernetes(ctx context.Context, task *model.Task) error {
	execution, created, err := s.createCurrentExecution(ctx, task)
	if err != nil || !created { return err }
	if err := s.kubernetesDispatcher.Dispatch(ctx, execution, task); err != nil {
		return s.failDispatch(ctx, task, execution, err)
	}
	_, err = s.repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionDispatching}, model.TaskExecutionStarting,
		model.ExecutionTransition{})
	return err
}
```

`createCurrentExecution` runs inside `Repository.WithTx`: it CASes task pending
to running, allocates the next attempt number, creates the execution, and sets
`current_execution_id`. The Asynq handler returns once `Dispatch` succeeds; it
does not release the project concurrency slot until terminal finalization.

- [ ] **Step 4: Preserve local/Docker behavior**

Branch in `HandleExecutionFromPayload`: when the Kubernetes dispatcher is set,
call `dispatchKubernetes`; otherwise retain the existing synchronous
`HandleExecution`. Remove the Kubernetes-only project concurrency cap of one.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./server/service -run 'DispatchCloudTask|HandleExecutionFromPayload' -count=1`

Expected: PASS.

```bash
git add server/service/task.go server/service/task_execution.go server/service/task_dispatch.go server/service/task_dispatch_test.go server/repository/task.go server/repository/repository.go
git commit -m "feat(server): dispatch kubernetes attempts asynchronously"
```

### Task 5: Add Workload Identity and Bootstrap API

**Files:**
- Create: `server/auth/execution_token.go`
- Create: `server/auth/execution_token_test.go`
- Create: `server/agent/kubernetes_identity.go`
- Create: `server/agent/kubernetes_identity_test.go`
- Create: `server/service/agent_bootstrap.go`
- Create: `server/service/agent_bootstrap_test.go`
- Modify: `server/handler/agent.go`
- Modify: `server/handler/agent_test.go`
- Modify: `server/router/router.go`

- [ ] **Step 1: Write failing execution-token and bound-workload tests**

```go
func TestExecutionTokenRejectsDifferentAttempt(t *testing.T) {
	svc := mustExecutionTokenService(t, "secret", time.Hour)
	raw, _ := svc.Issue(ExecutionClaims{UserID: "u", ProjectID: "p", TaskID: "t", ExecutionID: "e1"})
	claims, err := svc.Validate(raw)
	if err != nil || claims.ExecutionID != "e1" { t.Fatalf("claims=%+v err=%v", claims, err) }
}

func TestVerifyWorkloadBindsTokenPodJobAndExecution(t *testing.T) {
	verifier := newFakeBoundWorkloadVerifier("pod-uid", "job-e1", "e1")
	identity, err := verifier.Verify(context.Background(), "bound-token", "e1")
	if err != nil || identity.ExecutionID != "e1" { t.Fatalf("identity=%+v err=%v", identity, err) }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/auth ./server/agent ./server/handler -run 'ExecutionToken|Workload|Bootstrap' -count=1`

Expected: FAIL because execution identity does not exist.

- [ ] **Step 3: Implement execution JWTs and TokenReview verification**

```go
type ExecutionClaims struct {
	UserID string `json:"user_id"`
	ProjectID string `json:"project_id"`
	TaskID string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	jwt.RegisteredClaims
}
```

Issue HS256 tokens with issuer `anban-server`, audience
`creator-agent-execution`, subject equal to execution ID, and expiry no later than
the Job deadline. The workload verifier submits `authentication/v1.TokenReview`
for audience `anban-server`, requires the configured Agent ServiceAccount, reads
bound Pod name/UID extras, and verifies the owning Job labels.

- [ ] **Step 4: Implement bootstrap response construction**

```go
type AgentBootstrapResponse struct {
	ExecutionToken string `json:"execution_token"`
	TaskID string `json:"task_id"`
	TaskType string `json:"task_type"`
	ProjectID string `json:"project_id"`
	Prompt string `json:"prompt"`
	Model string `json:"model"`
	MaxTurns int `json:"max_turns"`
	AgentFlag string `json:"agent_flag"`
	AutoMemoryDirectory string `json:"auto_memory_directory"`
	Files []BootstrapFile `json:"files"`
}

type BootstrapFile struct {
	Path string `json:"path"`
	Text string `json:"text,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	Mode uint32 `json:"mode"`
}
```

Build settings, `CLAUDE.md`, resume `latest.md`, attachment indexes, montage
runtime files, and signed reference/attachment downloads without reading NAS.
Every path must pass `CleanTaskFileRelativePath` and remain below `/workspace`.
After identity validation and response construction succeed, CAS the execution
from `starting` to `running`, set `started=true`, record Pod UID/start time, and
refresh both execution and task heartbeat timestamps.

- [ ] **Step 5: Add bootstrap route and dual agent auth**

Register `POST /api/v1/agent/bootstrap` with workload-token middleware. Extend
normal Agent middleware to accept execution JWTs first and existing API keys
second so desktop/local execution remains functional. Store user, task, project,
and execution IDs in Fiber locals.

- [ ] **Step 6: Run tests and commit**

Run: `go test ./server/auth ./server/agent ./server/service ./server/handler -run 'ExecutionToken|Workload|Bootstrap' -count=1`

Expected: PASS.

```bash
git add server/auth/execution_token.go server/auth/execution_token_test.go server/agent/kubernetes_identity.go server/agent/kubernetes_identity_test.go server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go server/handler/agent.go server/handler/agent_test.go server/router/router.go
git commit -m "feat(server): bootstrap jobs with workload identity"
```

### Task 6: Add the One-Shot Agent Job Command

**Files:**
- Create: `agent/bootstrap.go`
- Create: `agent/bootstrap_test.go`
- Create: `agent/job.go`
- Create: `agent/job_test.go`
- Preserve/extend: `agent/home_template.go` (added by Task 3)
- Preserve/extend: `agent/home_template_test.go` (added by Task 3)
- Modify: `agent/main.go`
- Modify: `agent/config.go`
- Modify: `agent/reporter.go`
- Modify: `agent/artifact_upload.go`
- Modify: `agent/runner.go`
- Modify: `agent/runner_contract_test.go`

- [ ] **Step 1: Write failing bootstrap materialization tests**

```go
func TestMaterializeBootstrapRejectsWorkspaceEscape(t *testing.T) {
	err := materializeBootstrap(t.TempDir(), []BootstrapFile{{Path: "../secret", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") { t.Fatalf("err=%v", err) }
}

func TestJobCommandBootstrapsBeforeRunningClaude(t *testing.T) {
	order := []string{}
	cmd := newJobCommand(func(context.Context, JobConfig) (*BootstrapResponse, error) {
		order = append(order, "bootstrap"); return testBootstrap(), nil
	}, func(context.Context, *Config) error { order = append(order, "run"); return nil })
	runCommandForTest(t, cmd, "job", "--execution-id", "e1", "--server-url", "http://server", "--workload-token-file", writeToken(t))
	if diff := cmp.Diff([]string{"bootstrap", "run"}, order); diff != "" { t.Fatal(diff) }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./agent -run 'Bootstrap|JobCommand|HomeTemplate' -count=1`

Expected: FAIL because the Job command and bootstrap client do not exist.

- [ ] **Step 3: Implement bootstrap client and safe writer**

Read the projected token file, call bootstrap, replace it with the returned
execution JWT, create parent directories with `0755`, write text files with
declared safe modes, and stream download files with bounded size and HTTP
timeouts. Reject absolute paths, traversal, symlinks, and duplicate paths.

- [ ] **Step 4: Add `anban job` and execution-aware reporting**

```go
// Add this field to Config.
ExecutionID string

type agentEnvelope struct {
	TaskID string `json:"task_id"`
	ExecutionID string `json:"execution_id,omitempty"`
}
```

The Job command accepts only `server-url`, `execution-id`, `workspace`, and
`workload-token-file`; bootstrap supplies all task-specific options. Include
`execution_id` in progress, prepare, manifest, and completion requests. Keep
`anban run` unchanged for desktop/local callers.

The shared Runner must load `CLAUDE_PLUGIN_ROOT` with the SDK's local-plugin
option when that environment variable is set. Kubernetes mounts a writable
`emptyDir` over `/home/node`, so Job execution must discover the immutable
Anban plugin from `/anbanai` rather than relying on image-baked user-home state.
The image and Job must explicitly set `HOME=/home/node` and run Agent work as
numeric UID/GID `1000:1000`; do not depend on a `node` passwd or group name.
When `ANBAN_HOME_TEMPLATE` is set, seed missing files from that immutable image
directory into `$HOME` before constructing the Claude SDK client. Preserve any
newer runtime files, make seeded content writable by the runtime user, and fail
on symlinks, path escapes, special files, type conflicts, or credential files.
When `CLAUDE_PLUGIN_ROOT` is unset, preserve the existing local/desktop plugin
discovery behavior and continue passing the configured Agent flag. When
`ANBAN_HOME_TEMPLATE` is unset, local/desktop home behavior remains unchanged.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./agent -run 'Bootstrap|JobCommand|Reporter|Artifact|Runner|HomeTemplate|Plugin' -count=1`

Expected: PASS.

```bash
git add agent/bootstrap.go agent/bootstrap_test.go agent/job.go agent/job_test.go agent/home_template.go agent/home_template_test.go agent/main.go agent/config.go agent/reporter.go agent/artifact_upload.go agent/runner.go agent/runner_contract_test.go
git commit -m "feat(agent): run one-shot kubernetes jobs"
```

### Task 7: Scope Artifacts to Attempts and Publish Atomically

**Files:**
- Modify: `server/model/task_file.go`
- Modify: `server/repository/repository.go`
- Modify: `server/repository/task_file.go`
- Modify: `server/service/task_artifact_upload.go`
- Modify: `server/service/task_artifact_upload_test.go`
- Modify: `agent/artifact_upload.go`
- Modify: `agent/artifact_upload_test.go`

- [ ] **Step 1: Write failing attempt-isolation tests**

```go
func TestAttemptArtifactsStayPendingUntilPublished(t *testing.T) {
	svc, repo, _, task := newTaskArtifactTestService(t)
	exec := seedCurrentExecution(t, repo, task, "exec-1")
	req := validManifest(task, exec.ID, "output/article.md")
	if err := svc.FinalizeTaskArtifactManifest(context.Background(), task.ID, task.UserID, req); err != nil { t.Fatal(err) }
	if files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID); len(files) != 0 { t.Fatal("pending artifact leaked") }
	if err := repo.TaskFiles().PublishExecution(context.Background(), task.ID, exec.ID); err != nil { t.Fatal(err) }
	if files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID); len(files) != 1 { t.Fatal("published artifact missing") }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/repository ./agent -run 'AttemptArtifact|ArtifactsStayPending' -count=1`

Expected: FAIL because artifacts have no execution identity or publication state.

- [ ] **Step 3: Extend task files and upload contracts**

```go
const (
	TaskFilePending = "pending"
	TaskFilePublished = "published"
	TaskFileSuperseded = "superseded"
)

// Add these fields to TaskFile.
ExecutionID string `gorm:"type:char(36);uniqueIndex:idx_task_execution_path,priority:2;index" json:"execution_id,omitempty"`
State string `gorm:"type:varchar(20);default:published;index" json:"-"`
```

Change the unique key to task + execution + path. `FindByTaskID` returns only
published rows. Add `FindByExecutionID`, `PublishExecution`, and
`DiscardExecution`. Include execution ID in prepare/manifest requests and use
`uploads/users/<u>/projects/<p>/tasks/<t>/executions/<e>/artifacts/<path>`.

- [ ] **Step 4: Validate current execution ownership**

Prepare and manifest reject missing, stale, terminal, or foreign execution IDs.
Manifest rows are persisted as pending. `PublishExecution` transactionally marks
older published rows superseded and current pending rows published.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./server/model ./server/repository ./server/service ./agent -run 'Artifact|TaskFile' -count=1`

Expected: PASS.

```bash
git add server/model/task_file.go server/repository/repository.go server/repository/task_file.go server/service/task_artifact_upload.go server/service/task_artifact_upload_test.go agent/artifact_upload.go agent/artifact_upload_test.go
git commit -m "feat(server): scope task artifacts to executions"
```

### Task 8: Finalize Attempts, Cancel Jobs, and Reconcile Failures

**Files:**
- Create: `server/service/task_execution_complete.go`
- Create: `server/service/task_execution_complete_test.go`
- Create: `server/agent/kubernetes_reconciler.go`
- Create: `server/agent/kubernetes_reconciler_test.go`
- Modify: `server/service/task.go`
- Modify: `server/handler/agent.go`
- Modify: `server/handler/agent_test.go`

- [ ] **Step 1: Write failing terminal-race tests**

```go
func TestCompleteCloudExecutionOnlyCurrentAttemptWins(t *testing.T) {
	svc, repo, oldExec, currentExec, task := setupCompletionRace(t)
	err := svc.CompleteCloudExecution(context.Background(), oldExec.ID, successResult())
	if !errors.Is(err, ErrStaleTaskExecution) { t.Fatalf("old completion err=%v", err) }
	if err := svc.CompleteCloudExecution(context.Background(), currentExec.ID, successResult()); err != nil { t.Fatal(err) }
	if err := svc.CompleteCloudExecution(context.Background(), currentExec.ID, successResult()); err != nil { t.Fatal("duplicate must be idempotent") }
	assertSingleSettlement(t, repo, task.ID)
}

func TestCancelMarksAttemptBeforeDeletingJob(t *testing.T) {
	svc, dispatcher, task, exec := setupCancelJobTest(t)
	if err := svc.CancelForUser(context.Background(), task.UserID, task.ID); err != nil { t.Fatal(err) }
	if dispatcher.deleted != exec.JobName { t.Fatal("job not deleted") }
	if dispatcher.statusAtDelete != model.TaskExecutionCancelled { t.Fatal("delete happened before CAS") }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server/service ./server/agent ./server/handler -run 'CloudExecution|CancelMarksAttempt|Reconciler' -count=1`

Expected: FAIL because cloud completion and Job reconciliation do not exist.

- [ ] **Step 3: Extract one terminal finalizer**

```go
func (s *TaskService) CompleteCloudExecution(ctx context.Context, executionID string, result *agent.ExecutionResult) error {
	execution, task, err := s.validateCurrentExecution(ctx, executionID)
	if err != nil { return err }
	terminal := model.TaskExecutionFailed
	if result != nil && result.Success { terminal = model.TaskExecutionSucceeded }
	won, err := s.repo.TaskExecutions().Transition(ctx, execution.ID,
		[]string{model.TaskExecutionStarting, model.TaskExecutionRunning}, terminal,
		model.ExecutionTransition{TerminalReason: resultReason(result)})
	if err != nil || !won { return idempotentOrStale(ctx, executionID) }
	return s.finalizeTaskFromExecution(ctx, task, execution, result)
}
```

`finalizeTaskFromExecution` is the only entry point that promotes artifacts,
stores usage/result, rebuilds workflow state, validates deliverables, performs
publishing, settles or refunds, releases slots, dispatches pending work, and
notifies. Reuse existing helpers rather than duplicating their behavior.

- [ ] **Step 4: Implement cancellation ordering and reconciler**

Cancellation CASes the execution to cancelled and the task to cancelled before
calling dispatcher delete. The reconciler polls reconcilable attempts, calls
`Inspect`, updates Pod UID/started state, and terminalizes image pull, scheduling,
PVC mount, OOM, Job failure, deadline, heartbeat, and missing-completion cases.
Only pre-start failures below `PreStartRetryLimit` create a replacement attempt.

- [ ] **Step 5: Route cloud completion and run tests**

`AgentHandler.Complete` dispatches by authenticated execution identity to
`CompleteCloudExecution`; API-key desktop callers continue to use
`CompleteLocalTask`.

Run: `go test ./server/service ./server/agent ./server/handler -run 'CloudExecution|Cancel|Reconciler|Complete' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/service/task_execution_complete.go server/service/task_execution_complete_test.go server/agent/kubernetes_reconciler.go server/agent/kubernetes_reconciler_test.go server/service/task.go server/handler/agent.go server/handler/agent_test.go
git commit -m "feat(server): finalize and reconcile kubernetes jobs"
```

### Task 9: Wire Production and Delete the Reusable Pod Runtime

Task 9 must atomically provision the server TLS listener, certificate and trust
chain, Kubernetes Service TLS port, and the matching HTTPS
`claude.agent_server_url` before it enables the Job dispatcher. The current
legacy Pod runtime keeps its HTTP Service URL until that rollout; the Job Agent
itself remains HTTPS-only and therefore fails closed if enabled prematurely.

**Files:**
- Modify: `server/main.go`
- Modify: `server/mcp/mcp.go`
- Modify: `server/mcp/mcp_test.go`
- Delete: `server/agent/kubernetes_executor.go`
- Modify: `server/agent/kubernetes_executor_test.go`
- Modify: `server/service/project.go`
- Modify: `server/service/project_test.go`
- Modify: `deploy/k8s/ack-agent-runtime.yaml`
- Modify: `server/Deployment.yaml`
- Modify: `server/k8s_agent_runtime_test.go`
- Modify: `Dockerfile.agent`
- Modify: `server/agent/docker_runtime_contract_test.go`
- Modify: `server/config/config.go`
- Modify: `server/config.yaml`
- Modify: `server/config.example.yaml`
- Modify: `server/config/kubernetes_config_test.go`

- [ ] **Step 1: Rewrite manifest tests first**

```go
func TestACKAgentRuntimeUsesJobsWithoutExec(t *testing.T) {
	docs := parseACKRuntime(t)
	role := requireKind(t, docs, "Role")
	assertAllows(t, role, "batch", "jobs", "get", "list", "watch", "create", "delete")
	assertAllows(t, role, "", "persistentvolumeclaims", "get", "create", "delete")
	assertForbids(t, role, "", "pods/exec")
	clusterRole := requireKind(t, docs, "ClusterRole")
	assertAllows(t, clusterRole, "authentication.k8s.io", "tokenreviews", "create")
}

func TestKubernetesConfigHasNoReusablePodFields(t *testing.T) {
	typ := reflect.TypeOf(KubernetesConfig{})
	for _, name := range []string{"WorkspaceMountPath", "WorkspacePVCName", "PodRevision", "PodTTLSeconds", "ExecTimeoutSec"} {
		if _, ok := typ.FieldByName(name); ok { t.Fatalf("obsolete field %s remains", name) }
	}
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./server ./server/config -run 'ACKAgentRuntime|KubernetesConfigHasNoReusablePodFields' -count=1`

Expected: FAIL because the manifest still grants Pod exec and lacks
Job/PVC/TokenReview permissions, and the intermediate config still contains
legacy reusable-Pod fields.

- [ ] **Step 3: Wire dispatcher, identity verifier, and reconciler**

Create one Kubernetes client in `main.go`. In Kubernetes mode instantiate the
dispatcher, workload verifier, execution token service, and reconciler; set the
dispatcher on `TaskService`; set verifier/token service on `AgentHandler`; start
the reconciler with the Server lifecycle context. Do not construct a synchronous
Kubernetes `TaskExecutor` or OSS `ProjectMemoryManager` for Kubernetes mode.

Extend MCP bearer authentication to accept execution JWTs and centrally enforce
the claimed user/project/task/current-execution scope on every tool call before
dispatch. A token for task T1 must be unable to read, mutate, cancel, publish, or
generate assets for T2 even when both tasks share the same user. Preserve API-key
and static-key authentication for desktop/local callers, and add an HTTP tool-call
test proving the cross-task request is rejected. Wire this execution-aware MCP
verifier from `main.go` with the same token service and `TaskService` authorizer.

Add a project-memory lifecycle dependency to `ProjectService`. After the
existing guarded project deletion succeeds, call `DeleteProjectMemory` with the
deleted project ID. A missing PVC is success; a label mismatch is a hard error
and must never delete an unrelated claim. Add a service test that deletes a
project and observes exactly one deterministic PVC deletion request.

- [ ] **Step 4: Delete reusable Pod execution**

Delete remotecommand imports and all Pod reuse, tar bundle, workspace path,
permission init, config hash, memory archive, and `execAgentCommand` code. Keep
shared prompt/config pure builders used by bootstrap. Ensure `rg` returns no
production hits for:

```bash
rg -n -g '!**/*_test.go' "pods/exec|remotecommand|copyWorkspaceBundle|execAgentCommand|kubernetesAgentPodName|PodRevision|WorkspacePVCName" server deploy
```

Expected: no production hits; removal assertions in tests are allowed.

In the same cutover, delete `WorkspaceMountPath`, `WorkspacePVCName`,
`PodRevision`, `PodTTLSeconds`, and `ExecTimeoutSec` from `KubernetesConfig`;
remove their defaults and validation; and remove `workspace_mount_path`,
`workspace_pvc_name`, `pod_revision`, `pod_ttl_seconds`, and
`exec_timeout_seconds` from both YAML files. Verify config text contains none of
the removed runtime contract:

```bash
rg -n "pod_revision|pods/exec|workspace_pvc_name|exec_timeout_seconds" server/config/config.go server/config.yaml server/config.example.yaml
```

Expected: no matches. This atomic deletion is mandatory; no final compatibility
path remains.

- [ ] **Step 5: Update runtime manifests and Agent image**

Grant Server namespace Job/PVC/Pod-read permissions and cluster TokenReview
permission, remove Pod create/exec permissions, keep Agent ServiceAccount without
API permissions, and add optional NetworkPolicy. Ensure the image contains the
`anban job` command and writable directories are supplied only by Job volumes.
Keep `/anbanai` immutable and outside the writable `/home/node` volume, retain
Runner loading through `CLAUDE_PLUGIN_ROOT=/anbanai`, and make the image's numeric runtime
identity explicitly match the Job security context UID/GID `1000:1000`. Verify
the pinned base provides numeric UID and GID 1000, create `/home/node` with
numeric ownership, set `HOME=/home/node`, and use `USER 1000:1000` without
creating or requiring a `node` account. After
all numeric-runtime-user skill and plugin installation, allowlist only the three installed
skill directories, the known/installed plugin registry files, and the Anban
plugin cache into `/opt/anban-home-template`; never copy `.claude` or the user
home wholesale. Set `ANBAN_HOME_TEMPLATE` to that path and reject missing
required inputs, credentials, special files, and image-template symlinks. The
Runner must seed that immutable template into the writable Job home without
overwriting runtime-created files.

- [ ] **Step 6: Run targeted tests and commit**

Run: `go test ./server ./server/agent ./server/config ./agent -run 'Kubernetes|ACKAgentRuntime|DockerRuntime|Runner|HomeTemplate|Plugin' -count=1`

Expected: PASS.

```bash
git add server/main.go server/agent/kubernetes_executor.go server/agent/kubernetes_executor_test.go server/agent/docker_runtime_contract_test.go server/service/project.go server/service/project_test.go deploy/k8s/ack-agent-runtime.yaml server/Deployment.yaml server/k8s_agent_runtime_test.go Dockerfile.agent server/config/config.go server/config.yaml server/config.example.yaml server/config/kubernetes_config_test.go
git commit -m "refactor(server): replace kubernetes pod executor with jobs"
```

### Task 10: Full Contract Verification and ACK Smoke Test

**Files:**
- Modify when necessary: tests touched by Tasks 1-9 only
- Verify: `docs/superpowers/specs/2026-07-12-kubernetes-agent-job-runtime-design.md`

- [ ] **Step 1: Run focused execution-chain tests**

```bash
go test ./server/model ./server/repository ./server/config ./server/auth ./server/agent ./server/service ./server/handler ./agent -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full Go verification**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
git diff --check
```

Expected: all commands PASS with no whitespace errors.

- [ ] **Step 3: Validate manifests structurally**

```bash
go test ./server -run TestACKAgentRuntimeManifest -count=1
```

Expected: PASS and parsed RBAC contains Jobs/PVCs/TokenReview but no `pods/exec`.

- [ ] **Step 4: Run an ACK smoke task**

Deploy the Server and Agent image to a non-production ACK namespace, then verify
the following observable facts:

```text
1. one TaskExecution creates one batch/v1 Job;
2. the Pod mounts an empty workspace and the project's memory PVC;
3. bootstrap authenticates through the projected workload token;
4. Agent progress and heartbeat include execution_id;
5. a generated image uploads under the execution-specific OSS prefix;
6. successful completion publishes task_files exactly once;
7. Job TTL cleanup removes Job/Pod but preserves memory PVC and OSS result;
8. a second Job for the same project sees the prior .claude/memory;
9. a stale completion returns conflict and cannot change task or billing;
10. cancellation marks the attempt before Job deletion.
```

Expected: all ten observations hold. Record Job name, execution ID, PVC name,
OSS key, task terminal status, and billing transaction ID in the deployment
verification notes without recording tokens or credentials.

- [ ] **Step 5: Commit verification-only corrections**

If verification required test or configuration corrections, commit only those
files:

```bash
git diff --name-only -- server agent deploy Dockerfile.agent | xargs git add --
git commit -m "test: verify kubernetes agent job runtime"
```

If no correction was needed, do not create an empty commit.
