# Server-Internal LLM Boundary Design

## Goal

Remove the misleading `writing` model route and converge content reasoning into the managed Claude Code Agent workflows. Retain one explicitly server-internal LLM route for synchronous intent parsing that must happen before a task and Agent execution exist.

## Current Problem

`model_routes.writing` no longer writes WeChat article bodies. Article generation already belongs to the Article Agent and `content-writing` Skill, while generative writing MCP tools such as `write_article` were removed previously.

Despite that boundary, the route is still wired as a shared text model for unrelated operations:

- AI entry intent parsing before task creation;
- Seednote profile enrichment during project setup;
- standalone viral-note analysis;
- post-publication Seednote matching;
- live-slicer subject recognition, invalid-sentence detection, segmentation, and subject completion.

The name and Studio copy therefore misrepresent the route. They also let a per-user "writing model" override affect Server control decisions. Several current MCP tools additionally perform generative workflow reasoning that belongs in the active Claude Code Agent and its Skills.

## Architecture Decision

Use two explicit execution boundaries:

1. The managed Claude Code Agent execution model owns content creation, content analysis, workflow decisions, revisions, and structured creative artifacts.
2. A Server-owned `model_routes.server_internal` route owns only synchronous Server decisions that must complete before an Agent execution can exist.

The initial and only consumer of `server_internal` is AI entry intent parsing. It converts an incoming natural-language request into validated task-creation parameters. It must not write content, select a different Agent execution profile, or perform downstream workflow steps.

No compatibility alias or fallback from `model_routes.writing` will remain. A configuration containing the old key must fail startup validation.

## Configuration

The semantic route becomes:

```yaml
model_routes:
  server_internal:
    provider: "moonshot"
    model: "kimi-k2.7-code"
    timeout: 5m
```

The provider registry remains shared with image understanding, video understanding, and image generation. Removing the `writing` route does not imply removing `MOONSHOT_API_KEY` while another configured route still uses the Moonshot provider.

Runtime configuration will expose a `ServerInternal` text-model config rather than `Writing`. Startup will construct `serverInternalLLMClient` and inject it only into `AIEntryService`.

The AI entry path will not consult user model settings. Missing or invalid `server_internal` configuration makes AI entry intent parsing unavailable and returns a stable configuration error; it must not fall back to a user model, an understanding route, or a newly dispatched Agent.

## Consumer Migration

### WeChat Article Writing

No behavior change is required. The Article Agent continues to create the outline, Markdown body, revisions, SEO output, and review artifacts with its frozen execution profile. MCP remains responsible for project/resource access, deterministic rendering, image operations, progress, and publishing.

### Seednote Profile Setup

Project setup will keep provider-parsed profile fields and remove synchronous LLM enrichment from `ProjectHandler`. Claude Agents can interpret the stored project profile and source material during a later task execution. Project creation must not start a hidden Agent execution merely to decorate profile metadata.

Image analysis in project setup will use only the dedicated `image_understanding` route. The current fallback from image understanding to the text model will be removed.

### Viral Analysis

The standalone Server LLM generation path will be removed. `viral_analysis` remains a user-visible task type, but it becomes a normal managed task that maps explicitly to the Seednote Agent and existing Seednote viral-analysis Skill. It must carry an explicit Agent execution profile, create a normal `task_execution`, and settle through terminal Agent model usage. The Agent owns evidence extraction, reasoning, validation, and result files.

The implementation will remove the duplicate Server-side generative analysis flow rather than preserve two result systems. Admission, status, profile selection, billing, and Studio navigation must use the standard task lifecycle after the cutover. New requests must not write `viral_analyses` rows or enqueue the old standalone analysis worker.

### Live Slicing

The Live Slicer Agent will read TingWu transcript output and directly create the semantic artifacts currently returned by Server LLM tools:

- invalid sentences;
- subject candidates;
- coherent segment ranges;
- completed subject scripts.

The Agent/Skill contract will define and self-review their JSON shapes. Deterministic MCP tools will validate indexes and convert accepted semantic artifacts into clip timing and ffmpeg plans.

### Seednote Publication Tracking

Post-publication tracking will not ask a generic LLM to guess which public note matches a task. Publication must persist a stable external note ID or URL when the provider returns one. Tracking uses that identity deterministically.

When publication does not return a stable identity, tracking records an explicit unlinked or unresolved state. Unknown identity must not be treated as a successful match and must not dispatch a hidden Agent execution.

## MCP Deletions

Delete generative MCP tools whose reasoning moves into Claude Agent workflows:

- `recognize_live_subjects`;
- `recognize_live_invalid_sentences`;
- `recognize_live_segments`;
- `complete_live_subject`.

