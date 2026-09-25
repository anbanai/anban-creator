# Execution Identity Contract Design

## Status

Implemented as runtime contract v4. Current-execution requests derive execution identity from verified credentials; historical metadata tools retain an explicit target selector for user/API-key calls. Server and runtime images require a coordinated release: build and publish v4 runtime images before enabling the new Server; do not mix runtime contract versions. No database migration is required.

## Goal

Make authenticated credentials the sole authority for the current managed execution identity. Agent callers must not repeat `execution_id` in current-execution API arguments, bodies, headers, or MCP payloads. Preserve explicit execution IDs only when a user or administrator intentionally selects a historical execution.

## Current State

- The Server issues an HS256 execution JWT containing `user_id`, `project_id`, `task_id`, and `execution_id`; `sub` repeats the execution ID. The token is short-lived and validated with issuer, audience, time, identity, and subject checks.
- MCP validates execution JWTs, stores claims in request context, and rechecks current-execution authorization for each tool call. Some MCP tool schemas nevertheless require `execution_id` again.
- Agent REST authentication also validates execution JWTs and stores the claims in request locals. Progress-plan and artifact upload contracts still require the caller to repeat the execution ID; progress and completion already derive it from the token when omitted.
- Bootstrap authenticates a provider-owned workload, but its request body repeats `execution_id`. Docker workload JWTs already contain execution identity. Kubernetes verification binds the projected service-account token to a Pod and Job, then compares the Job labels with the caller-supplied ID.
- Historical content-metadata operations may legitimately target an execution other than the caller's current execution, particularly when invoked with a user API key.

## Selected Design

### 1. Trust boundary and identity context

The Server creates one typed execution identity from verified credentials: user, project, task, and execution IDs. Handlers and MCP tools obtain identity from this context; caller-supplied identity fields are never authoritative. The current-execution authorizer continues to run for every managed MCP tool call and execution REST request. Domain services continue receiving the trusted execution ID as an internal argument for current-execution checks, persistence, idempotency, and audit records.

`task_id` and `project_id` may remain explicit resource selectors where a tool operates on a named task or project. The Server must compare any such selector with the token-bound scope before dispatch. This design removes the redundant execution identity while retaining clear resource targeting.

### 2. Current-execution MCP and Agent REST contracts

Remove `execution_id` from request schemas for operations that can only act on the authenticated current execution:

- Agent progress plan and progress update.
- Agent completion.
- Artifact prepare, artifact manifest, and artifact content-stream identity headers.
- MCP progress tools and `submit_completion_metadata`.
- The execution identity fields inside the metadata JSON sent to `submit_completion_metadata`; the Runner strips those fields at the MCP boundary. Persisted report columns use the authenticated identity.

MCP tool scopes continue requiring and checking `task_id` or `project_id` where those values select a resource. Agent REST handlers use the authenticated identity for execution ownership and pass it to services. Artifact upload services no longer accept a caller-requested execution ID to compare with the trusted one.

### 3. Historical execution selectors

`recompute_content_tags`, `recompute_agent_feedback`, and `get_completion_metadata_status` can target a historical execution. For user/API-key calls, `execution_id` remains a required target selector and service authorization verifies that the target task belongs to the caller. For execution-token calls, the target defaults to the token-bound current execution; supplying a different target is rejected. Tool descriptions and validation distinguish these two authorization modes. Historical target selection is not treated as caller identity.

### 4. Workload bootstrap

Remove the `execution_id` field from the bootstrap request body. Change `WorkloadVerifier` to resolve identity solely from the workload credential and provider-owned runtime metadata:

- Docker: validate the signed workload JWT, inspect the claimed live container instance, and verify its ownership labels against the signed claims; take execution identity from the verified claims.
- Kubernetes: TokenReview the projected token, verify the bound Pod UID and Job owner, then derive execution/task/project/user identity from the verified Job labels and confirm Pod labels match. Reject missing, malformed, conflicting, or terminal runtime identity.

The bootstrap response continues returning identity claims and the short-lived execution JWT so the Runner can establish local context. Identity in the response is informational to the Runner and must be consistent with the returned JWT.

### 5. Runtime contract and rollout

Bump the Agent runtime contract version in both Server and `agent-ts`. The new Runner sends a bodyless bootstrap request and omits execution ID from current-execution report payloads and artifact headers. Old runtime images are rejected by the existing contract-version handshake; no compatibility shim or dual request format is added. Release the Server and runtime image together. No database schema migration is required.

### 6. Errors and security properties

- Missing, invalid, expired, wrong-audience, or wrongly signed credentials fail authentication.
- A valid but stale execution credential fails current-execution authorization before a tool or handler can mutate state.
- A task/project selector that conflicts with the token fails authorization; callers cannot redirect a current-execution operation by changing request identity fields.
- Workload bootstrap fails closed when provider metadata cannot uniquely bind the request to one live Job/container.
- Execution IDs in API responses and persisted records remain available for observability. JWT payloads are signed, not encrypted; IDs are not secrets and must not be used as bearer credentials.

## Alternatives Considered

1. **Remove only `execution_id` from MCP progress tools.** This addresses the observed progress-tool mismatch but leaves inconsistent HTTP, artifact, metadata, and bootstrap contracts. Rejected as incomplete.
2. **Remove all task/project/execution selectors from every MCP operation.** This makes user/API-key calls to project or historical-execution tools ambiguous and conflates current-execution identity with target selection. Rejected; resource selectors remain where needed and are scope-checked.
3. **Add a separate execution-ID header or session parameter.** This still duplicates identity outside the credential and creates another source of disagreement. Rejected.

## Verification Plan

- Execution JWT tests prove request identity is sourced from claims and current-execution authorization still rejects stale execution attempts.
- MCP schema and handler tests prove current-execution tools omit `execution_id`, cannot be redirected by a supplied identity, and historical user/API-key tools still require and authorize explicit target selectors.
- Agent REST tests prove progress, completion, artifact prepare/manifest, and streaming work without an execution ID in bodies or headers; old or foreign identity values cannot affect the authenticated target.
- Bootstrap tests prove no request body ID is needed, Docker identity comes from the signed token/runtime labels, and Kubernetes identity is derived from the authenticated Pod/Job rather than request JSON.
- Runtime tests prove the new contract version is sent and old versions are rejected; completion metadata strips identity fields before the MCP call.
- Run full Go tests, Agent TypeScript tests/typecheck/build, harness tests, and `make agent-pack-check`.

## Acceptance Criteria

1. No current-execution Agent request requires `execution_id` in JSON, headers, or MCP tool arguments.
2. The Server obtains current execution identity only from verified execution/workload credentials and provider-owned runtime identity.
3. Current-execution authorization and task/project scope checks remain enforced at request time.
4. Historical execution management remains possible only through explicit target selectors and user-scoped authorization.
5. The new Server/runtime contract has no compatibility mode; tests and generated Agent Packs reflect the new contract.
