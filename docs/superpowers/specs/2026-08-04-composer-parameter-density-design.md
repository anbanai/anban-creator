# Composer Parameter Density Design

## Goal

Rename the shared composer control from `创作设置` to `创作参数` and reduce popover height without making long-form choices harder to compare.

## Layout Rules

- Keep execution profiles vertical because each option contains a name, availability, description, price, and multiplier.
- Keep image capabilities vertical because each option contains a name, description, availability, and per-image price.
- Place short choice sets in compact horizontal rows with the section label on the left and controls on the right.
- Horizontal rows include image ratio, Designer size, quality, output format, and quantity.
- Allow short controls to wrap within their row when the available width is insufficient.
- Do not introduce tabs, nested popovers, collapsed advanced sections, or hidden defaults.

## Surface Contract

- Homepage, task creation, and plan creation use the shared `创作参数` trigger and dialog name.
- Designer uses the same `创作参数` trigger and dialog name.
- Task quantity remains labeled `任务数量`; Designer quantity remains labeled `图片数量`.
- Workflows without image parameters or quantity omit those rows entirely.

## Responsive Behavior

- Desktop uses a compact label-and-controls row for short choices and should fit the common parameter set without scrolling at a 768-pixel viewport height.
- Mobile keeps long-form selectors vertical and lets compact rows wrap without horizontal overflow.
- The existing available-height constraint remains as a safety boundary for unusually small viewports or expanded capability catalogs.
- Dynamic labels, disabled states, prices, and quantity changes must not resize or overlap neighboring controls.

## Accessibility

- Update trigger `aria-label`, popover title, and dialog accessible name to `创作参数`.
- Preserve existing group labels such as `图片比例`, `图像能力`, `尺寸`, `质量`, and `输出格式`.
- Preserve keyboard behavior from the existing Popover and ToggleGroup components.

## Verification

- Component tests assert the new `创作参数` accessible names.
- Layout tests assert that long-form selectors remain vertical and compact sections use label-and-controls rows.
- Existing interaction tests continue to prove profile, capability, ratio, format, and quantity changes.
- Run targeted tests, the full Studio test suite, the production build, and browser QA at desktop and mobile viewport sizes.
