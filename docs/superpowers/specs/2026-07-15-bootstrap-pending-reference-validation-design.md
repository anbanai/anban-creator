# Bootstrap Pending Reference Validation Design

## Goal

Prevent malformed or unverifiable pending-upload reference URLs from being persisted into tasks, plans, or projects and failing only when a Kubernetes Agent requests bootstrap files.

## Problem

Direct uploads are created under `uploads/pending/<user_id>/<upload_id>/<filename>`. Creation and update handlers call `FinalizePendingUploadURLs`, but that function currently skips a URL when `pendingUploadIDFromURL` cannot extract an upload ID. The surrounding request still succeeds and stores the URL.

The Kubernetes bootstrap path later resolves the same URL to a pending object key. Because it cannot recover the pending-upload identity, `authorizeBootstrapObject` returns `bootstrap pending upload identity mismatch`, and the Agent never starts.

This is a validation-order defect. A malformed pending reference is invalid at the write boundary and must not become an executable task input.

## Design

### Canonical Pending Identity

Pending-upload identity remains the tuple already recorded by `PendingUpload`:

- upload ID
- user ID
- purpose
- object key
- public URL
- finalized status

No ownership decision may be based only on a path prefix. The repository record and exact object key remain authoritative.

### Write-Boundary Validation

`FinalizePendingUploadURLs` will distinguish three input classes:

1. Empty or ordinary non-pending URLs: preserve current behavior.
2. Valid pending-upload URLs: resolve the record, verify user, purpose, expiry, status, and URL/object-key identity, then finalize it.
3. URLs that resolve to the pending namespace but do not contain a valid upload identity: return `ErrPendingUploadInvalidURL` instead of silently skipping them.

Handlers will continue mapping this failure to HTTP 400. No task, plan, project, or template mutation should occur after the validation error.

External URLs that merely contain similar text must not be treated as server-owned pending uploads. Pending classification must use the canonical storage URL/key parser and the configured storage ownership boundary where that dependency is available, rather than substring matching an arbitrary host.

### Bootstrap Defense

Bootstrap authorization remains fail-closed. It will not guess, repair, or accept a malformed URL. Its error should include a stable classification suitable for logs, while the HTTP response remains sanitized.

Existing persisted malformed records are not silently migrated. Operators must remove or re-upload the affected reference image before retrying the task. This avoids authorizing an object whose original upload identity cannot be proven.

## Error Handling

- Write boundary: HTTP 400 with a message that the pending upload URL is invalid and the reference must be uploaded again.
- Bootstrap boundary: conflict classification with task/execution identifiers in structured logs, without logging signed query parameters or secrets.
- Repository lookup failure: dependency-unavailable classification, not an ownership mismatch.
- Ownership, purpose, finalized-status, or key mismatch: access denied/conflict; never sign the object.

## Testing

Add table-driven tests covering:

- valid finalized task and project references;
- malformed server-owned pending URL with no extractable upload ID;
- cross-user pending URL;
- wrong-purpose pending URL;
- key or public-URL mismatch;
- ordinary external URL that contains `uploads/pending` text;
- handler behavior proving no task is created when validation fails;
- bootstrap remains fail-closed for legacy malformed data.

Run targeted service and handler tests, then `go test ./...` and both required Go builds.

## Non-Goals

- Moving finalized objects out of the pending prefix.
- Automatically repairing existing database rows.
- Relaxing bootstrap object ownership checks.
- Supporting arbitrary external reference downloads in the Kubernetes bootstrap path.
