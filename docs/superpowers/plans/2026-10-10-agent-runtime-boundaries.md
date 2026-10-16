# Agent Runtime Boundaries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the Claude managed runtime and DSH interactive runtime into independently buildable packages without changing their external business contracts.

**Architecture:** Move the existing one-shot Agent runtime to `runtime-claude/` and move only the DSH interactive bridge to `runtime-dsh/`. Update Server configuration, Dockerfiles, CI, release artifacts, contract tests, and active documentation so each execution surface builds only its own dependencies.

**Tech Stack:** TypeScript, Node 24.22.0, Claude Agent SDK, DSH SDK, Go contract tests, Docker, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-10-agent-runtime-boundaries-design.md`

## Global Constraints

- `runtime-claude` must not depend on DSH packages.
- `runtime-dsh` must not depend on the Claude Agent SDK.
- DSH wire methods remain `session/start`, `session/prompt`, `session/answer`, and `shutdown`.
- Project Creator Pack id and Server interactive API remain unchanged.
- Existing user modifications outside this runtime split remain untouched.
- Default interactive configuration remains disabled.

### Task 1: Separate package trees and dependency locks

**Files:**
- Rename: `agent-ts/` -> `runtime-claude/`
- Create: `runtime-dsh/package.json`, `runtime-dsh/package-lock.json`, `runtime-dsh/tsconfig.json`
- Move: the DSH Bridge source and its test from `runtime-claude/` to `runtime-dsh/`
- Modify: both package manifests and lockfiles

- [x] Move the one-shot runtime tree and isolate the DSH Bridge tree.
- [x] Remove DSH dependencies and the interactive script from `runtime-claude`.
- [x] Add only DSH SDK dependencies and the Bridge test/build scripts to `runtime-dsh`.
- [x] Regenerate both npm lockfiles and verify each package has only its permitted SDK family.
- [x] Run independent typecheck, tests, and builds for both packages.

### Task 2: Update runtime configuration and Docker packaging

**Files:**
- Modify: `server/config/config.go`, `server/config.example.yaml`, `docker-compose.yml`
- Modify: `deploy/docker/Dockerfile.server`
- Modify: `deploy/docker/Dockerfile.agent-*`
- Modify: Docker contract tests under `server/agent/`

- [x] Point interactive Bridge defaults and container paths at `runtime-dsh`.
- [x] Build only `runtime-dsh` in the Server image.
- [x] Build only `runtime-claude` in managed Agent images.
- [x] Add contract assertions for dependency isolation and the new paths.
- [x] Build the Server binary and run Dockerfile contract tests.

### Task 3: Update CI, release, Makefile, and active documentation

**Files:**
- Modify: `Makefile`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`
- Modify: `README.md`, `AGENTS.md`, `CLAUDE.md`, active runtime docs
- Modify: config and runtime path tests

- [x] Add separate runtime-claude and runtime-dsh install/test/build targets while keeping intentional compatibility aliases.
- [x] Test and package both runtimes independently in CI and Release.
- [x] Rename release tarballs to `anban-runtime-claude-*` and `anban-runtime-dsh-*`.
- [x] Update active documentation and tests; leave historical specs explicitly historical.

### Task 4: Review, verification, and integration

**Files:**
- Review all changed files and the runtime boundary spec.

- [x] Search for stale active `agent-ts` and DSH-in-Claude references.
- [x] Run focused runtime, Server, Studio, Agent Pack, and DSH protocol tests.
- [x] Run builds and `git diff --check`.
- [ ] Create a scoped commit without unrelated worktree changes.
- [ ] Merge the scoped commit into local `main` only after all checks pass.

## Verification Notes

- `runtime-claude`: `npm ci`, typecheck, 239 tests, and build passed.
- `runtime-dsh`: `npm ci`, typecheck, 11 tests, and build passed.
- Server agent/config contracts, Server build, interactive service packages, Studio full suite (1094 tests), `make agent-pack-check`, and both repository `git diff --check` commands passed.
- A real Docker image build was not run because the local Docker daemon is unavailable; Dockerfile contract tests passed and the Server builder now includes the native `node-pty` toolchain.
- `go test ./...` remains blocked by unrelated existing timeline authorization failures in `server/integration` (`TestE2E_FullUserFlow`, `TestE2E_InvalidInputs`, and `TestE2E_FindRunningByUserDoesNotLeak`). No runtime-boundary files are involved in those failures, so commit and merge remain intentionally pending.
