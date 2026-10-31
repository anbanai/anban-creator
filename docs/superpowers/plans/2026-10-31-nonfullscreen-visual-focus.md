# Non-Fullscreen Visual Focus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make both talking-head video Agents place the newest or most important non-fullscreen visual material in a lower-middle golden-focus zone, demote earlier material as new material arrives, never cover the speaker's head in any state, and keep the speaker present through a circular avatar inset when an asset goes full-frame.

**Architecture:** Put the normalized geometry, focus hierarchy, face-protection rule, avatar-inset treatment, collision rules, and transition state machine in one small shared Skill so the two Agents do not drift. `viral-ip-video-editor` owns the editorial decision about which asset is primary and why; `talking-head-montage` owns translating that decision into OpenMontage composition data and verifying the rendered sample. The existing Montage adapter schema remains unchanged; the new fields live in the existing visual-plan artifacts.

**Tech Stack:** Markdown Agent/Skill contracts, Agent Pack YAML/TOML generation, OpenMontage hybrid pipeline, JSON visual-plan artifacts, existing agent-pack and DSH validation commands.

**Spec:** User requests in the conversation: (1) non-fullscreen material should prioritize the lower-middle visual center, with new material taking the primary position and earlier material demoted; (2) non-fullscreen material must never cover the head in any state; (3) when material goes full-frame, consider circling the speaker's avatar and placing it in a suitable position.

## Global Constraints

- Preserve the speaker's actual meaning, evidence, conditions, chronology, stance, numbers, names, negations, and disclaimers.
- **Hard stop:** non-fullscreen material must not occlude the speaker's head or face in any state, including entrance, exit, and transition frames. This rule has no soft fallback.
- Keep the speaker visible for trust-building and key claims; never cover hands, product, key action, captions, or platform UI danger zones.
- Treat the golden position as a planning guide, not an unconditional fixed coordinate; collision avoidance and semantic clarity take priority over exact coordinates.
- Keep the existing `output/`, OpenMontage adapter, provider registry, failure contracts, review gates, and delivery contracts unchanged.
- Do not add ad hoc fields to the Montage adapter manifest; extend only the existing visual-plan and review evidence contracts.
- Keep the Agent Pack source of truth and generated Claude/Codex surfaces synchronized; bump both native plugin manifests with a patch version and update the changelog.
- Do not overwrite unrelated user changes in the current worktree; implementation runs in an isolated worktree.

## Review Focus

- Does the newest or most important asset visibly receive the strongest position and scale?
- Is the primary anchor near the lower golden line while staying clear of the speaker's head and the subtitle band?
- When a new asset appears, do earlier assets demote rather than compete at equal visual weight?
- Does every frame, including transitions, keep the head fully uncovered?
- When an asset goes full-frame, is the speaker still present as a readable circular avatar without covering the asset's own key content?
- Can every fallback, corner choice, and collision decision be explained in the visual plan and review evidence?
- Are the same rules present and behaviorally consistent in both Agents and both Claude/Codex generated surfaces?

## File Map

- `harness/skills/talking-head-visual-focus/SKILL.md`: shared routing entrypoint and workflow for non-fullscreen visual hierarchy, head protection, and the avatar inset.
- `harness/skills/talking-head-visual-focus/references/geometry.md`: normalized coordinates, golden-focus band, secondary slots, avatar inset geometry, collision fallback order, hierarchy rules, and visual-plan field guidance.
- `harness/skills/talking-head-visual-focus/tests/layout-cases.json`: compact cases covering one, two, and three-plus assets, head collision, caption collision, over-capacity demotion, and full-frame avatar placement.
- `harness/packs/viral-ip-video-editor/agent.claude.md`: canonical viral-IP orchestration integration at editorial planning and review.
- `harness/packs/viral-ip-video-editor/agent.codex.toml`: Codex-specific loading and execution reminder for the shared layout Skill.
- `harness/packs/viral-ip-video-editor/agent-pack.yaml`: shared Skill dependency declaration and Pack version.
- `harness/packs/talking-head-montage/agent.claude.md`: canonical OpenMontage execution and visual-QA integration.
- `harness/packs/talking-head-montage/agent.codex.toml`: Codex-specific loading and execution reminder for the shared layout Skill.
- `harness/packs/talking-head-montage/agent-pack.yaml`: shared Skill dependency declaration and Pack version.
- `harness/agents/viral-ip-video-editor.md`, `harness/agents/viral-ip-video-editor.toml`: generated native Agent surfaces; regenerate, do not hand-edit.
- `harness/agents/talking-head-montage.md`, `harness/agents/talking-head-montage.toml`: generated native Agent surfaces; regenerate, do not hand-edit.
- `harness/.claude-plugin/plugin.json`, `harness/.codex-plugin/plugin.json`: synchronized patch version bump.
- `harness/CHANGELOG.md`: release note for the visual-focus contract.
- `harness/agent-pack-catalog.json`, `harness/agent-pack-catalog.generated.json`: generated catalog outputs refreshed by the existing generator.

