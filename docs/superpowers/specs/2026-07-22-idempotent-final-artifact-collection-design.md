# Idempotent Final Artifact Collection Design

## Goal

Make the existing final artifact-collection hook the only Agent-side authority
for workspace deliverables. The hook runs after every execution regardless of
business success or failure, uploads the final workspace state idempotently,
and submits one execution-scoped manifest before terminal completion.

Keep `list_task_files` as a read-only terminal query for task inspection,
recovery diagnosis, and external automation. Agent workflows must not depend on
it to prove that the current execution delivered its files.

## Context

The runner already calls `UploadWorkspaceArtifacts` after the Agent process
returns and before it reports `/api/v1/agent/complete`. This call is independent
of `result.Success`; an upload failure turns an otherwise successful result into
a failure. The server already stores execution files as `pending` and atomically
changes them to `published` on success or `collected` on failure.

The remaining problems are contract clarity and retry behavior:

- The runner uploads every discovered file again when final collection is
  retried, even when the same bytes already exist at the deterministic object
  key.
- An object upload may succeed while manifest submission fails, leaving no
  durable database row that can identify the already uploaded bytes on retry.
- `generate_image` and related MCP paths register server-generated files during
  execution, while the final workspace manifest is submitted separately.
- Some shipped Agent instructions present `list_task_files` as an execution-time
  delivery check even though it returns only terminal `published` and
  `collected` rows, not the current `pending` manifest.

## Decision

Evolve the existing final artifact-collection hook. Do not add another Hook,
sidecar collector, Agent tool call, or lifecycle phase.

- Use `(task_id, execution_id, relative_path)` as the logical file identity for
  one execution.
- Use SHA-256 and size as the content fingerprint for idempotent upload.
- Keep the current execution-scoped deterministic OSS key layout.
- Make prepare hash-aware so a retry can skip an object whose stored SHA-256 and
  size already match.
- Always submit the workspace manifest, including an empty manifest.
- Treat the effective execution manifest as the union of the final workspace
  manifest and server-generated MCP artifacts owned by the same execution.
- Keep terminal publication immutable: a terminal execution cannot accept file
  or manifest changes.
- Remove `list_task_files` from Agent-side completion and delivery validation.
- Retain `list_task_files` as an owned, read-only query over terminal files.

This is a forward-only runtime contract. No compatibility path is required for
Agents that use `list_task_files` as a completion gate.

## Runtime Flow

```text
Agent exits with success or failure
              |
              v
existing final artifact hook
  - stop/await execution child processes
  - scan collectable workspace files
  - compute stable size + SHA-256
  - prepare each upload
  - upload only missing or changed objects
  - submit the complete workspace manifest
              |
              v
report /complete
              |
              v
server validates effective execution manifest
              |
       +------+------+
       |             |
    success        failure
       |             |
 pending ->       pending ->
 published        collected
```

The upload and manifest steps remain before `/complete`. A successful Agent run
cannot become a successful task when artifact collection or manifest submission
failed.

## Idempotent Upload Protocol

### Prepare Request

The existing prepare request continues to include:

- `task_id`
- `execution_id`
- `relative_path`
- `content_type`
- `size`
- `sha256`

The server derives the object key from the authenticated task, execution, and
clean relative path. Client-supplied object keys are never authoritative.

### Prepare Response

Extend the prepare response with an explicit `upload_required` boolean.

- If no object exists at the derived key, return `upload_required=true` and the
  existing scoped upload credentials.
- If the object exists and its stored SHA-256 and size match the request, return
  `upload_required=false` plus the existing object identity.
- If the object exists but the fingerprint differs, return
  `upload_required=true`; the running execution may replace that logical path.
- If the execution is stale or terminal, reject the request before returning
  upload authority.

Every uploaded object records its SHA-256 as execution-scoped OSS object
metadata. Size comes from object storage. Only the authenticated runner may set
this metadata within the execution prefix. This allows a retry to recognize the
case where the PUT succeeded but manifest submission did not.

The manifest endpoint continues to verify that every object key is inside the
authenticated task and execution prefix. It also verifies stored size and
SHA-256 metadata against the submitted manifest before persisting rows.

### Retry Semantics

- Same path and same fingerprint: no PUT and no duplicate row.
- Same path and different fingerprint while running: replace the object and the
  pending manifest entry.
- Repeated identical manifest: no externally observable change.
- Manifest retry after successful PUT: reuse the existing object.
- Prepare or manifest after terminalization: reject as an execution conflict.

Billing-linked MCP operations retain their existing operation-level idempotency
and atomic task-file/settlement outbox transaction. Final collection must never
create a second charge for an already registered MCP asset.

## Stable File Snapshot

The hook must upload the bytes whose fingerprint it reports. The current
separate hash and upload opens leave a narrow race if a surviving child process
changes a file between those operations.

Before scanning, the runner stops or awaits the execution process group. For
each file it then:

1. Opens one regular file without following a symlink.
2. Records file identity, size, and modification time from the open descriptor.
3. Computes SHA-256 from that descriptor.
4. Seeks the same descriptor back to the beginning and uploads from it when
   required.
5. Rechecks file identity, size, and modification time after hashing and upload.

If the snapshot changed, the runner discards that manifest entry and retries the
file with a bounded retry count. Exhaustion fails artifact collection and
therefore prevents a successful `/complete` result. Large files are streamed;
the runner does not buffer an entire video in memory.

## Manifest Authority And MCP Assets

The submitted workspace manifest is authoritative for files collected from the
workspace. Repeated submission replaces the current execution's workspace
entries, so a file removed before the final scan is not retained accidentally.

