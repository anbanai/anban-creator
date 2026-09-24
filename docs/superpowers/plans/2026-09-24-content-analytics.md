# Content analytics implementation plan

Historical first phase; superseded by `2026-09-24-unified-content-data.md`.

Goal: Make WeChat and Seednote analytics readable by date range and day/week/month, and keep import issues inside import dialogs.

Architecture: Existing Go services remain responsible for import matching and persistence. Studio shares date controls and trend presentation; counts and rates retain their declared semantics. No publication side effects from analytics imports.

Constraints: Preserve unrelated local edits. No compatibility scaffolding. No harness changes. Existing design tokens, React Query, Recharts, Bun.

- [x] WeChat server: preview every row with authoritative match status and explicit content type, accept selected source rows and revalidate them transactionally; test parser/matching/selection. Preserve source as provenance, never infer type from generic source labels.
- [x] Shared Studio: date range presets, inclusive date filters, day/week/month controls, trend component with visible single points and missing-data handling. WeChat snapshot trends select the latest observation per content per period, never sum repeated snapshots.
- [x] WeChat page: analytics first, metric cards, trends, searchable content performance, snapshot details; remove main-page exception queue. Preview all rows with usable/skip reasons and counts; confirm selected matched rows only; show observation time.
- [x] Seednote page: use shared time/trend controls, explicit import confirmation, keep unresolved imports inside dialog, expose loading/errors/empty-range recovery and content search. Preserve documented daily metric semantics.
- [x] Verification: targeted regression tests, full Studio tests/build, full Go tests/build, browser visual check when local preview is accessible, final independent code review.

Design decisions: User explicitly authorized whole-page improvements; proceed within existing design system without additional approval gates. Supplied screenshot is evidence of current layout. Preserve pending publication access in details/secondary section, not primary analytics.

Verification: Studio full suite passed 110 files / 951 tests; production build passed. Go full suite and server build passed. Focused final layout/selection checks passed. Browser verified desktop and narrow viewport, WeChat month aggregation, content type filtering, selected-row behavior, and Seednote layout using explicitly labeled sample data. Review findings (bounded date generation, explicit image aliases, mobile dialog sizing) addressed and re-reviewed.

Statistics: WeChat is cumulative observations per content, latest per selected bucket, without fabricated daily increments or summing repeated snapshots. Seednote retains the current service daily-count contract. Final repository review found no current MCP importer. The unified implementation requires explicit selected source rows revalidated inside the transaction.
