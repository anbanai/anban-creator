# Autonomous Agent Runtime Profiles and Recovery Design

**Date:** 2026-07-17

## Summary

Anban is a one-click creation platform. Managed Claude Code executions must not pause for permission prompts, clarification questions, or upstream creative approval gates. The platform will enforce a zero-interaction execution contract at both the agent-definition layer and the managed runtime layer.

Kubernetes Agent Jobs will select an immutable runtime image by task type. The initial profiles are `content` for the existing content workflows and `montage` for OpenMontage. Every task keeps a task-scoped NAS workspace so a later execution attempt can resume the same Claude session and file-backed workflow state.

The recovery contract covers all state required to resume correctly. It does not promise to preserve arbitrary files written to `/tmp` or the immutable container filesystem.

## Goals

- Make every managed Claude Code agent complete without asking the user questions.
- Use the official Claude Code `dontAsk` agent permission mode when supported by the pinned CLI.
- Keep the server-owned tool allowlist and deny policy as the authoritative security boundary.
- Give Montage a dedicated image containing the full OpenMontage runtime and dependencies.
- Run OpenMontage from a writable OpenMontage project root under the task NAS workspace.
- Preserve task state, Claude session state, and pipeline checkpoints across one-shot Jobs.
- Resume a terminal task in the same workspace and with the parent execution's runtime identity.
- Keep OpenMontage Backlot data available without exposing its local web server as a product page.

## Non-Goals

- Building an interactive `waiting_input` task state or mid-run question-and-answer UI.
- Removing business-level review flows such as explicit publishing approval.
- Preserving every cache or temporary file created anywhere in the container.
- Letting users select arbitrary runtime images.
- Exposing the OpenMontage Backlot server through a Kubernetes Service or Ingress.

## Autonomous Agent Contract

### Agent Definition

Every managed Claude Code agent under `claudecode/agents/` declares:

```yaml
permissionMode: dontAsk
```

Each agent prompt also carries a behavioral zero-interaction contract because permission mode alone only denies interactive tools; it does not prevent an agent from printing a question and ending its turn.

The behavioral contract is:

1. Never call `AskUserQuestion` or ask a question in assistant text.
2. Resolve missing choices in this order: task input, frozen project defaults, server pipeline defaults, capability registry recommendation.
3. Record defaults, fallbacks, and material decisions in file-backed artifacts and progress logs.
4. Continue automatically when the selected path stays inside the configured provider, capability, budget, and safety envelope.
5. Stop with a structured, terminal diagnosis when execution cannot continue safely or correctly.

Plugin distribution contract tests will treat `permissionMode` as a supported frontmatter field. The Claude Code version pinned in the runtime image must support `dontAsk` before an image is published.

### Managed Runtime Boundary

Agent frontmatter is defense in depth, not the platform security boundary. The runner continues to set:

- an explicit allowed tool surface;
- `Agent`, `ScheduleWakeup`, and `AskUserQuestion` as disallowed tools;
- an Anban MCP-only managed connection;
- a permission callback that denies unresolved tool requests;
- pre-tool hooks that enforce MCP and filesystem boundaries.

When the Go SDK exposes `PermissionModeDontAsk`, the runner will also set it at session level. Until then, the CLI-native agent frontmatter supplies `dontAsk` while the existing allowlist and deny callback keep executions headless. `bypassPermissions` is prohibited because it approves tools outside the allowlist.

### OpenMontage Approval Policy

Anban Montage executions use a fixed full-run preauthorization policy. The Montage adapter manifest records the policy and its origin:

```json
{
  "approval_policy": {
    "mode": "auto",
    "source": "anban_managed_task",
    "scope": "full_run"
  }
}
```

OpenMontage checkpoints remain mandatory. A normally human-gated stage is recorded as approved by the Anban managed-task policy, including the selected option, alternatives considered, cost snapshot, and relevant constraints. The agent must not skip checkpoints or omit decision history.

Provider authentication failures, unavailable required capabilities, hard budget violations, unsafe requests, corrupt required source media, and impossible delivery constraints are terminal failures. The agent writes `failure-diagnosis.md`, registers it as a task file, and does not ask for an alternative.

