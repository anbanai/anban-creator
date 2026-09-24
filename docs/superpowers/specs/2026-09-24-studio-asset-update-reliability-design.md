# Studio Asset Update Reliability Design

## Problem

Studio uses React lazy routes and Vite content hashed chunks. The nginx Service currently selects Pods by the shared `app` label while deployments carry a `version_switch` label. During a blue/green rollout, a request can receive `index.html` from one release and a lazy chunk from another release. Since each image contains only its own `/assets/` directory, the chunk can return 404 and Chromium reports `Failed to fetch dynamically imported module`. Long-lived browser sessions still reference old chunks. Immutable caching is correct for hashed files, but cannot replace retaining those files at the origin.

## Goals

- Ensure a Studio request is routed to one active release during a cutover.
- Keep previously published hashed assets available during a bounded rollback/session window.
- Recover once from a transient stale HTML or CDN race without an infinite reload loop.
- Keep HTML revalidation explicit and retain immutable caching for content-addressed assets.
- Make the contract testable in the repository and document deployment requirements.

## Design

### 1. Version-aware Service selection

The Studio Service selector will include `version: ${version_switch}`. The Deployment template already labels Pods with the same value. The deployment pipeline must render the Service with the active version and switch that selector only after the candidate Deployment is ready. The selector must never match both releases at once.

### 2. Release asset retention

**Review revision:** The implemented Docker path inherits the active image’s hashed `/assets/` directory before overlaying the new build. `studio/deploy.py` verifies inherited asset bytes before switching traffic. This is the available retention mechanism without introducing new infrastructure. The shared-origin architecture below remains a future migration, not a deployed capability. See `docs/deployment.md` for the executable update/rollback contract and propagation limits.


Build outputs must be published under a release-specific prefix (for example `/releases/<release-id>/assets/...`) to a shared origin or CDN before the HTML pointer is switched. The active HTML references the release prefix and is served with `Cache-Control: no-store, must-revalidate`. Hashed assets use `public, max-age=31536000, immutable`. The origin retains at least the previous release for the maximum expected browser session and rollback window; cleanup happens only after that window. A container-local directory alone is insufficient for this contract.

The existing Docker/nginx image remains valid for local development and single-release environments. Its configuration will explicitly document the shared-origin requirement for multi-release blue/green deployments.

### 3. Dynamic import recovery

Studio will wrap route-level lazy imports with a small helper. When a dynamic import rejects with a module-fetch/chunk-load error, the helper fetches uncached HTML and only reloads if the module entry changed. A verified durable session marker limits retries to once per target entry and at least five minutes between different targets. Successful imports do not clear the marker. Storage and network failures preserve the original error. ErrorBoundary offers an explicit document reload, and static HTML provides a bootstrap failure notice before React can run. This handles a short propagation race while preventing reload loops.

## Scope

- Pin both Deployment and Service selectors, add readiness, and gate candidate creation, asset verification and Service replacement in `studio/deploy.py`.
- Add a focused lazy import recovery helper and route usage in `studio/src/App.tsx`.
- Add unit tests for error classification, one-shot reload behavior, and the deployment selector/cache contract.
- Update `studio/default.conf.template` and deployment documentation with the release asset contract.

Out of scope: introducing a new CDN provider, changing the backend rollout controller, or adding a Service Worker. Those require infrastructure credentials and operational ownership outside this repository.

## Failure handling

- Candidate readiness failure: do not switch the Service selector.
- Missing old asset after the retention window: the one-shot reload may recover if the active HTML is available; otherwise the ErrorBoundary reports the failure.
- Repeated chunk failure in one session: stop reloading and show the existing error boundary.

## Verification

- Studio unit tests cover the helper and deployment contract.
- `cd studio && bun run test`
- `cd studio && bun run build`
- `cd server && go test ./...` is unchanged but remains the repository-level regression check when the full change is validated.