## Normalized Layout Contract

Use normalized frame coordinates with `(0, 0)` at the top-left and `(1, 1)` at the bottom-right.

- `primary_focus.anchor_y`: default `0.618`, the lower golden-section line measured from the top.
- `primary_focus.anchor_y_range`: default `0.56–0.68`; choose the nearest valid point inside this band after head, hand, product, action, caption, and platform-safe-area checks.
- `primary_focus.anchor_x`: default `0.5`; use `0.382` or `0.618` when look room, face placement, or visual balance requires a lateral golden-section node.
- `primary_focus.priority`: the newest or semantically most important material gets `primary`; do not choose primary by asset type alone.
- Subtitle placement remains governed by the existing `0.75–0.82` default band and its bottom safe-area rule; the primary material must not be placed behind it.
- Secondary positions prefer an upper supporting slot first. A lower supporting slot is allowed only when it clears subtitles, platform UI, and the head box. If the frame cannot hold all secondary assets legibly, older assets reduce scale, reduce emphasis, or exit, with the reason recorded.
- Primary and secondary assets must have visibly different hierarchy: the primary gets the clearest scale, contrast, motion, and z-order; secondaries are supporting context, not co-equal panels.
- The layout must preserve a clear first focal point within one second and must not create two competing focal points without an editorial reason.

## Head Protection

- `speaker_protection.face_box` and `speaker_protection.head_box` are recorded per shot in normalized coordinates, padded by 2–4% of frame width.
- No non-fullscreen element may overlap the head box in any state. Animations are checked across the full motion path, not only at start and end.
- On collision, resolve in this order: choose the nearest valid slot inside or near the golden band, then reduce scale, then re-crop or reform the material, then defer the material to a stretch where the head is outside the frame. Never accept partial overlap.
- When the speaker is intentionally off-frame or the shot is a deliberate cutaway with no head in frame, record that fact and the rule becomes non-binding for that time range.

## Full-Frame Material and the Avatar Inset

When material covers the full frame and the speaker's presence still matters, keep the speaker present as a synchronized circular inset rather than losing them:

- Use the live source crop at the same timecode so lip sync continues; do not use a still frame.
- Shape is a circle with a 2–4% width outline ring and subtle shadow for separation; if the capability only supports a rectangle, use rounded corners of at least 12% radius.
- Diameter is `0.14–0.20` of frame width, never above `0.22`.
- Corner priority, by circle center: top-left `(0.16, 0.14)`, top-right `(0.82, 0.14)`, bottom-left `(0.18, 0.66)`, bottom-right `(0.82, 0.66)`. Keep at least 4% margin from every edge and clear the subtitle band, platform side rails, and the asset's own key subject, text, numbers, and labels.
- Pick one corner per contiguous full-frame segment and keep it stable; do not hop between corners mid-segment. Reposition only at a segment boundary with a short coordinated move.
- The avatar is never the primary focal point and must not out-contrast the asset's key content. If no corner clears the asset's essential content, do not force full-frame: return to the non-fullscreen layout instead.

## Focus State Machine

1. **One asset:** place it at the primary golden-focus position unless collision checks require a documented nearest valid fallback.
2. **Second asset enters:** place the new asset at primary; move the first asset to the upper secondary slot, or to a safe lower/side slot when the upper slot is unavailable.
3. **Third asset enters:** place the third asset at primary; move the previous primary to the first secondary slot and the older asset to the second secondary slot. Keep the newest asset visually dominant.
4. **More than three assets:** keep at most the number of secondary assets that remain legible and non-competing; demote or exit the oldest assets with a recorded reason rather than shrinking the entire composition into noise.
5. **Transition:** use a coordinated demotion/entrance transition. The old primary must not remain at full emphasis while the new primary arrives. Duration and easing remain content- and tool-dependent, but the visual plan must name the entrance, demotion, and exit behavior, and every transition frame must still honor head protection.

Each visual-plan item records `focus_role`, `focus_rank`, `anchor`, `safe_area_decision`, `collision_targets`, `scale_tier`, `z_order`, `transition_in`, `transition_out`, and `demotion_reason` where applicable. Full-frame stretches additionally record the avatar inset decision.

