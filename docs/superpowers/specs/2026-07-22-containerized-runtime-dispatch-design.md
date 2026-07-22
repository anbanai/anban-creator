# Containerized Runtime Dispatch Design

## Context

The Server currently has three execution modes with two different managed-runtime
lifecycles:

- `local` runs Claude Code directly from the Server process.
- `docker` reuses a persistent Article container through `docker exec`, while
  Seednote and Montage use one-shot containers that inherit the persistent
  container's workspace volume.
- `kubernetes` creates one-shot Jobs that bootstrap their own task inputs, upload
  artifacts, and report completion.

The Docker path therefore couples the Article image to a persistent container,
uses host-readable workspaces, and requires Agents and Skills to call
`prepare_workspace`, create an output directory, and carry a `$DIR` variable
through every workflow. These responsibilities belong to the managed runtime,
not to content workflows.

## Goals

- Run every Server-managed task in a newly created container selected from the
  configured runtime images.
- Give Docker and Kubernetes one managed Job protocol for execution records,
  bootstrap, progress, heartbeat, artifact publication, completion, cancellation,
  reconciliation, and recovery.
- Keep task workspaces outside the Server filesystem.
- Preserve task workspace and project memory state across retries and explicit
  task resume.
- Remove workspace discovery and directory lifecycle control from Agents and
  Skills.
- Remove obsolete compatibility paths rather than translating deprecated config.

## Non-Goals

- Removing or containerizing the desktop Agent Runner. It remains a user-owned
  execution surface that claims tasks through the existing API.
- Replacing Docker or Kubernetes with another orchestrator.
- Changing the content, quality, publishing, or billing behavior of individual
  task workflows.
- Uploading arbitrary runtime caches, input files, session state, or project
  memory as user-visible artifacts.

## Chosen Architecture

The Server supports only `docker` and `kubernetes` as managed executors. Both
implement a common `RuntimeDispatcher` contract:

```go
type RuntimeDispatcher interface {
    ResolveRuntime(taskType string) config.RuntimeImageSelection
    Dispatch(context.Context, *model.TaskExecution, *model.Task) (*RuntimeIdentity, error)
    Inspect(context.Context, *model.TaskExecution) (*ExecutionState, error)
    Delete(context.Context, *model.TaskExecution) error
}
```

Infrastructure-specific workspace and project-memory lifecycle interfaces remain
separate from dispatch so resource deletion has an explicit owner.

`TaskService` always creates a durable `TaskExecution`, persists its selected
runtime profile and complete image reference, and dispatches it through the
configured `RuntimeDispatcher`. Kubernetes continues to create Jobs and PVCs.
Docker creates one task container per execution plus task and project-memory
volumes.

The old synchronous `DockerExecutor`, persistent-container `docker exec` path,
and Server `LocalExecutor` wiring are removed. Shared result types and helpers
still needed by the standalone Agent Runner move into focused files rather than
retaining an unused Server executor.

## Runtime Image Configuration

Runtime selection is independent of the infrastructure scheduler:

```yaml
claude:
  executor: "${ANBAN_AGENT_EXECUTOR}"
  runtime_images:
    article: "${ANBAN_AGENT_IMAGE_ARTICLE}"
    seednote: "${ANBAN_AGENT_IMAGE_SEEDNOTE}"
    montage: "${ANBAN_AGENT_IMAGE_MONTAGE}"

  docker:
    network: "${ANBAN_AGENT_DOCKER_NETWORK:-anban-creator-network}"
    cpu_cores: 2
    memory_mb: 4096
    timeout_sec: 3600

  kubernetes:
    namespace: "${ANBAN_AGENT_NAMESPACE:-anbanai-prod}"
```

`runtime_images` must contain exactly the supported runtime profiles `article`,
`seednote`, and `montage`, with non-empty values. The Server does not accept the
deleted `article_image`, `image_profiles`, `container_name`, `workspace_dir`, or
`local` executor values.

Task types map to runtime profiles in code. Article-compatible task types such as
Article, Moments, and Ecommerce use `article`; Seednote uses `seednote`; Montage
uses `montage`. The task-to-profile mapping is distinct from image configuration,
so configuration cannot accidentally introduce a new task type.

Docker Compose supplies local or registry-backed image references. Kubernetes
deployment supplies immutable digests or unique release tags. A resumed execution
inherits its parent execution's persisted profile and image instead of resolving
the current configuration again.

## Docker Dispatch

The Docker dispatcher uses deterministic, ownership-labeled resources:

- execution container: one per `execution_id`;
- task workspace volume: one per `task_id`;
- project memory volume: one per `project_id`.

The dispatcher performs these steps:

1. Validate that the execution's persisted runtime identity matches the task and
   the initial configured selection. Retries and resumes accept the inherited
   parent identity.
