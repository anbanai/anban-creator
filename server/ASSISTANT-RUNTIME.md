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

## Release observation

Use API access logs together with structured runtime logs and the durable
`assistant_sessions` / `assistant_checkpoints` records. Count successful bootstrap
requests separately from successful session admissions: viewing the home page
must never allocate a runtime. Group runtime observations by `mode_id`; never
use prompts or checkpoint payloads as metric labels.

- Start latency P50/P95: runtime startup duration logs, plus startup error counts.
- Conversion and unused sessions: bootstrap requests, session admissions, message
  requests, and closed session records in the same observation window.
- Switch and recovery outcomes: checkpoint-save response, old-session DELETE,
  and target-session POST (including checkpoint restore conflicts).
- Checkpoint save/restore and revision/lease conflict rates: corresponding
  Assistant endpoint response codes and structured runtime errors.
- Session duration: `closed_at - created_at`, grouped by mode. Resource cost is
  estimated from runtime requests/limits and measured Pod usage with
  `com.anban.session_id` / `com.anban.preset_id` labels.
- Review confirmation-to-validation: candidate and validated FeedbackInsight
  counts for `assistant_review`, grouped by creation period. User confirmation
  alone is not validation and must not count as a profile update.

Collect a production baseline before changing resource requests or adding a
prewarm pool. This release intentionally provides no shared persistent Agent
process, prewarm pool, or multiple Server replicas. A future prewarm pool must
bind each clean runtime to one session only and destroy it after use.
Home Assistant draft staging is interaction-scoped. The MCP call carries
`interaction_id`, `generation`, `attempt_id`, `mode_id`, `payload_digest`, and
`ready`; the Server persists the draft receipt before returning success. A
logical runtime session or MCP transport session is not the durable identity,
and retrying the same attempt returns the original draft receipt.
