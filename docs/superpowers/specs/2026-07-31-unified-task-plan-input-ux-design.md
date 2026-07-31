# Unified Task And Plan Input UX Design

**Date:** 2026-07-31

## Goal

Make task and plan creation expose one clear contract: Studio sends the user's
prompt and ordered uploaded files to the managed Cloud Agent, and the Agent
decides how each file should be used. Remove the visually and conceptually
duplicated task-reference uploader. Make execution-profile cost differences
visible at the point of selection.

This change covers task creation, task cloning, plan creation, and plan editing.
It does not change project editing: a project's brand reference remains part of
the project context available to the Agent.

## Product Decisions

1. New tasks and plans do not create a dedicated `reference_image`. All new
   user-supplied files are ordered `input_attachments`.
2. Studio does not infer a primary image or assign business meaning to an
   attachment. The prompt, optional per-file instruction, project context, and
   ordered file index are the Agent's inputs.
3. The attachment order is a durable end-to-end contract. References such as
   "the first image" must resolve identically in Studio, persisted JSON, the
   Agent workspace, and Agent instructions.
4. Execution-profile cards show both the current fixed credit price and a
   relative multiplier derived from the active billing catalog. No multiplier
   is hard-coded.
5. Task and plan forms use the same information hierarchy and attachment
   interaction. Plan-specific scheduling remains the only major structural
   difference.

## Form Information Hierarchy

Both forms use this order:

1. Project context and derived task type.
2. Plan schedule, for plans only.
3. Prompt and ordered materials.
4. Execution profile.
5. Generation settings.
6. Sticky price summary and submission actions.

The Prompt composer is the only file-upload surface. It accepts the file types
allowed by the selected task type, displays upload state in place, and keeps the
Prompt beside the material order it references.

Generation settings keep common choices visible, including quantity, image
ratio, and image model. Lower-frequency controls such as watermark, strong-goal
mode, and platform-specific image toggles move into a collapsed advanced
section. The section opens automatically when it contains invalid or cloned
non-default values, so data is never hidden from correction.

The footer remains visible while the form scrolls. For tasks it shows the
selected profile's fixed price, quantity, total, and projected balance. For
plans it shows the current per-run fixed price and explains that each future run
uses the then-active immutable SKU.

## Material Semantics

Studio sends:

- the user's prompt;
- ordered input-attachment descriptors;
- optional per-file user instructions;
- normal task metadata such as project, schedule, task type, and execution
  profile.

Studio does not send a generated role such as `primary_reference`, does not
copy the first image into `reference_image`, and does not choose which image an
image-generation provider should receive. The Cloud Agent reads the complete
request and chooses the relevant tools and files.

The project snapshot may still contain a project-level brand reference. This is
project context, not a second task upload control. Agent instructions must make
clear that project context and task materials are separate sources and that the
Prompt determines whether either source is relevant.

## Attachment Ordering Contract

### Studio

Files enter the list in browser selection, drop, or paste order. Uploads may run
concurrently, but completion, retry, or failure must not move an item. Each tile
has a visible ordinal and supports pointer and keyboard reordering.

Studio maintains two identifiers:

- `index`: one-based position among all materials;
- `type_index`: one-based position among materials of the same user-facing
  type. Images use the label `Image 1`, `Image 2`; documents use `Document 1`,
  and so on.

Thus "the first material" resolves by `index`, while "the first image" resolves
by the image `type_index`, even when a document precedes it. File names remain a
third, explicit way to identify a material.

Deleting or reordering compacts the affected ordinals. Studio never rewrites
the Prompt automatically. If the Prompt contains a recognizable ordinal
reference, a reorder or deletion shows a non-blocking warning to check those
references before submission.

### Server And Agent Workspace

The API and service layers preserve the attachment array exactly. The bootstrap
service writes deterministic ordinal file names and an `index.json` entry for
every ordinary material. Each entry contains at least:

```json
{
  "index": 2,
  "type": "image",
  "type_index": 1,
  "file_name": "front.jpg",
  "path": ".anban-creator/input-attachments/02-front.jpg"
}
```

Agent instructions read `index.json` rather than relying on upload completion,
directory enumeration, or lexical file-name guesses. They resolve ordinal
language using the same global and per-type rules as Studio.

## Existing Reference Data

Completed and historical tasks remain immutable.

Before the new plan editor ships, active plans with a dedicated task reference
are migrated by prepending that asset as an ordinary image attachment and then
clearing the plan's dedicated reference field. The migration is idempotent and
does not duplicate an asset already present in the attachment list. Existing
ordinary attachments retain their relative order after the migrated image.

The ordinary attachment descriptor gains a server-verified asset identity for
this migration path. The Server verifies user ownership, finalized state,
allowed image type, and an accepted source purpose before persisting or
materializing it. Browser-supplied storage keys or download URLs never establish
asset identity.

When cloning a historical task that has a task-level reference, the clone
service converts the verified task reference into an ordinary image attachment
at the beginning of the clone's material list. The new task does not persist a
dedicated task reference. A project-snapshot reference is not duplicated into
task materials; it remains available through the selected destination project's
snapshot.

