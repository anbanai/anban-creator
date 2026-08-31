# Task 2 Report: Publication Lifecycle Service And HTTP APIs

## Status

Completed and verified. The service owns durable WeChat draft intent,
response-loss recovery, API publication submission and polling, project-batched
manual reconciliation, conservative matching and selection, and automatic
analytics tracking creation. The HTTP surface exposes only lifecycle actions;
legacy approval, manual-published, and operator publication-resolution paths
were removed as part of the clean cutover.

## Files Changed

- `server/service/wechat_publication.go`: lifecycle service, normalized HTML
  fingerprinting, draft recovery, publish CAS, polling schedules, manual
  reconciliation, selection verification, `48001`, and tracking creation.
- `server/service/wechat_publication_test.go`: focused lifecycle, ownership,
  concurrency, schedule, matching, response-loss, selection, and tracking tests.
- `server/repository/wechat_publication.go`: publication persistence, publish
  claims, claimed updates, project batching, and atomic project reconcile claim.
- `server/repository/repository.go`: publication repository in normal and
  transactional repository aggregates.
- `server/handler/wechat_publication.go` and its test: thin authenticated
  lifecycle actions with explicit error/status mapping and `article_id`-only
  selection.
- `server/router/router.go`, `server/router/router_test.go`, and `server/main.go`:
  exact lifecycle routes and production dependency wiring.
- Task/project handler, service, repository, and tests: clean cutover from the
  removed project booleans, task publication flags, approval gate, and operator
  resolution APIs to `wechat_publish_mode` and draft-delivery terminology.
- `server/service/task_publish_approval.go` and its test were deleted because
  approval staging is not part of the new first-class lifecycle.

## Lifecycle Contract

- Draft intent is persisted before `draft/add`, keyed one-per-task, with a
  normalized structural HTML fingerprint.
- Every unresolved draft creation checks `draft/batchget` before any external
  retry. Ambiguous `draft/add` outcomes are never submitted blindly again.
- API publication uses a lease-backed database CAS, so concurrent confirmation
  requests have one `freepublish/submit` winner. Official `publish_id` and
  `msg_data_id` remain strings.
- API polling uses 5s, 15s, 30s, 1m, 2m, and 5m checks, then 10m checks within
  the two-hour window. Provider failures and empty responses persist their next
  attempt before returning.
- Manual reconciliation makes one
  `freepublish/batchget(no_content=false)` request per project, with 10m checks
  for two hours, hourly checks through 24 hours, and six-hour checks through 72
  hours.
- The 60-second project limit is acquired by one atomic database update before
  the provider request. Concurrent HTTP or scheduler calls cannot both reach
  WeChat for the same project.
- Automatic binding requires one unique article across exact normalized-body
  and exact title+digest+thumbnail signals. Weak or conflicting matches become
  candidates; articles without a provider URL are not bindable.
- Selection accepts only `article_id`, re-fetches the configured account, checks
  publication time and existing ownership, and never accepts a user-entered URL.
- Successful API or console binding creates one waiting analytics tracking row
  in the same transaction. API tracking derives composite `msgid` as
  `msg_data_id + "_1"`.
- Official status codes 2 through 6 become terminal `publish_failed`; WeChat
  error `48001` becomes `unsupported` with no URL fallback.

## RED Evidence

Initial Task 2 tests failed to compile before the lifecycle service/repository
symbols existed. Subsequent focused RED runs caught four behavioral defects:

```text
TestManualReconcileRequiresOneUniqueArticleAcrossAllExactSignals
conflicting exact signals auto-bound

TestManualReconcileDoesNotBindArticleWithoutProviderURL
article without URL was bound

TestWechatContentFingerprintPreservesMeaningfulInlineSpaces
meaningful inline whitespace was discarded

TestManualReconcileRateLimitIsAtomicAcrossConcurrentRequests
second Reconcile err=<nil>, want rate limited
```

The final audit added two more response-loss cases before their fixes:

```text
TestPublishResponseLossSchedulesReconciliationWithoutResubmitting
ambiguous publish ... NextCheckAt:<nil>

TestPollPublishMapsStatusesAndDurableSchedule/empty_response_remains_durably_scheduled
empty response did not persist its retry state
```

## GREEN Evidence

Fresh focused verification on the final production tree:

```text
go test ./server/service ./server/handler ./server/router ./server/repository ./server/model \
  -run 'Test(WechatPublication|CreateDraft|PublishUses|PublishResponseLoss|PollPublish|PollSuccess|PublishUnsupported|ManualReconcile|SelectRefetches|PublicationSchedules)' \
  -count=1

ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/handler
ok github.com/anbanai/anban-creator/server/router
ok github.com/anbanai/anban-creator/server/repository
ok github.com/anbanai/anban-creator/server/model
```

Fresh repository-wide verification:

```text
go test ./...
# all packages passed

go build -o /tmp/anban-creator-server-task2 ./server
# exit 0

git diff --check
# exit 0
```

## Downstream Interfaces

- Task 4 calls `WechatPublicationService.CreateDraft` from the atomic
  `create_draft` MCP capability.
- Task 6 schedules `Poll` from persisted `NextCheckAt` for API publication and
  `ReconcileProject` for project-batched console detection.
- Studio consumes the four exact routes:
  `GET /tasks/:id/wechat-publication` and POST actions `publish`, `reconcile`,
  and `select`.

## Self-Review And Concerns

- No plugin assets or manifests changed in Task 2.
- No user-entered article URL path remains in the Task 2 HTTP surface.
- The atomic project reconcile statement is exercised by concurrent SQLite
  tests and uses derived-table subqueries for MySQL's same-table update
  restriction. A live MySQL integration test was not run in Task 2.
- Scheduler/bootstrap invocation remains intentionally owned by Task 6; the
  service entry points and durable timestamps required for that wiring are in
  place.
