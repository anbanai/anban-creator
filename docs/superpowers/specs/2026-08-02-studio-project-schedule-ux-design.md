# Studio Project Selection and Schedule UX Design

## Goal

Make project choice the first and most recognizable decision in Studio creation flows, and make plan scheduling read as one coherent control with an explicit execution result.

## Scope

This change covers every interactive project selector in Studio:

- `ProjectContextControl` in Dashboard, task creation, plan creation, and Designer.
- `ProjectSelector` filters in Tasks, Plans, and Timeline.
- Read-only project context rendered by `ProjectContextControl` where the same visual identity is useful.
- The project control position in new and edited plan forms.
- `SchedulePicker` and its adjacent time-pricing feedback in plan forms.

The project data contract already contains the required `avatar_url`, `name`, `description`, and `platform` fields. No Server API or database change is required.

## Product Boundary

Project and content type remain separate concepts. A project is long-lived content context; content type is an execution choice. The current Server contract still derives important behavior from `Project.Platform`, so this UX change must not broaden projects to support arbitrary task types or alter routing, validation, billing, or runtime selection.

Selecting a project may continue to set the compatible content type using the existing behavior. The form must still render content type as its own labeled field so the relationship is visible rather than looking like a duplicate account selector.

## Unified Project Presentation

Introduce one reusable project identity renderer used by project selection controls.

Each full project option shows:

- Project avatar.
- Project name.
- One-line project description.
- Localized content-type label derived from `platform`.

Fallbacks are deterministic:

- Missing or failed avatar: project-name initial in a neutral avatar.
- Missing description: localized content-type description.
- Long names and descriptions: one-line truncation without resizing the control.

The selected-value trigger uses the same identity. Creation forms use the full trigger. Dense page filters may use a compact trigger, but their open option list retains the complete identity. Search matches the project name and description. Existing grouping by localized content type remains.

Loading, empty, disabled, no-project, create-project, and read-only states remain accessible. The combobox keeps its existing `项目上下文` accessible name so assistive technology and established flows remain stable.

## Plan Form Order

The plan form follows this order:

1. Project.
2. Content type.
3. Execution profile.
4. Schedule.
5. Creative prompt and attachments.
6. Type-specific image or Montage settings.

Project selection moves out of the prompt composer's context bar and becomes the first labeled form field. Editing keeps the associated project read-only but presents the same complete project identity. The prompt composer no longer renders a duplicate project context row.

Changing the project preserves the existing reset and defaulting behavior for content type, image ratio, agent input, and Montage input.

## Schedule Control

`SchedulePicker` becomes one bordered, unframed control containing:

- A required segmented frequency choice: daily or weekly.
- A seven-column weekday selector shown only for weekly frequency.
- A labeled execution-time picker.
- A plain-language schedule summary.
- The existing time-pricing result immediately below the summary, visually integrated with the control.

Weekday labels use full labels where space permits and `一` through `日` on narrow screens. Tracks remain stable so selecting a day does not resize the layout.

Weekly schedules require at least one weekday. An empty weekday selection shows an inline validation message and prevents form submission. Daily schedules clear stored weekdays. Valid values continue to serialize to the existing five-field cron expression, so no API change is needed.

The summary is the source of truth visible to users, for example `每周一、三、五 20:00 自动执行`. The cron expression remains internal.

## Responsive Behavior

The plan form remains single-column at all widths. Project options use a stable avatar and flexible text column; content-type labels do not compress the name to an unreadable width. On narrow screens, descriptions truncate and weekday labels shorten, while controls remain touch-sized and do not overflow the dialog.

## Error Handling

- Project list load errors continue through the owning page's current query error behavior.
- Empty project lists retain the create-project path where currently available.
- Avatar failures fall back locally and do not make the selector unusable.
- Invalid or unsupported cron input uses the current safe daily default when loaded.
- A user-created empty weekly selection is a visible validation error and cannot be submitted.

## Verification

Component tests will cover:

- Full and compact project identities, avatar and description fallbacks, search, grouping, selection, loading, empty, disabled, no-project, and read-only modes.
- Project controls across Dashboard, task creation, plan creation, Designer, Tasks, Plans, and Timeline.
- Plan form field order and absence of the former prompt-context duplicate.
- Daily and weekly cron parsing and serialization, weekday validation, summary text, and responsive weekday labels.
- Preservation of project-driven type/default resets.

Run the full Studio test suite and production build. Then use the in-app browser at desktop and mobile widths to verify the plan dialog, project menus, filters, keyboard interaction, focus behavior, truncation, and lack of overlap.

## Non-Goals

- Changing `Project.Platform`, task/plan routing, or allowing one project to execute arbitrary content types.
- Server, billing, or persistence changes.
- Redesigning project cards or the project creation form.
- Changing the plan list layout outside the project filter's selector presentation.
