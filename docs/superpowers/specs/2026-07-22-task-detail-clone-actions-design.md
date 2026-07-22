# Task Detail Clone And Actions Design

## Goal

Make terminal task actions discoverable on the task detail page and make cloning a true create-from-template workflow.

The completed experience must satisfy these rules:

- Continuing a task opens the existing supplemental prompt and attachment dialog and calls the resume API.
- Cloning opens the same complete form used to create a task, prefilled from the source task.
- All copied clone values are editable, including the project and project-derived task type.
- Clone and delete are visible header actions instead of items in an overflow menu.
- Published state is a checkbox rather than an action button.

## Task Detail Header

The task header uses one responsive action group. Controls wrap on narrow screens without changing their order or resizing the surrounding content.

- A completed task shows a `Published` checkbox. Checking or unchecking it updates the existing published-state endpoint. The control is disabled while the mutation is pending. A failed request leaves the server-backed state unchanged and shows an error toast.
- A terminal task that supports resume shows `Continue`. It opens `ResumeTaskDialog`; this remains the only prompt-only task dialog.
- Completed, failed, and cancelled tasks show `Clone task` as a visible secondary action.
- Completed, failed, and cancelled tasks show a visible destructive `Delete` action.
- Pending and running tasks keep the existing cancellation action and do not expose invalid clone or delete actions.
- The overflow menu and its task actions are removed. Unpublishing is performed by clearing the published checkbox.

The failed-task recovery button remains next to the failure explanation because it is contextual. The page must not render a duplicate resume action for the same failed state.

## Shared Task Form

Extract the task creation dialog and its form state from `TasksPage` into a reusable `TaskFormDialog` component.

The component owns the behavior shared by create and clone modes:

- React Hook Form and Zod validation.
- Project loading, selection, and project-derived task type.
- Prompt and input attachment composition.
- Quantity, image settings, reference image, watermark, goal mode, and content-image controls.
- Ecommerce and Montage-specific inputs.
- Image model availability, billing preview, local execution selection, upload state, dirty-close confirmation, and submission blockers.

Mode-specific inputs are limited to title, description, submit labels, initial values, attachment preview ownership, and the submit callback. `TasksPage` supplies project-default initial values for create mode. `TaskDetailPage` supplies source-task initial values for clone mode.

Changing the project in either mode follows the same current creation behavior: the selected project's platform is authoritative, the form task type changes with it, and project defaults replace type-specific settings that no longer apply.

## Form UX

Remove the static `01 Type / task type follows project` panel from the top of the dialog. It does not accept input and duplicates the lightweight type badge next to the project selector.

Keep the project selector and task type badge in the prompt context area.

Improve image settings as follows:

- On desktop, render cover ratio and image model in a two-column field group; stack them on mobile.
- Render cover ratio as four stable visual choices for `3:4`, `1:1`, `4:3`, and `16:9`. Each choice includes a simple aspect-ratio outline, orientation label, selected state, and project-default indicator.
- Keep image model as a searchable selector because the option list, providers, and account-tier availability are dynamic.
- The selected model control shows its display name, provider, and tier or custom status without expanding all models into the form.

## Clone Initial Values

Map the source task into the shared form without mutating the query result. Clone mode starts with quantity `1` and copies:

- Project ID and project-derived task type.
- Prompt.
- Original input attachments, excluding attachments added by resume operations.
- Task reference image asset.
- Image ratio and image model.
- Watermark.
- Goal and goal mode.
- Seednote content and tail image choices.
- Article cover and content-image choices.
- Ecommerce product photos, selected modules, target platform, selling points, and language.
- Montage input.
- Execution target as a current user choice where the shared create form already exposes it.

If a copied asset cannot be safely reused, the form stays open and identifies the asset that must be removed or uploaded again.

## Clone API Contract

Extend `POST /api/v1/tasks/:id/clone` to accept the editable task creation fields, including quantity and project ID. The source task ID remains in the path and is never client-controlled through the request body.

The server flow is:

1. Authenticate the user, load the source task, verify ownership, and require a terminal source status.
2. If the request contains the complete override contract, resolve the selected active project and treat its platform as authoritative.
3. Validate prompt length, quantity, model entitlement, billing, goal rules, type-specific inputs, reference assets, uploads, and input attachments using the same rules as task creation.
4. Create one to five new tasks from the submitted values and the selected project's current snapshot.
5. Preserve the root source-task provenance only for immutable source inputs reused by the clone. When the destination project differs, authorization resolves the trusted source task record and uses that record's project ID when checking the source object prefix; it must not derive the source prefix from the destination project or accept a client-supplied source project. This permits the executor to read only those source objects without widening storage access.
6. Return the first created task through the current Studio API helper, matching existing create behavior when quantity is greater than one.

When the request omits the complete override contract, the endpoint retains the existing exact-clone behavior. Bulk clone continues to call this active exact-clone branch so its existing behavior is unchanged.

Although the endpoint accepts both forms during the transition, this is not a legacy client compatibility layer: the empty request is the active bulk-clone contract.

## Error Handling

All validation happens before billing or task creation side effects. Errors use existing structured response and toast patterns.

- Invalid, inactive, or unauthorized projects are rejected.
- An unavailable image model is rejected against the caller's current tier.
- Invalid or inaccessible source assets are rejected without granting broader storage access.
- Billing rejection returns the existing payment-required response.
- The dialog remains open with the user's edited state after any failure.
- Successful clone creation invalidates task queries and navigates to the first new task. When quantity is greater than one, the success toast includes the number created.

## Test Strategy

Use test-driven development for each behavior change.

Studio tests cover:

- Visible task actions by task status and removal of the overflow menu.
- Published checkbox updates in both directions, pending disablement, and failed-request behavior.
- Resume continues to open only `ResumeTaskDialog` and submit the latest prompt and attachments.
- Create and clone render the same shared form component.
- Source tasks map to clone form values for standard, article, seednote, ecommerce, and Montage cases.
- Resume-only attachments are excluded from clone defaults.
- Project changes update type and project defaults.
- Visual ratio controls and searchable image model selection submit the expected values.
- Clone failures preserve edited values; success invalidates queries and navigates correctly.

Go tests cover:

- Full clone override request binding and validation.
- Project ownership and active-status enforcement.
- Current project snapshot and platform selection.
- Every supported task-specific field reaches the created task.
- Quantity bounds and multiple task creation.
- Immutable input source provenance across same-project and cross-project clones, plus cross-user rejection.
- Empty single-clone and bulk-clone requests preserve exact-clone behavior.
- Billing and model entitlement failures create no task.

Verification includes targeted Studio and Go tests, full `bun run test`, `bun run build`, `go test ./...`, and the relevant server build. Browser QA covers desktop and mobile header layout, publish toggling, resume, clone form defaults, project switching, and delete confirmation with no relevant console errors.

## Scope

This work changes the task form boundary, task detail actions, the single-task clone contract, and their direct tests. It does not redesign task list cards, bulk-action UX, project editing, billing policy, or task execution behavior.
