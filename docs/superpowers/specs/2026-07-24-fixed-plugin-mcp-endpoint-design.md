# Fixed Plugin MCP Endpoint Design

**Date:** 2026-07-24

## Goal

Make the official Anban Creator plugin use one fixed MCP endpoint everywhere:
`https://creator.anbanai.com/mcp`. Plugin installation asks only for the user's
API key and never exposes a Base URL or endpoint input.

## Terminology

Use **endpoint** for the complete MCP request URL, including the `/mcp` path.
Do not call it `base_url`, `api_url`, or `API URL`, because consumers must not
append another path segment.

Endpoint is a configuration concept in source, documentation, and tests, not a
user-configurable plugin variable. Declarative host configurations store the
canonical URL directly.

## Configuration Contract

### Claude Code

- `.claude-plugin/plugin.json` declares only the required, sensitive `api_key`
  under `userConfig`.
- `.mcp.json` sets the `creator` server URL directly to
  `https://creator.anbanai.com/mcp`.
- The Authorization header continues to use `Bearer ${user_config.api_key}`.
- Installation and enablement surfaces must not display an endpoint field.

### Codex

- `install/agents-registration.toml` registers the same fixed endpoint.
- Every distributed Codex Agent TOML declares the same fixed MCP endpoint.
- `ANBAN_API_URL` and Base URL fallback expressions are removed.
- Users configure only `ANBAN_API_KEY` for MCP authentication.

### Setup And Documentation

- `anban-setup` checks API-key presence, authentication, and connectivity
  against the fixed endpoint.
- Setup instructions must not ask users to configure, inspect, or override an
  API URL or endpoint.
- Claude Code and Codex installation documents describe the endpoint as an
  implementation detail, not an installation choice.
- Self-hosted and local MCP servers are outside this official plugin contract;
  no legacy URL override is retained.

## Validation

Contract tests must prove all of the following:

- Claude `userConfig` contains a required sensitive `api_key` and no endpoint
  or URL option.
- `.mcp.json`, Codex registration, and every Agent TOML use exactly
  `https://creator.anbanai.com/mcp`.
- Distributed plugin content contains no `api_url`, `ANBAN_API_URL`, Base URL
  interpolation, or instructions to configure a service URL.
- Existing MCP server key, authentication-header, and timeout contracts remain
  unchanged.

Run the targeted server Agent contract tests, then the full Go test suite. Since
plugin assets change, increment both native plugin manifest versions by one
patch release in the same implementation.

## Non-Goals

- Supporting self-hosted, local, staging, or alternate MCP servers.
- Changing API-key creation, secure storage, or bearer-token behavior.
- Changing the MCP server key or tool naming conventions.
- Adding compatibility aliases for removed URL variables.
