# Studio AI Assistant Navigation Design

## Goal

Remove the redundant `首页` section heading and `首页` navigation item pairing from the Studio sidebar. Present the root route as one direct, high-priority assistant entry.

## Design

- Keep the root route at `/` and preserve its exact-match behavior.
- Rename the root navigation item from `首页` to `AI助手`.
- Replace the dashboard icon with Lucide's `Sparkles` icon.
- Render this item directly at the top of the main navigation, without a section heading.
- Keep the Dashboard page title and route behavior unchanged; this change only affects navigation presentation and naming.
- Preserve collapsed-sidebar tooltips, active-state styling, mobile menu closing, command-palette discovery, and derived navigation collections through the existing `NavItem` contract.

## Alternatives Considered

1. Make the product logo link to `/` and remove the root menu item. This hides an important workflow behind branding and is less discoverable.
2. Keep the `首页` section heading and rename only its child. This removes the exact duplicate wording but leaves a one-item section with unnecessary hierarchy.
3. Render `AI助手` as a direct top-level item. This is the selected approach because it is explicit, compact, and consistent with the assistant-first product direction.

## Testing

- Update navigation unit tests to expect `AI助手` at `/`.
- Update sidebar tests to assert a single `AI助手` navigation link and no `首页` navigation label.
- Run the targeted Studio tests, then the full Studio test suite and production build.
