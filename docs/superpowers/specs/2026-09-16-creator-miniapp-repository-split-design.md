# Creator Miniapp Repository Split Design

## Goal

Move the tracked `miniapp/` source from `anbanai/anban-creator` into a private
`anbanai/creator-miniapp` repository. Preserve the Miniapp-specific Git history,
keep the Miniapp independently buildable, and remove the Miniapp source from the
main repository so it no longer expands the default coding context.

## Repository Creation And History

- Create `anbanai/creator-miniapp` as a private GitHub repository with `main` as
  its default branch.
- Derive its initial history from the current repository using a path-filtered
  split of `miniapp/`. Relevant historical commits remain available, although
  commit hashes are rewritten because `miniapp/` becomes the new repository
  root.
- Materialize the new checkout at `../creator-miniapp`.
- Do not copy ignored build output or dependencies such as `node_modules/` and
  `dist/`; the existing Miniapp `.gitignore` remains authoritative.

## New Repository Shape

The contents currently below `miniapp/` become the root of
`creator-miniapp`. Existing package scripts and lockfiles remain intact. Add a
short repository README only if the split source does not already contain one,
covering installation, test, type-check, and WeChat Mini Program build commands.

The new repository owns Miniapp-only tests and future Miniapp CI. It consumes
the Anban Server through its public API contract and must not require a sibling
checkout of the main repository to install, test, or build.

## Main Repository Changes

- Remove the tracked `miniapp/` tree.
- Remove stale directory entries and build-context exclusions from root
  documentation and `.dockerignore`.
- Replace documentation that implies the Miniapp is maintained in this
  monorepo with a pointer to `anbanai/creator-miniapp`.
- Remove Server tests that open Miniapp source files directly. Retain the
  reusable connection-guide scanner tests for Server-owned behavior; the
  Miniapp repository will own checks against its own guide pages.
- Keep Server API support for WeChat Mini Program authentication and all other
  Miniapp-facing contracts. This split changes source ownership, not product
  behavior or API availability.

## Verification

In `creator-miniapp`:

1. Confirm history includes the commits that originally changed `miniapp/`.
2. Run `npm ci` using the retained `package-lock.json`.
3. Run `npm test`, `npm run type-check`, and `npm run build:mp-weixin`.
4. Confirm the private GitHub remote and pushed `main` branch.

In `anban-creator`:

1. Confirm no tracked files remain under `miniapp/`.
2. Run the affected Server package tests.
3. Run `go test ./...` to detect remaining monorepo assumptions.
4. Search tracked files for stale `miniapp/` path references and retain only
   intentional historical or product terminology.

## Rollback

The split repository is additive and preserves source history. Before the main
repository removal is merged, rollback is simply deleting the new private
repository. Afterward, the original tree can be restored from the parent commit
without relying on the split repository.
