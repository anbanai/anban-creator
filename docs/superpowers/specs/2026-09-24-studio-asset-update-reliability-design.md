# Studio Asset Update Reliability Design

## Problem

Studio uses React lazy routes and Vite content hashed chunks. The nginx Service currently selects Pods by the shared `app` label while deployments carry a `version_switch` label. During a blue/green rollout, a request can receive `index.html` from one release and a lazy chunk from another release. Since each image contains only its own `/assets/` directory, the chunk can return 404 and Chromium reports `Failed to fetch dynamically imported module`. A one-year immutable cache makes stale HTML and mixed releases persist longer.

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

Build outputs must be published under a release-specific prefix (for example `/releases/<release-id>/assets/...`) to a shared origin or CDN before the HTML pointer is switched. The active HTML references the release prefix and is served with `Cache-Control: no-store, must-revalidate`. Hashed assets use `public, max-age=31536000, immutable`. The origin retains at least the previous release for the maximum expected browser session and rollback window; cleanup happens only after that window. A container-local directory alone is insufficient for this contract.

The existing Docker/nginx image remains valid for local development and single-release environments. Its configuration will explicitly document the shared-origin requirement for multi-release blue/green deployments.

### 3. Dynamic import recovery

Studio will wrap route-level lazy imports with a small helper. When a dynamic import rejects with a module-fetch/chunk-load error, the helper checks a session-scoped reload marker, records a diagnostic event, and reloads the current URL once. If the marker is already present, it rethrows so the existing ErrorBoundary can show a stable error page. Successful imports clear the marker. This handles a short propagation race while preventing reload loops.

## Scope

- Modify `studio/Deployment.yaml` Service selector.
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
