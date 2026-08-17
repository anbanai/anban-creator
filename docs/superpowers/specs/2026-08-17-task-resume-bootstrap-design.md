# Task Resume Bootstrap Repair Design

## Context

Managed Kubernetes executions reuse one task PVC so Claude session state,
workspace inputs, and generated artifacts survive across explicit task resume.
The Server currently emits `.anban-creator/settings.json` at the same path for
every execution, while the TypeScript bootstrap materializer rejects any
existing target whose bytes differ from the new payload.

Production evidence from execution `42ac3b2d-32a7-4fb1-ae63-e341b3eb9d9c`
shows the init container completed successfully and the Agent container exited
before Claude started with:

```text
bootstrap target conflicts with existing file: /workspace/.anban-creator/settings.json
```

The managed runner also validates and forwards `resume_session_id`, but it does
not consume `resume_context_path`. A resumed Claude session can therefore start
without being told to read the user's supplemental instructions.

## Goals

- Permit the Server-owned runtime settings file to change safely across
  executions that share a task workspace.
- Preserve fail-closed conflict behavior for attachments, user-authored files,
  and all bootstrap files not explicitly marked replaceable.
- Give managed and local resume execution the same supplemental prompt
  contract.
- Keep startup failures reportable through the existing Agent completion
  callback.

## Non-Goals

- Changing task lineage, session ID persistence, PVC lifecycle, billing, or
  retry policy.
- Allowing arbitrary bootstrap files to overwrite workspace content.
- Changing plugin Agents or Skills.
- Adding Server-side workflow orchestration to MCP handlers.

## Bootstrap Replacement Contract

`BootstrapFile` gains an optional JSON field:

```json
{
  "path": ".anban-creator/settings.json",
  "text": "{...}",
  "mode": 384,
  "replace_existing": true
}
```

The Server sets `replace_existing` only for
`.anban-creator/settings.json`. The field defaults to false, so existing
bootstrap responses and every other path retain their current immutable
behavior. The TypeScript validator accepts only a boolean value and carries the
policy through preflight without inferring ownership from the filename.

The materializer downloads or stages every input and validates every target
before changing the workspace. For a replaceable target it requires the
existing target to be a regular non-symlink file. It then atomically replaces
that target with the staged file. A symlink, directory, or other special file
still fails closed. Non-replaceable targets are accepted only when their bytes
already match; differing bytes remain a conflict.

Replacement is scoped to the declared target and does not delete surrounding
directories or unrelated workspace content. Before committing a replacement,
the materializer moves the existing regular file into its private staging
directory, installs the staged replacement, and tracks both operations. A
later commit failure restores the previous file; successful completion removes
the backup with the staging directory. The existing all-input precheck is also
preserved so a later immutable conflict prevents an earlier managed settings
replacement from being committed.

## Resume Prompt Contract

A shared TypeScript helper constructs the user prompt for both managed and
local execution. With no resume context path it returns the original prompt
unchanged. With a resume context path it appends the established continuation
instructions that tell Claude to:

- read the execution-scoped `latest.md` first;
- continue from existing drafts, assets, and outputs;
- avoid clearing or replacing existing deliverables unless explicitly asked;
- use `latest.md` to distinguish supplemental files with similar purposes.

The helper references the materialized relative path instead of embedding the
file contents. The managed runner passes the resulting prompt to the Claude
Agent SDK while continuing to pass `resume_session_id` through the SDK resume
option. Local execution uses the same helper and removes its duplicate prompt
implementation.

## Validation And Error Handling

- Bootstrap response validation rejects non-boolean `replace_existing` values.
- Server bootstrap tests pin that only runtime settings are replaceable.
- Materialization still rejects symlinked parents and non-regular targets.
- A failed replacement or immutable conflict remains a pre-run platform error
  and is reported through the existing completion callback in current runtime
  images.
- No secrets, settings content, or supplemental file contents are added to
  diagnostics.

## Testing

TypeScript tests will cover:

1. a differing replaceable settings file is atomically updated;
2. a differing file without the flag remains rejected;
3. a replaceable symlink or non-regular target remains rejected;
4. a later immutable conflict prevents an earlier replacement from committing;
5. managed and local prompts append the same resume instructions and path;
6. a non-resume prompt remains byte-for-byte unchanged; and
7. bootstrap validation accepts a boolean flag and rejects other types.

Go tests will assert that the Agent bootstrap response marks only
`.anban-creator/settings.json` as replaceable and that bootstrap file
validation preserves the field. Verification will include Agent tests,
typecheck, build, targeted Go tests, full Go tests, and `git diff --check`.

## Deployment

The repaired TypeScript runtime must be rebuilt and published for all managed
runtime profiles. ACS configuration must reference the rebuilt image by an
immutable digest or unique release tag. Updating Server code without rebuilding
the Agent images will not change workspace materialization or managed prompt
behavior inside existing runtime images.
