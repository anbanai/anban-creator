# WeChat Content Analytics Design

## Goal

Make the WeChat analytics experience useful for account-level editorial decisions by combining trustworthy account totals, time trends, traffic-source attribution, and content performance. Keep existing per-content cumulative observations and Seednote analytics behavior intact.

## User Outcomes

- Understand whether account reach and engagement are improving over comparable periods.
- See which WeChat discovery and sharing channels contribute reads, with both counts and shares.
- Identify which published articles contribute the most reads and how each performs over time.
- Tell the difference between a measured zero and data that was not imported or is not comparable.

## Current State

The shared content analytics service stores cumulative per-content observations and builds day/week/month projections. The WeChat workbook importer currently parses article-level rows (readers, shares, follows, delivery and completion rates). The React page already provides date selection, trend selection, article search/sort/pagination, and single-article detail.

The current contract does not contain account-level daily totals or traffic-source dimensions. Account totals must not be inferred by summing article cumulative snapshots, and source shares must not be inferred from aggregate article reads. The official screenshots are references for information architecture and metric presentation; their example values are not product data.

## Recommended Approach

Keep the existing shared content analytics contract as the source for per-content cumulative facts. Add a WeChat-owned account analytics contract and import path for account-level daily facts and source-level daily read facts. This keeps WeChat-specific dimensions out of the generic Seednote metric family and prevents aggregate account metrics from being confused with content snapshots.

Account facts are immutable, dated observations tied to an import batch and source provenance. Account-level metrics and traffic-source counts are queried from those facts; they are not merged into per-content projections. Revoking an account report excludes its facts and advances the project's analytics revision. Missing periods remain gaps, not zeroes.

## Information Architecture

The WeChat view is organized in this order:

1. **Data overview**: account-level headline metrics for yesterday, the recent 7 days, the recent 30 days, or a custom range. Show the data-through date and coverage. Show day-over-day and week-over-week comparisons only when both comparison windows have sufficient data.
2. **Traffic analysis**: metric selector, traffic-source selector, date range and daily trend. Supported source labels are WeChat message, recommendation, chat, Moments, official-account home, Search, and Other. A source selector includes All.
3. **Traffic sources**: ranked source counts and percentages for the selected metric and period. Read share is computed only from source counts whose coverage matches the displayed total; otherwise the percentage is unavailable.
4. **Content performance**: searchable and sortable article rows with title, publication date, readers, readers' share, and latest observation date. Readers' share is shown only when the selected content and account totals have compatible coverage and periods. Each row opens the existing per-content trend/detail view.
5. **Single-content detail**: retain existing content metrics, trend, source link, and observation history.

The Seednote branch and its route parameters are outside this design's implementation scope.

## Data Contract

### Account observations

- Store one dated account observation per imported reporting period and import batch, with typed nullable metrics for readers, shares, likes, collections, comments, follower gains, and published article count when those fields are present in a supported official report.
- Store traffic-source read counts as dated rows keyed by a normalized source enum. Preserve the original source label in import provenance for audit and forward-compatible remapping.
- Values are non-negative integers. `null` means the source report did not provide the metric; `0` is an observed zero.
- Facts include project, batch, stat date, source, received time, and effective/data-as-of time. Raw uploaded data remains in the existing file-backed import lifecycle with bounded payload handling.
- Queries use Asia/Shanghai calendar dates. Aggregate sums are allowed only for daily account facts, never for cumulative content observations.
- Account reporting is WeChat-only. Existing Seednote and generic per-content request/response semantics do not change.

### Import lifecycle

- Provide a report type for WeChat account/traffic exports alongside the existing content-performance import.
- Preview identifies report type, supported fields, date coverage, source labels, parse errors, and duplicate dates before applying data.
- Only explicitly recognized official export columns are mapped. Unknown columns are retained in raw provenance and are not guessed into metrics.
- Account and source facts are persisted transactionally with their import batch. Revoke removes the batch from winner selection and recomputes all affected account results. Re-importing the same idempotency key and request replays the original receipt; a changed request conflicts.
- Existing content import remains available and keeps its current matching and revoke behavior. Account reports do not require matching content titles.

