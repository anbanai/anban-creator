# User Media Library Design

## Intent

Give each account a reusable library of uploaded images and videos that the
Server analyzes independently. The resulting objective description helps the
AI understand what a file contains and when it may be useful during creation.

## Goals

- Store account-owned media metadata separately from immutable `assets` and
  separately from reusable topic `ContentAsset` records.
- Allow an optional owned project tag while keeping every active media item
  reusable across the account's projects.
- Show upload and analysis state, objective analysis, user-written usage notes,
  project tag, and safe preview in Studio.
- Allow upload, search, filtering, usage-note editing, re-analysis, and soft
  removal. Removed media stays in storage and historical task references remain
  valid.
- Expose a read-only `search_user_media_assets` MCP capability to an authenticated
  interactive session or current managed execution. Search results are limited
  to that identity's ready, non-removed media. An owned project tag affects
  ordering only; it does not hide cross-project media.
- Let Studio creation questions present multiple candidate media cards. A user
  selection carries a stable media ID into the draft and becomes an ordinary
  task input attachment only after confirmation.
- Record provider, model, and returned token usage for each analysis request.
  Media analysis does not debit user credits.

## Non-goals

- Permanent object deletion, deduplication, folders, public sharing, or
  collaborative libraries.
- Replacing `ContentAsset`, task attachments, or the existing direct-upload
  transport.
- Charging user credits for analysis.
- Frame-only substitutes for models configured for native video input.

## Product behavior

The `/media-assets` Studio page is the management surface. Upload uses the
existing OSS direct-upload flow with a dedicated `media_library` purpose that
accepts image and video only and applies current AI attachment size limits.
The Server finalizes the upload, creates account-level metadata, and enqueues
analysis. The UI shows queued, analyzing, available, or failed state and an
actionable failure reason. Re-analysis refreshes only AI-generated fields;
`usage_notes` is user-owned and never overwritten. Removal sets a timestamp and
does not delete the immutable file or task references.

When an interactive creation agent calls `search_user_media_assets`, the Server
uses the authenticated session's user and project scope (or the current
execution JWT's user and project scope). The query accepts search text, media
type, and a bounded result count; it accepts no caller-supplied user ID or
project ID. Results include stable `asset_id`, filename, media type, objective
description, recommended uses, optional usage notes, and a short-lived preview
URL. Project-matched items sort first, followed by recent items. Empty results
tell the agent to ask the user to upload media or choose a different request.

For an interactive question, candidate option labels use the stable form
`media_asset:<asset_id>`. Studio recognizes those options, resolves safe media
previews through the authenticated API, and renders selectable cards. Answer
values remain the stable ID label so the agent can continue without guessing
from filenames. Skipping or selecting no candidates leaves the current
creation attachments unchanged. On confirmation, Studio adds selected ready
assets to the creation composer without another upload. The Server rechecks
ownership, media-library membership, ready state, and non-removal when the
creation draft is committed.

## Server design

- Keep `model.Asset` immutable and introduce a separate account-scoped media
  record referencing its asset ID. Store optional project ID, `status`, AI
  `description`, AI `recommended_uses`, user `usage_notes`, `removed_at`, and
  timestamps on this record.
- Reuse `ImageAnalysisJob` for durable queueing, generation invalidation,
  recovery, and retry. Add a user-media subject and description kind. The media
  record is the user-facing status source; worker transitions update the job
  and record atomically.
- Analyze images using the configured image-understanding route. Analyze videos
  with the configured native-video route through a short-lived signed storage
  URL. A missing or unsupported model, storage failure, size/duration violation,
  or invalid output is a visible failed state, not a successful empty result.
- Route media analysis through the existing provider usage recorder. This
  stores token usage and provider/model identity in provider-cost evidence and
  never invokes wallet settlement.
- Add authenticated media-library service and handler operations. Validate
  project ownership, upload purpose, MIME/type, and file size on the Server.
  Search and list queries always include the authenticated owner and exclude
  removed rows unless history is explicitly being resolved.
- MCP remains an atomic transport. It validates the current execution/session
  identity, invokes one search capability, and returns its result without
  mutating media or composing a workflow.
- Creation commit resolution permits media-library asset IDs only when their
  metadata belongs to the caller, is ready, and is not removed. Stored task
  input attachments are independent historical references and remain readable
  after a library removal.

## Studio design

- Add a distinct `素材库` navigation item and a dense management page with
  upload, filename/description search, image/video and project filters, status,
  preview, user note editing, re-analysis, and removal.
- Preserve `选题资产` terminology and routes for content-reuse records.
- Change the creation composer `+` control into actions for local upload and
  selecting from the account media library. Library selections are represented
  by existing `asset_id` attachments and are not uploaded again.
- Render media-valued AskUserQuestion options as cards with image/video preview,
  filename, AI description, and the agent's short selection rationale. Support
  the question's existing single- and multi-select behavior and only add
  selected items after the user submits the answer.
- Keep empty, loading, failed, queued, and unavailable states explicit. A
  candidate that expires or becomes unavailable cannot be committed; the UI
  surfaces the Server's actionable error and the user can choose again.

## Validation

Go tests cover ownership isolation, upload policy, project ownership, analysis
state transitions, retry generation, user-note preservation, token usage
recording without wallet debit, soft removal/history, candidate filtering and
ranking, stable card selections, and creation-time availability checks. Studio
tests cover routing/navigation, media library management states, upload/retry,
chat picker behavior, candidate card multi-select/skip, and stable IDs carried
into draft commit.
