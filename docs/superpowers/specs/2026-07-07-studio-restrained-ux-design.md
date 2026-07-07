# Studio Restrained UX Design

**Date:** 2026-07-07
**Status:** Draft for user review

## Purpose

Anban Studio should feel restrained in the Steve Jobs sense: every visible choice should earn its place by making the user's next business action clearer, faster, or safer.

The goal is not to flatten the product visually. Warm backgrounds, shadows, rounded corners, badges, and subtle motion can stay when they clarify hierarchy, status, affordance, or brand feel. The work should focus on business functionality and usability: fewer unnecessary decisions, fewer repeated explanations, stronger defaults, clearer readiness, and shorter paths from intent to a completed creation task.

## Current Evidence

The current Studio already has useful foundations:

- `studio/src/pages/DashboardPage.tsx` is already close to the desired shape: one AI entry prompt, attachments, project selection, and configuration errors instead of a heavy dashboard.
- `studio/src/pages/TasksPage.tsx` contains a recovery workbench, status filters, bulk actions, and a stepped creation sheet.
- `studio/src/pages/ProjectsPage.tsx` owns platform-specific project configuration, visual templates, reference images, publishing settings, ecommerce defaults, and video defaults.
- `studio/src/pages/SettingsPage.tsx` frames configuration as a readiness center.
- `studio/src/lib/command-center.ts` already models readiness and next-best actions.
- Existing UX contract tests protect important business intent: Dashboard should not regress into passive panels, Tasks should keep recovery and creation path semantics, Settings should remain a readiness center, and Projects should keep platform-specific visual configuration order.

The usability risk is that some surfaces still ask users to interpret too many fields, status chips, sections, and low-frequency options before they can answer: "Can I create now, what should I fix, and what action should I take next?"

## Confirmed Direction

User feedback corrected the design direction:

- Primary optimization target: business function and ease of use.
- Secondary optimization target: tasteful visual restraint.
- Do not mechanically remove warm backgrounds, shadows, rounded corners, badges, or subtle decorative motion. These are compatible with "less is more" when they support hierarchy and feedback.
- Remove or reduce UI only when it duplicates business meaning, creates extra decisions, exposes irrelevant controls, or makes the next action less obvious.

## Scope

In scope:

- Logged-in Studio core workflow: Dashboard, Tasks, Projects, Settings, shared navigation, creation sheet, action states, and shared business UI components.
- Business-readiness checks before and during creation.
- Project-aware defaults that reduce manual choices.
- Platform-specific progressive disclosure in project and task forms.
- Recovery, publish approval, failed-task handling, and continuation paths.
- UX contract tests that protect simplified business flows.

Out of scope:

- Replacing the visual design system.
- Removing visual warmth or all shadows/rounded corners/badges by policy.
- Major API rewrites or new orchestration services.
- Redesigning login/register and connection guide pages beyond consistency cleanup.
- Changing agent execution contracts, plugin assets, or distribution manifests.
- Removing existing business capabilities just to make pages look simpler.

## Design Principles

1. **Business action first.** Each page should lead with what the user can do next, not with passive information.
2. **Defaults over choices.** When the selected project, platform, or environment implies a safe default, prefill it and avoid asking again.
3. **Progressive disclosure.** Show advanced options only when the current platform, task type, or user state makes them relevant.
4. **Readiness before failure.** Surface missing project, model, key, executor, publishing, credit, or upload requirements before the user reaches a dead submit button.
5. **One meaning, one expression.** Avoid repeating the same state as heading, description, badge, helper text, and alert unless the state is risky or blocking.
6. **Visual style serves function.** Warmth, depth, motion, and badges stay when they improve scanability or feedback; they are reduced only when they compete with the business task.
7. **Recoverability is core UX.** Failed, pending approval, running, and partial tasks should tell the user exactly how to continue.

## Product Flow

### 1. Dashboard: Intent Entry With Readiness

Dashboard remains a single creation entry surface.

Required behavior:

- Keep the main prompt as the primary first-screen object.
- Keep project selection and attachment upload close to the prompt.
- When no active project exists, show one short blocker with a direct create-project action.
- When required model/key/project readiness is unknown or not ready, show the shortest setup action instead of making the user discover the problem in Tasks or Settings.
- Keep passive summaries out of the first viewport unless they drive a specific action.

Design constraint:

- Do not bring back dashboard cards such as "今日创作态势", "接入状态", "下一步", or "最近任务" as standalone panels.

### 2. Task Creation: Project-Aware Path

The task creation sheet should feel like a short business path, not a form inventory.

Required behavior:

- Project selection determines task type, platform defaults, image ratio, ecommerce defaults, video defaults, and local/cloud execution defaults where available.
- Type should be visible as an outcome of project choice, not a second major decision after project selection.
- Advanced image and execution options should appear only for task types that can use them.
- Goal mode, watermark, image composition, ecommerce modules, and video controls should expose cost or irreversible consequences only where relevant.
- Cost estimate stays near submit and uses the selected project/task state.
- Insufficient credits, missing product photos, missing project, or missing model configuration should show direct recovery actions.

Design constraint:

- Keep the existing "任务创建路径" business semantics for tests and accessibility, but replace repeated visual step explanations with a quiet structure.

### 3. Tasks: Recovery Before Browsing

The Tasks page should prioritize work that needs action.

Required behavior:

- Keep the recovery workbench, but make it action-oriented: running queue, failed recovery, publish approval, and recent completion should link to the exact filtered list or task.
- Task rows should prioritize title, project, status, actionable state, created/completed time, and progress.
- Secondary chips such as platform, workflow readiness, local execution, published state, approval state, and progress should not overwhelm the title line.
- Bulk action copy should continue explaining which selected subset will be affected and which tasks will be skipped.
- Selecting tasks should reveal batch actions without making the entire list feel like a permanent bulk-operation screen.

