# Montage Studio and Project Completion Design

## Context

Montage already exists as an independent runtime, task type, plan type, agent,
MCP profile block, artifact contract, and billing surface. The remaining product
gap is the standard Studio project path:

- Studio types and schemas declare `montage` and `montage_defaults`, but the
  project form does not expose them.
- `ProjectService` rejects `montage` as an invalid project platform.
- The project HTTP request does not map `montage_defaults` into the model.
- Task and plan forms expose only part of the stable Montage input and do not
  provide source asset uploads or inherit project defaults.

This change completes those missing product paths while keeping Montage scoped
to its own project, task, and plan contracts.

## Goals

- Create and edit Montage projects through Studio and the project API.
- Persist every stable `MontageDefaults` field on the project.
- Initialize new Montage tasks and plans from the selected project's defaults.
- Expose every stable business-authored Montage input field in Studio.
- Upload image, video, audio, text, and document source assets through the
  existing direct-upload and pending-upload ownership flow.
- Preserve Montage's separate agent, runtime policy, task input, and artifact
  contract.
- Cover the complete project-to-task and project-to-plan paths with focused
  frontend and Go tests.

## Non-Goals

- Keep Montage on its dedicated agent, runtime, and artifact contracts.
- Do not expose `montage_input.advanced` as a raw Studio editor.
- Do not expose cloud/local execution target selection.
- Do not add a server-driven dynamic form or pipeline catalog API.
- Do not modify plugin distribution assets or the upstream Montage submodule.

## Recommended Approach

Complete the existing stable contract in place. Add a dedicated project-defaults
panel, expand the existing task/plan creation panel, adapt the shared reference
material uploader for Montage assets, and map the already-defined project model
through the HTTP and service layers.

This is preferred over a UI-only patch because the server currently rejects the
platform. It is preferred over a dynamic runtime-driven form because Montage's
stable Anban-facing contract is already defined and intentionally shields Studio
from upstream internal arguments.

## Studio Project Experience

Add `Montage` to the project platform selector and to project creation-intent
resolution. A Montage project shows the common project identity fields plus a
dedicated defaults section with:

- default pipeline
- aspect ratio
- duration in seconds
- style preference
- music prompt
- subtitle mode
- voiceover mode
- asset guidance
- delivery targets

The defaults section is implemented as a focused
`MontageProjectDefaultsPanel`. It does not reuse the task input panel because the
two contracts have different roots and semantics: projects store reusable
defaults, while tasks and plans require a brief and may include source assets.

Project form defaults use the same product defaults as task creation where the
contract has an established value: `9:16` and 30 seconds. Free-text fields start
empty. Editing a Montage project restores all saved defaults. Submitting a
Montage project sends `montage_defaults`; non-Montage projects omit it.

The existing video project choices and their forms remain unchanged.

## Studio Task and Plan Experience

Expand `MontageCreationPanel` to cover the stable authorable Montage input:

- required brief
- optional pipeline key
- aspect ratio and duration
- style and music prompts
- subtitle and voiceover modes
- source assets
- delivery targets

`advanced` remains supported by the API and form normalization code but is not
shown in Studio. It represents adapter escape-hatch data rather than a stable
product control.

When a user selects a Montage project for a new task or plan, the form merges
the project defaults into a fresh Montage input:

- `default_pipeline` becomes `pipeline_key`.
- project preferences initialize matching task preferences.
- project delivery targets initialize task delivery targets.
- the task or plan brief and source assets remain execution-specific.
- explicitly saved task or plan input wins over project defaults when editing.

The merge is implemented in the Montage form helper so TasksPage and PlansPage
share one deterministic rule.

## Source Asset Uploads

Add `montage_asset` to the Studio direct-upload purpose union. The backend
already accepts this purpose, validates ownership, finalizes pending Montage
uploads, and validates source asset URLs.

Create a small `MontageSourceAssetInput` adapter around the existing
`ReferenceMaterialInput`:

- pass `montage_asset` as the direct-upload purpose;
- allow image, video, audio, text, and document files;
- map shared attachment types to Montage asset types such as `image_url` and
  `video_url`;
- map `content_type`/`size` to `mime_type`/`file_size`;
- preserve order, file names, URLs, upload progress, retry, and removal behavior.

To support this without duplicating upload UI, `ReferenceMaterialInput` gains an
optional upload-purpose prop whose current default remains
`ai_entry_attachment`. Existing callers therefore keep their current behavior.

The form must prevent submission while uploads are still running, using the
same uploading-state pattern already used by task and plan attachment flows.

## Server Project Contract

Extend the existing project path rather than adding a Montage-specific endpoint:

1. Add `PlatformMontage` to `ProjectService`'s valid platform set.
2. Add `MontageDefaults *model.MontageDefaults` to `projectRequest`.
3. Map a present request value with `SetMontageDefaults` and set
   `MontageDefaultsSet`.
4. Apply explicitly supplied defaults during project update.
5. Reject Montage defaults on non-Montage projects so unrelated platform data
   cannot silently accumulate.
6. Validate duration as non-negative and at most 600 seconds, matching the
   stable Studio schema. Empty duration means the runtime default applies.

No database migration is needed. `projects.montage_defaults` and the snapshot
field already exist, and project snapshots already preserve Montage defaults.

## Data Flow

```text
Studio project form
  -> POST/PATCH project with montage_defaults
  -> project handler maps MontageDefaults
  -> project service validates and persists
  -> project selector returns saved Montage project
  -> Montage form helper merges project defaults into a new input
  -> user adds brief, per-run preferences, and source assets
  -> task/plan handler finalizes pending montage_asset uploads
  -> task/plan service derives type from project.platform
  -> project snapshot freezes montage_defaults
  -> existing Montage runtime and agent execute the task
```

## Error Handling

- Project API returns a validation error for Montage defaults on a non-Montage
  project or for an invalid duration.
- Upload errors remain local to the relevant asset row and support retry.
- Unsupported file types and oversized files are rejected before upload by the
  shared material input.
- Task and plan submission remains blocked until the required brief is present
  and all source asset uploads have settled.
- Runtime availability and execution-target failures continue to use the
  existing Montage service errors; this change does not mask them.

## Testing

### Studio

- Montage form helper tests for project-default inheritance and explicit input
  precedence.
- Project defaults panel tests for editing every stable default field.
- Montage creation panel tests for the expanded fields and source asset mapping.
- ProjectsPage tests proving Montage is visible, restored on edit, and submitted
  with `montage_defaults`.
- TasksPage and PlansPage tests proving project defaults and source assets reach
  the submitted `montage_input`.
- Direct-upload tests proving Montage uses the `montage_asset` purpose without
  changing existing attachment uploads.

### Go

- Project service tests proving Montage is accepted and its defaults survive
  create/update.
- Project handler tests proving request JSON maps all defaults and rejects
  invalid cross-platform defaults.
- Existing task, plan, snapshot, upload-finalization, MCP profile, and artifact
  tests remain part of regression verification.

### Verification Commands

```bash
go test ./server/service ./server/handler ./server/model ./server/mcp
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test
cd studio && bun run build
```

## Completion Criteria

- A user can create and edit a Montage project in Studio.
- Every stable project default is persisted and returned by the API.
- A new Montage task or plan starts with the selected project's defaults.
- Users can upload and remove supported Montage source assets before submit.
- Submitted tasks and plans contain the complete stable `montage_input`.
- Existing video project, task, and plan behavior is unchanged.
- Focused and full Go and Studio verification pass.
