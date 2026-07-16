# Task Detail Balanced Context UX Design

**Date:** 2026-07-17
**Status:** Draft for written review

## Purpose

The result-first task detail redesign removed the previous wall of expanded metadata, configuration, reference-material, and log panels. That hierarchy is directionally correct, but the implementation over-corrected in two ways:

1. The main page lost the middle layer of task context, leaving progress, an empty result destination, and a quiet details trigger separated by excessive whitespace.
2. The details sheet lost the Studio visual language of warm accents, icons, grouped surfaces, and compact cards, replacing it with a narrow, mostly flat list of dividers.

This follow-up keeps results and recovery actions primary while restoring enough context and visual continuity for the page to feel complete and familiar.

## Confirmed Direction

The user selected **A1: Workbench plus summary strip**.

The design keeps the existing result-first workbench and adds one compact context summary after results. The details sheet remains the home for complete metadata, configuration, materials, and logs, but it regains the established Studio visual system.

The goal is not to restore the old sequence of expanded panels. The goal is to provide a visible middle layer between the primary result workflow and deep details.

## Scope

In scope:

- A compact task-context summary on the main task detail page.
- Direct navigation from each summary item to the matching details tab.
- Controlled details-tab state that survives the credit-dialog round trip.
- Restored warm-accent, icon, border, radius, and grouped-surface styling in the details sheet.
- Correct desktop sheet width and full-width mobile behavior.
- Focused component, page, and browser tests for the new hierarchy and interactions.

Out of scope:

- Server APIs, task persistence, result formats, billing calculations, publishing behavior, or SSE protocols.
- Changes to specialized result components, file galleries, video production, workflow review, or Seednote analytics.
- Reintroducing expanded task information, configuration, materials, or logs into the default main-page scroll flow.
- A broader Studio design-system redesign.

## Main Page Information Architecture

The default order remains:

1. Header, task identity, status, project, and primary actions.
2. Current progress, failure recovery, cancellation recovery, billing lock, or publish decision state.
3. Workflow review, task-type-specific results, generated files, collected failure files, and analytics.
4. Compact task-context summary.
5. General More Details command.

Completed tasks continue to lead with deliverables. Failed and cancelled tasks continue to lead with recovery. The summary never moves above a blocking decision or result surface.

### Task Context Summary

Add a single grouped surface with four independently interactive items:

1. **Overview**
   - Project name.
   - Manual or scheduled source.
   - Net consumed credits.
2. **Configuration**
   - Content type or platform.
   - Visual style when available.
   - Image ratio when available; omit the third value instead of inventing a task-type fallback.
3. **Materials**
   - Input attachment count.
   - Whether a reference-usage summary file exists. The main-page summary does not fetch or parse that file.
4. **Logs**
   - Current log count.
   - Live, interrupted, or persisted-only connection state.
   - Current structured progress description while active; otherwise the latest non-empty displayed log line.

Each item uses an icon, a short label, and one or two values. Missing data renders as an honest short value such as `未设置`, `0 项`, or `暂无日志`; it does not remove the item or expand an error panel.

Clicking an item opens the sheet directly on its matching tab. The general More Details command opens Overview.

Desktop uses one horizontal four-item strip. Mobile uses a stable two-by-two grid. The summary is one grouped surface, not four standalone cards.

## Details Sheet

### Width And Structure

The current `Sheet` primitive applies `data-[side=right]:sm:max-w-sm`, which can override a plain `sm:max-w-xl` class. The task details sheet must override the same variant layer explicitly so the rendered desktop width is `576px`, not `384px`.

- Mobile: full viewport width.
- Desktop: `576px` maximum width.
- Header and tabs remain fixed while tab content scrolls independently.
- Opening and closing the sheet does not change the main page scroll position.

### Header

The header contains:

- `任务详情` as the accessible title.
- Task title as compact context.
- Task status and project identity in a restrained metadata row.
- The existing close action.

The header does not repeat primary task actions such as cancel, continue, publish, or delete.

### Tabs

Keep the existing four tabs:

- Overview.
- Configuration.
- Materials.
- Logs.

The selected tab uses the Studio warm accent for the indicator and relevant icon treatment. It does not use the current isolated black underline.

### Overview

Replace the single flat divider list with two lightweight grouped surfaces:

1. Task timing and source.
2. Project and credit usage.

Credit details remains an explicit action. Closing the credit dialog returns to the sheet and restores the originating tab.

### Configuration

Keep immutable task/project snapshot precedence. Group fields by meaning:

- Content and platform.
- Visual and image settings.
- Platform-specific creation settings.

Use compact labeled rows inside grouped surfaces. Do not wrap every individual field in its own card.

### Materials

Preserve the compact reference-summary behavior:

- Show the usage conclusion first when valid.
- Show the input snapshot beneath it.
- Show a quiet unavailable or malformed state without inventing a conclusion.
- Use compact attachment rows with the original warm icon treatment.

### Logs

Use one grouped execution-dynamics surface with:

