# DSH alpha.3 Interactive Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run every Project Creator interaction in an isolated DSH alpha.3 container while preserving Server-owned session state, NAS memory policy, SSE delivery, and profile confirmation semantics.

**Architecture:** Keep `runtime-dsh` as a small TypeScript JSON-RPC bridge. Add a Server-side runtime transport that launches one hardened Docker/Kubernetes workload per session and exposes the existing `Runtime` interface to `InteractiveService`. The Server remains the only browser-facing endpoint and authority for leases, draft commits, CAS memory writes, and lifecycle.

**Tech Stack:** Go/Fiber, GORM repositories, Docker Engine API, Kubernetes Jobs/Pods, TypeScript/Bun, Docker multi-stage builds, SSE.

**Spec:** User-provided “DSH alpha.3 升级与独立交互 Runtime 改造计划”.

## Global Constraints

- DSH packages remain locked to `0.2.1-alpha.3`; Python SDK is not a production dependency.
- Browser reconnects only to a live container; Server/container restart invalidates the session.
- Project `MEMORY.md`, `AGENTS.md`, and `profile/*` are read-only to DSH; formal writes use Server CAS after confirmation.
- Private `.claude/agent-memory/<agent-id>` is isolated per Agent Pack and writable only when explicitly allowed.
- Containers run non-root with fixed image identity, dropped capabilities, resource limits, no Docker socket, and no automatic restart.
- Every control command has an idempotency key; model prompts are never replayed automatically.
- One persistent project lease permits one active interactive session across Server replicas.

### Task 1: DSH container transport

**Files:** `server/agent/dsh_runtime.go`, `server/agent/dsh_runtime_test.go`, `server/service/interactive.go` wiring, relevant interfaces.

- [ ] Define a Docker-backed `RuntimeFactory` that creates one session container, attaches JSON-RPC stdin/stdout, forwards events, records exit, and closes/removes the workload.
- [ ] Apply image identity, non-root, capability, resource, network, labels, and timeout checks using the existing Docker executor patterns.
- [ ] Add duplicate command suppression and startup/exit error propagation tests.

### Task 2: Configuration and Server wiring

**Files:** `server/config/config.go`, config fixtures/tests, `server/main.go`, `server/agent` constructors.

- [ ] Add explicit DSH executor, image, network, volume, endpoint, resource, and cleanup settings with strict validation.
- [ ] Select container executor in production configuration and retain local execution only for explicit development mode.
- [ ] Ensure the Server instance ID and workload token are included in the start specification.

### Task 3: NAS mounts and project lease

**Files:** interactive repository/model/service files, memory store, lease migrations/tests.

- [ ] Persist project interaction leases with owner, session, expiry, and Server instance fields; acquire/release atomically.
- [ ] Build read-only project/profile mounts, isolated private memory mount, and temporary `DSH_HOME` for the container.
- [ ] Verify stale lease handling, concurrent acquisition, CAS conflict, and restart invalidation.

### Task 4: Runtime images and deployment

**Files:** `deploy/docker/Dockerfile.runtime-dsh`, `deploy/docker/Dockerfile.server`, Compose/Kubernetes manifests, Make targets/docs.

- [ ] Build and smoke-test an independent DSH image from the alpha.3 runtime.
- [ ] Add Docker and Kubernetes executor configuration, labels, resource limits, service-account/network policy, and orphan cleanup.
- [ ] Remove the embedded DSH runtime from the Server image once deployment wiring is active.

### Task 5: SSE and protocol verification

**Files:** Server SSE handlers/tests, runtime-dsh contract tests, deployment smoke scripts.

- [ ] Verify heartbeat, `Last-Event-ID`, proxy buffering, reconnect, timeout, close, and restart behavior.
- [ ] Run Go tests/build, Studio tests/build, runtime-dsh tests/typecheck/build/startup smoke, and harness contract checks.
- [ ] Review the complete diff and merge to `main` only after all checks pass.