2. Create the project-memory and task-workspace volumes for an initial execution.
   A retry or resume requires the existing task volume and fails permanently if
   it is missing.
3. Create the execution container from `TaskExecution.RuntimeImage`, mount the
   task volume at `/workspace`, and mount project memory at the runtime-owned
   memory path.
4. Apply the configured Docker network, CPU, memory, process, non-root user, and
   timeout controls.
5. Copy a short-lived bootstrap credential into the created container at a
   runtime secret path before starting it. The credential is not stored in an
   environment variable, command argument, image, or log.
6. Start `anban job` with the Server URL, execution ID, `/workspace`, and the
   credential file path.

The Agent container never receives the Docker socket. Only the Server controls
Docker resources.

Dispatch is idempotent. If a deterministic resource already exists, the
dispatcher verifies all required labels and immutable configuration. A matching
resource is reused; an identity or configuration mismatch is a permanent
dispatch failure. Resource discovery and orphan cleanup use labels, not name
prefix guesses.

## Bootstrap And Workspace Contract

Docker and Kubernetes execute the same `anban job` bootstrap flow. Bootstrap
materializes task inputs, project configuration, reference assets, resume state,
and runtime settings inside the mounted task workspace.

The runtime, before invoking Claude Code:

- creates `/workspace/output` as a real directory;
- creates all runtime-private session and cache paths outside `output`;
- supplies task and execution identity as structured runtime context;
- never asks an Agent or Skill to infer task identity from CWD or a directory
  name;
- uses `/workspace` as the normal task CWD;
- materializes Montage at `/workspace/openmontage`, runs Montage from that
  project root, and creates `openmontage/output` as a link to
  `/workspace/output`.

All managed workflows therefore use `output/<filename>` regardless of runtime
profile. Runtime code owns directory creation and validates that `output` is not
a symlink before artifact collection.

## Agent, Skill, And MCP Contract

The `prepare_workspace` MCP tool and `WorkspaceService` are removed completely,
including registration, service wiring, tests, tool allowlists, and documentation.

Claude Agent Markdown, Codex Agent TOML, Skills, hooks, references, and plugin
documentation are updated together:

- remove `$DIR` and replace final artifact references with explicit
  `output/<filename>` paths;
- remove `prepare_workspace` calls and availability checks;
- remove `mkdir -p`, output-directory discovery, relative-path fallback, and
  directory move or rename steps;
- remove `.task-context` and CWD-name task-ID inference instructions;
- retain logical artifact filenames, stage checkpoints, failure-state files,
  delivery validation, and MCP tool calls unrelated to directory management;
- keep intermediate files that must survive resume inside `output`; disposable
  caches stay in runtime-owned paths;
- preserve Claude and Codex Agent parity.

Changes under `plugins/` update both native manifest versions. The plugin
submodule is committed and published before the parent repository records the new
submodule commit.

## Artifact Publication

Only `/workspace/output` is eligible for artifact publication. The Agent never
uploads bootstrap inputs, Claude sessions, runtime home, caches, project memory,
or other container files.

OSS deployments continue to use the existing direct-upload preparation and
manifest APIs. Non-OSS storage uses a new execution-authenticated streaming
upload endpoint:

1. The Agent sends one bounded file stream with task ID, execution ID, relative
   path, content type, size, and SHA-256 metadata.
2. The Server validates task/execution ownership and a normalized path under
   `output`.
3. The Server streams the body into the configured storage provider without
   buffering the complete file in memory.
4. The Server returns the stored object identity.
5. The Agent submits the final manifest after every file is stored.

Manifest finalization remains the atomic publication boundary. Successful and
failed executions both attempt to upload collectible output. Failed execution
artifacts retain the existing `collected` visibility semantics and do not become
successful delivery artifacts.

## Execution State And Reconciliation

The Kubernetes-specific service and reconciler naming becomes runtime-neutral.
The shared execution state model covers created, dispatching, starting, running,
succeeded, failed, and cancelled states.

The Docker inspector derives state from the labeled execution container and its
exit information. Heartbeats still come from the Agent runtime. After a Server
restart, the reconciler finds active executions through database state and
verifies their Docker resources by label and deterministic identity.

The following rules prevent ambiguous recovery:

- an active matching container is observed, not recreated;
- a stopped container with a known exit code is finalized once;
- a missing pre-start container follows the bounded pre-start replacement policy;
- a missing container after confirmed start is a runtime failure;
- a stale heartbeat stops the current container and finalizes through the
  existing guarded state transition;
- completion, billing settlement, artifact publication, and cleanup remain
  idempotent under duplicate reports or reconciler retries.

Project memory retains the per-project concurrency cap of one managed task.

## Cancellation And Resource Lifecycle

Cancellation transitions database state before destructive infrastructure
cleanup. The dispatcher then stops and removes the current execution container.
The task workspace volume remains available for explicit resume.