Design constraint:

- Do not remove recovery, bulk download, cancel, clone, delete, publish marking, or approval visibility.

### 4. Projects: Configuration Without Irrelevant Fields

Projects should read as operating units and edit as platform-aware configuration.

Required behavior:

- Project cards summarize what affects creation: platform/account identity, positioning, publishing mode, visual/writer readiness, ecommerce/video defaults, and recent success.
- Repeated badges should collapse into a concise readiness summary when they describe the same configuration area.
- Project actions should emphasize create/edit/manage materials; destructive actions should stay available but visually secondary.
- The project form should show only fields relevant to the selected platform.
- Visual template, reference image, visual style, writer, theme, and publishing settings should explain their relationship through placement and labels rather than long helper paragraphs.
- Ecommerce and video defaults should remain in project configuration, but task-specific inputs such as product photos and concrete selling points stay in task creation.

Design constraint:

- Keep the tested visual configuration order: visual template/reference image before visual style for image-based project types.

### 5. Settings: Readiness Checklist

Settings should help the user understand whether Studio can create successfully.

Required behavior:

- Preserve the "接入就绪中心" concept.
- Present execution environment, model/key readiness, publishing channel, and account security as a checklist of dependencies.
- Each item should state status, business impact, and the shortest fix action.
- Detailed sections stay below the checklist and retain existing management controls.
- Web vs desktop differences should be clear without requiring the user to understand implementation details.

Design constraint:

- The readiness center should not become a decorative dashboard. It should behave as setup triage.

### 6. Navigation And Command Palette

Navigation should reduce decision cost without hiding useful capabilities.

Required behavior:

- Keep high-frequency creation paths near the top: Today/Dashboard, Projects, Tasks, Designer, Plans/Timeline, Templates.
- Keep low-frequency connection and account/settings paths grouped and visually secondary.
- Command palette should remain the power-user path for jumping, creating, recovering failed tasks, and opening readiness settings.
- Sidebar search should remain discoverable but not compete with the main Dashboard creation input.

Design constraint:

- Do not remove connection guides or settings access; make them lower-friction and lower-noise.

### 7. Shared Business UI

Shared components should encode restrained business behavior.

Required behavior:

- Empty states should name the missing business prerequisite and offer one primary action.
- Badges should communicate status or capability, not repeat nouns already present in adjacent text.
- Cards and panels should be used for meaningful grouping, not as the default answer to every section.
- Toasts should confirm completion or explain failure; they should not be the only place where a blocking issue is explained.
- Loading states should preserve layout and avoid drawing attention away from the user's current task.

## Data And Architecture

Preferred approach:

- Reuse existing frontend queries and `command-center` readiness modeling.
- Add helper functions in `studio/src/lib` only when multiple pages need the same readiness, label, or action decision.
- Keep handlers and server APIs unchanged unless the frontend cannot infer readiness safely.
- Avoid new global state for this UX pass.

Candidate frontend helpers:

- A readiness summarizer for Dashboard, Settings, command palette, and creation blockers.
- A project-default resolver for task creation defaults.
- A compact status-priority helper for task rows and recovery counts.

## Error Handling

Blocking states should use this hierarchy:

1. Prevent submission when the user cannot create a valid task.
2. Explain the missing requirement next to the relevant control.
3. Provide a direct action link when the fix lives on another page.
4. Use toast only as supplemental feedback for async failures and confirmations.

Examples:

- No project: link to create a project.
- Missing model/key readiness: link to Settings model/key section.
- Missing ecommerce product photos: focus product photo upload.
- Upload failure: keep the file context visible and allow retry.
- Insufficient credits: explain the cost gap and link to Credits.

## Testing Strategy

Studio changes should include focused tests that protect business UX rather than pixel-level styling.

Required test coverage:

- Dashboard still renders the single AI entry and does not reintroduce passive dashboard panels.
- Dashboard and task creation show direct blockers for missing project/configuration where applicable.
- Tasks still render recovery workbench labels and bulk-action skip explanations.
- Task creation still exposes the tested path semantics while relying on project-aware defaults.
- Projects keep visual template/reference before visual style and continue platform-specific field disclosure.
- Settings still presents execution, model/key, publishing, and account security readiness.

Verification commands for implementation:

```bash
cd studio && bun run test
cd studio && bun run build
```

Browser verification should inspect Dashboard, Tasks, Projects, and Settings at desktop and mobile widths.

## Acceptance Criteria

The UX pass is complete when:

- A new user with no project sees the shortest path from Dashboard to creating a project.
- A configured user can create a normal task from Dashboard or Tasks with fewer manual decisions because project defaults are applied.
- A user with failed, running, pending approval, or completed tasks can identify the next action without opening unrelated pages.
- A project editor only sees fields relevant to the selected platform and can understand what affects future task creation.
- Settings communicates readiness as actionable setup triage.
- Existing business capabilities remain available.
- Visual warmth, rounded geometry, depth, badges, and subtle motion remain where they help scanability or feedback.
- Tests and build pass for the Studio surface.

## Implementation Notes

Implementation should proceed in small slices:

1. Add or refine readiness/default helpers.
2. Improve Dashboard readiness blockers while preserving the single AI entry.
3. Simplify task creation flow around project-derived defaults and relevant-only controls.
4. Tune Tasks recovery/list actions for clearer next steps.
5. Tighten Projects cards/forms around platform relevance.
6. Convert Settings readiness into actionable checklist behavior.
7. Adjust UX contract tests and run Studio verification.

Each slice should keep behavior covered by tests before moving to the next.