Server-generated MCP assets use a separate execution-owned storage prefix and
may already have pending task-file rows before the final hook runs. The server
constructs the effective manifest as:

```text
final workspace manifest
UNION
pending server-generated MCP artifacts for the same execution
```

If both sets contain the same logical path, the final workspace entry wins
because it represents the post-execution snapshot. The merge must preserve any
existing operation settlement and outbox records as immutable billing evidence,
must not enqueue another charge, and must not import rows from another task or
execution. The superseded MCP object may remain as operation evidence, but it is
not part of the effective delivery manifest.

Submitting an empty workspace manifest is meaningful: it removes stale
workspace entries while preserving valid same-execution MCP assets. It does not
fabricate a task file. Existing completion validation decides whether the
effective manifest contains the required deliverables.

## File Update And Version Semantics

### Within One Execution

The final hook observes the workspace after Agent execution ends. If a logical
path was rewritten, its changed SHA-256 causes a new upload and replaces the
pending entry. A retry with unchanged final bytes skips the upload.

Pending files are not exposed by `list_task_files`, so consumers cannot mistake
an intermediate version for a completed deliverable.

### Across Executions

Every retry or resume has a new `execution_id` and therefore a new object prefix.
On successful completion, `PublishCurrentExecution` atomically changes the old
published set to `superseded` and the new pending set to `published`.

If the newer execution fails, its pending files become `collected`; it does not
replace the previous successful published set. Therefore:

- `state=published` identifies the latest successful deliverable version.
- `state=collected` plus `execution_id` identifies files from a failed attempt.
- `state=superseded` retains internal history but is not returned by
  `list_task_files` or used for delivery.

Because object keys contain `execution_id`, a newly published cross-execution
version has a distinct storage URL. Terminal objects are immutable, so normal
delivery does not depend on overwriting a cached published URL.

## `list_task_files` Contract

Keep the MCP tool and its task-ownership check. Clarify its description and
shipped instructions:

- It returns owned terminal files in `published` and `collected` states.
- It is suitable for post-run inspection, recovery diagnosis, and selecting a
  terminal file for a later operation.
- It is not a workspace listing, a live progress API, an upload confirmation,
  or a completion gate.
- It does not return `pending` or `superseded` rows.

Remove execution-time requirements or immediate-visibility claims from Agent
and Skill content, including Ecommerce and Seednote visual instructions. The
tool may remain available for an explicit terminal inspection workflow.

Because these edits change plugin runtime contracts, update both native plugin
manifest versions in the same implementation change:

- `plugins/.claude-plugin/plugin.json`
- `plugins/.codex-plugin/plugin.json`

Use a patch version bump unless the final implementation scope expands.

## Error Handling

- Invalid paths, symlinks, non-regular files, oversized files, and execution
  identity mismatches remain hard errors.
- Missing or mismatched stored fingerprint metadata requires a new upload; it
  must never be treated as an idempotent match.
- Object stat failure is a retryable storage failure, not proof that upload can
  be skipped.
- Manifest fingerprint mismatch is rejected before database mutation.
- Hook upload or manifest failure changes an otherwise successful execution
  result to failure and remains visible in terminal diagnostics.
- Repeated `/complete` calls continue through the existing idempotent terminal
  CAS and durable finalization stages.
- A failed newer execution never supersedes the last successful published set.

## Testing

Use test-first changes for each contract boundary.

### Agent Tests

- The final hook runs for successful and failed execution results.
- Same path and same fingerprint skips PUT.
- Same path and changed fingerprint performs PUT and reports the new manifest.
- A successful PUT followed by manifest failure can retry without another PUT.
- An empty scan still submits an empty workspace manifest.
- Stable snapshot detection retries a changed file and fails after bounded
  exhaustion.
- Large artifacts are streamed rather than fully buffered.
- Upload or manifest failure converts an otherwise successful result to failure.

### Server Tests

- Prepare returns `upload_required=false` only when stored size and SHA-256
  metadata match.
- A missing hash, wrong hash, or wrong size requires upload.
- Stale and terminal executions cannot prepare uploads or replace manifests.
- Manifest verification rejects object metadata mismatches and cross-prefix
  keys.
- Repeated identical manifests are idempotent.
- A changed same-path manifest replaces the pending entry while running.
- Empty workspace manifest submission removes workspace entries and preserves
  same-execution MCP assets.
- A same-path workspace entry deterministically replaces the MCP delivery entry
  without deleting its settlement evidence or enqueueing another charge.
- Successful publication atomically supersedes the previous published set.
- Failed collection preserves the previous published set and exposes the newer
  files as collected.
- `list_task_files` returns published plus collected files, rejects foreign
  tasks, and excludes pending and superseded files.

### Contract Tests

- Shipped Agent and Skill content does not use `list_task_files` as a completion
  or immediate upload-visibility check.
- MCP tool documentation describes terminal visibility accurately.
- Claude and Codex plugin manifest versions stay synchronized.

Run fresh verification after implementation:

```bash
go test ./agent ./server/service ./server/repository ./server/mcp -count=1
go test ./... -count=1
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

## Non-Goals

- Removing `list_task_files` from MCP.
- Exposing pending files as live progress.
- Adding a second final Hook, sidecar collector, or workspace watcher.
- Global content-addressed storage or cross-task blob deduplication.
- Rewriting historical task-file rows or OSS keys.
- Allowing a failed attempt to replace the last successful deliverables.
- Changing fixed-SKU billing or settlement policy.
