# Managed MCP Request Timeout Design

## Problem

Managed Agent executions inject the Anban HTTP MCP server through the Go Agent
SDK and enable `--strict-mcp-config`. This deliberately ignores plugin and user
MCP configuration, including the plugin's 900000 ms request timeout.

The injected `McpHTTPServerConfig` currently carries only `type`, `url`, and
`headers`. Claude Code therefore applies its shorter default MCP timeout. A
long-running `generate_image` call can time out in the Agent while the server is
still generating. The late image may reach the shared workspace after the Agent
has checked `list_task_files`, producing an unregistered image and a misleading
recoverable failure.

## Decision

Extend the vendored Go Agent SDK's HTTP MCP configuration with an optional
millisecond `timeout` field and set it to 900000 ms for the managed Anban MCP
server. This matches the canonical plugin MCP configuration.

Keep the existing timeout hierarchy:

- managed Claude Code MCP request: 15 minutes;
- complete server `generate_image` operation: 10 minutes;
- one image-provider attempt: 5 minutes;
- image-understanding verification: 90 seconds.

The client budget remains larger than the server operation budget, so normal
timeouts are classified and returned by the server before Claude Code abandons
the request.

## Components

### Agent SDK MCP contract

Add `Timeout int64` with JSON name `timeout` and `omitempty` to
`McpHTTPServerConfig`. The unit is milliseconds, matching Claude Code MCP JSON.
The SDK's existing config-file generator already serializes external server
configs directly, so no additional serialization path is needed.

### Managed runtime policy

Define one 15-minute managed MCP request timeout in `server/agent` and populate
the SDK HTTP server's `Timeout` field from `time.Duration.Milliseconds()`.
Continue using `--strict-mcp-config`, execution-scoped authorization, and the
existing server URL.

### Retry and persistence behavior

Do not increase Seednote retry budgets and do not change image prompts. The
existing server-owned operation timeout, provider-attempt timeout, cancellation
propagation, idempotent operation IDs, and task-file registration remain the
authoritative generation contract.

## Error Handling

With the larger client budget, provider and operation timeouts reach the Agent
as the server's structured `provider_timeout` or `operation_timeout` result.
Transport interruption and task cancellation continue to cancel the request.
Progress notifications remain an optimization, not the sole mechanism keeping
the request alive.

## Tests

1. Extend the SDK JSON contract test to prove a non-zero HTTP MCP timeout is
   serialized as `"timeout":900000` and zero remains omitted.
2. Extend the managed runtime policy test to prove the injected Creator server
   has a 900000 ms timeout while retaining URL, authorization, and
   `strict-mcp-config` behavior.
3. Run the targeted SDK and `server/agent` tests, followed by `go test ./...`
   and both required Go binary builds.

## Scope

This change does not alter plugin assets, server timeout values, provider
selection, billing, image persistence order, or Seednote workflow instructions.
The plugin manifests therefore do not require a version bump.
