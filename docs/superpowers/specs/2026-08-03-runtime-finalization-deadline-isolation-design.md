# Runtime Finalization Deadline Isolation Design

## Goal

Stop completed Agent workflows from becoming `runtime_failed` when workspace
artifact collection consumes the time needed for terminal completion. Apply one
small contract to the TypeScript and Go runtimes under Docker and Kubernetes,
without changing artifact persistence.

## Confirmed Failure Boundary

Production task `6a5cb3a3-1f67-435e-8718-bab4329dc24d`, execution
`9a00decd-298c-4a33-8d7c-63cf23265d2b`, completed the Seednote workflow and its
delivery gate. The Agent reported 14 files in `/workspace/output`.

The platform instead recorded exit code 1, `BackoffLimitExceeded`,
`runtime_failed`, and `result_subtype = NULL`. Only three MCP-generated images
reached `task_files`. No workspace manifest was committed. The first workspace
upload session, for `compliance-report.md`, was created at `08:29:46.173` and
remained `pending`; the Job failed at `08:29:51.315`.

The TypeScript runtime currently shares one absolute finalization window. The
production Job turns its 30-second grace into a 25-second runtime window:
artifact work receives 20 seconds and `/agent/complete` receives the final five
seconds. Go implements the same contract. Completion errors are swallowed, so
the reconciler sees only a failed Job and synthesizes `runtime_failed`.

This proves that workspace finalization and terminal completion did not finish.
Missing Pod logs prevent a truthful attribution to one internal call among
scan, hash, OSS upload, and manifest submission. The repair therefore makes the
whole artifact phase cancellable and records its failed operation class.

Agent-Reach is unrelated: it caused a documented topic-data fallback. Image
generation, content quality, and the Seednote gate succeeded.

## Decision

Keep the existing final hook. Replace the shared deadline with two independent
upper bounds:

- artifact finalization: 120 seconds
- completion reporting: 20 seconds

Success advances immediately; these limits add no fixed latency.

Both runtimes accept:

- `ANBAN_JOB_ARTIFACT_TIMEOUT`
- `ANBAN_JOB_COMPLETION_TIMEOUT`

Absent, malformed, non-positive, or out-of-range values use the defaults.
Artifact timeout is capped at five minutes and completion at one minute. Remove
the obsolete `ANBAN_JOB_FINALIZATION_TIMEOUT`; add no Server config field or
database column.

## Runtime Flow

```text
Agent returns
    |
    v
artifact phase (fresh 120s maximum)
  scan -> hash -> upload -> manifest
    |
    | success or attach artifact failure to result
    v
completion phase (fresh 20s maximum)
  retry idempotent /agent/complete
    |
    +-- acknowledged ------> exit with Agent result semantics
    |
    +-- not acknowledged --> exit 2: completion_report_failed
```

`SIGINT` or `SIGTERM` cancels Agent/artifact work and starts completion
immediately with a context independent of the cancelled workflow. Kubernetes
keeps its existing 30-second termination grace; normal artifact work is no
longer derived from that grace. Docker uses the same runtime behavior.

## Artifact Contract

Preserve all current semantics:

- stable size and SHA-256 snapshots
- idempotent prepare, upload, and execution manifest
- same-execution MCP artifact merge
- `pending -> published` on success
- `pending -> collected` on failure
- artifact failure converts an otherwise successful result to platform failure

Cancellation must interrupt directory walking, hashing, streamed/direct upload
waits, and further file processing. A cancelled or partial collection must not
submit a successful partial manifest. Artifact failure or timeout always flows
into the independent completion phase.

Do not add a sidecar, Server collector, second finalizer, recovery worker,
parallel upload, adaptive budget, or artifact state.

## Completion And Error Semantics

`/api/v1/agent/complete` remains the sole terminal callback. It receives a new
20-second deadline and keeps the current bounded, idempotent retry policy.

- Acknowledged success exits 0.
- Acknowledged Agent or artifact failure keeps the current failed-result exit.
- Unacknowledged completion exits 2.

Exit code 2 is reserved for `completion_report_failed`. TypeScript and Go emit
the same code. Docker and Kubernetes expose it through existing runtime state,
and the reconciler maps it instead of emitting generic `runtime_failed`. A
terminal result already accepted by the Server must win over later reconciler
observation.

No callback is attempted to report failure of the same callback. The runtime
writes a concise stderr message; the exit code is the durable fallback.

## Observability

Use only existing progress, stderr, result error, and execution diagnostics.
Record:

- artifact start with timeout
- artifact success with file count and elapsed time
- artifact failure with operation class and elapsed time
- completion start with timeout
- completion acknowledged or exhausted with elapsed time

Do not log credentials, signed URLs, tokens, or environment values. Do not add
a table, event pipeline, or logging service.

## Executor Behavior

- Runtime binaries own the two timeout defaults and optional environment
  overrides.
- Kubernetes stops injecting the shared timeout.
- Kubernetes keeps its one-shot Job, active deadline, 30-second termination
  grace, and reconciliation grace.
- Docker keeps its one-shot container and image-derived environment.
- Repository config comments distinguish active deadline and termination grace
  from normal runtime finalization.

No persisted execution or configuration schema changes are required.

## Testing

Test TypeScript and Go parity:

- artifact budget cannot reduce the fresh completion budget
- cancellation interrupts scan, hash, and upload waits
- `SIGTERM` skips/cancels artifact work and still calls completion
- artifact failure is sent through completion
- completion retry can recover
- completion exhaustion exits 2
- success adds no fixed wait

Test Server integration:

- Kubernetes no longer injects the shared timeout
- termination grace remains 30 seconds
- Docker and Kubernetes preserve exit code 2
- reconciler maps exit code 2 to `completion_report_failed`
- other exit and terminal-CAS behavior is unchanged
- existing artifact idempotency, manifest merge, and state tests still pass

Run:

```bash
cd agent-ts && bun test
go test ./...
go build -o /tmp/anban ./agent
go build -o /tmp/anban-creator-server ./server
```

Container and production proof remain separate from local tests.

## Release And Acceptance

No database migration is needed. In one deployment window:

1. Publish Article, Seednote, and Montage runtime images.
2. Publish Server and configure the three immutable runtime digests.
3. Roll out Server and create a fresh Seednote task.
4. Inspect the task, execution, manifest, upload sessions, and Job exit state.

Acceptance requires task `completed`, execution success, Job exit 0, every
expected workspace file in one published manifest, no stale pending upload
session, no `runtime_failed`, and no fixed successful-path delay. A controlled
terminal-delivery failure must produce `completion_report_failed`.

Rollback restores the previous Server and runtime image digests. No data
rollback is required.

## Non-Goals

- Artifact persistence redesign or terminated-Pod recovery
- Concurrent uploads or adaptive throughput estimation
- Seednote gate, Agent-Reach, billing, or publishing changes
- Compatibility support for the removed shared timeout
