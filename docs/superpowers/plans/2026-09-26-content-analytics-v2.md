# Content analytics v2 implementation contract

User-approved design: immutable observations, transactional incremental day/week/month content and account projections, bounded SQL reads, explicit cumulative/daily basis, no legacy query fallback or dual writes. Asia/Shanghai calendar. WeChat imports/API cumulative; Seednote imports must declare basis. Raw payloads off hot path. Unknown historical Seednote import basis archived but excluded. Preserve null versus zero; ratios/durations unaggregatable without reliable denominator. Source priority precedes effective time and ingestion sequence within same content/day/basis. Numeric counts int64 and finite exact decimal ratios.

## API contract (all under /projects/:id/content-analytics)
Common params from,to inclusive YYYY-MM-DD, granularity day|week|month, metric_basis cumulative|daily; optional expected_revision integer. All responses normal existing Success envelope. Revision conflict HTTP409; rebuilding initial HTTP503; bad input400; ownership403. Every read one consistent DB snapshot.
- GET /overview -> {revision,updated_at,metric_basis,totals:MetricMap,series:[{date,...MetricMap}],coverage:{contents:number},unavailable_metrics:Record<string,string>}
- GET /contents -> {revision,items:AnalyticsContent[],total,offset,limit}, query search,content_type,sort,direction asc|desc,offset,limit (25 default,100 max). AnalyticsContent {id,title,content_type,status?,url?,date?,metrics:MetricMap}. Stable content ids supplied by server; null last. Optional target task:<id>|wechat_publication:<id>|seednote_post:<id> on detail resolution endpoint below.
- GET /contents/:contentId -> {revision,updated_at,metric_basis,content:AnalyticsContent,totals,series,unavailable_metrics}. contentId supports explicit task:<id> / seednote_post:<id> / wechat_publication:<id> aliases for existing business deep links, and canonical IDs. Resolver deterministic recent note for task with several posts; all remain selectable individually.
- GET /contents/:contentId/observations -> {revision,items:[{id,stat_date,source,metric_basis,effective_at,received_at,revoked_at?,metrics:MetricMap}],total,offset,limit}; raw payload only explicit detail if needed, never overview.
- GET /dates?year=YYYY&metric_basis=... -> {revision,dates:string[]}; no batch download.
- existing /candidates becomes SQL pagination without full history scan.
MetricMap known platform keys -> number|null. Aggregate ratios null with unavailable reason; selected cumulative content snapshot may show original ratios. Cumulative totals last observation within selected range per content, not sum of daily account cumulative totals. Incomplete boundary weeks/months clipped exactly. Range rollup uses complete months + edge days, no raw observation scans. Week Monday. Max query span ten years as existing UI.

## Import
Existing platform import endpoints retained for parser/workflow, but write new stats facts only. Both requests require data_as_of_at; WeChat metric_basis=cumulative (server fixed); Seednote metric_basis required cumulative|daily. Request idempotency_key required, same key changed request409. Responses retain current receipts plus revision. Retain batch and raw import rows for history. Revoke transaction recomputes affected buckets and increments revision. Automatic collection writes same facts; old snapshot tables migration-only after cutover.

## Tasks and ledger
- [ ] Core models, repository, projection write and query service, deterministic tests.
- [ ] Integration import/revoke/collection, routes and candidate pagination.
- [ ] Studio bounded queries, explicit basis, cancellation, revision handling and tests.
- [ ] Migration/rebuild jobs and operational tool, recovery and tests.
- [ ] Full tests/builds, MySQL benchmark and independent review.

Ruling: isolated worktree /Users/medivh/WORKSPACE/anbanwriter-analytics protects unrelated ongoing work. No changes to harness expected.
Ruling: database precision and overflow validation must precede mutation. No invented denominator or guessed historical basis.
