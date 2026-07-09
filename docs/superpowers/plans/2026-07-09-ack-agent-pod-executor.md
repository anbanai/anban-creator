# ACK Agent Pod Executor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move production task execution from server-local Claude Code into ACK project-scoped Agent Pods while preserving local mode and using Studio-style OSS direct uploads for artifacts.

**Architecture:** Add a Kubernetes executor beside the existing local and Docker executors. Remote Agent Pods mount NAS and upload task artifacts directly to private OSS with server-issued STS credentials, then report a manifest that the server validates into `task_files`. Server finalization gains a remote-artifact branch that does not read NAS, while local mode keeps the current `WorkDir` and multipart upload paths.

**Tech Stack:** Go, Fiber v3, GORM, Alibaba OSS/STS, Kubernetes client-go remotecommand, ACK YAML manifests, existing Agent runner.

---

## File Map

- `server/config/config.go`: add Kubernetes executor config, defaults, validation, and Agent server URL behavior.
- `server/config.example.yaml`: document ACK executor settings.
- `server/service/direct_upload.go`: extend Studio direct-upload mechanics for `task_artifact`.
- `server/service/task_artifact_upload.go`: create focused task artifact prepare/finalize/manifest validation logic.
- `server/handler/agent.go`: add Agent endpoints for preparing artifact uploads and reporting manifests.
- `server/router/router.go`: register the new Agent endpoints under existing Agent auth middleware.
- `server/storage/storage.go`, `server/storage/oss.go`: expose object stat metadata needed to validate direct-upload manifests.
- `agent/artifact_upload.go`: scan workspace, request upload credentials, upload files to OSS, report manifest.
- `agent/reporter.go`: add JSON helpers for artifact prepare/manifest endpoints.
- `agent/main.go`: run artifact upload before result/complete reporting when enabled by config.
- `agent/config.go`: add flags/env for artifact direct upload mode.
- `server/agent/kubernetes_executor.go`: implement ACK Agent Pod creation/reuse and exec.
- `server/agent/kubernetes_naming.go`: deterministic user/project scoped Pod names and labels.
- `server/agent/kubernetes_executor_test.go`: unit tests for naming, command/env construction, and remote workdir behavior.
- `server/main.go`: wire `claude.executor=kubernetes`.
- `deploy/k8s/ack-agent-runtime.yaml`: RBAC and Agent NAS PVC/runtime template.
- `server/k8s_agent_runtime_test.go`: assert ACK manifest safety properties.

## Task 1: Config Foundation

**Files:**
- Modify: `server/config/config.go`
- Modify: `server/config.example.yaml`
- Test: `server/config/kubernetes_config_test.go`

- [ ] **Step 1: Write failing tests**

Create `server/config/kubernetes_config_test.go`:

```go
package config

import (
	"strings"
	"testing"
	"time"
)

func baseKubernetesConfigForTest() Config {
	cfg := Config{
		Database: DatabaseConfig{DSN: "dsn"},
		JWT:      JWTConfig{SecretKey: "secret", AccessExpiry: "24h", RefreshExpiry: "168h"},
		Asynq:    AsynqConfig{ContentGenerateTimeout: time.Hour},
		Claude: ClaudeConfig{
			Executor: "kubernetes",
			Kubernetes: KubernetesConfig{
				Namespace:          "anban",
				AgentImage:         "registry.example.com/anban-agent:latest",
				ServiceAccount:     "anban-agent-runner",
				WorkspaceMountPath: "/workspace",
				WorkspacePVCName:   "anban-agent-nas",
				ExecTimeoutSec:     3600,
			},
		},
		Storage: StorageConfig{
			Provider:        "oss",
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
			BucketName:      "bucket",
			STSRoleArn:      "acs:ram::123:role/upload",
		},
	}
	cfg.applyDefaults()
	return cfg
}

func TestValidateAcceptsKubernetesExecutor(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateKubernetesRequiresOSS(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Storage.Provider = "local"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes requires storage.provider to be \"oss\"") {
		t.Fatalf("Validate() error = %v, want OSS requirement", err)
	}
}

func TestValidateKubernetesRequiresWorkspaceVolume(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.Kubernetes.WorkspacePVCName = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "claude.kubernetes.workspace_pvc_name is required") {
		t.Fatalf("Validate() error = %v, want workspace pvc requirement", err)
	}
}

func TestAgentServerURLUsesConfiguredKubernetesServiceURL(t *testing.T) {
	cfg := baseKubernetesConfigForTest()
	cfg.Claude.AgentServerURL = "http://anban-server.anban.svc.cluster.local:8080/"
	if got := cfg.AgentServerURL(); got != "http://anban-server.anban.svc.cluster.local:8080" {
		t.Fatalf("AgentServerURL() = %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run:

```bash
go test ./server/config -run TestValidateKubernetes -count=1
```

Expected: FAIL because `KubernetesConfig` does not exist and executor validation rejects `kubernetes`.

- [ ] **Step 3: Implement config**

Add `KubernetesConfig` and `KubernetesResourceConfig` to `server/config/config.go`, add the field to `ClaudeConfig`, update defaults, `AgentServerURL`, and `Validate`.

Key behavior:

```go
type KubernetesConfig struct {
	Namespace          string                   `yaml:"namespace"`
	AgentImage         string                   `yaml:"agent_image"`
	ServiceAccount     string                   `yaml:"service_account"`
	ImagePullSecret    string                   `yaml:"image_pull_secret"`
	WorkspaceMountPath string                   `yaml:"workspace_mount_path"`
	WorkspacePVCName   string                   `yaml:"workspace_pvc_name"`
	PodTTLSeconds      int                      `yaml:"pod_ttl_seconds"`
	ExecTimeoutSec     int                      `yaml:"exec_timeout_seconds"`
	Resources          KubernetesResourceConfig `yaml:"resources"`
}