- Log count.
- Follow or pause-follow control.
- Copy action.
- Reconnect action when SSE is interrupted.
- Persisted and live Markdown log rendering.

An empty log state is compact and contextual. It does not leave a large blank white panel.

## Visual System

Reuse the existing Studio semantic tokens and component primitives.

- Warm accent for selected tabs, small icon containers, and active states.
- `6px` to `8px` radii, matching the operational Studio surface language.
- Thin semantic borders and restrained surface backgrounds.
- Icons from the existing Lucide set.
- Compact labels and values sized for scanning, not hero typography.
- No nested cards, decorative gradients, oversized empty states, or new raw color system.

The summary strip and details sheet should feel like the same product as the previous task panels, while using less vertical space.

## Component And State Boundaries

### `TaskDetailPage`

Remains responsible for:

- Queries and mutations.
- Task-state ordering.
- Specialized result selection.
- Credit dialog state.
- Details sheet open state and selected tab.

Expose one local helper equivalent to `openTaskDetails(tab)` so all summary entries and the general details command use the same transition.

### `TaskContextSummary`

Add a focused component under `studio/src/components/tasks/`.

It receives already-loaded task, project, file, log, credit, and connection-state values. It derives compact display values and emits a requested details tab. It performs no network request and owns no cross-page state.

### `TaskDetailsSheet`

Change the selected tab from internal-only state to a controlled contract supplied by `TaskDetailPage`:

- Selected tab value.
- Tab change callback.
- Existing open state and open-change callback.

Changing task IDs resets the page-owned selected tab to Overview. Opening credit details preserves the selected tab so closing the dialog restores the exact sheet context.

## Data Flow

No new request is introduced.

1. `TaskDetailPage` continues loading task, project, files, logs, and credit data.
2. It passes compact values to `TaskContextSummary` and complete values to `TaskDetailsSheet`.
3. A summary click selects a tab and opens the sheet.
4. Sheet tab interactions update the same page-owned selected-tab state.
5. Existing task polling, SSE updates, file queries, and credit queries continue independently.

The summary reflects the same live data as the sheet. It does not cache or duplicate server state.

## State And Error Handling

- Running and pending tasks show monotonic progress and result destination before the summary.
- Completed tasks show deliverables before the summary.
- Failed and cancelled tasks show recovery before the summary.
- Publish approval remains a primary page decision state.
- Billing lock continues to disable delivery actions without hiding summary context.
- Missing configuration values use short neutral fallbacks.
- The Materials summary shows `已生成使用结论` when the summary file exists and `仅任务输入` when it does not. Parsing remains inside the Materials tab so the summary introduces no request.
- Malformed reference summaries expose a compact degraded state and preserve input snapshots.
- SSE errors mark the Logs summary as interrupted and expose reconnect inside Logs. Active tasks without an SSE error show `实时`; terminal tasks show `已结束`.
- Empty logs display `暂无日志` or `等待输出` without a large decorative empty state.

## Accessibility And Interaction

- Summary items are real buttons with unique accessible names.
- Icons are decorative unless they convey a state not present in text.
- The summary has a semantic label describing it as task context.
- Tab selection, focus management, Escape, backdrop close, and close-button behavior continue through the existing primitives.
- Opening a summary item moves focus into the sheet through the existing dialog behavior.
- Closing returns focus to the summary item or general details trigger that opened it.
- Text and controls must not clip at desktop, tablet, or mobile widths.

## Testing Strategy

Focused tests must prove:

- The four summary items render the correct existing data.
- Each summary item opens the correct details tab.
- More Details opens Overview.
- Credit details closes back to the originating sheet tab.
- Changing tasks resets the selected tab to Overview.
- Summary order remains after results, deliverables, recovery, and decision states.
- Empty and malformed configuration, materials, and logs use compact honest fallbacks.
- The desktop sheet class overrides the base right-side width at the matching variant layer.
- Mobile uses a two-by-two summary and full-width sheet without horizontal overflow.
- Existing log follow, copy, reconnect, file delivery, task continuation, cancellation, publishing, and review actions remain covered.

Browser verification covers running, completed, failed, and publish-approval tasks on desktop and mobile. Capture the main page, each summary-to-tab transition, the restored desktop sheet, and the mobile sheet. Check console health, focus return, clipping, overlap, and horizontal overflow.

Verification commands:

```bash
cd studio && bun run test
cd studio && bun run build
```

## Acceptance Criteria

The change is complete when:

- The page retains a result-first hierarchy but no longer feels empty or unfinished.
- Project, configuration, material, and log context is visible without opening the sheet.
- Complete secondary information remains outside the default main scroll flow.
- Each summary item opens the matching details tab in one action.
- The sheet visibly matches the Studio warm-accent and grouped-surface language.
- The desktop sheet renders at `576px`; mobile renders full width.
- No old expanded panel wall returns.
- Existing task behavior and result surfaces remain unchanged.
- Focused tests, the full Studio suite, production build, and browser QA pass.
