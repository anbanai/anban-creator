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

## Fix Round 1

### Findings Addressed

- Publish preflight now paginates the published feed, reconciles an already
  published article before submission, and confirms the durable draft still
  exists before claiming `freepublish/submit`.
- Draft recovery, publication reconciliation, and operator selection scan all
  provider pages. Fingerprint recovery rejects zero timestamps and drafts
  updated before the persisted intent time, without replacing
  `DraftCreatedAt`.
- Project reconciliation uses a dedicated atomic project lease. Published
  article ownership uses an atomic `(project_id, article_id)` binding, so
  provider article IDs remain reusable across projects.
- Persistence failures are returned. Ambiguous submit transport/response
  outcomes remain durably pending, and API-origin recovery preserves
  `anban_api`, `msg_data_id`, and the derived composite `msg_id`.
- Stale `publish_submitting` rows reconcile through polling without resubmitting,
  and manual cadence is clamped to `DraftCreatedAt + 72h`.
- Project create/update reject invalid non-empty `wechat_publish_mode` values
  with HTTP 400. Empty create retains the global manual default; omitted update
  preserves the stored mode.
- SQLite concurrency paths use bounded, context-aware retries for transient
  table locks around guard claims and the adjacent publication reads/writes.

### RED Evidence

Focused RED runs demonstrated that page two was ignored, unsafe pre-intent
drafts were accepted, publish could submit without preflight, disappeared
drafts were resubmitted, cross-page ambiguity auto-bound, API origin was
overwritten, stale submissions could not reconcile, selection stopped after
page one, and the 72-hour cadence overshot. Repository tests initially lacked
the project lease and article-binding models and exposed non-atomic ownership.

The project-mode regression tests then failed with invalid create/update modes
being accepted and an omitted update clearing `api_confirmed`. The first full
suite run also reproduced an intermittent SQLite table lock in concurrent
selection; `-count=25` made the incomplete retry boundary deterministic before
the fix.

### GREEN Evidence

```text
go test ./server/service -run '^TestConcurrentSelectionHasOneAtomicArticleBindingWinner$' -count=25
ok github.com/anbanai/anban-creator/server/service

go test ./server/service ./server/repository ./server/handler ./server/model ./server/migrations -count=1
ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/repository
ok github.com/anbanai/anban-creator/server/handler
ok github.com/anbanai/anban-creator/server/model
ok github.com/anbanai/anban-creator/server/migrations

go test ./...
# all packages passed

go build -o /tmp/anban-creator-server-task2-fix1 ./server
# exit 0
```

### Self-Review And Scope

- Guard tables are covered by model, migration-fragment, repository concurrency,
  cross-project reuse, and service one-winner tests. SQLite executes the lease
  and binding behavior; no live MySQL integration environment was available.
- Provider pagination stops from `TotalCount` and per-page `ItemCount`, and
  fails closed if a provider reports remaining rows without pagination
  progress.
- Studio legacy-call cleanup remains Task 5. Scheduler due-query/bootstrap
  wiring remains Task 6. Neither surface changed in this fix round.

## Fix Round 2

### Findings Addressed

- Draft recovery compares the provider's integer `UpdateTime` with the intent at
  Unix-second precision. A same-second provider draft is eligible despite local
  nanoseconds; zero and prior-second timestamps remain ineligible.
- A submit response that omits `publish_id` now persists any returned
  `msg_data_id` before entering reconciliation. Candidate selection resolves
  lifecycle source instead of forcing `wechat_console`, preserving `anban_api`
  and deriving the exact `msg_data_id + "_1"` tracking identity.
- Publish preflight candidate and missing-draft transitions use narrow
  repository CAS updates guarded by publication ID, expected status, and
  `UpdatedAt`. A lost CAS reloads the durable publication instead of saving a
  stale full row over concurrent binding and analytics tracking state.
- SQLite project leases now use a subsecond `julianday('now')` epoch-microsecond
  clock, so a 60-second lease cannot expire nearly one second early.

### RED Evidence

```text
TestCreateDraftRecoveryAcceptsProviderTimestampInIntentSecond
WeChat publication outcome is pending reconciliation

TestAmbiguousAPIPublishSelectionPreservesResponseIdentity
pending MsgDataID=""

TestAmbiguousAPIPublishSelectionPreservesResponseIdentity
selected Source="wechat_console", want "anban_api"

TestPublishPreflightCandidateDoesNotRegressConcurrentBinding
Publish err=WeChat publication outcome is pending reconciliation

TestPublishPreflightMissingDraftDoesNotRegressConcurrentBinding
Publish err=WeChat publication outcome is pending reconciliation

TestWechatPreflightTransitionsRequireExpectedStatusAndVersion
TransitionToNeedsSelection and TransitionToPublishSubmitting undefined

TestWechatProjectReconcileLeasePreservesSQLiteSubsecondDuration
lease duration=59.08395s, want nearly the full minute
```

### GREEN Evidence

