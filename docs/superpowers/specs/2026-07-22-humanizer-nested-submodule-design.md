# Humanizer Nested Submodule Design

**Date**: 2026-07-22
**Status**: Approved

## Goal

Use the official `blader/humanizer` repository directly at
`plugins/skills/humanizer` without keeping a second mirrored `SKILL.md` or
installing Humanizer over the network while building an Agent image.

## Ownership Boundary

The approved Creator Skills migration makes `plugins/` a submodule backed by
`https://github.com/anbanai/creator-skills.git`. Humanizer therefore becomes a
nested submodule owned by the Creator Skills repository:

```text
anbanwriter
└── plugins -> anbanai/creator-skills
    └── skills/humanizer -> blader/humanizer
```

The Creator Skills repository records the Humanizer URL, `main` tracking
branch, and pinned gitlink in its `.gitmodules`. Anban Writer records only the
Creator Skills gitlink. The existing root-level `third_party/Humanizer`
submodule is removed.

## Runtime Distribution

Humanizer remains part of the Anban plugin's canonical Skill tree. The Agent
Dockerfiles continue to copy `plugins/` to `/anbanai/` and install the local
Anban plugin. They do not run `npx skills add`, install the standalone
Humanizer Claude plugin, or fetch Humanizer during the image build.

Claude discovers the Skill as `anban:humanizer`. Codex subagents continue to
load `__PLUGIN_ROOT__/skills/humanizer/SKILL.md`. Article, ecommerce, and
moments workflows keep their current Humanizer ownership; Seednote keeps its
compact built-in de-AI pass.

All source checkouts used for tests, packaging, or Docker builds must
initialize submodules recursively so both `plugins` and
`plugins/skills/humanizer` contain their pinned working trees.

## Update Flow

Humanizer updates are deliberate releases, not Docker build behavior. The
Creator Skills exposes a `make humanizer-update` target that:

1. Refuses to proceed when the nested Humanizer working tree is dirty.
2. Fetches `origin/main` in `skills/humanizer`.
3. Advances the nested gitlink to the fetched `origin/main` commit.
4. Prints the old and new commit IDs and requires review of the upstream diff.
5. Runs the Humanizer and plugin contract checks before the update is committed.

Because a checked-out submodule normally uses detached HEAD, the documented
workflow does not depend on running plain `git pull` inside the submodule.
Updating Humanizer requires committing the new nested gitlink in Creator
Skills, bumping both native plugin manifest versions, and updating the plugin
changelog. Anban Writer then commits the resulting outer `plugins` gitlink.

## Upstream Contents

The official repository is used without rewriting `SKILL.md`. Its version,
compatibility metadata, license, README, and native Humanizer plugin metadata
remain upstream-owned. Anban's auxiliary-document and frontmatter rules apply
to Anban-authored Skills; contract tests explicitly treat the Humanizer subtree
as an upstream repository boundary.

The Anban business workflows remain authoritative for autonomous execution,
information preservation, compliance ordering, and task-specific limits.
Those rules are not added to or patched into the upstream Humanizer Skill.

## Failure Handling

- A missing nested checkout fails tests and image builds instead of silently
  shipping an Anban plugin without Humanizer.
- A failed upstream fetch leaves both gitlinks unchanged.
- An unreviewed upstream commit is never selected dynamically by Docker.
- A Humanizer update is not complete until the Creator Skills remote contains
  the new nested gitlink and Anban Writer pins the corresponding Creator Skills
  commit.

## Verification

- Confirm Creator Skills records `skills/humanizer` as mode `160000` with the
  official repository URL and the intended upstream SHA.
- Confirm Anban Writer no longer declares `third_party/Humanizer`.
- Run Humanizer ownership, Agent preload, runtime policy, and unified plugin
  contract tests.
- Run strict Claude plugin validation and Codex plugin validation against the
  recursively initialized Creator Skills checkout.
- Build the affected Agent images and verify Claude's `system/init` inventory
  contains `anban:humanizer` for article and ecommerce tasks.
- Run the full Go test suite required by `AGENTS.md` before release.
