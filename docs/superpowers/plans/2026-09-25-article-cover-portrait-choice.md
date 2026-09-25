# Article Cover Portrait Choice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users require the project's default portrait on an article cover from a manual task or recurring plan, and preserve that choice through task creation and agent execution.

**Architecture:** Add an article-cover portrait boolean to Plan and Task, expose it through the server API and Studio forms, copy plan state into each scheduled task, and turn a true task value into a structured runtime prompt control. The Server validates that a requested cover and frozen project portrait exist; the Article agent contract makes the requirement cover-only and requires reference-path use.

**Tech Stack:** Go/Fiber/GORM, React/TypeScript/Zod/Vitest, harness Article agent and cover skill.

**Spec:** Approved in conversation on 2026-09-25: checked means the cover must use the project default portrait; plan-generated tasks inherit and freeze the value; unchecked leaves the existing Agent judgment behavior.

## Global Constraints

- The checkbox applies only to WeChat article covers.
- It is valid only when article cover generation is enabled and a project portrait reference exists in the task's frozen project snapshot.
- A required project portrait is passed to cover generation only; article body illustrations must not use it.
- Do not put the reference image, signed URL, or asset bytes in task prompts or persisted execution diagnostics.
- Preserve unrelated user modifications in the main checkout.

## Review Focus

- Manual article task with no portrait cannot request required portrait; reject it server-side and surface a clear error.
- Scheduled article task copies the plan requirement and project snapshot into the new task.
- Required portrait cannot coexist with article cover disabled.
- Cloning a task carries its portrait requirement when cloning the original contract.
- Non-article tasks and unchecked article tasks keep current behavior and prompt text.

---

### Task 1: Server task and plan contract

**Files:** `server/model/task.go`, `server/model/plan.go`, `server/service/task.go`, `server/service/plan.go`, `server/handler/task.go`, `server/handler/plan.go`, related Go tests.

**Interfaces:** `article_cover_use_portrait` is a boolean field on the task and plan API. Manual task creation accepts it; plan creation and update persist it; `CreateFromPlan` copies it to the spawned task. Creation rejects true unless the project snapshot contains a portrait and article cover generation is enabled.

- [ ] Write failing service/handler tests for manual create, invalid missing portrait/disabled cover, plan persistence/update, and plan-to-task inheritance.
- [ ] Run targeted Go tests and verify the assertions fail because the field is not yet wired.
- [ ] Add model/API/service fields and validation, preserving existing false defaults and task snapshots.
- [ ] Run `cd server && go test ./service ./handler ./model`.

### Task 2: Runtime prompt contract

**Files:** `server/agent/executor.go`, `server/agent/executor_test.go`, `server/service/agent_bootstrap.go`, related tests.

**Interfaces:** Add `ArticleCoverUsePortrait bool` to `UserPromptParams`; for article tasks with this flag true, emit `article_cover_portrait=required_project_portrait` as a runtime control. False emits no additional directive.

- [ ] Add a failing prompt test for required and unchecked article tasks.
- [ ] Run `cd server && go test ./agent -run TestBuildUserPrompt -count=1` and confirm the required case fails.
- [ ] Pass the frozen task value from bootstrap into `BuildUserPrompt` and emit the control only for article tasks.
- [ ] Run the targeted prompt and bootstrap tests.

### Task 3: Studio task and plan controls

**Files:** `studio/src/types/task.ts`, `studio/src/types/plan.ts`, `studio/src/lib/schemas.ts`, `studio/src/lib/task-form.ts`, `studio/src/components/tasks/TaskFormDialog.tsx`, `studio/src/pages/PlansPage.tsx`, related tests.

**Interfaces:** Add `article_cover_use_portrait` to request/form types. Show the control in article image settings when the selected project has a portrait; reflect required state when editing/cloning and submit it for task and plan create/update.

- [ ] Add failing schema/conversion/component tests for checked value submission, plan mapping, and task clone preservation.
- [ ] Run the targeted Studio tests and confirm the new contract assertions fail.
- [ ] Implement the checkbox and payload mapping; explain that the portrait is required on the cover and reserved for the cover.
- [ ] Run targeted Studio tests, `cd studio && bun run test`, and `cd studio && bun run build`.

### Task 4: Article Agent instructions and release metadata

**Files:** Article Agent and article-cover-design Skill sources under `harness/`, generated Article Pack surfaces as applicable, `harness/.claude-plugin/plugin.json`, `harness/.codex-plugin/plugin.json`, relevant contract tests.

**Interfaces:** When runtime control `article_cover_portrait=required_project_portrait` is present, set `portrait_decision.required_by_user=true` and `use=true`, choose `.anban-creator/project-portrait-reference.png`, include it in cover `ref_image_paths`, and do not use it for body images. Do not cite body-image style consistency as a reason to ignore the explicit request.

- [ ] Add/update contract assertions for the required-portrait directive, correct reference path, and cover-only scope.
- [ ] Run targeted harness contract tests and confirm the new required behavior is not yet documented.
- [ ] Update canonical source and generated pack surfaces; patch-bump both native manifests.
- [ ] Run `make agent-pack-generate` and `make agent-pack-check`, then targeted harness contract tests.

### Task 5: Whole-change verification and review

**Files:** all files above.

- [ ] Review the full diff for API naming consistency, task/plan inheritance, snapshot semantics, and preservation of the main checkout's pre-existing changes.
- [ ] Run full Go tests and build, full Studio tests and build, and harness validation required by `AGENTS.md`.
- [ ] Resolve any regressions and repeat only the checks affected by the fixes, followed by required fresh final verification.
