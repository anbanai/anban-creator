# Analytics and runtime repair rollout

This release fixes metric serialization and identity corruption; re-uploading
analytics files is not a repair. Keep the existing date range and cumulative
semantics. No production data, WeChat draft, execution or cluster is modified by
building/testing this release.

## Maintenance order

1. Stop old Server replicas and consumers, then back up the database. Suspend
   HPA/GitOps automatic reconciliation and scheduled producers during maintenance,
   and account for both rollout versions. Old replicas must not restart: their
   startup migration can corrupt identity again.
2. Follow [identity and schema repair](2026-10-04-identity-schema-repair.md):
   inspect the default dry run, apply only reviewed repairs, then apply both
   narrow forward schema migrations. Retain the audit and unresolved findings.
3. Start the new Server. Its read-only checks must pass before HTTP/workers start.
   Do not bypass a failed guard or roll back to the destructive startup version.
4. Check the same project and 2026-09-28 through 2026-10-04 in Studio. Compare
   API overview, contents, detail and observations against the database below.
   Counts must retain zero versus null. Content types must have matching labels
   and filters. `last_stat_date` must describe the selected statistical record,
   never the content creation/publication date.
5. Observe publication reconciliation and runtime cleanup. A WeChat 48001 keeps
   the existing draft and enters awaiting_manual_publish; it must not trigger
   another draft upload. A failed task stays failed when its workload is cleaned.

If rollback is necessary, stop writers and use the pre-maintenance backup plus a
version with the destructive identity startup migration disabled. Do not restore
only part of the identity/publication evidence graph.

## Release artifacts and maintenance tools

Build and publish both Server and Studio from the same reviewed commit using
unique immutable image references. This change does not require rebuilding Agent
images. Keep the configured runtime namespace and existing historical image
references unchanged during this repair. Use the existing deployment pipeline;
this repository's generic release workflow is for the DSH plugin, not Server.

```sh
make docker-server-image SERVER_IMAGE=YOUR_REGISTRY/creator-server:REPAIR_COMMIT
make docker-studio-image STUDIO_IMAGE=YOUR_REGISTRY/creator-studio:REPAIR_COMMIT
```

The production Server image does not contain Go or the maintenance commands.
On a controlled host with database network access and the matching source,
initialize submodules and use the Go toolchain declared in `server/go.mod`:

```sh
git submodule update --init --recursive
mkdir -p release/ops
(cd server && go build -o ../release/ops/repair-wechat-identities ./cmd/repair-wechat-identities)
(cd server && go build -o ../release/ops/runtime-cleanup ./cmd/runtime-cleanup)
```

These binaries run on the OS/architecture where they were built; cross-compile
explicitly if transferring them. The commands require the complete matching
Server configuration, referenced billing files and environment substitutions.
`-config` loads and validates that configuration without starting Server. Keep
secrets in protected config/environment files, never shell arguments or reports.
The `go run` examples below can equivalently use these binaries.

Apply the SQL files individually with a protected MySQL client options file,
using one connection for all statements in each file:

```sh
mysql --defaults-extra-file=/secure/mysql-client.cnf YOUR_DATABASE < server/migrations/20261004_wechat_publication_status_constraint.sql
mysql --defaults-extra-file=/secure/mysql-client.cnf YOUR_DATABASE < server/migrations/20261004_runtime_cleanup_diagnostic.sql
```

Do not use the client's `--force` option: a failed migration must stop the rollout.
Resolve reported conflicts from durable evidence, then repeat the dry run. Deploy
the new Server and Studio, verify one Server's startup/readiness, and restore the
intended replica count and automation after acceptance. Re-uploading statistics
or restarting historical generation tasks is not part of this rollout.

## Read-only analytics diagnosis

Use the normal authenticated Studio/API client for the selected project. Do not
paste credentials into diagnostics. Run these SELECTs in a read-only transaction
on the same project; replace the sample UUID and dates before executing:

```sql
START TRANSACTION READ ONLY;
SET @repair_project = 'PROJECT_UUID';
SET @repair_from = '2026-09-28';
SET @repair_to = '2026-10-04';
SELECT project_id, active_generation, revision, status
FROM analytics_project_states WHERE project_id = @repair_project;
SELECT source, metric_basis, COUNT(*) AS observations,
       MIN(stat_date) AS first_date, MAX(stat_date) AS last_date,
       SUM(read_users IS NOT NULL) AS rows_with_reads
FROM analytics_observations
WHERE project_id = @repair_project AND revoked_at IS NULL
GROUP BY source, metric_basis;
SELECT stat_date, COUNT(*) AS observations, SUM(read_users IS NOT NULL) AS rows_with_reads
FROM analytics_observations
WHERE project_id = @repair_project AND revoked_at IS NULL
  AND metric_basis = 'cumulative' AND stat_date BETWEEN @repair_from AND @repair_to
GROUP BY stat_date ORDER BY stat_date;
SELECT generation, granularity, COUNT(*) AS buckets, MAX(last_stat_date) AS last_stat_date
FROM analytics_buckets
WHERE project_id = @repair_project AND metric_basis = 'cumulative'
  AND bucket_start BETWEEN @repair_from AND @repair_to
GROUP BY generation, granularity;
SELECT COUNT(*) AS imported_snapshots, MAX(data_as_of_at) AS latest_import_date
FROM wechat_analytics_snapshots WHERE project_id = @repair_project;
SELECT COUNT(*) AS collected_snapshots, MAX(s.stat_date) AS latest_collection_date
FROM wechat_metric_snapshots s
JOIN wechat_article_trackings t ON t.id = s.tracking_id
WHERE t.project_id = @repair_project;
COMMIT;
```