### API

Add a WeChat-specific read contract under `/projects/:id/wechat-analytics`:

- `GET /overview?from&to` returns revision, selected range, data-through date, coverage dates, daily series, period totals, and previous-day / previous-week comparisons where defined.
- `GET /traffic-sources?from&to&metric` returns revision, metric, total, coverage, and ranked `{source, label, value, share}` items. Percent shares are `null` when the denominator is missing or source totals do not reconcile with the corresponding account metric.
- Existing `/content-analytics/contents`, `/contents/:contentId`, and observation APIs remain authoritative for article-level cumulative metrics and continue to serve Seednote unchanged.

Responses preserve null-versus-zero semantics. A comparison is unavailable when either period is missing, the denominator is zero, or coverage is incomplete; the API must not return a fabricated `0%` change.

## Comparison Semantics

- All date ranges are inclusive and interpreted in Asia/Shanghai.
- Yesterday compares with the immediately preceding calendar day for day-over-day and the same weekday one week earlier for week-over-week.
- The recent 7-day and 30-day presets compare with the immediately preceding equal-length window. Their weekly context uses the equal-length window ending seven days earlier.
- Custom ranges compare with the immediately preceding range of equal duration; comparison labels state the exact dates.
- Percentage change is `(current - previous) / previous`. If the prior value is zero, the change is unavailable and the UI shows an em dash.
- A period is complete only when all expected daily account observations in that period exist. The API returns coverage so the UI can explain incomplete periods.

## UI Behavior

- Use period tabs for Yesterday / Last 7 days / Last 30 days / Custom; preserve the selected range in the URL.
- Keep the account overview visually distinct from the existing per-article cumulative metrics so users do not read one as a rollup of the other.
- Traffic-source rows show count and percentage together and use an accessible table/list equivalent in addition to the chart.
- The trend chart has metric and source controls. Selecting a source filters the trend to that source; All uses account totals.
- Content readers' share uses a server-provided, coverage-qualified value or is omitted. The client must not derive it from unrelated or incomplete totals.
- Loading, empty, partially covered, unavailable, and error states are explicit. No sample screenshot values are rendered as fallback data.

## Persistence And Revision Rules

- Account observations and traffic-source rows belong to a WeChat account import batch and project.
- Revisions advance on successful import, revoke, or correction that changes visible analytics facts. Overview and source responses from a read use one consistent database snapshot and return the same revision contract used by existing analytics queries.
- Batch winner selection is deterministic: active batch first, then effective/data-as-of time, received time, and stable row identity. Two batches for the same project/date do not get added together as duplicate daily account totals.
- Revocation restores the next valid observation for an affected date; if none remains, that date becomes uncovered.
- Do not change generic analytics observation winner rules, content projections, Feedback Agent read boundaries, or Seednote data.

## Failure And Data Quality

- Unsupported file layouts fail preview with a clear explanation and do not persist facts.
- Invalid metric values mark affected rows invalid; a batch with invalid rows cannot silently import partial account totals.
- Mismatched source totals are preserved as imported facts but shares are unavailable and the response exposes a data-quality reason.
- Stale or incomplete coverage is surfaced with exact available dates. No backfill or interpolation is performed.

## Verification

- Go tests cover parser header recognition, source normalization, null versus zero, date comparison windows, deterministic winner selection, coverage, percentages, idempotency, revoke restoration, and revision changes.
- Handler tests cover authentication, validation, response shape, and revision behavior.
- Studio tests cover period selection, comparisons, source filtering, partial-data states, and preservation of the existing WeChat per-content workflow.
- Run `cd server && go test ./...`, `cd server && go build -o /tmp/anban-creator-server .`, `cd studio && bun run test`, and `cd studio && bun run build`.
- Visually verify the WeChat page at desktop and mobile widths using representative test data with gaps, zeros, mismatched source totals, and a fully covered range.

## Non-Goals

- Changing Seednote analytics or its design.
- Automatically fetching private WeChat analytics from the platform.
- Inferring source attribution from article totals or reconstructing unavailable historical days.
- Treating the reference screenshots' figures or article titles as application data.
