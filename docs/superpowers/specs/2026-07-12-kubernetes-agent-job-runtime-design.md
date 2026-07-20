# Kubernetes Agent Job Runtime Design

## Context

The current Kubernetes executor keeps one long-lived Agent Pod per project and
uses `pods/exec` plus a tar stream to copy each task bundle into a NAS-backed
workspace. This creates mutable cross-task state in the execution workspace and
makes task startup depend on the ownership and mode of files left by earlier
containers. A resumed task can therefore fail before Claude starts when a
non-root Agent cannot overwrite root-owned `.anban-creator`, `.claude`, or
`CLAUDE.md` entries.

The new runtime does not preserve that model. Production execution moves to one
immutable Kubernetes Job per execution attempt. Task workspaces are ephemeral,
Agent memory is the only NAS-backed filesystem state, and user-facing results
are private OSS objects registered through `task_files`.

## Goals

- Run every cloud execution attempt in a dedicated Kubernetes Job.
- Remove long-lived project Agent Pods, `pods/exec`, and tar-based workspace
  injection.
- Isolate task workspaces with `emptyDir` volumes.
- Give Claude Code native, low-latency access to persistent project memory.
- Store user-facing results in private OSS and expose them through validated
  `task_files` metadata.
- Make execution dispatch, completion, cancellation, timeout, billing, and
  artifact publication idempotent.
- Use short-lived workload identity instead of long-lived API keys in Jobs.
- Keep Server independent of NAS mounts and task filesystem reads.

## Non-Goals

- Preserve or migrate long-lived Agent Pods or their NAS task workspaces.
- Retain the Kubernetes `pods/exec` execution path.
- Persist an entire `.claude` directory, Claude sessions, caches, credentials,
  plugin state, or task workspaces.
- Build a separate memory locking, snapshot, merge, or reconciliation system.
- Guarantee multi-session memory consistency beyond Claude Code's own behavior.
- Make OSS objects public.
- Automatically retry Agent or model work after the Agent has started.

## Decisions

### One Job per execution attempt

`TaskExecution` is the unit of cloud execution. Each attempt has a unique ID and
one deterministic Kubernetes Job name. A retry or manual resume creates a new
attempt and a new Job. No Job or workspace is reused.

The Job uses:

- `restartPolicy: Never`;
- `backoffLimit: 0`;
- `activeDeadlineSeconds` derived from runtime configuration;
- `ttlSecondsAfterFinished` for automatic cleanup;
- explicit CPU and memory requests and limits;
- labels for task, execution, user, and project identity.

Kubernetes schedules the work but does not decide business retries or task
terminal state.

### Three data planes

The runtime has three deliberately separate storage planes:

| Data | Durable store | Job path and lifecycle |
| --- | --- | --- |
| Project Agent memory | Project-scoped NAS PVC | Mounted read-write as Claude Code's `autoMemoryDirectory`; survives Jobs |
| Task workspace and downloaded inputs | `emptyDir` | Mounted at `/workspace`; deleted with the Job |
| User-facing results | Private OSS plus `task_files` | Uploaded directly by Agent; independent of Job deletion |

Database rows remain authoritative for tasks, attempts, billing, artifact
metadata, and terminal state. Kubernetes objects are runtime projections, not
business records.

### Project memory on NAS

Each project owns a dedicated PVC provisioned by the configured NAS CSI storage
class. The PVC contains only Claude Code project memory, for example:

```text
MEMORY.md
topics/
```

The Agent Job mounts the PVC at `/workspace/.claude/memory` and passes that path
as Claude Code's `autoMemoryDirectory`. Claude reads and updates the files
directly. There is no URL, archive download, extraction, or memory upload step.

The runtime must not persist the whole `.claude` directory. Home-directory
state required for a single run uses an `emptyDir` mount and disappears with the
Job. Project deletion explicitly deletes the project's memory PVC according to
the product's deletion policy; Job cleanup never deletes it.

