# Task 1 Report: Schema, Configuration, And WeChat Provider Contracts

## Status

Completed and committed as a clean cutover. The repository-wide suite is not
yet green because the legacy publication service/handler consumers are owned by
the downstream lifecycle tasks and now correctly fail to compile against the
removed contract.

## Files Changed

- `server/model/project.go`: replaced the two booleans with
  `wechat_publish_mode`; zero-value mode resolves to `manual`.
- `server/model/constants.go`: added the exact three project-mode constants and
  validation helper; removed approval-state constants.
- `server/model/task.go`: removed `Published`, `PublishedAt`, and approval
  staging fields.
- `server/model/task_execution.go`: renamed publishing persistence and
  finalization constants to draft-delivery semantics.
- `server/model/wechat_publication.go`: added the one-per-task lifecycle model,
  exact source/status checks, provider identifiers, draft metadata/fingerprint,
  API/console tracking metadata, timing, candidates, and claim fields.
- `server/model/model.go`: registers `WechatPublication` with `AutoMigrate`.
- `server/model/task_execution_test.go`: updates the durable execution schema
  expectation to draft-delivery names.
- `server/migrations/20260831_wechat_publication_lifecycle_contract.sql`: clean
  one-time DMS cutover migration that drops obsolete task columns, renames
  execution columns, and creates `wechat_publications` without a backfill.
- `app/wechat/official_api.go`: typed official-account transport for all six
  lifecycle endpoints, retaining `publish_id` and `msgid` as strings.
- `app/wechat/service.go`: exposes the typed transport through the existing
  cached official-account access-token source.
- Tests: `server/model/wechat_publication_test.go`,
  `server/migrations/wechat_publication_contract_test.go`, and
  `app/wechat/official_api_test.go`.

## RED Evidence

1. Before implementation:

   ```text
   go test ./server/model ./app/wechat -run 'Test(...OfficialAPI)' -count=1
   undefined: OfficialAPI
   undefined: DraftAddRequest
   undefined: WechatPublishModeManual
   undefined: WechatPublication
   ```

2. Before adding source/status database checks:

   ```text
   go test ./server/model -run TestWechatPublicationSchemaIsOnePerTaskAndHasLifecycleContract -count=1
   invalid publication ... Status:"completed" ... persisted
   ```

3. Before adding plan-required metadata extensions:

   ```text
   undefined: WechatPublicationSourceAnbanAPI
   result.List[0].ContentURL undefined
   ```

## GREEN Evidence

Focused validation completed successfully:

```text
gofmt -w ...
go test ./server/model ./server/migrations ./app/wechat -count=1
ok github.com/anbanai/anban-creator/server/model
ok github.com/anbanai/anban-creator/server/migrations
ok github.com/anbanai/anban-creator/app/wechat
```

The full suite was also run:

```text
go test ./...
```

It fails at the expected clean-cutover boundary because unchanged downstream
files still reference removed `GetEnablePublishing`, `GetRequirePublishApproval`,
`PublishingStatus`, and approval fields. No focused task-1 package failed.

## Decisions

- Mode values are exactly `disabled`, `manual`, and `api_confirmed`; omitted
  mode is resolved as `manual`.
- Publication source is constrained to `anban_api` or `wechat_console`.
- Publication status is constrained to exactly the eight lifecycle values.
- `WechatPublication.TaskID` is unique, and `ArticleIndex` defaults to `1`.
- Formal publication IDs use string `PublishID`, `MsgDataID`, and `MsgID`; no
  integer SDK conversion is used.
- Provider payloads preserve draft and published article URL, author, digest,
  HTML content, thumbnail media ID, and update time. Detail metrics expose
  reads, shares, favorites, likes, looking, comments, completion rate, average
  read time, read-to-subscribe fields, and `content_url`.
- The migration is intentionally destructive and has no update/backfill path.

## Downstream Interfaces

- `model.Project.GetWechatPublishMode()` and `model.WechatPublishMode*`.
- `model.WechatPublication` with `DraftMediaID`, `DraftTitle`, `DraftAuthor`,
  `DraftDigest`, `DraftThumbMediaID`, `DraftContentFingerprint`, `PublishID`,
  `MsgDataID`, `MsgID`, `ArticleID`, `ArticleURL`, `ArticleIndex`,
  `WechatStatusCode`, `DraftCreatedAt`, `PublishedAt`, scheduling/error/candidate
  fields, and `ClaimToken`/`ClaimedAt`.
- `TaskExecution.DraftDeliveryStatus` / `DraftDeliveryResult`,
  `TaskExecutionFinalizationDraftDelivery`, and `TaskExecutionDraftDelivery*`.
- `wechat.OfficialAPI` methods: `AddDraft`, `BatchGetDrafts`,
  `SubmitFreePublish`, `GetFreePublish`, `BatchGetFreePublishes`, and
  `GetArticleTotalDetail`.

## Self-Review And Concerns

- `git diff --check` is clean.
- No plugin files were changed.
- The one-time SQL migration must be applied only after downstream service,
  handler, scheduler, MCP, and Studio consumers land. Its deliberate absence
  of a compatibility path means a partial deployment will not compile/run.
- `wechat_publish_mode` is JSON-backed, so model code supplies exact constants
  and validation; request validation belongs to the project API/UI cutover task.
