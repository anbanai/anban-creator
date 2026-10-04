# WeChat identity and publication schema repair

The former startup identity migration used substring replacement. Repeated
starts could turn `wechat-article` into `wechat-wechat-article`, then truncate a
later replacement at the `tasks.type` column width. Runtime profiles and other
identity-bearing strings were also exposed to that replacement.

Server startup no longer invokes `MigrateWechatIdentity`,
`MigrateMultiAgentChannelIdentity`, or its dependent
`MigrateProjectAgentConfigRemoval` cutover. Startup checks executable task,
execution, and plan-entry identities without rewriting their evidence. Empty
new databases still use AutoMigrate. Existing databases must already have the
canonical Agent/channel/task-kind schema and plan entries. A deployment that
has not completed the earlier multi-agent cutover must complete that separately
under an explicit reviewed maintenance operation; restarting Server is not a
migration mechanism. The historical cutover functions remain for controlled
operations and regression tests, not routine startup. Do not run them to fix
substring pollution or mutate an existing frozen execution.

## Offline identity repair

Stop Server, schedulers, and other database writers. Take a database backup.
From `server/`, inspect the default dry run:

```sh
go run ./cmd/repair-wechat-identities -config /secure/path/config.yaml > identity-dry-run.json
```

The command prints candidate counts, conflicts, affected row IDs, before/after
values, and evidence. It does not start HTTP, workers, schedulers, or providers.
Dry run creates no audit table and performs no writes. Review its findings,
then explicitly apply:

```sh
go run ./cmd/repair-wechat-identities -config /secure/path/config.yaml -apply > identity-applied.json
go run ./cmd/repair-wechat-identities -config /secure/path/config.yaml > identity-after.json
```

Only known article-prefix corruption in `tasks.type` is repaired, and only when
`agent_id`, `channel`, and `task_kind` validate against the canonical Agent Pack
and imply `wechat-article`. This covers repeated prefixes and truncated values.
An incomplete identity or a conflicting valid task type is reported and left
unchanged. No identity is inferred from a broken type, snapshot, title, or log.

`strategy_snapshots.applicable_tasks` is parsed as a JSON string array. The
complete legacy token `article` and complete repeated-prefix article tokens
(such as `wechat-wechat-article`) become `wechat-article`. Arbitrary substrings
and unrelated tokens stay intact. A truncated token has no independent identity
evidence, so its entire array is left unchanged and reported as a conflict.
Malformed arrays, including null/non-string elements, are conflicts.

Each applied change and its evidence are committed together in
`wechat_identity_repair_audits`; audit failure rolls back all row updates. The
repair neither changes frozen task/execution snapshots, billing records, runtime
images, nor execution runtime profiles. Runtime-profile discrepancies are
reported separately as `runtime_profile_conflicts` (excluded from the ordinary
`conflicts` count) and require inspection against the actual runtime evidence.
A frozen historical profile need not match today’s Agent Pack configuration.
Readiness rejects missing profiles and known `wechat-wechat…` corruption on
active executions, but does not reject a complete historical profile solely
because current catalog policy differs.
The command intentionally does not reconstruct missing historical identity or
perform the old project/plan/schema cutover. Repeating apply after successful
repair produces no further updates or audit rows.

## Narrow MySQL forward migrations

Apply these files to the backed-up maintenance database through your normal
MySQL migration client, on one connection per file:

- `server/migrations/20261004_wechat_publication_status_constraint.sql`
- `server/migrations/20261004_runtime_cleanup_diagnostic.sql`

The first changes only `chk_wechat_publication_status`. It detects whether the
constraint exists and uses one atomic ALTER to replace/add an enforced check.
It works when AutoMigrate has already added current columns but left the stale
named constraint. It does not replay old schema scripts, add duplicate columns,
or translate publication states. Existing unsupported states make the ALTER
fail; resolve each from durable publication evidence before retrying. In
particular, do not blanket-map an old state to `ambiguous`.

The current allowed states are `drafting`, `drafted`,
`awaiting_manual_publish`, `ambiguous`, `publishing`, `published`,
`needs_selection`, `publish_failed`, and `unsupported`. A provider 48001 response
continues to preserve the draft and allow manual publication; no provider calls
are issued by these repairs.

The second adds only the private `task_executions.cleanup_diagnostic` TEXT column
when absent. It does not change cleanup eligibility or reopen blocked cleanup.
See the runtime cleanup command for reviewed cleanup retry operations.

Startup checks the live MySQL CHECK clause's exact state set and enforcement,
both before existing-schema migration and after AutoMigrate for new databases.
A stale/missing/disabled guard prevents HTTP, scheduler, and worker startup.
It also rejects executable identity inconsistencies and missing cleanup evidence
schema with actionable migration hints.

## Verification

```sh
cd server
go test ./service -run 'TestWechatIdentityRepair|TestExecutableIdentityReadiness' -count=3
go test ./migrations -run 'TestPublicationStatus' -count=3
go test . -run 'TestStartupIdentity|TestRequireRuntimeCleanup' -count=3
```

A real MySQL 8 integration regression is opt-in:

```sh
ANBAN_MYSQL_TEST_DSN='test_user:password@tcp(127.0.0.1:3306)/anban_test_publication?parseTime=true' \
  go test ./migrations -run '^TestWechatPublicationStatusMySQL8$' -count=1
```

Use an empty, dedicated test database named `anban_test_*`; the test refuses a
different name or a nonempty database. Never use the application database or its
DSN. The test creates/drops only its own table, exercises the stale guard,
repeats the forward migration/readiness three times, verifies every current
state and invalid-state rejection, and proves an old lifecycle row is not
silently remapped. With no test DSN configured it is explicitly skipped.