Delete their handlers, Server LLM service methods, schemas, billing hooks, tests, Agent instructions, and tool-list expectations. Do not retain deprecated tool names, aliases, or fallback calls.

Keep atomic or deterministic MCP capabilities, including:

- project, resource, history, and task-state access;
- TingWu upload, task creation, and result querying;
- `build_live_clip_plan` and `build_live_subject_clip_plan` for validated timing and ffmpeg planning;
- clip-manifest construction;
- article template rendering;
- image understanding and image generation;
- publishing and durable task-file registration.

The deterministic clip-plan tools must accept Agent-produced semantic JSON and reject malformed indexes, ranges, or source identities without attempting to repair them with another model.

## User Model Configuration Cleanup

Remove the per-user text-model override because the remaining Server model is internal infrastructure, not a user-selectable writing capability. This includes:

- text fields in the model-config API DTOs and service methods;
- `TextUserConfig`, `UserModelConfig.TextConfigJSON`, and a forward migration that drops `user_model_configs.text_config_json`;
- Studio text-model form state, validation, requests, copy, and tests;
- `GetEffectiveWritingConfig`, text proxy helpers, and all fallbacks to user text configuration.

Keep the per-user image-model configuration and its existing API behavior. The Studio section must describe only the remaining image-generation override instead of presenting a generic "MCP model configuration" surface.

## Service Cleanup

Remove dead text-generation state from `WritingService`, including its default text client, timeout, user model-config dependency, and unused resolver. Keep or rename the service only for the cohesive capabilities that remain: deterministic WeChat rendering plus dedicated image/video understanding clients.

The shared OpenAI-compatible client abstraction remains available for `server_internal`, image understanding, and video understanding. Its name and comments must describe a generic model client rather than writing-specific behavior.

## Failure Semantics

- AI entry without a configured `server_internal` client returns a stable configuration-unavailable result before task creation.
- AI intent output must pass strict JSON parsing and existing task-field validation. A bounded repair attempt may use the same `server_internal` model; it may not switch routes.
- Claude Agent semantic artifacts that fail their Skill review remain inside the current Agent correction loop.
- Deterministic MCP validation failure terminates or marks the task recoverable according to the owning Agent contract; the Server does not invoke another model to repair the artifact.
- Missing publication identity remains explicitly unresolved and never becomes a guessed match.

## Data And Billing

The Server-internal intent request remains observable with provider/model/usage evidence appropriate to the existing provider-cost system. It is an internal parsing operation, not an article-writing SKU and not a user-selectable Agent profile.

Claude-owned analysis and writing costs remain part of terminal Agent model usage. Removing semantic MCP model calls must also remove their separate operation-cost attribution so the same reasoning is not charged twice.

The viral-analysis cutover must use one standard task admission and terminal settlement path. Historical records remain readable, but new work must not enter the removed standalone generation path.

## Plugin And Contract Updates

Update the canonical plugin Agent/Skill instructions so they no longer call deleted MCP tools and instead create the required semantic artifacts directly. Because runtime-affecting plugin assets change, bump both native manifests in the same change:

- `plugins/.claude-plugin/plugin.json`;
- `plugins/.codex-plugin/plugin.json`.

Update MCP tool-list and boundary tests to prove deleted tools are absent and retained tools remain atomic.

## Testing

Add or update focused tests for:

- parsing and deriving `model_routes.server_internal`;
- startup rejection of `model_routes.writing`;
- AI entry using only the Server-internal client with no user override or model fallback;
- removal of Seednote profile LLM enrichment and text fallback;
- project image analysis requiring `image_understanding`;
- `viral_analysis` explicitly mapping to the managed Seednote Agent, requiring an execution profile, and entering the standard task lifecycle;
- deterministic publication tracking by external identity and explicit unresolved state;
- absence of the four deleted live semantic MCP tools;
- Live Slicer Agent/Skill artifacts satisfying deterministic clip-plan schemas;
- removal of user text-model DTOs, persistence, Studio controls, and requests;
- preservation of user image-model configuration;
- terminal Agent usage replacing removed semantic MCP usage attribution;
- coordinated Claude and Codex plugin manifest versions.

Run the targeted Server config, service, handler, MCP, and plugin-contract tests first. Before completion, run `go test ./...`, build both Go binaries to `/tmp`, run the full Studio test suite and production build with Bun, and run `git diff --check`.

## Non-Goals

- Changing the three selectable Agent execution profiles or their providers.
- Making `server_internal` user-editable in Studio.
- Using `server_internal` as a fallback for image or video understanding.
- Starting hidden Agent executions during project setup or publication tracking.
- Preserving old `writing` config keys, deleted MCP names, or standalone generative workflows.