## Runtime Image Profiles

### Configuration

Kubernetes configuration keeps a required default image and adds server-owned task mappings:

```yaml
agent_image: registry.example.com/creator-agent-content@sha256:...
image_profiles:
  seednote: registry.example.com/creator-agent-content@sha256:...
  montage: registry.example.com/creator-agent-montage@sha256:...
```

`ImageForTask(taskType)` returns the mapped image or the default image. The selected image is used by both the workspace init container and the main Agent container.

The mapping is not accepted from task input, project data, or MCP calls. Configuration validation rejects empty mappings, mutable production tags, and unsupported task-type keys.

### Initial Profiles

`content` contains:

- the `anban` runner;
- Claude Code and the Anban plugin;
- Agent-Reach and shared content dependencies;
- common media utilities required by article, Seednote, moments, ecommerce, and existing video workflows.

`montage` contains:

- the same `anban` runner, Claude Code version, and Anban plugin release;
- a complete, non-sparse OpenMontage checkout;
- OpenMontage Python and Node dependencies;
- FFmpeg and the required rendering runtimes;
- image-build health checks for registry discovery and pipeline loading.

Both images are built from the same source revision and release identifier. Production configuration uses immutable digests.
The image registry retention policy must keep every digest referenced by a task that is still advertised as resumable.

### Execution Lineage

Each `TaskExecution` records:

- `runtime_profile`;
- `runtime_image` as the resolved immutable reference;
- the existing parent execution and resume session identifiers.

An initial attempt resolves the current configured profile. A resume attempt reuses the parent execution's runtime profile and image reference. If that image or the original workspace is unavailable, resume fails closed with an explicit diagnostic rather than silently switching environments.

## Montage Workspace Layout

The immutable image copy at `/app/third_party/OpenMontage` is a runtime template, not the execution root. The workspace init container materializes it into the task PVC only when the writable root is absent:

```text
/workspace/
  openmontage/                 writable OpenMontage root
    projects/<task-id>/        upstream project and checkpoints
  .anban-runtime-home/         task-private Claude runtime home and sessions
  .claude/memory/              project-memory PVC mount
  .anban-creator/              bootstrap and resume inputs
  output/                      normalized Anban deliverables
```

For Montage tasks:

- Claude Code starts with `/workspace/openmontage` as its `cwd`.
- `ANBAN_MONTAGE_SUBMODULE_PATH` points to `/workspace/openmontage`.
- Anban bootstrap inputs are referenced by absolute workspace paths or copied into the upstream task project.
- OpenMontage writes project state under `projects/<task-id>`.
- final deliverables are copied or linked into `/workspace/output` and registered through Anban MCP.

Initialization never overwrites an existing writable root during resume. It verifies the embedded OpenMontage revision against workspace metadata and fails closed on incompatible drift.

## NAS Recovery Contract

### Durable State

The task-scoped NAS PVC mounted at `/workspace` preserves:

- source inputs and materialized attachments;
- drafts, generated assets, scripts, manifests, checkpoints, and decision logs;
- the writable OpenMontage project root;
- the task-private Claude runtime home and session transcripts;
- Anban resume context and supplemental files;
- output files pending or already registered in OSS.

Project memory remains on its separate project-scoped NAS PVC. Published task artifacts remain in OSS. Database rows retain task status, execution lineage, runtime image identity, artifact metadata, and the Claude result/session identifier.

### Ephemeral State

`/tmp`, container-layer changes, and disposable caches remain ephemeral. Tools that need a file for later stages or resume must write it under `/workspace`. Montage-specific environment variables and tool configuration will direct resume-critical caches and working directories into the writable OpenMontage root.

The implementation must document which directories are intentionally ephemeral. It must not redirect every cache into NAS because browser, package-manager, and render caches can create unbounded storage growth without improving recovery correctness.

### Resume Flow

1. The user opens a terminal task and submits supplemental instructions or files through the existing continue-execution UI.
2. The server persists resume input, resets the same task to pending, and creates a child execution.
3. The dispatcher requires the existing task and project PVCs; it never creates replacement PVCs for a resume attempt.
4. The child execution uses the parent runtime image and Claude SessionID.
5. Bootstrap materializes only new resume input and leaves existing workspace state intact.
6. The runner starts Claude Code with `WithResume(sessionID)` and appends the resume context to the prompt.
7. The normal artifact and completion pipeline runs again.

