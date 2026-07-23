# MCP Capability Boundary Design

**Date:** 2026-07-23

## Objective

Make MCP a stateless capability transport rather than a business workflow engine.
Agents and Skills own sequencing, quality decisions, retries, and stop/continue
behavior. MCP handlers expose focused application capabilities and do not decide
what the workflow should do next.

This is a forward-only contract change. Obsolete MCP parameters and response
fields will be removed without compatibility shims.

## Boundary

An MCP handler may:

- obtain the authenticated user and execution identity;
- decode and validate the protocol request;
- enforce transport and security constraints;
- invoke one application capability;
- map the capability result or typed error to an MCP response;
- emit transport-level logs, progress heartbeats, and deadlines.

An MCP handler must not:

- call multiple domain services to assemble or advance a workflow;
- choose a business path, fallback, quality threshold, or retry policy;
- decide whether an Agent should stop or continue;
- run a second capability conditionally after the first one;
- construct business reports or persist workflow state;
- expose provider-routing, billing, or runtime diagnostics as Agent-facing
  workflow inputs or artifacts.

Domain-named commands such as `claim_topic`, `finalize_task_title`, and
`publish_draft` remain valid MCP capabilities when the handler delegates the
whole command to one application service. A domain command is not itself a
workflow. The violation occurs when the MCP transport layer implements the
command's business rules or chains it with other commands.

Transactional integrity remains mandatory. Operations that must commit
together, such as generated image persistence, task-file registration, and
fixed-SKU settlement, remain one atomic application capability. Their
transaction belongs in the service/repository layer rather than the MCP
handler.

## Image Capability

`generate_image` becomes one focused capability:

1. Resolve the task-owned image route internally.
2. Generate one requested image.
3. Persist and register the durable task asset.
4. Commit fixed-SKU settlement atomically with the task file.
5. Return the durable asset identity needed by the Agent.

The tool will no longer perform visual verification or CDN upload. Remove
`verify_with_vision`, `verification_prompt`, and `upload_to_cdn` from its input
schema and implementation. Remove the combined generation-verification-upload
branches and their response fields.

Visual analysis remains available as the separate `analyze_image` capability.
CDN upload remains available as the separate `upload_image` capability. Agents
and Skills decide whether and when to call either tool and what to do with the
result.

The Agent will no longer supply `operation_id`. The application capability will
derive a deterministic idempotency identity from the execution and semantic
request. An identical request in the same execution replays the durable result;
a changed prompt, role, target, size, or reference set is a new generation.

Agent-facing results will contain durable asset information and semantic fields
needed for the workflow. Provider, model, selection reason, response type,
revised prompt, output MIME diagnostics, physical pixel dimensions, billing
details, and provider request details remain in server logs, persisted internal
records, or cost ledgers rather than Agent-authored artifacts.

## Seednote Contract

The Seednote Agent and visual-design Skill continue to own image planning and
call MCP tools directly. They provide semantic inputs such as image purpose,
visual prompt, target asset name, aspect ratio, and selected references.

`image-prompts.md` records only the planned image identity/purpose and the
creative prompt. It must not record provider, model, selection reason, response
type, output MIME, physical dimensions, generation attempts, verification
objects, timeout diagnostics, or billing/routing details.

Seednote will generate every image in `image-plan.md` unless image generation
itself fails. Visual analysis is optional workflow behavior owned by the Agent
and Skill. An unavailable or malformed analysis response cannot cause
`generate_image` to fail or prevent later planned images from being generated.

`image-review.md`, when produced, is a content-quality artifact. It may describe
visible subject, text, composition, and policy issues, but must not become a
transport/provider diagnostic log. Infrastructure failures belong in the
structured task failure state and server observability.

## Repository-Wide MCP Audit

Every production handler under `server/mcp` will be classified and adjusted:

- **Thin adapters:** keep the tool and make the handler decode, delegate once,
  and encode.
- **Mixed handlers:** extract business computation and multi-service assembly
  into a focused service/application capability, then delegate once.
- **Compound tools:** split conditional follow-up actions into existing or new
  independent capabilities.
- **Workflow descriptions:** remove tool text that tells the Agent which tool to
  call next, when to retry, or when to stop. That guidance belongs in Agents and
  Skills.

Known mixed or compound areas requiring adjustment include:

- `generate_image`: route resolution, generation, verification, upload,
  registration, settlement, and replay are currently combined in the handler;
- `get_project_profile`: the handler currently composes project, task, image
  capability, resource, and runtime configuration decisions;
- `score_article`: scoring and recommendations are implemented in MCP code;
- `export_seednote`: Seednote content parsing and format rules are implemented
  in MCP code;
- rendered-image registration and download-plus-upload options: optional
  follow-up uploads must be separated from registration/download capabilities.

Live slicing, publishing, topic-pool, template, workspace, progress, resource,
and external Seednote tools will also be audited. Existing domain services may
remain the implementation when the MCP handler already delegates one complete
command. Request decoding helpers and protocol-only validation may remain in
`server/mcp`.

## Architectural Enforcement

Add the boundary rule to `CLAUDE.md` and keep `AGENTS.md` aligned. The rule will
state that Agents and Skills own workflows, while MCP exposes stateless,
independently callable capabilities.

Add focused contract tests that make regressions visible. Tests will verify:

- removed image-generation fields are absent from the MCP schema;
- technical routing metadata is absent from the public image result;
- exact duplicate image requests replay without duplicate settlement;
- changed semantic requests generate new operations;
- generation does not invoke image analysis or CDN upload;
- Seednote instructions and artifact examples do not contain removed technical
  fields or mandatory verification gates;
- handlers identified as mixed delegate to extracted application capabilities;
- tool descriptions do not encode cross-tool workflow sequencing.

Tests will assert observable contracts rather than source-shape trivia where
possible. Small architecture checks may scan registrations or handlers only
when behavior cannot prove the ownership boundary.

## Delivery Strategy

Implementation will use small, independently verified commits:

1. Add architectural rules and failing boundary tests.
2. Refactor the image application capability and public MCP contract.
3. Simplify Seednote Agent/Skill artifacts and remove the verification gate.
4. Extract remaining mixed MCP handlers into application capabilities.
5. Audit every MCP tool description and schema.
6. Run targeted tests, full Go tests/builds, and plugin contract tests.
7. Perform merge-readiness review before integrating to `main`.

Because the public MCP and plugin contracts change incompatibly, both native
plugin manifests will receive the same major version bump. Only the runtime
images and server components affected by the final diff will be rebuilt.

## Acceptance Criteria

- No production MCP handler owns retry, quality-gate, stop/continue, or
  cross-capability workflow decisions.
- Each production MCP handler delegates to at most one application capability
  after protocol/security validation.
- `generate_image` cannot invoke image analysis or CDN upload.
- `generate_image` no longer accepts the removed technical/workflow fields.
- Image generation remains durably registered and exactly-once settled for an
  identical semantic request within one execution.
- Seednote generates later planned images without depending on visual-analysis
  availability.
- Seednote artifacts contain creative/business content, not provider/runtime
  diagnostics.
- `CLAUDE.md` and `AGENTS.md` describe the same MCP ownership rule.
- Both plugin manifests carry the same new major version.
- Targeted tests, `go test ./...`, server and agent builds, and relevant plugin
  contract tests pass before merge.
