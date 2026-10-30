# Talking-Head Workflow Audit Implementation Plan

> **For agentic workers:** This plan is being executed in the current session under the user's explicit request to assess, improve, review, and merge the workflow locally.

**Goal:** Align the talking-head Agent and Skills with the managed runtime that is actually shipped, while adding evidence-based audio assembly checks that protect final dialogue quality.

**Architecture:** Keep runtime dependency versions pinned in the image build for reproducibility, but expose capability checks and observed versions to Agents. Route semantic transcription through the installed upstream `video-use` helper using Server-injected credentials, and keep MCP-only claims limited to actual MCP capabilities. Add audio stream normalization and post-assembly verification to the rough-cut evidence contract.

**Tech Stack:** Markdown Skills and Agent Pack assets, Go Server profile metadata, generated Agent Pack files, Go tests.

**Spec:** User-provided workflow audit attachment and repository `AGENTS.md` instructions.

## Global Constraints

- Keep the published editing order and review gate intact.
- Do not allow Agents to create ad hoc HTTP clients or expose credentials.
- Preserve reproducible dependency pins in Docker image construction.
- Do not merge or push until fresh verification and review are complete; merge locally only.

## Review Focus

- A managed task must not block on a nonexistent video-use MCP tool; check the installed helper and Server-provided credential contract.
- Runtime image pins must not become Agent-facing version gates; check Agent/Skill/profile text separately from Docker build validation.
- Multi-source audio must not be concatenated with incompatible stream parameters or accepted without continuity and level checks.
- Sample review and final-delivery required artifacts must remain phase-aware.
- Generated Claude/Codex Pack surfaces must stay in sync with their canonical sources.

### Task 1: Correct the managed video-use contract and runtime guidance

**Files:** `harness/skills/video-use/SKILL.md`, `harness/packs/talking-head-editor/agent.claude.md`, `harness/packs/talking-head-editor/agent.codex.toml`, `server/service/agent_project_profile.go`, corresponding generated assets.

- [x] Verify the image-installed helper names and Server-injected credential path against Dockerfile and bootstrap code.
- [x] Replace the nonexistent MCP-only requirement with the shipped helper invocation contract and explicit credential-handling rules.
- [x] Replace Agent/Skill exact auto-editor and HyperFrames version gates with executable/capability checks and observed-version evidence.
- [x] Keep dependency pins in the image build and remove build implementation versions from the Agent-facing runtime profile.
- [x] Generate and check the canonical Agent Pack surfaces.

### Task 2: Add audio assembly verification

**Files:** `harness/skills/auto-editor/SKILL.md`, `harness/skills/auto-editor/references/verification.md`, rough-cut procedure in talking-head Agent.

- [x] Document inspection of selected audio streams and compatibility normalization before concatenation where required by the chosen concat method.
- [x] Require post-assembly checks for duration, channel layout, sample rate, discontinuities, and dialogue level against source evidence.
- [x] Keep source media immutable and make any normalized intermediates explicit in evidence.

### Task 3: Verify, review, and integrate

**Files:** plugin manifests and tests as required by changed plugin assets.

- [x] Bump both native plugin manifest patch versions.
- [x] Run Pack generation/checks, targeted contract tests, full Go tests, and skill validation.
- [x] Review the final diff and confirm phase-aware artifact validation remains intact.
- [ ] Commit the work and merge the feature branch into local `main`; do not push.
