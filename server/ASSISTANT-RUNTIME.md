# Assistant runtime deployment

The first Assistant release runs one Server control-plane replica. Session streams,
Bridge handles, in-flight commands, and creation request coalescing are process-local.
Database leases prevent duplicate scope acquisition; they do not provide cross-Server
routing or live runtime adoption.

Keep `Deployment.yaml` at `replicas: 1` with `strategy.type: Recreate`. Do not use an
HPA or a rolling update that overlaps Server processes. Startup marks prior sessions
interrupted and reaps orphan containers/Jobs. Overlapping Servers could invalidate
one another's sessions. These startup operations are intentional recovery for the
single control-plane deployment.

Graceful Server shutdown closes active DSH runtimes before HTTP/storage teardown,
with a bounded grace period. After an unclean stop, startup orphan cleanup runs
before enabling new sessions. A runtime never resumes its transcript: a new session
receives a validated structured checkpoint. Expired uncommitted checkpoints are
removed on Assistant bootstrap; committed checkpoints retain their idempotency
receipts. Confirmed profile commits can replay their durable receipt after restart.

Structured Server logs include session identity, mode, lease scope, runtime startup
latency, command outcomes, and commit duration/outcome. They exclude prompts,
checkpoint payloads, transcripts, and credentials. Checkpoint and conflict errors
are returned through the existing structured API errors.

Creation and review runtime tokens may only call `stage_assistant_draft`; their
project/work evidence comes from the frozen Server context. Profile mode retains
the existing project-creator read capabilities. User confirmation submits the
saved checkpoint's `payload_digest` as `checkpoint_digest`, so a newer runtime
draft cannot silently replace the displayed approval. An already-admitted creation
checkpoint cannot be restored or rewritten; clients should open the original task
identified by the conflict hint instead of creating another paid task.
