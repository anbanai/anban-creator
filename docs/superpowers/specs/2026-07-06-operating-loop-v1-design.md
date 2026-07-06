# Operating Loop v1 Design

**Date:** 2026-07-06
**Status:** Draft for user review

## Purpose

Anban already has the core pieces of a creator platform: Studio, projects, manual tasks, scheduled plans, agent execution, workflow artifacts, publish approval, credits, usage, templates, and plugin distributions. The product risk is no longer missing raw capability. The risk is that users must understand too many separate surfaces before they can answer the daily operating question: what should I do next?

Operating Loop v1 turns the platform into a user-facing creator operating loop:

`接入就绪 -> 项目作战单元 -> 创建任务/计划 -> Agent 执行 -> 产物复盘 -> 发布审批 -> 复用/追踪 -> 经营洞察`

The goal is not to rebuild Anban. The goal is to make existing capabilities feel like one best-practice business chain from a platform user's point of view.

## Current Evidence

The current repository already contains useful building blocks:

- `studio/src/lib/command-center.ts` builds next-best actions from tasks, projects, plans, credits, and publishing approval.
- `studio/src/pages/DashboardPage.tsx` renders a "今日指挥中心" with running, failed, approval, schedule, and credit signals.
- `studio/src/pages/TasksPage.tsx` has a recovery workbench, stepped task creation sheet, publish approval badges, readiness labels, and bulk operations.
- `studio/src/pages/TaskDetailPage.tsx` supports continue execution, clone, publish approval/reject, credit details, project parameters, logs, files, and review summary.
- `studio/src/components/TaskWorkflowPanel.tsx` can render workflow stages and review summaries from `workflow_status`.
- `server/service/workflow_status.go` persists creation workflow status from canonical artifacts and `review.json`.
- `server/model/project.go` and `server/model/task.go` include publish approval configuration and task approval state.
- `studio/src/pages/SettingsPage.tsx` frames settings as an "接入就绪中心".

Operating Loop v1 should connect these capabilities with consistent semantics and a shared action model rather than introducing a parallel product layer.

## User Value

For a creator or operator, the platform should answer five questions without forcing them to inspect every page:

1. Am I ready to create today?
2. Which project or account should I operate next?
3. Which tasks are blocked, running, recoverable, or ready to reuse?
4. Which content is safe to move into the publishing channel?
5. What did recent outputs teach me for the next task or plan?

When these questions are answered consistently, Anban becomes a daily operating workspace instead of a toolbox.

## Scope

In scope for v1:

- Make Dashboard the primary next-action center.
- Make Projects behave as account/project operating units, not only configuration cards.
- Make Tasks and Task Detail present a consistent recovery and publishing action model.
- Surface workflow stages, review readiness, warnings, and artifacts as the main user-facing task narrative.
- Connect settings readiness and credit risk to Dashboard and creation flows.
- Keep publish approval language consistent: publishing means creating a WeChat draft-box entry, not mass sending.
- Add focused frontend and service-level tests where behavior changes.

Out of scope for v1:

- Parent/child fanout tasks for one topic across multiple projects.
- Major database rewrites or a new workflow orchestration service.
- Replacing the agent pipeline or rewriting Claude/OpenClaw/Codex skill distributions.
- Miniapp parity work.
- New billing products.
- Collaborative editing or multi-user approval workflows.

## Design Principles

1. **Next action first.** Every operational surface should make the next user action obvious.
2. **Project as source of truth.** A project is the user's account, style, publishing, and default operating context.
3. **Task as inspectable production run.** A task should explain what happened, what was produced, what is risky, and what can be done next.
4. **Recovery is a first-class path.** Failed and partial tasks should lead to continue, clone, download, delete, or inspect actions.
5. **Publishing is gated and precise.** Users must understand whether content is only staged to a draft box or manually marked as published.
6. **Reuse before reinvention.** v1 should compose existing APIs, models, and components before adding new infrastructure.

## Product Flow

### 1. 接入就绪

Users start from readiness rather than from configuration pages.

Dashboard should summarize:

- project readiness: at least one active project exists
- model/key readiness: user has API keys and model config status is known
- execution readiness: local executor state when in desktop, cloud fallback otherwise
- publishing readiness: at least one project can publish and whether approval is required
- credit readiness: balance and daily sign-in state

Settings remains the detailed center, but its state should feed Dashboard and command palette decisions.

### 2. 项目作战单元

Projects are operating units. Each project card should communicate:

- platform and account identity
- positioning/instructions summary
- publish readiness and approval mode
- task count, completed count, and success rate
- selected defaults that affect creation, such as visual style, image ratio, writer/theme/author, ecommerce modules, or video defaults
- direct actions: create task, create plan when supported, edit, open topic/material pool

The project page should help users decide whether a project is ready to produce, not merely whether a row exists.

### 3. 创建任务/计划

The stepped task creation sheet is the right direction. v1 should preserve it and make it more state-aware:

- selecting a project resolves type and default operating parameters
- cost estimate should remain visible near submit
- insufficient credits should explain the required action
- local/cloud execution should be a clear delivery choice when available
- goal mode should communicate cost and retry behavior before submission

Plans should use the same mental model, with per-run cost and next trigger clarity.

### 4. Agent 执行

Running tasks should present live progress in business terms:

- current stage title and description from structured progress events
- monotonic progress bar
- logs available as diagnostic detail, not the primary user story
- queue state visible for pending tasks

The server should continue to persist latest progress and workflow status. v1 does not require live per-stage workflow mutation, but it should leave a clean path for it.

### 5. 产物复盘

Task detail should show the production narrative in this order:

1. terminal status and next actions
2. publish approval gate when present
3. workflow stage progress
4. review summary and readiness
5. project parameters used by the task
6. output files and previews
7. logs and credit details

This preserves debug power while making the user-facing story easier to scan.

### 6. 发布审批

Publish approval is a business gate:

- pending: draft is ready but held before entering the WeChat draft box
- approved: user allowed the draft-box operation
- rejected: content did not enter the draft box and can be continued or cloned

Dashboard, task list, task detail, and command palette should all use the same labels and destinations for approval tasks.

### 7. 复用/追踪

Completed tasks should expose what can be reused:

- clone task with full configuration preserved
- continue execution with supplemental instructions/files
- download task files
- mark published when publishing happened outside the system
- use review risks and next actions to guide the next prompt

Seednote tracking and future post-publish analytics can attach after this loop without changing the core task model.

### 8. 经营洞察

Dashboard and Usage should remain lightweight in v1:

- recent trend and status distribution remain useful
- failed, approval, upcoming, and credit signals outrank passive charts
- usage and credits should explain blockers before the user reaches a submit button

More advanced analytics can be a later batch after the core loop is consistent.

## Frontend Design

### Dashboard

`DashboardPage` should become the canonical entry point for "what now?"

Changes:

- Expand `CommandCenterReadiness` so it can represent known/unknown states instead of defaulting model, key, and local executor to ready.
- Add action ranking rules for setup blockers, failed tasks, publish approval, credit risk, upcoming plans, and creation.
- Ensure each tile links to the exact recovery or setup destination.
- Keep charts secondary to action cards.

### Projects

`ProjectCard` should become a compact operating card.

Changes:

- Add readiness badges for publishing, approval, missing style/defaults, and recent success rate.
- Keep direct create task/create plan actions.
- Keep topic/material pool access visible.
- Avoid turning the project page into a wizard; editing stays in the dialog.

### Tasks

`TasksPage` should keep the recovery workbench and make list badges consistent with Dashboard.

Changes:

- Use the same readiness labels as workflow review summary.
- Make publish approval items link to task detail and appear in recovery counts.
- Keep bulk operations but ensure copy describes which selected subset each operation affects.

### Task Detail

`TaskDetailPage` should render `WorkflowStageProgress` in the main body, not only `WorkflowReviewSummary`.

Changes:

- Place workflow stages before output galleries.
- Keep review summary visible when `review.json` exists.
- Make continue/clone actions contextual:
  - continue: use original work directory and supplemental input
  - clone: new billed run with preserved configuration
- Keep credit details in a dialog to avoid crowding the operational view.

### Settings

`SettingsPage` remains the detailed readiness center.

Changes:

- Expose enough status through existing APIs or lightweight frontend queries so Dashboard can avoid optimistic defaults.
- Keep API key, model config, local executor, notification, and account security grouped by operating dependency.

## Backend Design

v1 should avoid a new aggregate endpoint unless existing client-side queries become too expensive.

Preferred backend posture:

- Keep `tasks`, `projects`, `plans`, `credits`, `api-keys`, and `model-config` APIs as sources.
- Use existing `workflow_status` for review and stage state.
- Use existing `publish_approval_state` for approval actions.
- Add small response fields only if the frontend cannot infer readiness safely from existing data.

