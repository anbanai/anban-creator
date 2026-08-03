# Composer Parameter Simplification Design

## Goal

Unify the homepage, task creation, plan creation, and Designer prompt composers around two visible controls: project selection and `创作设置`.

## Project Selection

- Show the real project name as the primary label.
- Remove repeated type suffixes such as `公众号项目`, `种草笔记项目`, and `朋友圈项目` from triggers and project options.
- Keep the complete project description when one exists.
- Group projects by platform using the short platform label and the existing platform icon/color system.
- Apply the selected platform color to the compact trigger so its type remains recognizable when the menu is closed.

## 创作设置

- Replace the separate execution profile, image settings, and quantity controls with one `创作设置` popover.
- Render execution profiles vertically, matching the existing vertical image-capability selection pattern.
- Keep image ratio choices compact and image capabilities vertical.
- Keep finite quantity visible and editable inside the popover.
- Use `任务数量` for task creation and `图片数量` for Designer generation.
- Omit sections that do not apply to the selected project or workflow.

## Surface Contract

- Homepage: project plus `创作设置`; the popover contains execution profile, applicable image settings, and task quantity.
- Task creation: same structure and labels as the homepage.
- Plan creation: project plus `创作设置`; the popover contains execution profile and applicable image settings.
- Designer: project plus `创作设置`; the popover contains image capability, size, quality, output format, and image quantity.

## Accessibility And Responsive Behavior

- Preserve accessible labels for the selected project and parameter summary.
- Use the existing Base UI `Popover`, `Combobox`, `ToggleGroup`, and `QuantityStepper` components.
- Constrain popovers to the available viewport height. Keep creation settings in one column; show project groups in two columns on desktop and one column on mobile.
- Keep both composer controls stable at mobile and desktop widths without nested popovers.

## Verification

- Component tests cover project grouping/color, removal of repeated type suffixes, vertical execution profiles, and combined parameter behavior.
- Page tests assert that each composer exposes exactly the intended two top-level controls.
- Run the Studio targeted tests, full test suite, production build, and desktop/mobile browser interaction checks.
