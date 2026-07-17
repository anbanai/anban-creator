# Finalized Reference Assets Design

## Problem

Reference images are currently persisted as storage URLs in projects, plans,
tasks, and task project snapshots. A URL can still point at the temporary
`uploads/pending/` namespace even after the upload row has been claimed. New
tasks then freeze that temporary URL and fail later during agent bootstrap when
the stricter ownership boundary refuses to sign it.

The failure is detected too late. Task creation and billing can succeed before
bootstrap discovers that the stored reference cannot be resolved safely.

This design is forward-only. It does not preserve URL-based reference-image
request fields or attempt to interpret historical pending URLs.

## Goals

- Separate temporary upload capability from permanent asset identity.
- Keep browser-to-OSS uploads direct; application servers never proxy file
  bytes.
- Ensure no pending key, storage key, public URL, or signed URL is persisted as
  a reference-image business fact.
- Finalize uploads before a project, plan, or task commits a reference.
- Store only immutable asset IDs in projects, plans, tasks, and snapshots.
- Resolve and sign assets at read/bootstrap time through repository-backed
  ownership, not URL or path-prefix inference.
- Fail synchronously at the write boundary before task creation, dispatch, or
  billing.

## Non-Goals

- Migrating historical `reference_image_url` values.
- Supporting arbitrary external reference-image URLs.
- Converting every existing media field, video reference, ecommerce photo, or
  input attachment to the new asset contract in this change.
- Revoking an already-issued OSS signed URL or STS session. Isolation makes
  revocation unnecessary.
- Deleting finalized assets or adding reference-counted garbage collection.

## Chosen Architecture

Use two explicit records:

1. `UploadSession` is temporary. It authorizes one browser upload to one exact
   staging key and expires after a short window.
2. `Asset` is permanent and immutable. It owns the finalized OSS key and is the
   only identity that business records may persist.

The current combined pending/finalized row is split because upload expiry and
asset lifetime are different concepts. Treating a finalized pending-upload row
as a permanent asset leaves the same semantic ambiguity that caused URL state
to leak into business records.

### UploadSession

`upload_sessions` contains:

- `id`
- `user_id`
- `purpose`
- `staging_key`
- `file_name`
- `content_type`
- `size`
- `status`: `pending`, `finalizing`, `finalized`, `expiring`, or `expired`
- `expires_at`
- `finalization_token` and `finalization_claimed_at`
- `asset_id` after successful finalization
- cleanup claim fields and timestamps

The default reference-image upload window remains 15 minutes. The credential
policy grants write access only to the session's exact staging key. Other
purposes may choose a longer policy later, but this change does not globally
extend the credential lifetime.

### Asset

`assets` contains:

- `id`
- `user_id`
- `purpose`
- `storage_key`
- `file_name`
- `content_type`
- `size`
- `etag`
- `created_at`

The final storage key is deterministic:

```text
assets/users/{user_id}/{asset_id}/{sanitized_filename}
```

The row and object are immutable after creation. No update API changes the key,
owner, purpose, or metadata.

## API Contract

### Prepare

`POST /api/v1/uploads/prepare` continues to accept purpose and file metadata.
It returns an `upload_session_id`, staging upload credentials, staging preview
URL, expiration, and size limit. Transport metadata required by existing
non-reference consumers may remain in the response, but the reference-image
control exposes only the session ID as submitted form state. Neither its staging
key nor preview URL is a reference-image business value.

### Reference Selection

Project, plan, and task create/update requests use a tagged reference:

```json
{
  "reference_image": {
    "upload_session_id": "uuid"
  }
}
```

An existing finalized selection uses:

```json
{
  "reference_image": {
    "asset_id": "uuid"
  }
}
```

The two fields are mutually exclusive. `reference_image: null` clears the
selection. Omission on patch-style update means unchanged.

The old `reference_image_url` request field is rejected with HTTP 400 instead
of being ignored.

### Read Model

Responses expose a derived reference image:

```json
{
  "reference_image": {
    "asset_id": "uuid",
    "file_name": "reference.jpg",
    "content_type": "image/jpeg",
    "size": 12345,
    "download_url": "short-lived signed URL",
    "download_expires_at": "RFC3339 timestamp"
  }
}
```

The Studio may use `download_url` for preview only. It must never submit that
URL back or store it in form state as the reference identity.

## Finalization Flow

When a form submits an `upload_session_id`, the backend performs:

1. Validate the session ID, user, purpose, state, and expiry.
2. Acquire a bounded `pending -> finalizing` claim with a token. Concurrent
   requests either observe the same finalized asset or receive a state conflict.
3. Fetch staging object metadata without reading its body.
4. Validate exact size and normalized content type against prepared metadata.
5. Use OSS `CopyObject` with source ETag precondition and destination
   `ForbidOverWrite` to create the deterministic final object. File bytes stay
   inside OSS.
6. Fetch and validate final object metadata.
7. In one database transaction, insert the immutable `Asset`, attach its ID to
   the upload session, and mark the session finalized.
8. Return the asset ID to the business write transaction.
9. Delete the staging object best-effort. A lifecycle rule and the existing
   cleanup loop remain the fallback for abandoned or recreated staging objects.

If OSS succeeds but the database transaction fails, retry uses the deterministic
destination and validates the existing object before completing the database
state. Finalization is therefore idempotent without overwriting an asset.

An unexpired browser credential may still write the staging key after submit,
but it cannot write the asset namespace. No business record reads staging after
finalization, and cleanup removes the unreferenced staging object.

