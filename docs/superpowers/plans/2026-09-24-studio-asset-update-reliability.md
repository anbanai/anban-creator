# Studio Asset Update Reliability Implementation Plan

> Historical initial plan. The review found its success-clears-guard behavior unsafe and documentation-only retention insufficient. The final implementation supersedes those steps: verified HTML entry change, durable retry guard, actual inherited image assets, guarded candidate deployment, and explicit reload fallbacks. The current release contract is in `docs/deployment.md`; the original checkboxes below describe the initial proposal, not outstanding work.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent Studio blue/green releases from serving mismatched HTML and lazy route chunks, and recover once from transient stale chunk failures.

**Architecture:** Route-level imports use a small `lazyWithRecovery` helper that reloads the current page at most once per session after a module-fetch error. The Kubernetes Service selects the active release label so a request stays on one version. Nginx and deployment docs define the HTML revalidation and shared release asset retention contract.

**Tech Stack:** React 19, TypeScript, Vitest, Vite, nginx, Kubernetes YAML.

**Spec:** `docs/superpowers/specs/2026-09-24-studio-asset-update-reliability-design.md`

## Global Constraints

- Dynamic import recovery may reload at most once per browser session for the current failure.
- HTML must be revalidated; hashed assets remain immutable.
- The Service selector must match exactly one `version_switch` release.
- Do not add a Service Worker or a new CDN dependency.
- Preserve unrelated existing working-tree changes.

---

### Task 1: Add the lazy import recovery helper

**Files:**
- Create: `studio/src/lib/lazy-with-recovery.ts`
- Test: `studio/src/lib/lazy-with-recovery.test.ts`

**Interfaces:**
- Produces `lazyWithRecovery<T>(loader: () => Promise<{ default: T }>, options?: { storage?: Storage; reload?: () => void; onFailure?: (error: unknown) => void }): React.LazyExoticComponent<T>`.
- Uses session key `anban:studio:chunk-reload` and only handles errors whose message indicates a failed dynamic import or chunk load.

- [ ] **Step 1: Write failing tests** for classifying chunk errors, reloading once with a storage marker, rethrowing on a second failure, and clearing the marker after a successful import.
- [ ] **Step 2: Run `cd studio && bun run test -- src/lib/lazy-with-recovery.test.ts` and verify the missing helper causes the expected failure.
- [ ] **Step 3: Implement the helper with React `lazy`, session storage access guarded for browser availability, and dependency injection for deterministic tests.
- [ ] **Step 4: Run the focused test and verify it passes.
- [ ] **Step 5: Commit only the helper and test with `feat(studio): recover from stale lazy chunks`.

### Task 2: Use recovery for route-level imports

**Files:**
- Modify: `studio/src/App.tsx`

**Interfaces:**
- Replaces the direct `React.lazy` route declarations with `lazyWithRecovery` while preserving component types and route behavior.

- [ ] **Step 1: Add an assertion to the helper test that successful loading clears the marker.
- [ ] **Step 2: Run the focused helper test to verify the assertion fails before wiring the implementation if needed.
- [ ] **Step 3: Replace each route-level `React.lazy` call in `App.tsx` with `lazyWithRecovery` and keep the existing `LazyPage` boundary.
- [ ] **Step 4: Run `cd studio && bun run test -- src/lib/lazy-with-recovery.test.ts src/App.admin-routes.test.ts` and verify it passes.

### Task 3: Pin the active Kubernetes release

**Files:**
- Modify: `studio/Deployment.yaml`
- Modify: `studio/src/lib/nginx-proxy-config.test.ts`

**Interfaces:**
- The Service selector gains `version: ${version_switch}` and therefore routes only to Pods from the rendered active release.

- [ ] **Step 1: Add a failing contract assertion requiring the Service selector to include the rendered version label.
- [ ] **Step 2: Run `cd studio && bun run test -- src/lib/nginx-proxy-config.test.ts` and verify the assertion fails.
- [ ] **Step 3: Add the `version` selector to the Service manifest.
- [ ] **Step 4: Run the focused contract test and verify it passes.

### Task 4: Document cache and release retention contracts

**Files:**
- Modify: `studio/default.conf.template`
- Modify: `docs/deployment.md` (create if absent)

**Interfaces:**
- Nginx emits explicit `Cache-Control: no-store, must-revalidate` for HTML fallback responses and keeps one-year immutable caching for hashed assets.
- Deployment docs state that multi-release deployments publish assets under a release prefix to shared storage/CDN and retain the previous release through the rollback/session window.

- [ ] **Step 1: Extend the nginx contract test with HTML and asset cache assertions.
- [ ] **Step 2: Run the focused contract test and verify the new assertions fail.
- [ ] **Step 3: Add the nginx headers and the deployment contract documentation.
- [ ] **Step 4: Run the focused contract test and inspect the rendered nginx template for valid directive placement.

### Task 5: Full Studio verification

**Files:**
- No source changes expected.

- [ ] **Step 1: Run `cd studio && bun run test`.
- [ ] **Step 2: Run `cd studio && bun run build`.
- [ ] **Step 3: Inspect `git diff --check` and `git status --short` to ensure only intended files from this task are staged or committed; leave unrelated user changes untouched.
