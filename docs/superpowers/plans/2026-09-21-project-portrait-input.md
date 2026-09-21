# Project portrait input implementation plan

**Goal:** Always supply the configured WeChat project portrait to manual, AI-entry, and scheduled article tasks. The Agent decides whether the cover should use it.

**Architecture:** Retain the portrait asset in the frozen project snapshot. Materialize it independently at `.anban-creator/project-portrait-reference.png` and expose `resolved_profile.project_portrait_reference_path`. Keep direct task references and project style references separate. Remove the task/plan opt-in field and UI switches. Image tools resolve the portrait through the task snapshot and enforce asset ownership and purpose.

**Tech stack:** Go services/bootstrap/MCP capability services; React forms; canonical Harness Agent Pack and cover Skill.

**Spec:** User instruction in this task: configured references are always input parameters; Agent decides actual use. Existing tasks use frozen snapshots; scheduled tasks snapshot the project when created.

- [x] Add failing coverage for manual/plan portrait input, bootstrap/profile, image-reference authorization and optional Studio input presentation.
- [x] Implement snapshot-backed portrait resolution and materialization; remove opt-in request/model fields and the AI-entry-only workaround.
- [x] Replace Studio switches and mandatory capability blockers with automatic-input descriptions; update form/clone contracts.
- [x] Update canonical Article Agents and cover Skill: record availability, selected path, use decision and reason; enforce identity only after selection. Probe Agent behavior before/after.
- [x] Bump both plugin versions and generate native Agent assets.
- [x] Run targeted regression tests, MCP tests, full Go tests/build, Studio tests/build and Agent Pack drift check. Review only this task's diff and preserve concurrent edits.

## Verification

- Regression tests reproduced the missing portrait input for manual, scheduled and AI-entry tasks; the new input is now exposed by profile and materialized from the frozen snapshot even without cover generation.
- Selected portraits reach the image provider; unselected portraits do not. Foreign assets, wrong asset purposes and missing snapshot references are rejected.
- Agent instruction probes covered author stories, technical diagrams, explicit likeness requests with unsupported reference generation, product-plus-person references, and explicit no-person requests. The final instructions distinguish availability from selection and preserve identity gates only when selected.
- Independent review found no actionable issues in this change.
- Full Go tests and Server build passed. Studio: 103 test files / 889 tests passed; production build passed. MCP tests, Agent Pack drift check and the five DSH publication-contract tests passed.
- Changes are local; no deployment or image-generation request was performed.

Pre-merge review verified this change in an isolated checkout without concurrent publication changes. A combined portrait-plus-product generation regression also passed, checking both reference bytes and order.