## Business Write Boundaries

Project, plan, and task handlers share a `ReferenceAssetService` rather than
duplicating upload-state checks.

The service accepts either an upload session ID or an existing asset ID and an
allowed purpose set. It returns a verified finalized asset ID only when:

- the owner matches the authenticated user;
- the asset purpose is valid for the field;
- the session was finalized successfully or the asset already exists; and
- the asset metadata describes an allowed reference image.

Business records change as follows:

- `Project.ReferenceImageAssetID`
- `Plan.ReferenceImageAssetID`
- `Task.ReferenceImageAssetID`
- `ProjectSnapshot.ReferenceImageAssetID`

The persisted and JSON snapshot URL fields are removed. Task creation copies
the project asset ID into the snapshot. Plan-level and task-level asset IDs keep
their current precedence rules. Validation happens before task creation and
before any credit reservation.

## Runtime Resolution

Agent bootstrap selects the effective asset ID using the existing precedence:

1. task reference asset, when present;
2. project snapshot reference asset when task-level reference is absent and
   `skip_reference_image` is false.

It loads the `Asset` by ID and requires an exact user and allowed-purpose match.
Only then does it sign `Asset.StorageKey` and materialize
`.anban-creator/reference.png`.

Docker and local executors use the same asset resolver and bounded storage read.
They do not accept an external URL fallback. App settings continue to point at
the materialized local reference path and do not receive an asset URL.

Download/preview resolution also accepts only `asset_id`. Caller-supplied keys
are removed from this reference-image path.

## Studio Changes

The reference upload component stores a discriminated reference selection:

- a pending `upload_session_id` immediately after browser upload;
- the server-returned `asset_id` after project/plan/task save;
- an ephemeral signed preview URL kept outside submitted form data.

Save buttons remain disabled while browser upload is in progress. A completed
browser upload can be submitted even though it is not finalized yet; submit is
the claim boundary. If finalization fails, the form stays open and shows the
server's actionable error.

All project, plan, task, and detail types stop treating `reference_image_url` as
editable business data. AI-entry attachments keep their existing attachment
contract; when an AI-entry flow needs to populate a reference-image field, the
server derives the asset identity from the finalized attachment rather than
persisting its URL.

## Error Contract

- `400`: malformed selection, both IDs supplied, unsupported legacy URL field,
  wrong content metadata, or purpose mismatch.
- `403`: asset/session belongs to another user. The response does not reveal
  whether the foreign identity exists.
- `409`: session is concurrently finalizing or already claimed incompatibly.
- `410`: upload session expired before finalization.
- `503`: OSS metadata, copy, or signing dependency is unavailable.

No project, plan, or task row is written when reference finalization fails.
Task dispatch and billing are not reached.

## Concurrency And Failure Recovery

- Finalization claims use compare-and-swap state plus a stale-claim lease.
- The final key is deterministic and cannot be overwritten.
- Source ETag validation prevents copy-after-change races.
- Existing final objects are reusable only when all expected metadata matches.
- Business writes may retry with the same session and receive the same asset ID.
- Staging deletion is not part of correctness; it is cleanup after durable
  finalization.
- Final assets are never deleted by pending-session cleanup.

## Security Properties

- Browser credentials cannot write final assets.
- Business APIs do not accept storage URLs or object keys as ownership proof.
- Ownership comes from repository rows keyed by opaque IDs.
- Runtime signing is fail-closed on missing, foreign, wrong-purpose, or
  non-finalized assets.
- Signed download URLs are presentation data with bounded lifetime.
- Path-prefix checks remain defense in depth inside storage, not the primary
  authorization mechanism.

## Testing

### Go

- Upload-session prepare policy permits only one staging key.
- Finalization validates size, type, ETag, owner, purpose, and expiry.
- OSS copy never invokes a bounded read or local file path.
- Concurrent finalization returns one immutable asset ID.
- Retry after copy-before-database failure reuses the matching object.
- Project, plan, and task handlers persist asset IDs and reject URL fields.
- Task creation rejects an invalid project/plan/task asset before charging.
- Project snapshots contain asset IDs only.
- Bootstrap, Docker, and local executors resolve the same asset identity.
- Cross-user, wrong-purpose, pending, expired, and missing assets fail closed.
- Preview resolution signs repository-owned asset keys without caller keys.

### Studio

- Upload controls submit session IDs, never preview URLs.
- Existing assets submit asset IDs.
- Clear, upload-in-progress, finalization-error, and successful-save states are
  covered.
- Project, plan, and task payload tests reject legacy URL behavior; AI-entry
  tests prove any promoted reference uses the finalized attachment asset ID.

### Verification

- Targeted server service, handler, repository, storage, and agent tests.
- `go test ./... -count=1` after plugin submodules are initialized.
- Server and agent builds to `/tmp`.
- Full Studio tests and production build with Bun.
- `git diff --check`.

## Rollout Contract

This is a coordinated forward cutover of server and Studio. The server does not
accept the former reference URL field, and the Studio does not send it. Deploy
the server before or together with the matching Studio build so no current UI
can create URL-backed reference records.

The upload-session and asset repositories replace the current combined upload
row for every direct-upload purpose, so prepare, cleanup, and finalization keep
one shared state machine. Only reference-image business fields switch to
asset-ID persistence in this change; other media fields retain their current
shape and resolve through the same finalized asset internally.

The database schema must be applied before traffic reaches the new handlers.
Because historical compatibility is explicitly out of scope, existing
URL-backed reference values are not read or migrated by application code.
