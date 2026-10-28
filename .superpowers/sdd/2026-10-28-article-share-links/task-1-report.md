# Task 1: Server Sharing

## Result

Implemented Server-owned sharing for tasks whose AgentID/Channel are both
`wechat-article` or both `seednote`, with `TaskKind=content_generation`.
No Harness, Agent, MCP, publishing, or Studio changes are included.

- Added private `TaskShare` persistence, task primary key and unique token index,
  regular/transaction repository wiring, AutoMigrate registration, and additive
  migration `server/migrations/20261028_task_shares.sql`.
- Tokens use 32 cryptographically random bytes encoded as 43 URL-safe characters.
  Existing active links retain their token; expired/revoked links rotate it.
  Status reads, updates, and revocation serialize through the existing task row
  lock inside repository transactions. Expiry uses service time after content
  validation, immediately before saving, with exact 24-hour days.
- PUT requires a complete readable current delivered document and its public
  images. Pending/no-file tasks cannot acquire a share. GET status remains
  available as `none`, even before delivery. Revocation is idempotent.
- Public reads use only the current delivered file set and its owning execution's
  frozen delivery contract. They require the correct main role/MIME and a
  delivered manifest, reject mixed executions, and omit retained, pending,
  undeclared and internal files. The task's current running/failed execution
  does not hide a previous delivered version. Deleting/deleted tasks return 404.
- Public image roles are cover/image/tail, sorted by role and numeric filename.
  Body image references map relative paths and known TaskFile platform, WeChat,
  preview/download and storage URLs to this share's image endpoint. Unknown body
  image sources make content unavailable, without outbound fetching.
- HTML is parsed with x/net/html, strips active elements/fetch attributes and
  unsafe inline CSS, and rewrites src/data-src. Goldmark locates Markdown image
  destination source slices, preserving prose/code and reference syntax; raw
  HTML nodes are removed. Studio still owns its sandbox/CSP rendering defense.
- Text reads are explicitly bounded. Images validate object ownership, size,
  metadata, and sniffed MIME, then stream through storage abstractions without
  redirecting to storage. Every image request reloads share/task/current manifest.
- All handler responses use no-store/nosniff; public responses additionally use
  noindex/nofollow and no-referrer. Management authentication failures also get
  no-store/nosniff. Request logs redact token path segments, including mixed-case
  routes. Share SQL queries suppress SQL tracing so bound tokens cannot leak.
  Raw persistence models serialize as `{}`.

## HTTP Contract

The design contains five endpoints; the brief's original mention of six was a
typo confirmed by the controller.

1. `GET /api/v1/tasks/:id/share`, authenticated owner:
   existing envelope `{"code":0,"msg":"success","data":{"status":"none"}}`,
   or `data={status:"active"|"expired"|"revoked",share_path:"/share/<token>",expires_at:"<RFC3339>"}`.
2. `PUT /api/v1/tasks/:id/share`, authenticated owner, body
   `{"expires_in_days":7}`: same envelope with active status. Accepts numeric
   integer values from 1 through 365 (including JSON `1.0`); missing, null,
   fractional, string and out-of-range values return 400.
3. `DELETE /api/v1/tasks/:id/share`, authenticated owner: 204, no body.
4. `GET /api/v1/public/shares/:token`, anonymous:
   `data={type:"wechat-article"|"seednote",title:string,format:"html"|"markdown",body:string,images:[{id:string,url:string,alt:string,role:"cover"|"image"|"tail"}],expires_at:"<RFC3339>"}`.
   `images` is always an array. Image URLs are relative API paths.
5. `GET /api/v1/public/shares/:token/images/:fileId`, anonymous:
   matching delivered image bytes, explicit raster MIME and content length.

Management ownership/unsupported/deleting/missing tasks return 404; missing login
returns 401. Public unknown/rotated tokens or missing/deleting tasks return 404;
expired/revoked tokens return 410; unavailable delivery returns 409 with
`error_code="task_share_unavailable"`; infrastructure errors return 500. Errors
use the existing envelope and fixed public messages, without internal details.

## TDD And Verification

RED evidence:

- Initial `go test -p 1 ./service -run TestTaskShare -count=1` failed because
  TaskShareService/model/DTO symbols did not exist.
- Initial logger test failed because request paths contained `secret-token`.
- Added mixed-case logger case failed before its case-insensitive redaction fix.
- `TestTaskShareRepositoryDoesNotLogOrSerializeToken` failed with bound tokens in
  GORM SQL logs before scoped SQL logging suppression.
- `TestTaskSharePutRequiresReadableDeliveredContent` failed with nil error before
  adding the creation content gate.

GREEN evidence:

- Final `go test -p 2 ./service ./repository ./handler ./middleware ./router
  -run 'TestTaskShare|TestRequestLoggerRedacts' -count=1`: all five packages pass,
  14 top-level feature tests plus their table/subtest cases.
- Covers real SQLite ownership/transactions, concurrent token stability, exact
  expiry edge/range, rotation/revocation/deletion, supported identity, current
  manifest switching and failed continuation, private file refusal, relative and
  platform images, numeric image order, Markdown references/prose/code, HTML
  active-content removal, missing objects, safe DTOs, and infrastructure errors.
- Actual HTTP integration covers owner PUT, anonymous HTML, exact streamed PNG
  bytes/headers, DELETE 204 and subsequent anonymous image 410. Router tests prove
  public routes bypass authentication and management routes require it.
- Real LocalProvider is exercised. An OSS-named storage fake implements the same
  stat/bounded-read/stream interfaces, panics on unbounded Read, and verifies one
  bounded body read plus streamed images. Existing storage OSS adapter tests also
  passed in the full suite; no live OSS credentials or remote bucket were used.
- `go test -p 2 ./...`: all packages other than service passed; service had the
  exact same 12 pre-existing WeChat publication/reconciliation failures as the
  clean HEAD baseline, with no additional top-level failures. Log:
  `/tmp/anban-share-go-final.log`. After the final creation/logging refinements,
  all affected focused packages passed again; controller owns the final
  integrated full-suite rerun after frontend completion.
- `go build -o /tmp/anban-share-server .`: passed after final production edits.
  Log: `/tmp/anban-share-build-final.log`.
- One early handler attempt failed because build-cache files disappeared during
  execution; a subsequent normal run passed. Initial baseline attempts also hit
  disk exhaustion. Neither environment failure required unrelated code changes.

## Self-Review

Checked scope, ownership, token rotation, metadata-only status API, SQL/request
token logging, current-execution filtering, object namespace ownership, bounded
reads, MIME checks, and error classification. Removed incidental gofmt changes
to unrelated repository methods. No cleanup cron or outbound image fetcher was
added. Apply the additive database migration before Server deployment; Studio
and Nginx integration remain the controller's subsequent tasks.
