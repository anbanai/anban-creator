# ACK Agent Pod Executor Design

## Context

Production currently runs Claude Code and the Go server in the same container when
tasks use local execution. Large agent runs can exhaust container memory and take
the server down with them. Production runs on Alibaba Cloud ACK, so the target
runtime should use Kubernetes pods instead of Docker socket scheduling.

The required model is one Agent Pod per project. The Agent Pod mounts NAS and owns
the project workspace. The server does not mount NAS and does not read project
files from the filesystem. Task artifacts are uploaded from the Agent Pod to OSS
using the same direct-upload security model already used by Studio.

Local mode must remain available and must keep its current behavior.

## Goals

- Move production agent execution out of the server container to prevent shared
  memory exhaustion.
- Use ACK/Kubernetes as the production scheduler.
- Keep one long-lived Agent Pod per project, with project data on NAS.
- Keep both NAS and OSS object layouts rooted by user, not only by project.
- Preserve local execution behavior and fallback paths.
- Preserve task progress, files, publishing, billing, refunds, memory updates,
  timeout handling, retries, and observable logs.
- Avoid server-side NAS dependency.

## Non-Goals

- Do not use Docker socket based scheduling for ACK production.
- Do not remove the existing local executor.
- Do not remove the existing `/api/v1/agent/upload` multipart endpoint.
- Do not make OSS objects public.
- Do not trust URLs reported by the Agent Pod as proof of file ownership.

## Recommended Approach

Add a Kubernetes executor alongside the existing local and Docker executors.
The server creates or reuses a project-scoped Agent Pod, execs `anban run` inside
that pod for each task, and watches completion through Kubernetes exec status and
agent heartbeats.

The Agent Pod mounts NAS at `/workspace`. The workspace layout keeps the user as
the root boundary:

```text
/workspace/users/<user_id>/
  projects/<project_id>/
    CLAUDE.md
    memory/
    tasks/<task_id>/
      workspace/
      output/
  shared/
    assets/
    templates/
    cache/
```

OSS task artifacts use the same user-root boundary:

```text
uploads/users/<user_id>/
  projects/<project_id>/
    tasks/<task_id>/
      artifacts/<relative_path>
  shared/
    assets/
    templates/
```

The Agent Pod uploads files directly to OSS with short-lived STS credentials
issued by the server. After upload, it reports a manifest to the server. The
server validates the manifest, writes `task_files`, and finalizes the task using
database and OSS metadata only.

## Alternatives Considered

### A. Server uploads from NAS

The server and Agent Pods would mount the same NAS volume. This is simple for
code reuse because `uploadMissingTaskFiles` can keep reading `WorkDir`, but it
makes the server depend on project storage and widens the blast radius of file
system or permission mistakes. It also conflicts with the desired boundary that
the Agent Pod owns project data.

Rejected.

### B. Agent multipart uploads to server

The Agent Pod could reuse `/api/v1/agent/upload` for every artifact. This keeps
server logic simple, but large files still flow through the server container and
can create memory, bandwidth, and timeout pressure.

Keep as compatibility and local fallback, but do not make it the ACK primary
path.

### C. Agent direct uploads to OSS with server-issued STS

The server prepares an upload, issues scoped STS credentials, and the Agent Pod
uploads directly to OSS. This matches Studio's upload model and keeps large file
traffic out of the server container. The server remains the authority by
validating task ownership, object prefixes, hashes, sizes, and roles before
recording task files.

Recommended.

## Configuration

Extend Claude executor config with a Kubernetes mode:

```yaml
claude:
  executor: kubernetes
  kubernetes:
    namespace: anban
    agent_image: registry.example.com/creator-agent:latest
    service_account: creator-agent-runner
    image_pull_secret: ""
    workspace_mount_path: /workspace
    workspace_pvc_name: anban-creator
    pod_ttl_seconds: 86400
    exec_timeout_seconds: 3600
    resources:
      requests:
        cpu: "500m"
        memory: "1Gi"
      limits:
        cpu: "4"
        memory: "8Gi"
```