## Implementation Tasks

### Task 1: Add the shared visual-layout Skill

- [ ] Write the failing layout cases in `tests/layout-cases.json` for one asset, second-asset promotion, third-asset promotion, head collision, caption collision, over-capacity demotion, and full-frame avatar placement.
- [ ] Write `SKILL.md` with only routing, inputs, outputs, the focus-state workflow, head protection, the avatar inset, and a direct reference to the geometry document.
- [ ] Write `references/geometry.md` with the normalized coordinate contract, golden-focus band, secondary slot selection, avatar geometry, collision fallback order, hierarchy rules, and visual-plan field guidance.
- [ ] Verify the Skill is host-neutral, does not call MCP, does not own publication state, and keeps detailed geometry in the reference file.

### Task 2: Integrate editorial planning into viral-ip-video-editor

- [ ] Add the shared Skill to the Pack dependency list and both native loading surfaces.
- [ ] At visual planning time, require the Agent to classify every non-fullscreen `video_insert`, `image_insert`, and `motion_graphic` by semantic importance, record the head box, and assign the focus state for each time range.
- [ ] Require `visual_plan.json` to record primary/secondary roles, normalized anchors, head-safe decisions, collision decisions, scale hierarchy, promotion/demotion transitions, and avatar-inset choices without changing the Montage adapter schema.
- [ ] At Plan Review and Final Review, reject plans or renders where a newer primary is visually weaker than an older asset, where the primary is trapped away from the golden band without a recorded reason, where any frame covers the head, or where assets cover protected speaker/caption regions.

### Task 3: Integrate execution and rendered QA into talking-head-montage

- [ ] Add the shared Skill to the Pack dependency list and both native loading surfaces.
- [ ] Before image/graphic composition, translate the editorial focus state into OpenMontage composition inputs while preserving the declared asset classes and source evidence.
- [ ] Implement the full-frame case as a synchronized circular speaker inset and verify it does not cover the asset's key content.
- [ ] During motion-sample review, inspect the actual frame sequence for the one/two/three-asset transitions, lower-golden primary placement, secondary hierarchy, head clearance on every frame, caption clearance, and first-focal-point timing.
- [ ] Record the measured or frame-reviewed result and any fallback reason in `motion-review.json` and the final quality evidence; rerender when the rendered hierarchy disagrees with the plan.

### Task 4: Regenerate distribution surfaces and version metadata

- [ ] Run the existing Agent Pack generator from the canonical Pack sources.
- [ ] Run the Agent Pack consistency check and confirm the generated root Agent files contain the shared Skill dependency and new rules.
- [ ] Bump both native plugin manifests with a patch version, synchronize the corresponding Pack versions as required by the repository, and add a concise `CHANGELOG.md` entry.
- [ ] Confirm no generated catalog or preset drift remains and do not edit generated outputs by hand.

### Task 5: Verify behavior and contracts

- [ ] Run the focused contract checks for the two Agent Packs and the new Skill cases.
- [ ] Run `make agent-pack-check` and the repository's harness checks.
- [ ] Run the relevant server MCP/agent-pack tests, then the full required suite for the changed surface.
- [ ] Review the final diff for duplicate or conflicting golden-position rules, unchanged failure/publication boundaries, and preserved unrelated worktree changes.

## Acceptance Examples

- A single non-fullscreen product/demo asset is centered around the lower golden band and its head-box check passes.
- When a second asset appears, it occupies the lower-golden primary position immediately while the first asset moves to a visibly smaller upper supporting position.
- When a third asset appears, it becomes primary, the previous primary becomes secondary one, and the oldest asset becomes secondary two; the three do not have equal scale or contrast.
- No frame in any transition covers any part of the speaker's head; a planned slot that would have done so is rejected and the nearest safe slot is used with the reason recorded.
- A full-frame asset keeps the speaker present as a stable circular inset in a clear corner, and the asset's own title or labels stay unobstructed.
- If no corner clears the asset's essential content, the Agent abandons full-frame presentation instead of covering the head or the asset.
- A rendered sample that leaves the older asset at equal prominence, hides the new asset behind subtitles, or creates competing focal points fails motion review and is rerendered.

## Suggested Execution Method

This is a small but cross-surface contract change spanning a shared Skill, two Agent Packs, generated native surfaces, and release metadata. Use `superpowers:subagent-driven-development` if implementation is delegated in independent tasks; otherwise use `superpowers:executing-plans` task-by-task with a fresh verification after each task. Implementation runs in an isolated Git worktree because the main checkout contains unrelated server and plan changes.
