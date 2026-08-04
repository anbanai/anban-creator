# Composer Parameter Density Design

## Goal

Rename the shared composer control from `创作设置` to `创作参数` and keep the common parameter catalog fully visible without internal scrolling.

## Layout Rules

- In shared task composers, show execution profiles as one horizontal three-column row.
- Keep `ExecutionProfileSelector` vertical by default so non-composer surfaces such as bulk clone retain their existing layout.
- Keep image capabilities vertical because each option contains a name, description, availability, and per-image price.
- Place short choice sets in compact horizontal rows with the section label on the left and controls on the right.
- Horizontal rows include image ratio, Designer size, quality, output format, and quantity.
- Keep the shared task-composer image ratio on one line, including at the 390-pixel mobile viewport.
- Do not introduce tabs, nested popovers, collapsed advanced sections, or hidden selectable options and prices.

## Surface Contract

- Homepage, task creation, and plan creation use the shared `创作参数` trigger and dialog name.
- Designer uses the same `创作参数` trigger and dialog name.
- Task quantity remains labeled `任务数量`; Designer quantity remains labeled `图片数量`.
- Workflows without image parameters or quantity omit those rows entirely.

## Responsive Behavior

- The shared task popover is up to 48rem wide, matching the homepage prompt input at the common desktop size.
- Desktop profile cards keep their name and tier on one row and their description and price on one row.
- Mobile keeps three profile cards in one row. Available-profile descriptions may be hidden below the `sm` breakpoint, while tier, price, multiplier, disabled reason, and availability remain visible.
- The existing available-height constraint remains as a safety boundary, but it must not activate scrolling for the supported catalog at 1440x768, the default 1280x720 window, or 390x844.
- At all acceptance viewports, the popover must satisfy `scrollHeight === clientHeight` and `scrollWidth === clientWidth`.
- Dynamic labels, disabled states, prices, and quantity changes must not resize or overlap neighboring controls.

## Accessibility

- Update trigger `aria-label`, popover title, and dialog accessible name to `创作参数`.
- Preserve existing group labels such as `图片比例`, `图像能力`, `尺寸`, `质量`, and `输出格式`.
- Preserve keyboard behavior from the existing Popover and ToggleGroup components.

## Verification

- Component tests assert the new `创作参数` accessible names.
- Layout tests assert the composer-only horizontal execution mode, the single-line ratio row, and the wider popover.
- Existing interaction tests continue to prove profile, capability, ratio, format, and quantity changes.
- Browser QA measures the homepage, task-create, and plan-create popovers rather than relying on screenshots alone.
- Run targeted tests, the full Studio test suite, the production build, and browser QA at desktop and mobile viewport sizes.
