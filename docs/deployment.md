# Deployment contracts

## Studio blue/green releases

Studio is a content hashed SPA. A release is ready to receive traffic only after its image is healthy and its static assets have been published. The Studio Service must select both labels below:

```yaml
selector:
  app: ${micro_service_name}
  version: ${version_switch}
```

The selector is the cutover switch. Keep it pointed at the old version until the new Deployment is ready, then update it once. Do not run a Service selector that matches `app` alone while two releases are live.

## HTML and assets

`index.html` is the release pointer and must be fetched again after a deployment. The nginx template sends `Cache-Control: no-store, must-revalidate` for it. Vite's hashed JavaScript and CSS files can use `Cache-Control: public, immutable` for one year.

For multi-release production deployments, publish each build's static files under a release-specific prefix in shared object storage or a CDN, for example:

```text
/releases/2026-09-24.abc123/index.html
/releases/2026-09-24.abc123/assets/LoginPage-BfBMpo2K.js
```

The active HTML pointer may be switched atomically, but the previous release's assets must remain readable for the maximum browser session and rollback window. Delete old prefixes only after that retention window. A container-local `/usr/share/nginx/html/assets` directory cannot satisfy this requirement once traffic has moved to a different image.

Local Docker and single-release environments may continue serving the image-local files. Their HTML and asset cache headers should still follow the nginx template so behavior matches production as closely as possible.
