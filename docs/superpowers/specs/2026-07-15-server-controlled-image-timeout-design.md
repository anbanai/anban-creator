# Server-Controlled Image Generation Timeout Design

## Goal

Make the server the authoritative owner of `generate_image` execution deadlines, cancellation, retries, and timeout classification. MCP client timeout settings must not determine normal business outcomes.

## Problem

The current image providers apply a default 300-second HTTP timeout, but `model_routes.image_generation` has no timeout field and therefore cannot express provider-specific policy. `ImageService` can retry transient failures three times with backoff, so the total operation has no explicit server-owned deadline.

More importantly, `app/image.Processor` starts provider generation with `context.Background()`. Cancellation from the MCP request, Agent execution, or a future server deadline is not propagated to the provider call. A client can therefore report a timeout while the server continues generating, registering, or billing an image.

The managed Agent also replaces plugin MCP configuration through `strict-mcp-config`. Client configuration is therefore an unreliable place for business timeout policy.

## Design

### Two Server-Owned Deadlines

The server will define two related limits:

1. Provider-attempt timeout on each `model_routes.image_generation` route. This bounds one request to Volcengine, OpenAI, or Gemini.
2. `generate_image` operation timeout in MCP server configuration. This bounds the complete tool call: preflight, provider attempts, retry backoff, task-file registration, usage settlement, optional vision verification, and optional CDN upload.

The operation timeout must be greater than one normal provider attempt plus required post-processing. Validation at server startup will reject non-positive values and inconsistent configurations. The operation deadline is always the outer limit; retries stop when insufficient time remains.

The defaults are a 5-minute provider-attempt timeout and a 10-minute complete `generate_image` timeout. The outer budget leaves room for the existing 90-second image-understanding timeout and post-processing after one normal provider attempt. A retry starts only when its backoff plus a complete provider-attempt budget fits before the outer deadline; otherwise the server returns the last provider failure immediately.

### Context Propagation

`generateImageHandler` will derive a child context from the configured operation timeout. That context will flow through:

`generateImageHandler -> ImageService.GenerateImage -> ImageService.generateWithRetry -> Processor -> Provider.Generate`

`Processor` will no longer create `context.Background()` for generation. Context-aware methods will be the canonical API. Existing internal callers must pass their request or job context explicitly.

Provider SDK request timeouts remain defense in depth, but the effective deadline is the earlier of the provider-attempt deadline and the remaining operation deadline.

### Retry Semantics

- Retry only errors already classified as transient.
- Never retry `context.Canceled`.
- Retry a provider-attempt deadline only when the outer operation still has enough time for the configured backoff and another attempt.
- Never switch provider or model during a retry.
- Stop billing and post-processing as soon as the outer context expires.
- Preserve a generated durable task file if timeout occurs after registration; do not regenerate it automatically.

### MCP Transport Liveness

`generate_image` will emit progress notifications at an interval well below the MCP transport idle threshold. These notifications keep the HTTP stream alive and provide observability only; they do not extend or reset the server operation deadline.

The MCP client may retain a coarse transport fail-safe greater than the maximum server operation timeout. That ceiling is not a configurable business policy and must not normally fire before the server returns a terminal result.

### Timeout Result Contract

Timeouts will be returned as a structured MCP error result with a stable classification:

- `provider_timeout`: one provider attempt exceeded its server-configured limit and no retry completed successfully.
- `operation_timeout`: the complete `generate_image` deadline expired.
- `request_cancelled`: the task execution or caller cancelled the request.

The response and structured logs will include stage, provider, model, task ID, elapsed duration, configured timeout, attempt number, and whether a durable task file already exists. They must not include credentials, authorization headers, or signed URL query strings.

Agent feedback should preserve this classification rather than infer timeout from the absence of a response.

## Configuration

Configuration remains server-owned:

```yaml
mcp:
  api_key: "${ANBAN_MCP_API_KEY}"
  tool_timeouts:
    generate_image: 10m

model_routes:
  image_generation:
    cover:
      provider: "volcengine_ark"
      model: "doubao-seedream-5-0-pro-260628"
      timeout: 5m
```

Designer and content routes use the same field. Defaults and validation apply consistently to dynamically resolved presets so choosing another accessible model cannot remove the timeout policy.

## Testing

Use deterministic, short test deadlines and blocking fake providers to cover:

- provider receives the MCP-derived context;
- provider attempt is cancelled at its configured timeout;
- outer operation timeout stops retry backoff and later stages;
- caller cancellation is not retried;
- transient provider timeout retries only when budget remains;
- progress heartbeat occurs without extending the operation deadline;
- timeout responses contain the stable classification and safe metadata;
- no task-file registration or billing occurs after generation timeout;
- a task file registered before a later-stage timeout remains discoverable;
- config parsing, defaults, and invalid timeout relationships;
- Volcengine, OpenAI, and Gemini constructors receive the resolved per-attempt timeout.

Run targeted `app/image`, `server/service`, and `server/mcp` tests, followed by `go test ./...` and both required Go builds.

## Non-Goals

- Making image generation asynchronous in this change.
- Letting Agent prompts or MCP arguments choose timeout values.
- Changing image model selection or billing prices.
- Retrying with another provider.
- Treating heartbeat progress as proof that generation succeeded.