```text
go test ./server/service ./server/repository \
  -run 'Test(CreateDraftRecovery(AcceptsProviderTimestampInIntentSecond|RejectsProviderTimestampInPriorSecond)|AmbiguousAPIPublishSelectionPreservesResponseIdentity|PublishPreflight(Candidate|MissingDraft)DoesNotRegressConcurrentBinding|WechatPreflightTransitionsRequireExpectedStatusAndVersion|WechatProjectReconcileLeasePreservesSQLiteSubsecondDuration)$' \
  -count=1
ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/repository

go test ./server/service \
  -run 'Test(ConcurrentSelectionHasOneAtomicArticleBindingWinner|PublishPreflight(Candidate|MissingDraft)DoesNotRegressConcurrentBinding)$' \
  -count=25
ok github.com/anbanai/anban-creator/server/service

go test ./server/repository \
  -run 'TestWechat(ProjectReconcileLeaseHasOneConcurrentWinnerAndExpires|ArticleBindingHasOneConcurrentWinnerPerProject)$' \
  -count=25
ok github.com/anbanai/anban-creator/server/repository

go test ./server/service ./server/repository -count=1
ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/repository

go test ./...
# all packages passed

go build -o /tmp/anban-creator-server-task2-fix2 ./server
# exit 0
```

### Self-Review And Scope

- The stale-object tests execute a real concurrent `bindPublished` transition
  inside the provider callback and verify publication, binding, and tracking
  rows all retain the winner.
- CAS methods update only lifecycle columns and do not carry stale article,
  binding, or tracking fields into persistence.
- The SQLite precision test enters a controlled 600-800ms wall-clock window and
  asserts both the early and late lease boundaries.
- No live MySQL integration environment was available; MySQL continues to use
  `CURRENT_TIMESTAMP(6)`. Studio and scheduler scope remain in Tasks 5 and 6.

## Fix Round 3

### Findings Addressed

- Reconciliation source now depends on durable evidence that
  `freepublish/submit` was attempted: `submit_attempted_at`, `publish_id`, or
  `msg_data_id`. Lifecycle status and `last_error` are no longer treated as
  submit evidence.
- `ClaimPublish` persists `submit_attempted_at` atomically with the claim before
  the provider call. A failed claim write returns without calling
  `SubmitFreePublish`.
- Missing-draft preflight remains a CAS transition to `publish_submitting`, but
  does not set submit evidence. A later exact reconciliation is therefore
  attributed to `wechat_console`.
- Ambiguous transport outcomes without provider IDs retain durable attempt
  evidence and reconcile as `anban_api`. Existing `msg_data_id` and composite
  `msg_id` behavior remains unchanged.

### RED Evidence

```text
go test ./server/service ./server/model ./server/migrations \
  -run 'Test(MissingDraftPreflightReconcilesAsWechatConsole|AmbiguousTransportSubmitReconcilesAsAnbanAPI|PublishDoesNotSubmitWhenAttemptEvidenceCannotPersist|WechatPublicationSchemaIsOnePerTaskAndHasLifecycleContract|WechatPublicationMigrationMatchesCanonicalColumnAndIndexContract|WechatPublicationModelMatchesCanonicalNullabilityAndDefaults)$' \
  -count=1

server/service/wechat_publication_test.go:446:26:
stored.SubmitAttemptedAt undefined

WechatPublication missing SubmitAttemptedAt
migration has 30 columns, want 31
model column submit_attempted_at = migrations.modelColumn{notNull:false,
defaultValid:false, defaultValue:""}, want notNull=false default valid=false
value=""

TestMissingDraftPreflightReconcilesAsWechatConsole
Source="anban_api", want "wechat_console"
```

### GREEN Evidence

```text
go test ./server/service ./server/model ./server/migrations \
  -run 'Test(MissingDraftPreflightReconcilesAsWechatConsole|AmbiguousTransportSubmitReconcilesAsAnbanAPI|PublishDoesNotSubmitWhenAttemptEvidenceCannotPersist|AmbiguousAPIPublishSelectionPreservesResponseIdentity|WechatPublicationSchemaIsOnePerTaskAndHasLifecycleContract|WechatPublicationMigrationMatchesCanonicalColumnAndIndexContract|WechatPublicationModelMatchesCanonicalNullabilityAndDefaults)$' \
  -count=1
ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/model
ok github.com/anbanai/anban-creator/server/migrations

go test ./server/service ./server/repository \
  -run 'Test(PublishUsesSingleCASWinnerAndKeepsStringIdentifiers|PublishResponseLossSchedulesReconciliationWithoutResubmitting|PublishTransportAmbiguityPersistsPendingBeforeReturning|PublishPreflightFindsPublishedArticleAcrossPagesBeforeSubmit|PublishPreflightPersistsPendingWhenDraftDisappeared|PublishPreflightCandidateDoesNotRegressConcurrentBinding|PublishPreflightMissingDraftDoesNotRegressConcurrentBinding|PublishUnsupportedAndMissingDraftNeverBlindSubmit|PollRecoversStalePublishSubmittingWithoutResubmit|WechatPreflightTransitionsRequireExpectedStatusAndVersion)$' \
  -count=1
ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/repository

go test ./server/service ./server/repository ./server/model ./server/migrations -count=1
ok github.com/anbanai/anban-creator/server/service
ok github.com/anbanai/anban-creator/server/repository
ok github.com/anbanai/anban-creator/server/model
ok github.com/anbanai/anban-creator/server/migrations

go test ./...
# all packages passed

go build -o /tmp/anban-creator-server-task2-fix3 ./server
# exit 0
```

### Self-Review And Scope

- The attempt timestamp is intentionally nullable and unindexed. It is durable
  causal evidence used while reconciling a known publication, not a query key.
- The provider call remains strictly after the successful atomic claim write;
  missing-draft and lost-CAS paths never acquire attempt evidence.
- No live MySQL integration environment was available. The canonical MySQL
  migration contract and GORM/SQLite model behavior are covered; Studio and
  scheduler scope remain in Tasks 5 and 6.