## Execution Profile Pricing

`ExecutionProfileSelector` receives the active task type and billing catalog in
addition to profile capabilities. For each available profile it looks up the
active `task_admission` SKU and displays:

- profile display name;
- relative multiplier;
- exact fixed price with `/ task` or `/ run` context;
- short quality/provider description;
- access-tier or unavailability state.

The baseline is the lowest current `price_credits` among available profiles for
the selected task type. Its multiplier is `1x`; every other multiplier is
`profile price / baseline price`, formatted without meaningless trailing zeros
and with at most two decimal places. The calculation uses the user's active
catalog price, including tier pricing, rather than provider cost, token usage,
or a legacy billing multiplier.

If a profile has no matching SKU, its card shows that pricing is unavailable
and the existing form-level creation blocker remains authoritative. The sticky
summary continues to show the exact total and discount detail; the multiplier
is comparative information, not a billing input.

## Component Boundaries

### `AgentPromptInput`

- Renders ordered, numbered attachment tiles.
- Exposes accessible move-before and move-after actions in addition to pointer
  drag and drop.
- Keeps retry in the same position.
- Emits a reorder callback through the attachment controller.
- Shows the ordinal-reference warning after destructive order changes.

### `usePromptAttachments`

- Owns stable ordered attachment state.
- Adds `move(id, targetIndex)` without recreating attachment identity or upload
  attempts.
- Serializes in rendered order.
- Preserves selection order independently of upload completion.

### Task And Plan Forms

- Remove `ReferenceAssetUpload` and its upload-blocking state.
- Stop writing new `reference_image` values.
- Share the same composer, execution-profile pricing presentation, advanced
  settings grouping, and footer summary pattern.
- Continue applying task-type attachment admission rules.

### Server

- Accepts a verified asset-backed ordinary attachment for migration and clone
  conversion.
- Migrates active plan references atomically and idempotently.
- Preserves attachment ordering through create, update, plan execution, clone,
  and bootstrap.
- Emits both global and per-type order in `index.json`.

### Agent Contracts

Relevant Claude and Codex Agent instructions explicitly state that the Agent,
not Studio, determines file usage from Prompt semantics. The canonical plugin
source remains host-neutral where possible. Any changes under `plugins/` update
both native manifest versions with the same patch bump.

## Error Handling

- Submission remains blocked while any attachment uploads, has failed, or
  violates the selected task type's admission policy.
- Retry retains the original ordinal.
- Removing a failed attachment uses the same renumber-and-warn behavior as any
  other deletion.
- Reordering is disabled only for a tile during the exact pointer/keyboard
  operation, not for unrelated concurrent uploads.
- Missing catalog prices use an explicit unavailable state; Studio never
  estimates a billing amount from a multiplier.
- Migration aborts before clearing a plan reference if asset verification or
  attachment persistence fails.
- Bootstrap rejects duplicated indices, unsupported asset identities, ownership
  mismatches, and attachments without a content source.

## Accessibility And Responsive Behavior

Attachment ordinals are visible text and part of each tile's accessible name.
Reordering is available without drag and drop. Status announcements identify
the affected file and its new ordinal. Focus remains on the moved or retried
tile.

Execution-profile cards remain a single-select control. Their accessible names
include profile name, multiplier, exact price, availability, and disabled
reason. On mobile, profile cards stack vertically and the sticky footer wraps
price information above the actions without covering form content.

## Verification

### Studio Tests

- Selection order survives out-of-order upload completion.
- Retry retains position; delete and move serialize the new order.
- Global and per-type numbering are correct for mixed file types.
- Keyboard reorder controls and announcements work.
- Ordinal Prompt references trigger a warning only after order changes.
- Task and plan forms expose one upload surface and do not submit a new
  `reference_image`.
- Historical plan material and historical-task clone conversion hydrate in the
  unified list.
- Profile cards show catalog-derived multipliers and exact prices for every task
  type, tier discount, missing SKU, and unavailable profile.
- Advanced settings auto-expand for validation errors and cloned non-defaults.

### Go Tests

- Handler and service table tests preserve array order across task create, plan
  create/update/run, and clone.
- Asset-backed attachments enforce ownership, finalized state, MIME/type, and
  allowed-purpose constraints.
- The plan migration is atomic, idempotent, and duplicate-safe.
- Bootstrap file names and `index.json` contain matching global and per-type
  indices.
- Agent/plugin contract tests require ordered-index instructions and updated
  native manifest versions.

### Fresh Validation

- Run targeted Studio component/page tests, then the full Studio test suite and
  production build with Bun.
- Run targeted server service, handler, migration, and MCP/plugin contract tests,
  then `go test ./...` and both repository Go builds.
- Use the in-app Browser to verify task create, plan create/edit, clone, upload,
  reorder, retry, desktop, and mobile flows with console and screenshot checks.

## Out Of Scope

- Studio-side image-content analysis or automatic role assignment.
- Provider-specific reference-image selection.
- Changes to project reference-image editing.
- Token-based or post-run variable pricing for managed Agent execution.
- Automatic rewriting of user Prompt text after attachment reorder.
