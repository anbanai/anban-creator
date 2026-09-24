# Unified content analytics

Implement the user-approved unified content-data design. Single route `/content-data`, account picker across article/seednote projects, URL-backed dates/granularity/content, remembered per-user account, compact metric strip/full-width trend, searchable/filterable/sortable 25-row table and full-width individual content analysis. Retain filters/scroll on back. Unified wide two-stage import dialog with preview, manual same-project target selection, explicit selected rows, and import history/revocation.

Matching: all task states eligible. Target refs are `{kind: 'task'|'wechat_publication'|'seednote_post', id: string}`. No foreign-account targets, no unknown-content creation, no publication side effects. Unselected rows skipped. Duplicate canonical targets rejected. Automatic ID/link then title/date then unique title with no conflicting identity. Preview has no analytics persistence; confirmation validates transactionally.

API coordination contract:
- `POST /projects/:id/{wechat|seednote}-analytics/imports/preview`: upload_id/timezone; returns file_name,total_rows,rows with source_row,title,content_type,match_status,parse_error, target?: AnalyticsTarget and native metrics/date.
- Confirm native `/imports` accepts `selections: Array<{source_row,target}>` plus upload_id,data_as_of_at,timezone. Explicit selections are authoritative and required. Repository review found no current MCP importer; no compatibility importer or whole-workbook fallback is retained.
- `GET /projects/:id/content-analytics/candidates?search=&offset=&limit=` returns `{items: AnalyticsCandidate[],total}`. Candidate: `{target,title,content_type,status,date?,url?}`. Include current project tasks all states, dedup existing platform records by task. Target stable canonical ref prefers task if linked.
- Wechat article view: keep native publication-backed views working; add optional `task` identity `{id,title,status,created_at}` and nullable publication for task-only snapshots. Top-level target, content_type and url expose the canonical content identity.
- Seednote native posts/overview return `genre`/content_type where known.

Preserve unrelated local edits. The user authorized final review and merge into local main; do not push or deploy. Previous authorized analytics edits are the starting state. Full Studio tests/build and Go tests/build plus browser QA required.

## Implementation results

- Unified page, routes, account selection, URL-backed state and single-content dashboard implemented. Superseded pages, import preview and helper UI removed.
- Shared preview/manual matching/import/history implemented; all-state same-account targets only, selected rows only, no publication mutation.
- Task-only WeChat snapshots remain readable after a real publication appears. Seednote learned matching and imported date evidence carry active-batch provenance so revocation removes their matching influence.
- Independent review findings addressed and regression-tested: mixed-offset timestamp ordering, task/publication read continuity, revoked matching evidence.
- Browser verified desktop and narrow layouts, account switch retaining date/granularity, single-content trends, both platform import previews and confirmations using explicitly labeled sample-only fixtures. Narrow import is full-height with fixed actions.
- Go full tests and server build passed. Final Studio full tests (110 files / 934 tests) and production build passed, including URL-default regression. Go full tests/build passed after all service fixes.

## Final merge review

- Normalize Seednote overview and ranking dates to Asia/Shanghai before daily deduplication, independent of database connection timezone. Regression covers UTC midnight within one Beijing day.
- Empty single-content ranges offer date recovery independently of other content; date and granularity changes participate in browser history.
- Remove unused WeChat post-import resolve endpoints and service behavior; reassociation requires revoke and reimport. Compare historical WeChat URLs by platform identity, ignoring tracking parameters.
- Validate the feature in an isolated checkout using the committed dependency manifests and harness revision, excluding unrelated runtime and dependency edits.