Validation rules:

- `claude.executor` accepts `local`, `docker`, and `kubernetes`.
- When `executor=kubernetes`, `namespace`, `agent_image`,
  `workspace_mount_path`, and either `workspace_pvc_name` or an explicit volume
  template are required.
- Kubernetes mode requires OSS storage for direct task artifact uploads.
- Local mode does not require Kubernetes or OSS direct upload settings beyond
  what it already needs today.

## ACK Resources

Add deployment manifests for production:

- Server ServiceAccount, Role, and RoleBinding with permissions for `pods`,
  `pods/log`, and `pods/exec` in the configured namespace.
- Agent Pod template labels:
  - `app.kubernetes.io/name=creator-agent`
  - `anban.ai/user-id=<user_id>`
  - `anban.ai/project-id=<project_id>`
- Existing NAS-backed PVC `anban-creator` is referenced and mounted only by Agent Pods; the runtime manifest does not create it.
- Resource requests and limits on Agent Pods.
- Optional image pull secret.

The server deployment does not mount the NAS PVC.

## Agent Pod Lifecycle

Pod name is deterministic and sanitized from user and project IDs, for example:

```text
creator-agent-u-<short_user_hash>-p-<short_project_hash>
```

The Kubernetes executor:

1. Resolves task, user, and project metadata.
2. Ensures the project Agent Pod exists and is Ready.
3. Creates the user/project/task directories on NAS inside the pod.
4. Builds the same `anban run` command used by Docker Agent execution.
5. Runs the command with Kubernetes `pods/exec`.
6. Streams logs to task progress.
7. Updates heartbeat while exec is active.
8. Leaves the pod alive for project memory reuse.

Same-project concurrency should default to one running task per project. Different
projects may run concurrently. This prevents concurrent writes to `CLAUDE.md`,
project memory, and shared project workspace files.

## Direct Upload Protocol

Studio already uses:

```text
POST /api/v1/uploads/prepare
PendingUpload row
OSS STS credential scoped to one object key
Client uploads directly to OSS
Business endpoint finalizes or claims the pending upload
```

Agent task artifacts should reuse the same mechanism with a new purpose such as
`task_artifact`.

Prepare request for Agent task artifacts includes:

```json
{
  "purpose": "task_artifact",
  "task_id": "<task_id>",
  "relative_path": "output/article.md",
  "filename": "article.md",
  "content_type": "text/markdown",
  "size": 12345,
  "sha256": "<optional precomputed hash>"
}
```

The server derives and signs only this object key:

```text
uploads/users/<user_id>/projects/<project_id>/tasks/<task_id>/artifacts/<relative_path>
```

STS policy allows only write and multipart actions for that object key. The OSS
bucket remains private.

After upload, the Agent reports a manifest:

```json
{
  "task_id": "<task_id>",
  "files": [
    {
      "relative_path": "output/article.md",
      "object_key": "uploads/users/<user_id>/projects/<project_id>/tasks/<task_id>/artifacts/output/article.md",
      "content_type": "text/markdown",
      "size": 12345,
      "sha256": "<hash>",
      "etag": "<oss-etag>",
      "role": "markdown"
    }
  ]
}
```

Server validation:

- The bearer token can access the task.
- The task belongs to the expected user and project.
- Every object key is under the exact task artifact prefix.
- Relative paths are clean and pass task-file skip rules.
- Size, content type, and hash are within policy.
- The OSS object exists, checked with server storage credentials when practical.
- Existing same-path files are updated idempotently.
- Existing same-hash files are deduplicated consistently with current task file
  behavior.

The server stores object keys and task file metadata. It does not trust or store
Agent-supplied public URLs as authority.

## Local Mode Compatibility

Local mode keeps the current paths:

- Server-local execution can still read `result.WorkDir`.
- `uploadMissingTaskFiles` remains the server-side safety net.
- `/api/v1/agent/upload` multipart remains available for desktop or legacy local
  agents.
- `/api/v1/agent/complete` remains the terminal path for local-claimed tasks.