type KubernetesResourceConfig struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}
```

Validation must accept only `local`, `docker`, `kubernetes`; require OSS and STS role for Kubernetes direct upload; require namespace, image, mount path, and PVC name.

- [ ] **Step 4: Verify**

Run:

```bash
go test ./server/config -run 'TestValidateKubernetes|TestAgentServerURL' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/config/config.go server/config.example.yaml server/config/kubernetes_config_test.go
git commit -m "feat: add kubernetes executor config"
```

## Task 2: Task Artifact Direct Upload Service

**Files:**
- Modify: `server/service/direct_upload.go`
- Create: `server/service/task_artifact_upload.go`
- Test: `server/service/task_artifact_upload_test.go`
- Modify: `server/storage/storage.go`
- Modify: `server/storage/oss.go`

- [ ] **Step 1: Write failing tests**

Tests cover:

- `PrepareTaskArtifactUpload` builds `uploads/users/<user>/projects/<project>/tasks/<task>/artifacts/<relpath>`.
- It rejects `../bad.md`, dotfiles, wrong task access, non-OSS storage, missing STS role.
- `FinalizeTaskArtifactManifest` rejects object keys outside the exact task prefix.
- Finalize persists a `TaskFile` with role from `DetermineTaskFileRole`.

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./server/service -run TestTaskArtifact -count=1
```

Expected: FAIL because service functions do not exist.

- [ ] **Step 3: Implement service**

Create focused request/result types:

```go
const DirectUploadPurposeTaskArtifact = "task_artifact"

type TaskArtifactPrepareRequest struct {
	TaskID       string `json:"task_id"`
	RelativePath string `json:"relative_path"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

type TaskArtifactManifestRequest struct {
	TaskID string                     `json:"task_id"`
	Files  []TaskArtifactManifestFile `json:"files"`
}