Multiple Jobs for one project may mount the memory PVC concurrently. This
design explicitly assumes Claude Code handles concurrent session memory. Anban
does not add locks, serialize project Jobs, or merge memory files.

### Results in private OSS

The Agent scans the ephemeral workspace for allowed result files, requests
short-lived scoped OSS credentials, uploads directly to an attempt-specific
prefix, and reports a manifest. The Server validates task ownership, attempt
ownership, key prefix, path, size, hash, MIME type, and artifact role before
recording or promoting `task_files`.

An uploaded object is not a successful task result by itself. Files remain
pending until the current attempt completes successfully. Failed-attempt files
may be retained as recovery or diagnostic material but are not presented as
successful deliverables. Unclaimed objects are removed by TTL cleanup.

## Components

### Task execution model

Add a `TaskExecution` record with at least:

- execution ID and task ID;
- attempt number;
- execution target;
- Kubernetes namespace and Job name;
- lifecycle status;
- dispatch, start, heartbeat, completion, and failure timestamps;
- terminal reason and structured runtime diagnostics;
- whether the Agent reached the started boundary;
- artifact manifest status;
- idempotency/version field for terminal CAS.

`Task.current_execution_id` identifies the only attempt allowed to finalize the
user-visible task. Reports from older attempts are stored for diagnostics but
cannot mutate task state, billing, or published artifacts.

### Kubernetes Job dispatcher

The dispatcher converts an execution record into a Job specification and
creates it using the deterministic execution name. A repeated dispatch treats
an identical existing Job as success. A conflicting Job identity is an error
and is never silently adopted.

The dispatcher ensures the project memory PVC exists before creating the Job.
PVC creation is idempotent and tied to project lifecycle, not execution
lifecycle.

The dispatcher returns after Kubernetes accepts the Job. It does not wait for
Pod readiness or stream process output.

### Agent bootstrap

The Agent container starts the existing runner in a Job-oriented mode. Before
Claude starts, it calls a bootstrap endpoint with the execution ID and a
projected Kubernetes service-account token. The endpoint:

1. validates the token with Kubernetes TokenReview for the `anban-server`
   audience;
2. resolves the bound Pod and owning Job;
3. verifies Job labels against the current `TaskExecution`;
4. returns task/project configuration and signed input downloads;
5. issues a short-lived execution credential bounded by the Job deadline.

The execution credential is used for MCP, progress, heartbeat, artifact, and
completion calls. It is scoped to one user, project, task, and execution and is
not reusable by another attempt. No long-lived user API key is embedded in the
Job or a Kubernetes Secret.

Bootstrap materializes settings, `CLAUDE.md`, reference images, input
attachments, resume inputs, and runtime-specific files into `/workspace`.
Project memory is already present through the PVC mount and is not part of the
bootstrap payload.

### Job reconciler

A Server-side control loop watches Jobs and periodically reconciles database
records with Kubernetes state. Database compare-and-swap operations make the
loop safe when multiple Server replicas observe the same event.

The Agent completion callback is the primary source of business success. The
Job reconciler is the infrastructure fallback for scheduling failures, image
pull failures, PVC mount failures, OOM termination, deadline expiry, missing
completion callbacks, and deleted Jobs.

## Execution State Machine

`TaskExecution` uses the following lifecycle:

```text
created -> dispatching -> starting -> running
                                  -> succeeded
                                  -> failed
                                  -> cancelled
                                  -> timed_out
```

Transitions have these meanings:

- `created`: the database attempt exists but no Job has been accepted;
- `dispatching`: the dispatcher is ensuring the PVC and creating the Job;
- `starting`: Kubernetes accepted the Job but Agent bootstrap has not completed;
- `running`: bootstrap succeeded and the Agent crossed the started boundary;
- terminal states: exactly one CAS finalized the attempt.