Possible lightweight additions:

- A settings readiness DTO if Dashboard needs server-authoritative readiness beyond API keys/model config.
- Workflow warning enrichment if publish approval or publish failure should appear directly inside `workflow_status`.

Do not add a new orchestration service in v1.

## Data Flow

Dashboard signal flow:

1. Fetch tasks, projects, plans, credit balance, sign-in status, API keys, model config, and local executor status where available.
2. Normalize them into `CommandCenterSignals`.
3. Build `NextBestAction[]`.
4. Render the highest priority actions consistently in Dashboard and command palette.

Task detail flow:

1. Fetch task and completed files.
2. Parse `workflow_status`.
3. Render approval gate from `publish_approval_state`.
4. Render stage progress from workflow stages.
5. Render review readiness from workflow review.
6. Render files, logs, project parameters, credit details, and recovery actions.

Project flow:

1. Fetch active/archived projects and project stats.
2. Compute per-project operating status from platform config, publishing config, style/defaults, and stats.
3. Render direct create/plan/edit/topic actions.

## Error Handling And Recovery

Error handling should be expressed as user actions:

- missing project: create first project
- missing key/model config: open settings readiness center
- low credits: sign in or review credits
- failed task: open task detail for continue/clone/logs
- publish approval pending: open detail and approve or reject
- rejected publish: continue or clone
- invalid workflow JSON: hide broken panel and keep files/logs visible
- missing review: show files and logs without blocking task completion

The UI should never leave the user with only a status badge and no next action.

## Testing Strategy

Frontend tests:

- `command-center` tests for ranking setup blockers, failed tasks, approval, credit risk, upcoming plans, and create actions.
- Dashboard contract test for next-action center and readiness tiles.
- Project card/page tests for operating unit badges and direct actions.
- Task list tests for recovery workbench, approval badges, readiness labels, and bulk action copy.
- Task detail tests for workflow stage panel, review summary, approval gate, continue/clone semantics, and malformed workflow JSON.
- Settings readiness contract tests for dependency grouping.

Backend tests:

- Existing workflow status and publish approval tests should remain.
- Add tests only if new backend fields or enrichment are introduced.

Verification commands for implementation batches:

```bash
cd studio && bun run test
cd studio && bun run build
go test ./...
go build -o /tmp/anban-creator-server ./server
```

Implementation batches can run narrower tests while developing, but completion should run the relevant full surface checks.

## Rollout Plan

### Batch 1: Signal Model And Navigation

- Extend `CommandCenterReadiness` to represent real readiness states.
- Feed Dashboard and command palette with the same action priorities.
- Add tests for ranking and links.

### Batch 2: Project Operating Cards

- Add project-level operating badges and clearer stats.
- Preserve existing edit and topic pool flows.
- Add project page/card tests.

### Batch 3: Task Detail Production Narrative

- Render workflow stage progress in `TaskDetailPage`.
- Align action order around approval, recovery, review, files, logs, and credits.
- Add task detail tests.

### Batch 4: Recovery And Publishing Consistency

- Align Dashboard, task list, command palette, and task detail copy for approval and failed tasks.
- Confirm WeChat draft-box language is consistent.
- Add regression tests for labels and destinations.

### Batch 5: Readiness Feedback Into Creation

- Reflect readiness and credit blockers in task/plan creation.
- Avoid submitting tasks when prerequisites are clearly missing.
- Keep server-side validation as the final authority.

## Acceptance Criteria

Operating Loop v1 is complete when:

- A new user can start from Dashboard and reach the correct setup or first project/task action.
- An active operator can see failed tasks, approval tasks, upcoming plans, and credit risk without visiting multiple pages.
- Project cards communicate whether the project is operationally ready.
- Task detail explains what was produced, what stage it reached, what review said, and what the next action is.
- Publish approval has consistent language and destinations across Dashboard, command palette, task list, and task detail.
- Failure recovery paths are visible and differentiated between continue execution and clone.
- Existing task creation, project editing, publish approval, workflow status parsing, and credit details continue to work.
- Frontend and backend verification for touched surfaces passes.

## Future Batches

After v1, the platform can safely pursue larger workflow improvements:

- multi-project fanout for one topic
- post-publish analytics collection and learning loops
- richer Seednote tracking and benchmark dashboards
- miniapp parity for the operating loop
- live workflow stage mutation during agent execution
- project memory and template recommendation surfaces

These should build on the same operating loop rather than adding separate navigation concepts.
