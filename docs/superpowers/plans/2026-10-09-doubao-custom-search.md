# Doubao Custom/Global Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Ark Responses model-backed web search with the official Volcengine Doubao Search Custom or Global API, selected by Server configuration, and expose one execution-scoped Anban web-search MCP tool.

**Architecture:** The Server calls either `POST https://open.feedcoopapi.com/search_api/web_search` or `POST https://open.feedcoopapi.com/search_api/global_search` with the selected edition's API key and documented PascalCase payload; no Ark model is involved. The application service retains identity, idempotency, billing, and provider-cost recording. Agents see only the unified `search_web` MCP tool.

**Tech Stack:** Go, Fiber MCP server, GORM billing repositories, `net/http` with `httptest`.

**Spec:** User-approved search MCP plan in conversation, corrected to the official [Doubao Search Custom API](https://docs.volcengine.com/docs/Networkedsearch/Networkedsearch-2?lang=zh).

## Global Constraints

- The Server exclusively owns provider, endpoint, and credentials; Agents cannot select a provider or call the external API.
- Both Doubao Search APIs use `Authorization: Bearer <API_KEY>` and have no model field.
- Custom web result count is capped at 50 and Global web result count at 20, following the official API limits.
- Search errors never trigger an automatic provider fallback.
- Each search capability uses the existing execution identity, idempotency, quote, charge, and provider-cost workflows.
- Never log or return API keys or raw authorization headers.
- MCP handlers validate and invoke one capability; they do not compose billing or provider workflows.

---

## File Map

- `server/config/config.go`, `server/config.example.yaml`, `server/config/search_test.go`: Custom/Global endpoint/key/timeout/result caps, defaults, and startup validation; remove Ark model configuration.
- `server/service/search.go`, `server/service/search_test.go`: typed Custom/Global web requests, official wire structs, normalized result models, response/error handling, request fingerprints, and billing/cost metadata.
- `server/mcp/search_tools.go`, `server/mcp/search_tools_test.go`, `server/mcp/boundary_contract_test.go`, `server/mcp/mcp_test.go`: one atomic `search_web` tool with edition-appropriate filters and execution identity.
- `server/billing/products.yaml`, `server/billing/catalog_contract_test.go`: the zero-price standalone SKU for web search.
- `server/service/provider_cost_test.go`: search cost evidence identifies search type and records usage/result metadata without inventing a model or cost.
- `server/main.go`: register the Custom provider without any model selection.

## Task 1: Replace the Provider Protocol and Configuration

1. Add `httptest` contract cases that require the exact Custom and Global endpoints, bearer API key, `Query`/`SearchType`/`Count` or `DocCount` payload, and documented `ResponseMetadata`/`Result` responses; assert that no `model`, `input`, or `tools` property is sent.
2. Verify those new tests fail against the existing Ark Responses implementation.
3. Implement typed response models for Custom `WebResults` and Global `Documents`; normalize IDs, URLs, summaries, publish times, rank/authority metadata, request IDs, usage, and provider metadata.
4. Replace config's Ark `base_url`/`model`/`max_keyword` settings with edition-specific official endpoints, API keys, timeouts, and bounded web result caps. Validate only the selected edition's required key when enabled.
5. Cover empty results, provider-declared errors, HTTP 4xx/5xx, timeout, malformed/oversized responses, and key non-disclosure.
6. Run `go test ./service ./config` in `server/`.

## Task 2: Expose the Web MCP Capability

1. Add MCP contract tests for registration, execution identity, provider override rejection, query/limit validation, and edition-specific filter validation.
2. Add official Custom Web filters (`time_range`, `sites`, `block_hosts`, `need_content`, `need_url`, `auth_info_level`, `content_formats`, `industry`, `query_rewrite`) to the typed request and tool schema; Global rejects Custom-only filters.
3. Validate the 100-character query limit, Custom API time-range/date format, documented enum values, domain counts, and count caps before quote/charge.
4. Keep the handler atomic and bind it to the current user/project/task/execution identity. Preserve the existing `search_web` operation.
5. Run `go test ./mcp ./service` in `server/`.

## Task 3: Preserve Billing, Idempotency, and Provider Cost Evidence

1. Add service tests proving search requests select the standalone quote/charge operation and replay idempotently by identity and normalized parameters.
2. Retain `mcp.search_web` at `price_credits: 0` with route `search.web`.
3. Include search type, provider request ID, request count, result count, query, usage, and documented filter metadata in provider cost evidence. Mark cost unreconciled when the Custom API supplies no attributable cost; do not fabricate a model or cost.
4. Verify zero-price calls create one durable charge and no wallet debit, and that provider cost request IDs remain idempotent.
5. Run `go test ./billing ./model ./service` in `server/`.

## Task 4: Full Verification and Integration

1. Run `gofmt` on changed Go files.
2. Run focused provider/MCP/config/billing tests, then `go test ./...` and `go build -o /tmp/anban-creator-server .` from `server/`.
3. Review the full diff for stale Ark Responses endpoint/model configuration, secret leakage, mismatched Custom wire field casing, and changes outside this feature.
4. Merge the reviewed feature branch into local `main` after all required checks pass; do not push remote branches.
