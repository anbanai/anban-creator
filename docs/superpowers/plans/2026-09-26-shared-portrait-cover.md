# Shared portrait cover implementation

**Goal:** Opt-in portrait covers for Article, Seednote, Montage and Hypit, with one maintained design skill.

**Approved design:** Platform entrypoints delegate selected portrait covers to `portrait-cover-design`; otherwise preserve ordinary platform cover workflows. User clarified that the common input contract may change; preserve core design and all style templates. Platform-specific routing and delivery stay outside the design method.

**Constraints:** Preserve server identity, frozen snapshots, atomic MCP capabilities and publication ownership. No production publishing. Portrait selection is explicit; unselected portraits are not used for covers. Missing required portraits fail clearly. Article cover-off takes precedence. No source/template duplication for portrait design.

## Tasks

- [x] Server: replace article-specific public option with `cover_use_portrait` / `CoverUsePortrait`; support article, seednote, montage, hypit; validate selected project portrait; preserve existing stored selections via migration; propagate create, plan, clone, retry, bootstrap. Emit `cover_portrait=required_project_portrait|disabled` for supported workflows. Preserve task/product and project style references independently. Add behavior tests.
- [x] Studio: common portrait cover control and project portrait uploads for the four platforms; update types, schemas, task/plan serialization, defaults, clone/reset behavior and explanatory text. Keep unrelated reference uploads intact. Test selection, disabled states and payloads.
- [x] Harness: rename video-cover-design to portrait-cover-design, generalize its input contract and preserve design/style reference templates. Add concise integration instructions for external platform bindings and strict portrait/title checks. Route four canonical packs and visual skills correctly; ordinary Montage cover uses a separate concise platform reference, Hypit keeps frame extraction when disabled. Generate native pack surfaces and catalog; bump both native manifests.
- [x] Verify: skill validator, preservation comparison, independent scenario review, pack generation/check, targeted MCP/Agent tests, full Go tests/build, Studio tests/build. Review final diff.

## Interface

`cover_use_portrait` is boolean, default false. Portrait source is the project's independent `portrait_reference_image`, frozen into task snapshots; it is never inferred from a product/style/task image. Runtime selection is `cover_portrait=required_project_portrait` or `disabled`. Portrait source path stays `.anban-creator/project-portrait-reference.png`. Platform entrypoints supply `$COVER_ASPECT_RATIO`; unchanged templates substitute their `$VIDEO_ASPECT_RATIO` placeholder with this effective cover ratio, preserve platform safe areas, supply exact title and pass the frozen portrait path explicitly through the integration contract. No materialized reference file is overwritten.

## Progress

- Initial diagnosis: Article rules default to no text and accept missing titles; video workflow mandates title rendering.
- Implementation complete on `codex/shared-portrait-cover` in repository and harness; original worktrees were clean. Changes remain uncommitted.
- All ten style templates, examples and LICENSE are byte-identical to the original video Skill (12 files). Common contract and platform integration were generalized with user approval.
- Four platforms share the same opt-in control and frozen portrait contract. Unselected workflows use ordinary covers; selected covers require both the supplied identity and readable title.
- Hypit uses normalized video preferences for selected cover ratios; source-sized videos resolve after inspection. Preflight checks supported ratios and portrait readability before paid Build. Manual/clone/plan lifecycle and actual image-service admission are covered.
- Independent forward tests and final review completed; reported routing/profile/ratio conflicts were corrected.
- Studio: 1,030 tests passed and build passed. Agent Runner: 218 tests passed, typecheck and build passed.
- Skill validation, byte-preservation comparison, pack consistency, workflow context budgets, DSH package/smoke checks, targeted Agent/MCP tests and whitespace checks passed. Both native plugin manifests are 4.2.11.
- Fresh Server build and full `go test -p 1 ./...` passed. Package concurrency was limited to reduce unrelated SQLite lock contention observed in earlier full-suite attempts.
- No paid media generation, publishing or deployment performed.


## Follow-up business flow review

User requested a second review of Studio upload entrypoints, task/plan selection and Agent instructions. Independent Studio and Server inspections found all four platforms wired through the canonical paths:

| Surface | Evidence | Result |
| --- | --- | --- |
| Project portrait upload | `ProjectsPage.tsx`, `project.go`, `ReferenceAssetService` | Article/Seednote/Montage/Hypit expose the independent upload; Server validates owner/purpose and persists it separately from style/task references. |
| Task selection | `PortraitCoverControl`, `TaskFormDialog`, `task-form.ts`, task handler/service | Explicit opt-in, false serialized, missing portrait cannot be enabled, Article cover-off clears it. |
| Plan selection/execution | `PlansPage`, plan handler/service/repository, `CreateFromPlan` | Create/edit save true/false; immediate and scheduled execution share task creation and freeze current project portrait. |
| Clone/resume | `task_retry.go`, shared cover lifecycle tests | Exact clone preserves selection, prompt and frozen identity even after project portrait removal; editable clone uses destination configuration; resume retains the existing task. |
| Agent prompt | `BuildUserPrompt`, Bootstrap, Runner, four canonical Agent packs | User brief retained with explicit `cover_portrait` control; each Agent routes enabled/disabled covers and consumes frozen portrait paths. |

One issue was found and fixed: Montage still labeled generic task material as a system portrait in the generated prompt. It now says `Video material reference`; Claude/Codex Montage instructions distinguish video material from the separately selected cover identity. Regression tests cover all four combinations of task material present/absent and portrait cover enabled/disabled, including preservation of the user's title request.

Additional regressions exercise selected Seednote/Montage/Hypit task submission, all four plan types toggling off and reopening, and all four exact clone paths with enabled/disabled selection. No paid generation or deployed-environment validation was performed.

Follow-up verification passed: full Server `go test -p 1 ./...` and build; fresh Agent/MCP suites; Studio 1,037 tests and build; pack consistency, workflow audit and both repository whitespace checks.