Resume remains available only in Kubernetes NAS mode. It does not reserve a new base task charge. Usage-producing MCP operations and runtime settlement continue to follow their existing billing contracts.

### Retention

Job TTL deletes completed Job objects but does not delete task PVCs. The task workspace remains until the task is deleted. A separate retention policy may be added later, but it must never remove a workspace while the task is still advertised as resumable.

Before automated retention is introduced, the UI and API must expose the real resumability state rather than deriving it only from terminal task status.

## Product Surface

The task detail page remains the product surface for progress, logs, files, failure diagnosis, and continue execution. OpenMontage Backlot is not exposed directly.

Backlot-compatible files remain useful as a structured source for a future native task timeline, scene filmstrip, provider decision view, and cost breakdown. That future UI reads normalized task data or registered artifacts; it does not proxy the local Backlot server.

## Failure Handling

- A permission request outside the allowlist is denied and logged with the tool name.
- A workflow that attempts to ask a user question is treated as an agent contract violation.
- Missing required task defaults use the deterministic resolution order; unresolved hard requirements fail terminally.
- A missing resume PVC, parent execution, SessionID-dependent state, or pinned runtime image fails closed.
- Image/profile mismatch is a permanent dispatch failure, not a retry on another profile.
- OpenMontage revision mismatch during resume produces a diagnosis and preserves the workspace for inspection.
- Artifact upload failure does not delete the NAS workspace.

## Testing

### Agent Contracts

- Every managed Claude agent declares `permissionMode: dontAsk`.
- No managed agent or invoked skill instructs Claude to call `AskUserQuestion`.
- Agent prompts contain the deterministic zero-interaction decision contract.
- Montage records full-run preauthorization and never treats an upstream approval gate as a reason to end successfully without final deliverables.

### Runtime Policy

- Allowed tools run without a permission callback prompt.
- `AskUserQuestion` and unresolved tools are denied.
- `bypassPermissions` is absent from managed execution paths.
- The pinned Claude Code CLI accepts agent-level `dontAsk`.

### Image Routing

- Table-driven tests cover mapped and default task types.
- Both init and main containers use the resolved image.
- Job config hashing changes when the selected image changes.
- Task execution rows persist the resolved profile and immutable image.
- Resume attempts reuse the parent image and reject missing images.

### Workspace and Resume

- New Montage workspaces materialize a writable root once.
- Resume never overwrites existing OpenMontage project files.
- Files under `/workspace` survive a replacement Job; `/tmp` is not part of the contract.
- Resume fails when either required PVC is absent.
- Resume restores supplemental input and passes the parent SessionID to the SDK.
- Deleting a task removes its task workspace PVC; deleting a Job does not.

### Verification

- Run targeted Go tests for runtime policy, Kubernetes Job construction, dispatcher lineage, bootstrap, and task resume.
- Run `go test ./...` and build both server and agent binaries.
- Run Claude plugin contract tests and bump the Claude plugin manifest patch version with agent-definition changes.
- Build both images and run their health checks.
- In a Kubernetes verification environment, execute and resume one Seednote task and one Montage task, confirming Pod image digests, PVC reuse, SessionID lineage, and registered deliverables.

## Rollout

1. Add agent-level `dontAsk`, behavioral contracts, and contract tests.
2. Add generic image-profile configuration and persist runtime image lineage.
3. Build and publish immutable `content` and `montage` images.
4. Materialize the writable Montage root and enforce Montage `cwd`.
5. Deploy configuration with only Montage routed to the new image first.
6. Verify a new Montage run and a resumed Montage run against real Pod image IDs and PVCs.
7. Route Seednote and remaining content tasks through the content image.
8. Monitor permanent dispatch failures, resume failures, NAS utilization, and artifact completion rates.

Rollback changes the task-type mapping for new tasks only. Existing resumable tasks keep their recorded runtime image and workspace contract.
