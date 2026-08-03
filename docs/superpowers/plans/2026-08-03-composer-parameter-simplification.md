# Composer Parameter Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce every prompt composer to a project control and one consolidated `创作设置` control while preserving workflow-specific settings.

**Architecture:** Add a shared task-parameter popover that composes the existing selectors without nested overlays. Reuse the existing platform identity map for project grouping and the Designer settings panel for image workflows.

**Tech Stack:** React 19, TypeScript, Base UI/shadcn components, Tailwind CSS v4, Vitest, Testing Library.

---

### Task 1: Simplify Project Identity

**Files:**
- Modify: `studio/src/components/agent-prompt/ProjectContextControl.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectIdentity.tsx`
- Test: `studio/src/components/agent-prompt/ProjectContextControl.test.tsx`
- Test: `studio/src/components/agent-prompt/ProjectIdentity.test.tsx`

- [x] Add failing assertions that compact triggers and project options omit repeated `*项目` labels while group headings keep short platform names.
- [x] Run `bun run test -- src/components/agent-prompt/ProjectContextControl.test.tsx src/components/agent-prompt/ProjectIdentity.test.tsx` and confirm the new assertions fail.
- [x] Render type-free project identity in the selector, add colored platform group headers, and omit synthetic fallback descriptions.
- [x] Re-run the targeted tests and confirm they pass.

### Task 2: Consolidate Task Parameters

**Files:**
- Create: `studio/src/components/tasks/TaskComposerParameters.tsx`
- Create: `studio/src/components/tasks/TaskComposerParameters.test.tsx`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.tsx`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.test.tsx`
- Modify: `studio/src/components/ImageGenerationToolbar.tsx`
- Modify: `studio/src/components/ImageGenerationToolbar.test.tsx`

- [x] Add a failing component test for a single `创作设置` trigger containing vertical execution profiles, image settings, and `任务数量`.
- [x] Add a failing layout assertion that execution profile choices use one grid column.
- [x] Run the targeted tests and confirm failure because the shared component and vertical contract do not exist.
- [x] Extract reusable inline image settings and add `TaskComposerParameters` using `Popover`, `ExecutionProfileSelector`, and `QuantityStepper`.
- [x] Re-run the targeted tests and confirm they pass.

### Task 3: Integrate All Four Composer Surfaces

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/components/designer/DesignerGenerationToolbar.tsx`
- Modify: corresponding existing page/component tests.

- [x] Update page tests so the toolbar exposes `项目` and `创作设置` as its only top-level parameter controls.
- [x] Run the focused page tests and confirm the old separate controls fail the contract.
- [x] Replace separate execution, image, and quantity toolbar controls with `TaskComposerParameters` on homepage, task, and plan composers.
- [x] Rename the Designer generation trigger to `创作设置` while retaining vertical settings and `图片数量`.
- [x] Re-run focused page and Designer tests.

### Task 4: Verify And Publish

**Files:**
- Review all files in `git diff origin/main...HEAD` and `git diff`.

- [x] Run Studio full tests and `bun run build`.
- [x] Run affected Go tests and then `go test ./...` because the pending release includes server changes.
- [x] Inspect desktop and mobile composer layouts in the local browser and exercise both popovers.
- [x] Run `git diff --check`, audit the staged set, and review the complete release diff.
- [x] Fix all critical or important review findings and rerun affected verification.
- [ ] Commit intended changes, push `main`, and prove local `main`, `origin/main`, and `git ls-remote origin refs/heads/main` match.
