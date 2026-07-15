# Task Detail Streamlined UX Design

**Date:** 2026-07-15
**Status:** Draft for written review

## Purpose

The task detail page should answer three questions in order:

1. What is the task doing now?
2. What has it produced?
3. What should the user do next?

The current page gives task metadata, project configuration, reference-material audit data, and execution logs the same visual weight as progress and results. This produces a long sequence of bordered sections, including a large fallback reference-material panel when no usage summary exists. The approved direction is a single-line workbench with an independent details sheet.

## Confirmed Direction

The user selected:

- **A: Single-line workbench** for the page structure.
- **A1: Independent right-side sheet** for secondary details.

Progress and results are the primary reading path. Metadata, configuration, reference inputs, and logs remain available but leave the page's default scroll flow.

## Scope

In scope:

- `studio/src/pages/TaskDetailPage.tsx` information hierarchy and composition.
- A focused task-details sheet using the existing Studio `Sheet` primitive.
- Compact presentation of reference inputs and reference-usage conclusions.
- Focused tests for main-page ordering, sheet navigation, fallback material state, and existing task actions.
- Desktop and mobile responsive verification.

Out of scope:

- Server API or task-model changes.
- Changes to task execution, persistence, publishing, billing, or result generation.
- Redesigning specialized result components such as video production, ecommerce galleries, workflow review, or Seednote analytics.
- Removing task actions or diagnostic information.
- A broader Studio visual-system redesign.

## Information Architecture

### Main Page

The default vertical order is:

1. Header: back navigation, title, task type, status, project identity, and task actions.
2. Current state: running/pending progress, failed-task recovery, or cancelled-task recovery.
3. Blocking or decision states: billing lock and publish approval when applicable.
4. Results: workflow review and the existing task-type-specific result surfaces.
5. More details: one quiet trigger that opens the task-details sheet.

Completed tasks do not render a redundant progress panel. Their results move directly below any blocking or decision state.

For a running task without result files, the result area uses a compact pending state. It identifies that results will appear there without creating a large decorative empty-state card or implying that the user must remain on the page.

### Task Details Sheet

The right-side sheet contains four tabs:

- **Overview:** created, started, and completed timestamps; source; project; credit summary and access to transaction details.
- **Configuration:** the immutable project/task snapshot already exposed by the page, including visual style and platform-specific creation settings.
- **Materials:** initial reference inputs and, when available, the agent's reference-usage conclusion.
- **Logs:** persisted or live execution logs, SSE error state, follow-output control, and copy action.

Opening the sheet does not change the main page's scroll position. The sheet uses `w-full sm:max-w-xl`, producing a full-width mobile surface and a maximum desktop width of `576px`.

The sheet keeps one selected tab for the current open session. It opens on Overview each time the user enters a different task so stale tab state does not carry across tasks.

## Reference Material Behavior

Reference material must no longer occupy a standalone main-page section.

When a usage conclusion exists:

- Show the conclusion first.
- Show the referenced input items beneath it in a compact list.

When no usage conclusion exists:

- Show `未生成素材使用结论，仅展示任务输入。` as quiet supporting text.
- Show the input snapshot directly.
- Do not render the current large warning box, the standalone disclaimer row, or an additional framed card around each single item.

When no reference input exists, use a short empty state inside the Materials tab. Do not show the Materials tab as a warning or error.

Malformed usage-summary data remains an honest degraded state: show a compact parse-failure note and the intact input snapshot. Do not invent a usage conclusion.

## Component Boundaries

`TaskDetailPage` remains responsible for task queries, mutations, result selection, and primary task actions.

Add a focused task-details sheet component under `studio/src/components/tasks/`. It receives already-loaded task, project, log, and credit data plus the callbacks needed for credit details and log controls. It does not fetch the task again or introduce global state.

Keep reference-summary parsing in `ReferenceUsageSummary`. The component gains an explicit compact sheet variant; the task page and sheet do not duplicate its parsing logic.

Existing specialized result components remain unchanged and continue to render in the main content flow.

## Interaction And Accessibility

- The More details trigger is a text-and-icon command because it opens a distinct information surface.
- The sheet has an accessible title and description.
- Sheet tabs use the existing tab primitives and keyboard behavior.
- Close behavior supports the close button, `Escape`, and backdrop interaction through the existing `Sheet` primitive.
- Log copy and follow-output controls retain accessible names.
- Destructive actions remain outside the sheet in the existing header action model.
- Sheet content scrolls independently while the header and tab controls remain available.

## State And Error Handling

The page preserves current task-state behavior:

- Running and pending tasks show monotonic progress and the latest structured progress description.
- Failed tasks show the server-provided failure reason and a continuation action.
- Cancelled tasks show the preserved-context recovery path.
- Billing-locked tasks keep delivery actions disabled and expose the recharge action.
- Publish approval remains a primary decision state on the page.
- SSE errors appear in the Logs tab and do not replace persisted logs.
- Missing or malformed reference-usage summaries degrade inside the Materials tab without obscuring progress or results.

No new network request, persistence format, or server fallback is introduced.

## Testing Strategy

Focused component/page tests must prove:

- Progress and results remain in the main reading path.
- Task information, configuration, reference material, and logs are not expanded on the main page.
- More details opens the sheet on Overview.
- Each sheet tab exposes its expected content.
- Missing and malformed reference-usage conclusions render compact, honest fallback copy and preserve the input snapshot.
- Running logs still render Markdown, follow output, and copy raw text.
- Existing cancel, continue, clone, delete, publish marking, approval, credit detail, download, and video-result workflows remain available in their current states.
- Opening a different task resets the sheet tab to Overview.

Verification commands:

```bash
cd studio && bun run test
cd studio && bun run build
```

Browser verification covers:

- Running, completed, failed, and publish-approval states.
- Desktop layout at the reference screenshot scale.
- Mobile layout with a full-width sheet.
- Sheet tab keyboard/click navigation, close behavior, result visibility, and absence of horizontal overflow.

## Acceptance Criteria

The redesign is complete when:

- A running task's progress and result destination are understandable in the first viewport.
- A completed task leads with deliverables rather than execution metadata.
- No standalone reference-material audit panel appears in the main task flow.
- All prior metadata, configuration, reference input, and log information remains reachable within one action.
- The sheet is usable on desktop and mobile without obscuring or clipping its controls.
- Existing task actions and specialized result surfaces keep their behavior.
- Focused tests, the complete Studio test suite, and the Studio production build pass.
- Browser verification finds no overlap, overflow, accidental wrapping, or incoherent empty space.