These counts locate missing data; do not sum cumulative observations to obtain
UI totals. Winner priority/revocation and one latest observation per content are
applied by the existing projection pipeline.

- Existing active-generation buckets with values but missing API fields indicate
  the serialization defect fixed here. No rebuild or re-import is necessary.
- Existing observations/raw snapshots but missing active projections require a
  reviewed rebuild using the existing command below. The old published generation
  stays readable while the new generation is built and atomically published.
- No observations in the selected interval is a data-availability result, not a
  reason to change date semantics or invent zeros. Inspect import history and
  revoked batches before requesting a genuinely missing source file.

Only after confirming projection damage, from `server/`:

```sh
go run ./cmd/content-analytics-rebuild -config /secure/path/config.yaml -project-id PROJECT_UUID
```

Retain the rebuild report and verify counts/revision. Repeatedly importing the
same spreadsheet is unnecessary and can obscure the incident evidence.

## Reviewed runtime cleanup

From `server/`, export the blocked execution's current evidence:

```sh
go run ./cmd/runtime-cleanup --config /secure/path/config.yaml --execution-id EXECUTION_UUID > cleanup-review.json
```

Independently inspect the provider workload's execution/task/user/project labels,
namespace/name, UID or container ID, and immutable image evidence. Correct the
underlying ownership/access problem before reopening; never rewrite a historical
execution's image/profile merely to match current configuration. A fully persisted
Docker container identity can be verified after tag rollover. When that identity
is incomplete and the mutable tag has lost all verifiable historical image
association, retain the block.

Kubernetes executions with a persisted runtime scope use their original namespace.
A prebind execution without a persisted scope can recover a Job in the configured
namespace only after its ownership, UID (when recorded), and image are verified.
A 404 in that namespace permanently blocks cleanup: it cannot prove absence from
an unknown historical namespace. Drain pending/prebind executions before changing
namespaces. For a blocked execution, independently establish the original scope
and workload evidence before reopening; restoring access to that original
namespace permits recovery of a verified existing Job. If the scope/evidence
cannot be resolved, retain the block. Reopening alone does not supply missing
identity evidence or establish that the old namespace is clean.

After that review, use the exact unedited evidence document:

```sh
go run ./cmd/runtime-cleanup --config /secure/path/config.yaml --reopen --reviewed-identity --evidence cleanup-review.json
```

The command uses a compare-and-swap against that blocked revision. Stale or
changed evidence is rejected. Reopening only permits another cleanup attempt;
it does not start an Agent, change task outcome, publish content or alter billing.

## iLink: collect evidence before changing deployment

Local inspection found no configured Kubernetes context, so the supplied timeout
log does not establish an infrastructure root cause. On an authorized cluster,
set `ANBAN_NAMESPACE` and `ANBAN_SERVER_POD` to the actual deployment values:

```sh
kubectl -n "$ANBAN_NAMESPACE" get service sidecar-ilink
kubectl -n "$ANBAN_NAMESPACE" get endpointslices -l kubernetes.io/service-name=sidecar-ilink
kubectl -n "$ANBAN_NAMESPACE" get pods -l app=sidecar-ilink -o wide
kubectl -n "$ANBAN_NAMESPACE" describe deployment sidecar-ilink
kubectl -n "$ANBAN_NAMESPACE" logs deployment/sidecar-ilink --tail=100
kubectl -n "$ANBAN_NAMESPACE" get networkpolicy
```

Using diagnostic binaries already present in the Server Pod, test DNS resolution
and HTTP `http://sidecar-ilink:18070/health/live` from that Pod's network. Compare
with direct endpoint IP access and local Pod listening/probe results. Do not infer
cluster reachability from a developer machine or port-forward alone.

- Missing endpoints: inspect selector, readiness, process startup and PVC events.
- Pod-local health succeeds but Server access times out: inspect DNS, network
  policy and routing before application changes.
- Pod-local health also fails: inspect process listener, restart/OOM events and
  resource contention before changing probes.

Do not increase timeouts or disable health checks without evidence. The monitor
keeps its original timeout/backoff behavior; identical failures are summarized
at most every five minutes, with immediate failure changes and recovery logs.

## Local verification record — 2026-10-04

- Full `go test ./...` and Server build passed.
- Studio: 119 test files / 1,022 tests passed; production build passed.
- Real isolated MySQL 8.4.11: stale constraint rejects manual-publication status;
  readiness rejects stale schema; migration/readiness succeeds three times;
  all current statuses succeed and invalid statuses fail. An invalid existing
  lifecycle row makes the atomic ALTER fail while retaining the previous
  enforced constraint and original row.
- Identity repair/readiness repetition, canonical analytics values/types/dates,
  WeChat 48001 behavior, historical runtime cleanup, ownership rejection,
  permanent/transient error handling and sidecar log throttling are covered.
- A separate review found no remaining actionable issues after the detail
  metadata and provider cleanup error-classification corrections.

These checks do not establish production data completeness, real cluster health,
or three successful production Server restarts. Perform the maintenance and
same-project acceptance steps above in the deployment environment.
