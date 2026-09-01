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

## Fix Round 1

### Review Findings Addressed

- Replaced the invented `details` analytics shape with the official
  `detail_list` contract. The response now has `is_delay`; each published item
  has its composite `msgid` in `MsgID` and `publish_type`; samples expose
  `read_user`, `read_user_source`, `share_user`, `zaikan_user`, `like_user`,
  `comment_count`, `collection_user`, `praise_money`, `read_subscribe_user`,
  `read_delivery_rate`, `read_finish_rate`, `read_avg_activetime`, and
  `read_jump_position`.
- `FreePublishSubmitResponse` now retains the official string `msg_data_id`
  alongside `publish_id`. It is distinct from the composite analytics `msgid`.
- Replaced the analytics `httptest` fixture with a complete official response
  and asserted a one-day `begin_date == end_date` request range.
- The DMS migration drops `idx_task_executions_publishing_status` before the
  column rename and creates only
  `idx_task_executions_draft_delivery_status` afterward.
- Aligned GORM tags with the DDL's non-null/default contract for publication
  identifiers, metadata, errors, and claim token. Added executable SQLite
  schema checks plus DDL fragment checks for all non-null lifecycle fields.

### Tests Added Or Adjusted

- `app/wechat/official_api_test.go`: submit `msg_data_id`, official
  `detail_list`, full official metric fixture, one-day request range, and
  distinct identifier assertions.
- `server/migrations/wechat_publication_contract_test.go`: index replacement
  checks and model/DDL nullability/default equivalence matrix.

### RED Evidence

```text
go test ./app/wechat ./server/migrations -run 'Test(OfficialAPIUsesOfficialPublicationEndpointsAndStringIdentifiers|WechatPublicationContractMigrationIsACleanCutover|WechatPublicationMigrationMatchesModelNullabilityAndDefaults)' -count=1
app/wechat/official_api_test.go:42:67: result.MsgDataID undefined
app/wechat/official_api_test.go:78:30: result.IsDelay undefined
app/wechat/official_api_test.go:78:61: result.List[0].MsgID undefined
app/wechat/official_api_test.go:78:108: result.List[0].PublishType undefined
app/wechat/official_api_test.go:78:143: result.List[0].DetailList undefined
migration missing "DROP INDEX `idx_task_executions_publishing_status`"
migration missing "ADD INDEX `idx_task_executions_draft_delivery_status` (`draft_delivery_status`)"
model column draft_media_id ... want notNull=true default="''"
```

### GREEN Evidence

```text
go test ./app/wechat ./server/model ./server/migrations -count=1
ok github.com/anbanai/anban-creator/app/wechat
ok github.com/anbanai/anban-creator/server/model
ok github.com/anbanai/anban-creator/server/migrations
```

`go test ./...` was also rerun. It remains blocked by unchanged downstream
legacy service consumers (`GetEnablePublishing`, `PublishingStatus`, and
approval fields), not by any task-1 package.

### Self-Review

- `git diff --check` passes.
- Submit-time `MsgDataID` and analytics `MsgID` now use different fields and
  tags, preventing accidental cross-domain identifier reuse.
- The migration explicitly replaces the old index rather than relying on a
  database implementation to rename it implicitly.
- SQLite renders empty-string defaults with double quotes while MySQL DDL uses
  single quotes; the equivalence assertion normalizes quote syntax but still
  verifies nullability and semantic defaults.

## Fix Round 2

### Review Findings Addressed

- `getarticletotaldetail` now decodes `is_delay` as a boolean, restores
  per-item `content_url`, and keeps analytics `MsgID` separate from submit-time
  `MsgDataID`.
- Analytics source and jump-position values now preserve the official array and
  object JSON shapes instead of attempting to decode them as scalar integers.
- `WechatPublication` explicitly marks its primary key and `CreatedAt` /
  `UpdatedAt` fields `not null`, matching the migration contract.
- Migration validation now parses ordered ALTER operations and the complete
  `wechat_publications` table contract. It checks old-index drop before column
  renames and new-index creation, every column's null/default declaration, and
  the precise column-to-index matrix.
- SQLite schema inspection retains `sql.NullString.Valid`, so an absent default
  cannot satisfy an explicit `DEFAULT ''`; a dedicated mutation assertion
  exercises that distinction.

### Tests Added Or Adjusted

- `app/wechat/official_api_test.go`: realistic boolean `is_delay`, composite
  analytics `msgid`, exact `content_url`, array source data, and object jump
  positions, all asserted after decoding.
- `server/migrations/wechat_publication_contract_test.go`: deterministic ALTER
  operation ordering, canonical column/default/index parsing, and absent versus
  empty default validation.

### RED Evidence

```text
go test ./app/wechat ./server/model ./server/migrations -count=1
FAIL TestOfficialAPIUsesOfficialPublicationEndpointsAndStringIdentifiers/article_total_detail
decode WeChat /datacube/getarticletotaldetail response:
json: cannot unmarshal bool into Go struct field ArticleTotalDetailResponse.is_delay of type int
```

The timestamp mutation check also failed as intended after temporarily removing
the two GORM tags:

```text
go test ./server/migrations -run TestWechatPublicationModelMatchesCanonicalNullabilityAndDefaults -count=1
model column updated_at ... notNull:false, want notNull=true
model column created_at ... notNull:false, want notNull=true
```

### GREEN Evidence

```text
go test ./app/wechat ./server/model ./server/migrations -count=1
ok github.com/anbanai/anban-creator/app/wechat
ok github.com/anbanai/anban-creator/server/model
ok github.com/anbanai/anban-creator/server/migrations

git diff --check
```

### Self-Review And Limitation

- The focused API/model/migration suite passes after the correct types and
  constraints are restored.
- There is no live MySQL fixture or container in this task, so the MySQL DDL
  was not executed. The migration is instead checked with deterministic DDL
  parsing plus SQLite-generated GORM schema inspection; this report makes no
  claim of live MySQL execution.

## Fix Round 3

### Review Findings Addressed

- Replaced the dynamic analytics maps with typed nested response structs:
  `read_user_source` decodes `[]ArticleReadUserSource` with `user_count` and
  `scene_desc`; `read_jump_position` decodes
  `[]ArticleReadJumpPosition` with `position` and `rate`.
- Retained the correct boolean `is_delay`, per-item `content_url`, and composite
  analytics `MsgID` behavior from Fix Round 2.

### RED Evidence

```text
go test ./app/wechat -count=1
FAIL TestOfficialAPIUsesOfficialPublicationEndpointsAndStringIdentifiers/article_total_detail
decode WeChat /datacube/getarticletotaldetail response:
json: cannot unmarshal array into Go struct field
ArticleTotalDetailMetric.list.detail_list.read_jump_position
of type wechat.ArticleReadJumpPosition
```

### GREEN Evidence

```text
go test ./app/wechat -count=1
ok github.com/anbanai/anban-creator/app/wechat

git diff --check
```

### Self-Review

- The official response fixture includes two source records and two jump
  records; assertions verify every nested field value after real JSON decoding.
- No `map[string]any` remains in the analytics response contract.
