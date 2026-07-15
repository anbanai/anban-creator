# Bootstrap Pending Reference Validation Design

## Goal

Accept legitimate OSS URLs whose object-key slashes are percent-encoded, while preventing malformed or unverifiable pending-upload references from being persisted and failing only when a Kubernetes Agent requests bootstrap files.

## Problem

Direct uploads are created under `uploads/pending/<user_id>/<upload_id>/<filename>`. OSS may return a signed or public URL whose path is `uploads%2Fpending%2F<user_id>%2F<upload_id>%2F<filename>`. `storage.StorageKeyFromURL` correctly decodes that URL to the canonical object key, but `pendingUploadIDFromURL` searches the raw URL for the literal `/uploads/pending/` substring and returns no upload ID.

Creation and update handlers call `FinalizePendingUploadURLs`, which currently skips a URL when `pendingUploadIDFromURL` cannot extract an upload ID. The surrounding request succeeds and stores the URL. Kubernetes bootstrap later resolves the same URL to a pending canonical object key, cannot recover its pending-upload identity through the raw-string parser, returns `bootstrap pending upload identity mismatch`, and never starts the Agent.

The root cause is inconsistent URL canonicalization plus a validation-order defect. All pending-upload identity and equality checks must use the same decoded storage key representation.

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

`pendingUploadIDFromURL` will first resolve the URL through the canonical storage key parser, then require the exact key shape `uploads/pending/<user_id>/<upload_id>/<filename>`. It will not search or split the raw URL string.

`pendingUploadURLMatches` will compare decoded URL paths and exact hosts against the recorded `PublicURL` and canonical object key. Query parameters and fragments remain irrelevant to identity. Percent-encoding may change representation but cannot change the decoded key.

### Write-Boundary Validation

`FinalizePendingUploadURLs` will distinguish three input classes:

1. Empty or ordinary non-pending URLs: preserve current behavior.
2. Valid pending-upload URLs: resolve the record, verify user, purpose, expiry, status, and URL/object-key identity, then finalize it.
3. URLs whose canonical key resolves to the pending namespace but does not contain a valid upload identity: return `ErrPendingUploadInvalidURL` instead of silently skipping them.

Handlers will continue mapping this failure to HTTP 400. No task, plan, project, or template mutation should occur after the validation error.

External URLs that merely contain similar text must not be treated as server-owned pending uploads. Pending classification must use the canonical storage URL/key parser and the configured storage ownership boundary where that dependency is available, rather than substring matching an arbitrary host.

### Bootstrap Defense

Bootstrap authorization remains fail-closed. It will use the same canonical pending identity parser as the write boundary, then verify the repository record, task owner, purpose, finalized status, exact key, and decoded URL identity before signing. Its error should include a stable classification suitable for logs, while the HTTP response remains sanitized.

Existing persisted percent-encoded OSS URLs require no migration and become executable after deployment because their decoded key and upload record already prove ownership. Truly malformed records remain rejected and must be removed or uploaded again.

## Error Handling

- Write boundary: HTTP 400 with a message that the pending upload URL is invalid and the reference must be uploaded again.
- Bootstrap boundary: conflict classification with task/execution identifiers in structured logs, without logging signed query parameters or secrets.
- Repository lookup failure: dependency-unavailable classification, not an ownership mismatch.
- Ownership, purpose, finalized-status, or key mismatch: access denied/conflict; never sign the object.

## Testing

Add table-driven tests covering:

- valid finalized task and project references;
- valid pending references with `%2F`-encoded object-key separators and signed query parameters;
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
- Rewriting existing database rows whose encoded URL already resolves to the correct canonical key.
- Relaxing bootstrap object ownership checks.
- Supporting arbitrary external reference downloads in the Kubernetes bootstrap path.
