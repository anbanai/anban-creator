# Creator MCP Reverse Proxy Design

## Goal

Serve the Anban Creator MCP endpoint at
`https://creator.anbanai.com/mcp` by routing that exact path through the Studio
Nginx container to the existing Go Server upstream.

## Current State

The Studio Nginx container serves the SPA at `/` and proxies `/api/` and `/ws`
to `${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT}`. Because `/mcp` has no
dedicated location, the SPA fallback currently returns `index.html` for MCP
requests. The Go Server already handles `/mcp` and does not need a route change.

## Design

Add an exact-match `location = /mcp` block to
`studio/default.conf.template`. It will:

- proxy to the existing configurable backend upstream without rewriting the
  request path;
- use HTTP/1.1 for MCP Streamable HTTP;
- preserve the original host and forwarding headers;
- leave the `Authorization` header untouched so Nginx forwards it normally;
- disable proxy buffering and caching so SSE responses are delivered live;
- use one-hour read and send timeouts, matching the existing long-running API
  behavior and exceeding the plugin's 15-minute operation timeout.

An exact match keeps `/mcp` out of the SPA fallback without claiming ownership
of unrelated paths such as `/mcp-docs` or `/mcp/anything`.

## Testing

Extend the Studio Nginx proxy contract test before changing the template. The
test will require the exact `/mcp` location, the shared backend upstream,
HTTP/1.1, disabled buffering and caching, and long read/send timeouts.

Run the targeted Studio test, then the full Studio test and production build.
The Go router test remains the server-side proof that `/mcp` is registered.

## Deployment And Verification

The change takes effect only after the Studio image is rebuilt and deployed to
the workload serving `creator.anbanai.com`. After deployment:

1. POST an MCP `initialize` request without a bearer token and require `401`.
2. Confirm the response is not `text/html` and no longer contains the SPA.
3. Repeat with a valid API key and require a successful MCP JSON/SSE response.

The current workspace has no `kubectl` binary or cluster context, so repository
verification cannot by itself prove that the production image was deployed.

## Non-Goals

- Do not change the Go Server MCP route.
- Do not restore or modify `api.creator.anbanai.com`.
- Do not change plugin endpoint declarations in this step.
- Do not add a second public MCP path or a configurable user-facing endpoint.
