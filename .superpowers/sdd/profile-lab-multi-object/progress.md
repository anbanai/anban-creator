# SDD ledger — plan: user-approved Profile Lab multi-object plan

## Plan scan

| Task | Files/interfaces | Check | Ruling |
|---|---|---|---|
| Server contract | Project persistence, create API, profile task input and result application | Studio consumes `agent_ids` plus `profile_setup`; legacy projects keep derived IDs | Server owns validation, persistence, task admission, atomic profile replacement and history |
| Studio UX | Project list, full-page create/edit route, typed API and tests | UI sends the Server contract and polls existing profile status | Create and edit use the same workspace; existing project settings remain accessible |
| Harness profile analysis | Pack/Agent input schema and instructions | Agent sees account sources, answers, samples and current confirmed profile | Agent proposes a full profile result; Server applies atomically without an approval screen |
| Integration | Routes, migration, Agent use-chain tests and verification | All six Agents consume the same confirmed/applied project profile | Keep source tags and preference hard constraints; do not use global memory |

## Rulings

- Ruling: implement in a clean feature worktree based on the current committed branch — the source checkout has unrelated user changes that overlap target files; costs an explicit later integration step.
- Ruling: `Project.Platform` remains a compatibility account context while persisted `agent_ids` is authoritative for selected outputs — this supports arbitrary combinations without changing task identity semantics.
- Ruling: current profile remains active during analysis; successful Server result application replaces it atomically and records the new revision, while failure only changes lifecycle/error fields — this follows the approved auto-apply behavior and preserves rollback through revisions.

## Progress

- Task 1: implemented `690038a9`, independent review pending — Server project capabilities and profile setup persistence/API.
- Task 2: complete — Studio full-page Profile Lab and project profile management.
- Task 3: complete — Harness profile-analysis input and application semantics, including frozen input and required result artifacts.
- Task 4: complete — integrated review, tests, visual verification, and local main merge.

## Verification and review evidence

- Local Playwright mock QA passed ten scenarios: required name, all six outputs, optional multi-platform accounts, automatic application, manual dimension edit, history restore, failure preserves profile, retry, mobile overflow, skip and start. No page errors. `/tmp/profile-lab-browser-qa.log`; screenshots `/tmp/profile-lab-qa/`. This does not assert live model/crawler execution.
- Harness verification: 460 passed / 1 Windows-only skipped; audit, typecheck, build, pack generation and pack checks passed.
- Contract review found required result artifact absent (Important); Claude Skill preload documentation mismatch and question transport mapping (Minor). All accepted for correction before merge.
- Ruling: document distributed Pack dependencies separately from eagerly injected Claude Skills, retaining existing deferred workflows; eagerly inject the short shared profile rules into all six consumers — prevents unrelated article startup regressions while making profile rules deterministic. Cost if wrong: future dependency changes could omit startup rules; explicit native contract tests mitigate that.
- Studio review fixes: numeric profile fields now preserve explicit null clears; create responses cannot navigate after the user leaves the route.
- Browser QA: local Playwright mock API covered desktop/mobile creation, all six object combinations, automatic apply, retry, failure preservation, editing, and version restore; no page errors or mobile horizontal overflow.
