# Agent Pack Catalog Drift Guard Design

## Problem

Managed Agent images carry `harness/agent-pack-catalog.json`, while the Server
embeds `server/agentpack/catalog.generated.json`. Tasks freeze a Pack identity
at creation. If a Server built from a newer Catalog dispatches to an older
Agent image, the Agent currently detects the mismatch only after receiving the
full bootstrap response. CI and Docker builds do not consistently prove that
the two generated Catalog files match.

## Goals

- Fail CI when generated Agent Pack outputs are stale.
- Fail CI before image publication when generated Agent Pack outputs are stale.
- Detect an Agent image Catalog mismatch before the Server returns task
  prompts, credentials, or bootstrap files.
- Preserve the execution-scoped frozen Pack identity check in the Agent; it
  protects retries and old executions even when both images agree with each
  other.
- Make production image identity and rollout requirements explicit.

## Design

The generated Catalog bytes are the release identity. The Server computes the
SHA-256 digest from its embedded Catalog bytes. The Agent computes the digest
from the Catalog file it reads from `/anbanai/agent-pack-catalog.json` and sends
it as `X-Anban-Agent-Pack-Catalog-Digest` with the existing runtime contract
header.

The bootstrap handler compares the supplied digest with the Server's embedded
digest before invoking the workload verifier or constructing a bootstrap
response. A mismatch returns a stable `agent_pack_catalog_mismatch` error code
and logs only the received and expected digests. This is an early compatibility
check, not an authentication mechanism; existing workload authentication and
execution token scoping remain unchanged.

The Agent continues validating the task's frozen Pack ID, version, digest,
runtime profile, adapter, and Agent flag against its local Catalog after the
response. This second check is required because matching global Catalog files
do not make a task frozen under an older Pack identity safe to resume against a
newer Pack; that execution must still be refreshed as a new task.

Adding the required digest header increments the managed runtime contract
version. Older Server and Agent binaries therefore fail closed instead of
silently ignoring the new header.

CI runs `make agent-pack-check`. Dockerfiles package the generated Catalog
without requiring a manually supplied build argument or OCI label. Local and
CI image targets run the Catalog check before building. Production manifests
record the Catalog SHA-256 for audit, while runtime bootstrap compares the
Server's embedded Catalog with the Catalog shipped in each Agent image.
Production documentation requires the Server and all managed Agent images to be
built from one source revision and deployed using immutable image references.

## Scope and boundaries

- Do not change Pack versioning or digest calculation.
- Do not make the Server build or select runtime images.
- Do not auto-upgrade an execution or weaken the existing frozen identity
  validation.
- Do not add a deployment controller or image registry integration.
- Catalog digest mismatch is an actionable compatibility failure. Execution
  identity remains private and task-scoped.

## Components and tests

- `server/agentpack`: expose the SHA-256 of the exact embedded Catalog bytes;
  test it against the checked-in generated artifact.
- `agent-ts/src/bootstrap.ts`: send the digest of the exact local Catalog
  bytes; test the request header and existing stale-execution rejection.
- `server/handler/agent.go`: reject missing or mismatched Catalog digests
  before workload verification; test matching, missing, and mismatched values,
  including response code and safe diagnostics.
- Dockerfiles and Make targets: package the Catalog without a required build
  argument or label; add repository contract tests covering every runtime
  Dockerfile.
- `.github/workflows/ci.yml`: run the generated Catalog drift check before
  compiling/testing the Server.
- Deployment docs: describe same-revision image publication, immutable image
  references, rollout order, and creating a new task after a Pack identity
  change.

## Rollout and compatibility

The new Agent sends an additional bootstrap header and uses the incremented
runtime contract version. Deploy the Server and Agent images from the same
source revision. A rolling rollout must keep task dispatch paused until both
new Server and Agent images are available; old Servers reject the new runtime
contract, and the new Server rejects old Agents that do not send the digest.
Existing persisted executions keep their frozen runtime image; after a Pack
update, start a new task to refresh its frozen Pack identity.

## Acceptance criteria

- CI fails on generated Catalog drift.
- `make agent-pack-check` fails if the Server and Harness Catalog files differ.
- A matching Agent Catalog reaches normal bootstrap; a missing or different
  digest is rejected before workload verification and bootstrap data creation.
- The Agent still rejects a frozen Pack identity that differs from its local
  Catalog.
- Server and Agent tests pass, and the catalog-generation check is clean.