The new direct-upload manifest path is added for remote Agent execution. It must
not make local mode depend on ACK, NAS, or Kubernetes.

## Server Finalization

Remote Agent finalization must not read `result.WorkDir`.

For Kubernetes execution, the task service should finalize from:

- streamed progress logs,
- `ExecutionResult`,
- uploaded `task_files`,
- OSS object metadata,
- database task/project/channel state.

Any workflow rebuild, artifact validation, draft extraction, publish approval,
auto-publish, billing settlement, refunds, and memory merge must use task files
or explicit agent-reported metadata instead of server-local filesystem reads.

If required artifacts are missing, the server should fail the task with a
specific message that names the missing artifact role or path.

## Security Boundaries

- Server signs STS credentials. Agent Pods never receive permanent OSS keys.
- STS credentials are task scoped and short lived.
- Agent Pods cannot write outside their task artifact object key.
- Server stores private OSS object keys and generates signed download URLs when
  needed.
- Agent Pod labels include user and project IDs for observability, but sensitive
  tokens are kept in env vars or mounted secrets.
- Agent API tokens still gate progress, upload preparation, manifest report, and
  completion calls.
- No secrets are written into task artifacts, logs, or manifests.

## Failure Handling

- Pod creation failure: task fails with a Kubernetes scheduling error and no
  billing charge beyond existing refund rules.
- Pod not Ready: wait until timeout, then fail with an actionable message.
- Exec failure: record logs and execution error.
- Agent upload failure: retry direct upload once with fresh STS credentials.
- Manifest validation failure: fail task, do not record untrusted files.
- Server restart during task: heartbeat/watchdog reaps stale tasks; future work
  can add pod reattachment if needed.
- Agent Pod OOM: Kubernetes marks the container terminated; server records a
  clear OOM failure and releases the project slot.

## Rollout Plan

1. Add config and validation while keeping default executor unchanged.
2. Add direct-upload task artifact purpose and manifest endpoint.
3. Add Agent side workspace scanner and OSS direct uploader.
4. Add Kubernetes executor behind `claude.executor=kubernetes`.
5. Add ACK manifests for RBAC and the Agent Pod template, referencing the existing NAS PVC without creating it.
6. Run staging with one internal project and compare outputs against local mode.
7. Enable per environment through config, then gradually enable production.

Rollback is config-only: switch `claude.executor` back to `local` or `docker`.
Local mode code paths remain in place.

## Testing

Unit tests:

- Config accepts and validates `executor=kubernetes`.
- Invalid Kubernetes config fails startup validation.
- Pod naming is deterministic, DNS-safe, and scoped by user plus project.
- Kubernetes command construction matches the existing Agent command contract.
- Direct upload policy creates object keys under
  `uploads/users/<user_id>/projects/<project_id>/tasks/<task_id>/artifacts/`.
- Manifest validation rejects cross-user, cross-project, cross-task, absolute,
  parent-directory, and unprepared object keys.
- Agent workspace scanner skips runtime directories and dotfiles.
- Local task completion and `/api/v1/agent/upload` behavior remain unchanged.

Integration or staging tests:

- ACK Agent Pod can execute a task and keep Server memory stable.
- Agent uploads artifacts directly to OSS and Server records `task_files`.
- Auto-publish and approval-required flows work from uploaded task files.
- Same-project tasks are serialized.
- Different-project tasks run concurrently.
- Pod OOM produces a failed task without killing the server.

## Open Implementation Notes

- The current `agent/main.go` reports result and completion but does not perform
  a final workspace sweep. Kubernetes mode needs Agent-side artifact scanning
  before final result reporting.
- The current server finalization path uploads missing files from `WorkDir`.
  Kubernetes mode needs a remote finalization branch that treats uploaded
  `task_files` as authoritative.
- Existing Studio direct-upload logic uses `uploads/pending/...` and business
  finalize. Agent artifact uploads should reuse the credential issuer and policy
  mechanics, but write directly to task artifact prefixes and finalize via a
  manifest endpoint.
