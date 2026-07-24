# Creator MCP Reverse Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `https://creator.anbanai.com/mcp` reach the Go Server MCP handler instead of the Studio SPA fallback.

**Architecture:** Add one exact-match `/mcp` location to the Studio Nginx template and reuse the same environment-configured backend upstream as `/api/`. Preserve the path and request headers, disable response buffering for MCP Streamable HTTP, and retain long proxy timeouts.

**Tech Stack:** Nginx, React Studio container, Vitest, Go Fiber router, curl

---

### Task 1: Add The Tested MCP Proxy Route

**Files:**
- Modify: `studio/src/lib/nginx-proxy-config.test.ts`
- Modify: `studio/default.conf.template`

- [ ] **Step 1: Write the failing Nginx contract test**

Append this test inside the existing `describe` block in
`studio/src/lib/nginx-proxy-config.test.ts`:

```ts
  it('proxies the exact MCP endpoint to the backend as an unbuffered HTTP stream', () => {
    const template = readFileSync('default.conf.template', 'utf8')
    const mcpLocation = template.match(/location = \/mcp \{([\s\S]*?)\n    \}/)?.[1]

    expect(mcpLocation).toBeDefined()
    expect(mcpLocation).toContain(
      'proxy_pass ${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT};',
    )
    expect(mcpLocation).toContain('proxy_http_version 1.1;')
    expect(mcpLocation).toContain('proxy_set_header Host $host;')
    expect(mcpLocation).toContain('proxy_set_header X-Forwarded-Proto $scheme;')
    expect(mcpLocation).toContain('proxy_buffering off;')
    expect(mcpLocation).toContain('proxy_cache off;')
    expect(mcpLocation).toContain('proxy_read_timeout 1h;')
    expect(mcpLocation).toContain('proxy_send_timeout 1h;')
    expect(mcpLocation).not.toContain('proxy_set_header Authorization')
  })
```

- [ ] **Step 2: Run the targeted test and verify the red state**

Run:

```bash
cd studio && bun run test -- src/lib/nginx-proxy-config.test.ts
```

Expected: FAIL in `proxies the exact MCP endpoint...` because `mcpLocation` is
`undefined`. The existing backend proxy test must remain green.

- [ ] **Step 3: Add the minimal exact-match Nginx location**

Insert this block in `studio/default.conf.template` after the `/api/` location
and before `/ws`:

```nginx
    # MCP Streamable HTTP. Keep this exact path out of the SPA fallback and
    # stream JSON/SSE responses without proxy buffering.
    location = /mcp {
        proxy_pass ${BACKEND_SCHEME}://${BACKEND_HOST}:${BACKEND_PORT};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
    }
```

Do not add a trailing slash location, path rewrite, or explicit Authorization
header. Nginx forwards the incoming Authorization header by default.

- [ ] **Step 4: Run the targeted test and verify the green state**

Run:

```bash
cd studio && bun run test -- src/lib/nginx-proxy-config.test.ts
```

Expected: PASS with both tests in `Studio nginx backend proxy contract` green.

- [ ] **Step 5: Commit the route and contract test**

```bash
git add studio/default.conf.template studio/src/lib/nginx-proxy-config.test.ts
git commit -m "fix(studio): proxy creator MCP endpoint"
```

### Task 2: Verify The Repository And Production Boundary

**Files:**
- Verify: `studio/default.conf.template`
- Verify: `studio/src/lib/nginx-proxy-config.test.ts`
- Verify: `server/router/router.go`

- [ ] **Step 1: Run the full Studio test suite**

Run:

```bash
cd studio && bun run test
```

Expected: exit code 0 with no failed Vitest tests.

- [ ] **Step 2: Build the production Studio bundle**

Run:

```bash
cd studio && bun run build
```

Expected: `tsc -b && vite build` exits 0 and writes the production bundle to
`studio/dist/`.

- [ ] **Step 3: Verify the server still owns `/mcp`**

Run:

```bash
go test ./server/router -count=1
```

Expected: PASS, retaining the Fiber-to-MCP handler route at `/mcp`.

- [ ] **Step 4: Audit the final diff**

Run:

```bash
git diff HEAD^ --check
git diff HEAD^ -- studio/default.conf.template studio/src/lib/nginx-proxy-config.test.ts
```

Expected: no whitespace errors and only the tested exact-match proxy block plus
its contract test.

- [ ] **Step 5: Deploy the rebuilt Studio image through the existing ACK pipeline**

Publish the commit from Task 1 through the pipeline that renders
`studio/Deployment.yaml`. Confirm it supplies the existing `BACKEND_HOST`,
`BACKEND_SCHEME=https`, and `BACKEND_PORT=8443` values. No Kubernetes manifest
change is required.

- [ ] **Step 6: Probe production without an API key**

Run:

```bash
curl --connect-timeout 8 --max-time 15 -sS -D /tmp/anban-mcp-headers \
  -o /tmp/anban-mcp-body \
  -X POST https://creator.anbanai.com/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"deployment-check","version":"1.0"}},"id":1}'
sed -n '1,20p' /tmp/anban-mcp-headers
```

Expected: HTTP `401 Unauthorized`. The response must not be `200 text/html`,
must not contain the Studio `index.html`, and must not be an ALB `503`.

- [ ] **Step 7: Probe production with a valid API key**

First require the key without printing it, then issue the same request:

```bash
test -n "$ANBAN_API_KEY"
curl --connect-timeout 8 --max-time 15 -sS -D /tmp/anban-mcp-auth-headers \
  -o /tmp/anban-mcp-auth-body \
  -X POST https://creator.anbanai.com/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -H "Authorization: Bearer ${ANBAN_API_KEY}" \
  --data '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"deployment-check","version":"1.0"}},"id":1}'
sed -n '1,20p' /tmp/anban-mcp-auth-headers
sed -n '1,20p' /tmp/anban-mcp-auth-body
```

Expected: HTTP `200`, an MCP-compatible JSON or SSE content type, and an
`initialize` response whose `id` is `1`. Do not print or persist the API key.