The user-visible `Task` remains `pending`, `running`, `completed`, or `failed`.
It points at the current execution rather than embedding Kubernetes lifecycle
details in the task status.

## Completion and Terminal Authority

The Agent sends heartbeats and progress with its execution credential. On exit,
it submits the result, artifact manifest, usage, and terminal status through an
idempotent completion endpoint.

Completion succeeds only when:

- the execution is the task's current execution;
- the execution is `starting` or `running`;
- the credential is scoped to that execution;
- the artifact manifest passes validation when success requires deliverables.

The terminal CAS is the single gate for:

- task status changes;
- artifact promotion;
- billing settlement or refund;
- slot release;
- downstream dispatch;
- notifications and publishing.

A duplicate completion returns the stored terminal outcome. A stale attempt
returns a conflict and cannot overwrite a newer attempt.

## Failure Handling

### Pre-start infrastructure failures

Scheduling, image pull, PVC provisioning, PVC mount, and bootstrap failures are
recorded with a structured infrastructure reason. Because the Agent has not
crossed the started boundary, the dispatcher may create a bounded replacement
attempt according to policy.

### Post-start execution failures

After the Agent starts, failures are never retried automatically. This avoids
duplicated model costs, MCP side effects, uploads, and publishing actions. The
task enters a recoverable failed state and a user resume creates a new attempt.

### Missing completion

If Kubernetes reports Job completion but the Server has not received Agent
completion, the reconciler waits a short configurable grace period. It then
fails the attempt as an execution protocol error. A late completion after the
terminal CAS is diagnostic only.

### Timeout and heartbeat loss

`activeDeadlineSeconds` is the hard runtime ceiling. A heartbeat watchdog
detects stalled Agents before that ceiling when the Job is unhealthy. Either
path terminalizes the attempt once and deletes the Job.

### Cancellation

Cancellation first CASes the current execution to `cancelled`, then deletes the
Job. This ordering prevents a racing Agent completion from winning after the
user cancelled. Cancellation does not delete project memory or already durable
OSS objects; unpromoted attempt artifacts follow TTL cleanup.

## Security

The Agent container runs with:

- `runAsNonRoot: true` and the image's explicit Agent UID/GID;
- `allowPrivilegeEscalation: false`;
- all Linux capabilities dropped;
- `seccompProfile: RuntimeDefault`;
- a read-only root filesystem;
- writable `emptyDir` mounts only for `/workspace`, `/tmp`, and required
  per-run home state;
- only the current project's memory PVC mounted read-write;
- `automountServiceAccountToken: false` plus an explicit projected token volume
  with the `anban-server` audience.

The Agent service account has no Kubernetes API permissions. The Server service
account may manage Jobs and project memory PVCs and inspect Pods and logs, but
it does not receive `pods/exec` permission.

Network policy should allow Agent Jobs to reach only DNS, the Anban Server, OSS,
and explicitly configured model or media providers.

## Kubernetes Resources and RBAC

The production runtime contains:

- Server ServiceAccount;
- Agent Job ServiceAccount with no API permissions;
- namespace-scoped Server Role/RoleBinding for Jobs, Pod read/log access, PVC
  lifecycle, and Events;
- a minimal Server ClusterRole/ClusterRoleBinding that permits only
  `create` on `authentication.k8s.io/tokenreviews`;
- NAS CSI StorageClass reference for project memory PVCs;
- optional NetworkPolicy and Pod security admission labels.

Remove `pods/exec` permissions and all manifests or tests that require a
long-lived Agent Pod. The Server Deployment still does not mount NAS.

## Configuration

Kubernetes configuration should describe the Job runtime rather than Pod reuse:

