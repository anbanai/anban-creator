# Design QA

## Target

- Source visual truth: `/var/folders/c4/50vh622s75782gcbmt8yrhv00000gn/T/codex-clipboard-e2e30e98-2dc2-4394-989c-d0a58fa68aff.png` (2880 x 1624 px; displayed reference resized to 2048 x 1155 px).
- Implementation route: `http://127.0.0.1:5174/tasks/<task-id>`.
- Intended state: authenticated failed Seednote task with its execution stages visible.
- Browser: Codex in-app browser.

## Capture

- Source and implementation could not be compared at matching state. The local implementation redirected to `/login`; no authenticated session or local failure-state preview fixture was available.
- Browser capture showed the Studio login page at 434 x 803 px. This capture is rejected as implementation evidence because it does not show the requested task screen. It was not saved as a QA screenshot.
- CSS viewport and device pixel ratio: unavailable.
- Full-view comparison: not performed; the login page does not contain the target screen.
- Focused-region comparison: not performed for the same reason.

## Findings

- [P1] Visual verification is blocked. The changed page requires an authenticated task route, while the available local browser session is signed out. The targeted tests and production build pass, but they do not replace a rendered comparison. Reopen the route with a valid local session and compare the failed-stage row, recovery icon, and header actions against the source.

## Comparison History

- Initial capture reached the login page. No visual fixes were made from that capture because it did not represent the requested state.

## Implementation Checklist

- Remove the page-level failure banner.
- Keep recovery beside the failed or interrupted execution stage.
- Use borderless, background-free icon actions with tooltip and accessible names.
- Re-run visual comparison at the source viewport after opening an authenticated task.

final result: blocked

## Content Analytics Redesign QA

### Target

- Source visual truth: `/var/folders/c4/50vh622s75782gcbmt8yrhv00000gn/T/codex-clipboard-afa749e5-c3ab-4c44-8d43-2a58ba6e6df3.png`, `/var/folders/c4/50vh622s75782gcbmt8yrhv00000gn/T/codex-clipboard-d5ece3c1-fe49-4745-adc7-a7f48bdc6308.png`, and `/var/folders/c4/50vh622s75782gcbmt8yrhv00000gn/T/codex-clipboard-60fbb0ab-0f42-44f1-af83-d46e7f11077d.png`.
- Implementation route: `http://127.0.0.1:4174/content-analytics`.
- Intended states: authenticated analytics page at desktop and narrow-screen viewports, with the 30-day range selected.

### Capture

- Source 1: 2880 x 1624 px, normalized to 1440 x 812 for the before/after full-view comparison. Sources 2 and 3 are 2226 x 940 px and 2230 x 1540 px; their device pixel ratio is unknown.
- Desktop implementation: [anban-content-analytics-desktop.png](/tmp/anban-content-analytics-desktop.png), 1440 x 812 px at CSS 1440 x 812, deviceScaleFactor 1. The account trend comparison also uses [anban-content-analytics-desktop-chart.png](/tmp/anban-content-analytics-desktop-chart.png), 1440 x 1000 px at CSS 1440 x 1000, deviceScaleFactor 1.
- Narrow implementation: [anban-content-analytics-mobile.png](/tmp/anban-content-analytics-mobile.png), 390 x 844 px at CSS 390 x 844, deviceScaleFactor 1. The lower-page capture is [anban-content-analytics-mobile-lower.png](/tmp/anban-content-analytics-mobile-lower.png), at the same viewport after scrolling the content pane.
- State: authenticated isolated visual-test account; API responses were mocked with 24 available days in a 30-day range, six missing dates, partial totals, and a content list. No real account data was used.
- Full-view comparison: [before/after comparison](/tmp/anban-content-analytics-before-after.png) places the original 0/30-day state and the updated 24/30-day state together. The updated page shows partial totals and coverage counts while missing dates remain gaps.
- Focused comparisons: [account overview comparison](/tmp/anban-content-analytics-reference-comparison.png) and [trend comparison](/tmp/anban-content-analytics-trend-comparison.png) place the supplied references beside the corresponding implementation regions. Anban's existing orange tokens are retained in place of the reference product's green palette.
- Responsive check: document width matched viewport width at 1440 px and 390 px. No horizontal overflow was detected. The four summary metrics form a two-column grid on narrow screens; chart controls wrap without overlap.
- Interaction check: selecting “近 7 天” updated the selected state and URL to 2026-10-13 through 2026-10-19. Page errors: none. The console emitted one React script-tag diagnostic on both viewports; it did not prevent rendering or interaction.

### Fidelity Review

- Typography and copy: compact headings and labels remain readable; metric values use tabular numerals and fit their cells.
- Layout and rhythm: date controls, coverage, four key metrics, and trend form a clear first-screen sequence. Narrow layout remains within the viewport.
- Color and tokens: restrained Anban orange highlights selection and incomplete coverage; semantic warning contrast remains clear.
- Assets: no product imagery is required for these data views. Existing chart and icon components render.
- Content: partial totals and per-metric coverage are visible; comparison values are omitted for incomplete coverage; missing dates are not presented as zero.

### Findings

- No actionable P0/P1/P2 visual or responsive differences found. The official screenshots were used as structural references; their product-specific green palette was intentionally not copied.

### Follow-up Polish

- Investigate the repeated React script-tag console diagnostic separately; it did not produce a page error or affect this screen.

final result: passed

## Persona Selector Redesign QA

### Target

- Source visual truth: `/var/folders/c4/50vh622s75782gcbmt8yrhv00000gn/T/codex-clipboard-0e626695-554d-4525-b5a0-37eb7bcf25c1.png` (2048 × 1156 px; selector open, content type labels visible, keyword tags on a separate row).
- Target state: Studio content analytics page with the persona selector open.

### Verification

- Component tests verify that content types render as accessible icons without visible labels and keyword tags share their row.
- Visible Studio terminology uses “人设”; API and internal project identifiers retain their existing names.
- Automated browser capture was unavailable, so rendered visual comparison and responsive inspection remain unverified.

### Findings

- No actionable issues found in the implementation review or automated tests. Visual verification remains blocked pending a matching authenticated capture.

final result: blocked