type TaskArtifactManifestFile struct {
	RelativePath string `json:"relative_path"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	ETag         string `json:"etag"`
	Role         string `json:"role"`
}
```

Reuse `UploadCredentialIssuer` and `directUploadPolicyJSON`. Add `storage.StatObject` support so finalize can `HEAD` when provider is OSS.

- [ ] **Step 4: Verify**

```bash
go test ./server/service -run TestTaskArtifact -count=1
go test ./server/storage -run TestOSS -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/service/direct_upload.go server/service/task_artifact_upload.go server/service/task_artifact_upload_test.go server/storage/storage.go server/storage/oss.go
git commit -m "feat: validate agent task artifact uploads"
```

## Task 3: Agent Artifact Endpoints

**Files:**
- Modify: `server/handler/agent.go`
- Modify: `server/router/router.go`
- Test: `server/handler/agent_test.go`

- [ ] **Step 1: Write failing handler tests**

Add tests for:

- `POST /api/v1/agent/artifacts/prepare` requires agent auth and task access.
- `POST /api/v1/agent/artifacts/manifest` persists manifest files.
- Cross-user task access is rejected.

- [ ] **Step 2: Run tests**

```bash
go test ./server/handler -run TestAgentArtifact -count=1
```

Expected: FAIL because routes and handlers do not exist.

- [ ] **Step 3: Implement handlers**

Add methods on `AgentHandler`:

```go
func (h *AgentHandler) PrepareArtifactUpload(c fiber.Ctx) error
func (h *AgentHandler) ReportArtifactManifest(c fiber.Ctx) error
```

Both use `ValidateAgentTaskAccess`. Register under existing authenticated Agent group:

```go
agentAPI.Post("/artifacts/prepare", svc.AgentHandler.PrepareArtifactUpload)
agentAPI.Post("/artifacts/manifest", svc.AgentHandler.ReportArtifactManifest)
```

- [ ] **Step 4: Verify**

```bash
go test ./server/handler -run TestAgentArtifact -count=1
go test ./server/router -run Test -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/handler/agent.go server/handler/agent_test.go server/router/router.go
git commit -m "feat: add agent artifact manifest endpoints"
```

## Task 4: Agent Workspace Scanner and OSS Direct Uploader

**Files:**
- Modify: `agent/config.go`
- Modify: `agent/reporter.go`
- Create: `agent/artifact_upload.go`
- Test: `agent/artifact_upload_test.go`
- Modify: `agent/main.go`

- [ ] **Step 1: Write failing tests**

Tests cover:

- Scanner prefers `output/` but preserves relative paths from workspace root.
- Scanner skips `.claude`, `.anban-creator`, `.git`, `node_modules`, dotfiles, package lock files.
- Uploader calls prepare endpoint, uploads to OSS, then reports manifest.
- Upload disabled by default for local compatibility.

- [ ] **Step 2: Run tests**

```bash
go test ./agent -run TestArtifact -count=1
```

Expected: FAIL because scanner/uploader does not exist.

- [ ] **Step 3: Implement**

Add config flag/env:

```text
--artifact-upload-mode=off|direct
ANBAN_ARTIFACT_UPLOAD_MODE
```

Default `off`. Kubernetes executor passes `direct`.

Use `github.com/aliyun/aliyun-oss-go-sdk/oss` or the existing module already in
`go.mod` to upload with STS credentials returned by the server.

In `runAgent`, after `runner.Run` and before `ReportResult`, call:

```go
if cfg.ArtifactUploadMode == "direct" {
    _ = uploader.UploadWorkspaceArtifacts(context.Background(), result)
}
```

On upload failure, report progress and keep result failure policy explicit:
for Kubernetes, missing required artifact validation on the server will fail the
task; for local, mode is off and behavior is unchanged.

- [ ] **Step 4: Verify**

```bash
go test ./agent -run 'TestArtifact|TestConfig|TestReport' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add agent/config.go agent/reporter.go agent/artifact_upload.go agent/artifact_upload_test.go agent/main.go
git commit -m "feat: upload agent artifacts directly to oss"
```

## Task 5: Remote Finalization Branch

**Files:**
- Modify: `server/agent/executor_interface.go`
- Modify: `server/service/task_execution.go`
- Test: `server/service/task_execution_decouple_test.go`

- [ ] **Step 1: Write failing tests**

Add tests showing:

- When executor result has remote artifact mode, `HandleExecution` does not call `uploadMissingTaskFiles`.
- Article/seednote artifact validation uses task files instead of `WorkDir`.
- Auto-publish approval can extract article draft from uploaded task files.
- Existing local `WorkDir` path still uploads and validates from disk.

- [ ] **Step 2: Run tests**

```bash
go test ./server/service -run 'TestHandleExecution.*Remote|TestCompleteLocalTask' -count=1
```

Expected: FAIL until remote finalization exists.

- [ ] **Step 3: Implement**

Add a small marker to `ExecutionResult` or `ExecutionOptions` output, for example:

```go
RemoteArtifacts bool `json:"remote_artifacts,omitempty"`
```

Kubernetes executor sets it true. Local and Docker leave false.

In `HandleExecution`:

- If `RemoteArtifacts` is true, skip host-side `uploadMissingTaskFiles`.
- Always rebuild workflow after remote manifest.
- Validate using `agent.ValidateTaskArtifactsFromTaskFiles` for remote tasks.
- Add helper to extract markdown/html article drafts from `TaskFile` via storage `Read`.
- Keep memory merge from `WorkDir` only when local workdir exists.

- [ ] **Step 4: Verify**

```bash
go test ./server/service -run 'TestHandleExecution.*Remote|TestCompleteLocalTask|TestUploadMissingTaskFiles' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/executor_interface.go server/service/task_execution.go server/service/task_execution_decouple_test.go
git commit -m "feat: finalize remote agent artifacts from task files"
```

## Task 6: Kubernetes Executor

**Files:**
- Create: `server/agent/kubernetes_naming.go`
- Create: `server/agent/kubernetes_executor.go`
- Test: `server/agent/kubernetes_executor_test.go`
- Modify: `server/main.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Write failing tests**

Tests cover:

- Pod names are deterministic and DNS-safe.
- Labels include user and project IDs.
- Workspace path is `/workspace/users/<user>/projects/<project>/tasks/<task>/workspace`.
- Command matches Docker `anban run` flags and includes `--artifact-upload-mode direct`.
- Env includes `ANBAN_API_URL`, `ANBAN_DEFAULT_PROJECT`, filtered Claude env.

- [ ] **Step 2: Run tests**

```bash
go test ./server/agent -run TestKubernetes -count=1
```

Expected: FAIL because Kubernetes executor does not exist.

- [ ] **Step 3: Implement**

Use `k8s.io/client-go/kubernetes`, `k8s.io/client-go/rest`,
`k8s.io/client-go/tools/remotecommand`, and `k8s.io/apimachinery`.

Executor responsibilities:

- Build in-cluster config.
- Ensure Agent Pod exists and Ready.
- Mount configured PVC at `workspace_mount_path`.
- Container command is long-lived sleep.
- Exec `mkdir -p` for user/project/task dirs.
- Exec `anban run` with direct artifact mode.
- Stream stdout/stderr to progress and parse final JSON result.
- Set `result.RemoteArtifacts = true`.

- [ ] **Step 4: Verify**

```bash
go test ./server/agent -run 'TestKubernetes|TestDocker|TestBuildAutoMemory' -count=1
go test ./server -run Test -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/agent/kubernetes_naming.go server/agent/kubernetes_executor.go server/agent/kubernetes_executor_test.go server/main.go go.mod go.sum
git commit -m "feat: run agents in ack project pods"
```

## Task 7: ACK Deployment Manifests

**Files:**
- Create: `deploy/k8s/ack-agent-runtime.yaml`
- Test: `server/k8s_agent_runtime_test.go`

- [ ] **Step 1: Write failing tests**

Assert the manifest includes:

- ServiceAccount for server pod scheduling.
- Role verbs for `get`, `list`, `watch`, `create`, `delete` pods.
- Role verbs for `create`, `get` pods/exec.
- No NAS PVC mount on server deployment.
- Agent PVC placeholder is named consistently with config example.

- [ ] **Step 2: Run tests**

```bash
go test ./server -run TestACKAgentRuntimeManifest -count=1
```

Expected: FAIL because manifest does not exist.

- [ ] **Step 3: Implement manifest**

Create ACK runtime YAML with:

- `ServiceAccount/anban-server`
- `Role/anban-agent-runner`
- `RoleBinding/anban-agent-runner`
- `PersistentVolumeClaim/anban-agent-nas` placeholder for ACK NAS storage class
- comments explaining that server does not mount the PVC

- [ ] **Step 4: Verify**

```bash
go test ./server -run 'TestACKAgentRuntimeManifest|TestACKSidecars' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add deploy/k8s/ack-agent-runtime.yaml server/k8s_agent_runtime_test.go
git commit -m "deploy: add ack agent runtime rbac"
```

## Task 8: Full Verification

**Files:**
- No new files unless fixes are needed.

- [ ] **Step 1: Run targeted Go tests**

```bash
go test ./server/config ./server/service ./server/handler ./server/agent ./agent ./server -run 'Kubernetes|TaskArtifact|AgentArtifact|Remote|Docker|Local|Upload|ACK' -count=1
```

- [ ] **Step 2: Run full Go tests**

```bash
go test ./...
```

- [ ] **Step 3: Build binaries**

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

- [ ] **Step 4: Report production rollout notes**

Summarize required ACK config, STS role policy requirements, NAS PVC, and local-mode preservation.