Terminal execution cleanup removes only the execution container and secret
material. Failure to delete the container is recorded in the existing cleanup
lease and retry mechanism and does not roll back a finalized task.

Task and project resources have separate lifetimes:

- task workspace volume: retained across attempts and resumes; deleted only when
  the task is permanently deleted;
- project memory volume: retained across tasks; deleted only through project
  memory lifecycle deletion;
- execution container: deleted after terminal finalization and artifact handling;
- remote task artifacts: deleted through the existing task deletion flow.

Task deletion first cancels any active execution, then deletes the task volume.
If workspace deletion fails, the task database record is not deleted. This keeps
resource ownership recoverable and prevents silent volume leaks.

## Docker Compose And Deployment

Docker Compose removes the persistent `agent` service, its `sleep infinity`
override, `depends_on.agent`, workspace bind mount, and the deprecated Docker
container/workspace environment variables.

The Server keeps access to the Docker socket and joins the configured Agent
network. It no longer mounts the task workspace. Runtime images are built and
tagged independently as `creator-agent-article`, `creator-agent-seednote`, and
`creator-agent-montage`.

Kubernetes keeps its existing one-shot Job topology, PVCs, workload identity,
and ServiceAccount verification. Its implementation is adapted to the common
dispatcher types without weakening Kubernetes-specific security checks.

## Testing

Implementation follows test-driven development. Coverage includes:

### Configuration And Selection

- accept only `docker` and `kubernetes` managed executors;
- require exactly three runtime images;
- reject every deleted field and deprecated environment-driven shape;
- map every supported task type to its expected profile and image;
- preserve parent runtime identity for resume.

### Docker Dispatcher

- build the expected container, volume, labels, mounts, network, resources,
  non-root identity, secret path, and `anban job` command;
- create resources idempotently and reject ownership/config mismatches;
- require an existing task volume for retry or resume;
- inspect all execution states and preserve exit diagnostics;
- stop/remove execution containers without deleting task volumes;
- delete task and project volumes only through their lifecycle APIs.

### Reconciliation And State

- cover created, running, succeeded, failed, cancelled, missing, and heartbeat
  timeout states;
- recover observation after Server restart;
- prevent duplicate completion, settlement, artifact publication, and cleanup;
- exercise cleanup lease retry behavior.

### Bootstrap And Artifacts

- materialize a fresh Docker volume and a resumed volume;
- pre-create a safe `output` directory and the Montage output link;
- scan only `output` and reject symlink/path escapes;
- verify OSS direct upload and provider-neutral streaming upload;
- enforce size, SHA-256, task identity, execution identity, and atomic manifest
  publication;
- retain collectible failure artifacts.

### Plugin Contracts

- reject `prepare_workspace`, `$DIR`, output-directory `mkdir`, CWD-derived task
  identity, and directory fallback instructions across owned plugin assets;
- assert required workflow artifacts still use explicit `output/<filename>`
  paths;
- assert Claude Markdown and Codex TOML parity;
- assert both native manifest versions advance together.

### Final Verification

- targeted Go tests for configuration, dispatch, reconciliation, bootstrap,
  artifacts, and plugin contracts;
- `go test ./...`;
- build the Server and Agent binaries to explicit `/tmp` paths;
- build all three Agent images;
- run a real Docker smoke test covering dispatch, bootstrap, artifact upload,
  completion, cleanup, and resume-volume reuse.

## Migration And Release

This is a forward-only cutover. There is no compatibility parser or fallback for
the removed executor values, config fields, environment variables, MCP tool, or
workspace instructions.

Deployment order:

1. Publish the updated plugin submodule and three runtime images.
2. Configure `runtime_images` with the published image identities.
3. Deploy the Server containing the common dispatcher and new bootstrap/artifact
   contract.
4. Verify a Docker task for each runtime profile and one resume flow.
5. Verify Kubernetes Article, Seednote, and Montage Jobs still complete through
   the common protocol.

A deployment with old configuration fails validation at startup with explicit
deleted-key errors. This prevents silently running a persistent container or an
unexpected fallback image.

## Acceptance Criteria

- No Server-managed task runs through `docker exec` or a Server-local Claude
  process.
- Every Docker and Kubernetes task executes in a newly scheduled container from
  its persisted runtime image.
- Docker Compose has no persistent Agent service and the Server has no task
  workspace mount.
- Retry and resume reuse the task workspace resource without recomputing the
  runtime image.
- Agents and Skills contain no workspace preparation or directory lifecycle
  workflow.
- Only runtime-owned `output` content is published as task artifacts.
- Cancellation, restart recovery, artifact publication, settlement, and cleanup
  remain idempotent.
- All targeted, full, build, image, plugin-contract, and Docker smoke checks pass.
