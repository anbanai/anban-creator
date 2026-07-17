# Server Task Workspace Cleanup Design

## Goal

Remove the legacy local workspace archive model and make server-owned tasks the
only supported workspace contract. Every execution writes deliverables to one
canonical `output/` tree, while `task_id`, `execution_id`, `task_files`, and OSS
provide isolation, versioning, persistence, and delivery.

## Context

`archive_workspace` originated before server task persistence. It moved a shared
local staging directory into a title- or date-named directory so a later local
run would not overwrite earlier output.

The current platform already assigns each run to a task and execution, persists
its manifest in `task_files`, and stores durable bytes in OSS. The old archive
step now moves files from `output/` into `output/<content-type>/<title>/` before
collection. Because task files are identified by their full logical path, the
same deliverable can be registered under both its working and archived paths.
The Studio displays only the basename, making these distinct records appear as
duplicate files.

## Decision

Delete the archive contract instead of deprecating or reimplementing it.

- Remove the `archive_workspace` MCP tool.
- Remove `ArchiveResult`, `WorkspaceService.Archive`, archive path generation,
  and archive-only filename sanitization.
- Make `prepare_workspace` require a non-empty `task_id`.
- Keep `content_type` as required workflow context, but return only the canonical
  task-relative path `output`.
- Do not retain a no-task local fallback or a no-op compatibility tool.
- Do not create a second server-side or OSS archive copy.

## Runtime Contract

An agent starts a managed workflow by calling:

```text
prepare_workspace(content_type=<workflow>, task_id=<task id>)
```

The tool returns:

```json
{"path":"output"}
```

The agent creates that directory if needed and writes all deliverables beneath
it. It never moves the deliverables after generation. Completion validation,
the direct-upload manifest, and MCP-produced task-file registration therefore
refer to the same canonical logical paths.

Requests without `task_id` fail with a structured tool error. This is an
intentional forward-only boundary: standalone local directory management is no
longer a responsibility of the server MCP.

## Workflow Changes

Remove archive calls, archive variables, title-derived directory moves, and
archive completion gates from every shipped workflow that uses them, including
Seednote, Article, Ecommerce, and Moments surfaces.

Final workflow stages must:

1. Validate required deliverables in `$DIR`.
2. Remove a resolved failure-state artifact when the existing workflow requires
   that cleanup.
3. Report `$DIR` as the result directory.
4. Leave files in place for the task artifact uploader and server finalizer.

No workflow may use a title or timestamp to change the deliverable path after a
file has been generated or registered.

## Distribution Changes

Update all affected checked-in distributions:

- `claudecode/` agents, skills, hooks, and related documentation.
- `openclaw/` mirrored skills.
- `codex/` mirrored skills, hooks, and documentation.

Keep equivalent workflow contracts synchronized. Increment each affected plugin
manifest by one patch version in the same change, as required by `AGENTS.md`.
Preserve unrelated user-owned files already present in managed submodules.

## Server Changes

The server MCP registry exposes `prepare_workspace` but no longer exposes
`archive_workspace`. `WorkspaceService` becomes a task-path resolver rather than
a local filesystem organizer. The existing task artifact upload and execution
publication paths remain responsible for persistence and do not gain a second
archive phase.

Runtime permission policy and managed-agent tool allowlists must stop advertising
or requiring `archive_workspace`.

## Existing Data

This change is forward-only for execution behavior. It does not delete or merge
existing `task_files` rows and does not remove existing OSS objects. Historical
tasks remain downloadable exactly as stored, including any pre-existing duplicate
records. Automatic cleanup would risk deleting files that differ despite sharing
a basename.

## Error Handling

- Missing `content_type` remains an explicit request error.
- Missing `task_id` becomes an explicit request error.
- Workflow completion fails through the existing artifact validation path when
  required files are absent from `output/`.
- No fallback directory or archive path is synthesized.

## Testing

Use test-first changes for each behavioral boundary:

- Workspace service tests require `task_id` and assert the canonical `output`
  result.
- MCP registry and tool tests assert that `archive_workspace` is absent and that
  taskless preparation is rejected.
- Runtime policy tests assert that managed workflows do not advertise the
  removed tool.
- Agent/skill contract tests assert that shipped workflows contain no archive
  call, archive variable, or post-generation move and still report `$DIR`.
- Distribution parity tests cover matching Claude Code, OpenClaw, and Codex
  workflow files where parity is expected.
- Full verification runs `go test ./...` and builds both server and agent
  binaries to `/tmp`.

## Non-Goals

- Deduplicating historical task rows.
- Changing OSS object-key layout for current execution artifacts.
- Adding a new content library or user-facing archive feature.
- Preserving standalone local workspace compatibility.
- Hiding duplicates only in the Studio UI.
