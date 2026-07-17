# Direct Upload Immutable Promotion Design

## Goal

Eliminate the direct-upload time-of-check/time-of-use window by promoting each verified browser-writable pending object into a deterministic, server-only immutable key before any database claim or runtime persistence.

## Storage Contract

Storage providers expose `ConditionalObjectPromoter.PromoteObject(ctx, sourceKey, finalKey, expectedETag)`. Promotion must atomically require the verified source ETag and must not overwrite an existing final object. OSS uses `CopyObject` with `CopySourceIfMatch` and `ForbidOverWrite(true)`. Local storage serializes provider writes and promotion, computes a content ETag, and creates the final file exclusively.

Stable errors distinguish source-precondition failure from an existing destination. The service may reuse an existing destination only after validating its metadata and only at the deterministic final key derived from the same owner and upload ID.

## Identity And Persistence

`PendingUpload.Key` remains the browser-writable source identity. `PendingUpload.FinalizedKey` stores the immutable runtime identity. Claims carry both values. A finalized row is reusable only when its owner, upload ID, purpose, source key, and finalized key all match.

Client assertions may use the source key for submit retries or the finalized key for clone/edit workflows. Every successful verification returns only `FinalizedKey` as its runtime `Key`. Existing finalized rows without `FinalizedKey` are rejected from new verified runtime paths.

## State Machine

For a pending row, derive `uploads/finalized/{userID}/{uploadID}/{filename}`. If that object already exists, validate its size and MIME and reuse it as an orphan from an earlier successful promotion. Otherwise HEAD the source, validate size/MIME/non-empty ETag, then conditionally promote. A source replacement after HEAD fails the promotion precondition and cannot claim the row.

All storage promotion occurs before the database transaction. A batch is claimed in one transaction only after every final object is ready. A failed claim leaves no partially finalized database rows; deterministic orphan final objects are safe to reuse on retry. Concurrent finalizers converge on the same final key and identical claim identity.

Pending cleanup deletes both the source and deterministic orphan final key. Any delete failure reopens the cleanup lease so the operation remains retryable. Finalized rows are not cleanup candidates.

## Runtime Data

AI-entry attachments, legacy project/task/plan/template fields, nested video references, and montage assets are rewritten to final keys or owned final URLs before persistence. Clone and retry paths copy these final identities unchanged. Bootstrap authorization, signed downloads, config building, bounded reads, and media materialization use only `FinalizedKey`; no runtime fallback reads the pending source.

STS policy remains scoped to the exact pending source ARN for regular PUT and multipart actions. The finalized namespace is never present in the client policy.

## Verification

Tests cover local and OSS conditional promotion, structured 412/409 classification, source replacement after HEAD, immutable final bytes, exact STS resources, persisted final identities, orphan retry, concurrent reuse, atomic batch claims, cleanup retry, and final-key-only signing/materialization. Full Go tests, targeted race tests, vet, both builds, and diff checks gate completion.