```yaml
claude:
  executor: kubernetes
  kubernetes:
    namespace: anban
    agent_image: registry.example.com/creator-agent:latest
    service_account: creator-agent-runner
    image_pull_secret: ""
    memory_storage_class: alicloud-nas
    memory_size: 1Gi
    active_deadline_seconds: 3600
    completion_grace_seconds: 30
    ttl_seconds_after_finished: 600
    pre_start_retry_limit: 1
    resources:
      requests:
        cpu: "500m"
        memory: "1Gi"
      limits:
        cpu: "4"
        memory: "8Gi"
```

Delete configuration for project Pod revision, reusable Pod TTL, exec timeout,
workspace PVC name, and workspace mount path. There is no compatibility parser
or deprecation bridge for those settings.

## Removal Scope

Implementation removes or replaces:

- project-scoped Kubernetes Pod naming and reuse;
- Pod drift hashing and recreation;
- root workspace permission init containers;
- tar bundle creation and `copyWorkspaceBundle`;
- `execAgentCommand` and SPDY remote command support;
- Kubernetes workspace path ownership and NAS task directories;
- remote memory archive capture and OSS memory merge for Kubernetes mode;
- Kubernetes `pods/exec` RBAC;
- `pod_revision`, `pod_ttl_seconds`, `exec_timeout_seconds`,
  `workspace_mount_path`, and `workspace_pvc_name` runtime configuration.

Local and desktop execution remain separate products, but Kubernetes mode does
not retain compatibility branches for the removed Pod runtime.

## Observability

Logs and metrics must use execution identity as the primary runtime dimension:

- task ID, execution ID, Job name, Pod UID, user ID, and project ID;
- dispatch latency, pending duration, bootstrap duration, run duration, and
  completion latency;
- terminal source: Agent callback, Job reconciler, timeout, or cancellation;
- structured Kubernetes reason and container exit code;
- heartbeat age and artifact manifest state.

Job logs are diagnostic. Durable user-visible progress continues through Agent
progress callbacks so TTL cleanup does not erase task history.

## Testing

### Unit and contract tests

- deterministic Job and PVC naming;
- Job security context, volumes, deadlines, retry, TTL, and labels;
- absence of `pods/exec`, long-lived Pod commands, and NAS workspace mounts;
- project memory PVC mounted only at the configured memory path;
- bootstrap identity binding and execution credential scope;
- attempt transition table and stale completion rejection;
- idempotent terminal settlement, artifact promotion, refund, and cancellation;
- pre-start retry allowed and post-start retry forbidden;
- failed-attempt artifacts never presented as successful deliverables;
- manifest and deployment RBAC parsed structurally.

### Integration tests

- create a Job, bootstrap, run a minimal Agent task, upload a result, complete,
  and observe TTL cleanup;
- run two attempts and prove the older attempt cannot finalize the task;
- cancel a running Job while completion races;
- simulate image pull, PVC mount, OOM, heartbeat, and completion-loss failures;
- run consecutive Jobs for one project and verify memory survives while task
  workspace files do not;
- run concurrent Jobs for one project and verify both receive the same project
  memory mount, without asserting merge behavior owned by Claude Code.

### Required verification

- targeted `server/agent`, `server/service`, Agent runner, and Kubernetes
  manifest tests;
- `go test ./...`;
- `go build -o /tmp/anban-creator-server ./server`;
- `go build -o /tmp/anban ./agent`;
- a real ACK smoke test covering Job creation, memory PVC mounting, direct OSS
  upload, completion, and cleanup before production rollout.

## Acceptance Criteria

- Cloud tasks run only through one-shot Kubernetes Jobs.
- The Server never uses `pods/exec` or copies a workspace tar stream.
- Each Job has a new ephemeral workspace and cannot observe a prior task's
  workspace files.
- Project `.claude/memory` survives across Jobs through its own NAS PVC.
- User-facing results survive Job deletion through private OSS and `task_files`.
- A stale or duplicate execution cannot double-settle billing or overwrite task
  state.
- Kubernetes failures produce specific, recoverable task errors.
- No compatibility code remains for the long-lived project Pod executor.
