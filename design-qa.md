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
