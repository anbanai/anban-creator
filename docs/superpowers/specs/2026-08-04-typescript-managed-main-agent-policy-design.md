# TypeScript Managed Main Agent Policy Design

## Problem

The TypeScript managed runtime starts the selected plugin Agent as the main
Claude Code session, but currently gives that session unrestricted access to
the `Agent` tool. An Article main Agent can delegate the same workflow to a
nested Agent, return after announcing that the workflow started, and leave the
runtime `output/` directory empty. Server deliverable validation then replaces
the nominal success with the secondary error `no meaningful output files`.

The Go managed runtime already prevents this behavior by disallowing `Agent`,
`ScheduleWakeup`, and `AskUserQuestion`. The TypeScript runtime intentionally
removed all tool restrictions when it moved to `bypassPermissions`, creating a
cross-runtime contract mismatch.

## Design

Keep `bypassPermissions` and unrestricted access to ordinary filesystem, MCP,
research, and workflow tools. Add only the three managed-session exclusions
already enforced by Go:

- `Agent`: the selected plugin Agent is already the main workflow owner and
  must not delegate that workflow to an unowned nested session.
- `ScheduleWakeup`: managed executions are one-shot and server-scheduled.
- `AskUserQuestion`: managed tasks are non-interactive.

The TypeScript runner will also count assistant `tool_use` blocks by tool name
and include `tool_use_count` and `tool_use_summary` in its terminal result. This
keeps Server diagnostics accurate if a future runtime or resumed session still
produces a nested-only execution pattern.

No Agent/Skill text, plugin manifest, task schema, billing behavior, or Studio
surface changes are required.

## Failure Handling

The runtime continues to upload any files under `output/` before reporting
completion. If Claude Code returns success without deliverables, Server
validation remains authoritative. Tool summaries improve the classification;
they do not bypass deliverable validation or convert failure into success.

## Tests

1. `buildQueryOptions` retains `bypassPermissions`, does not add an allowlist or
   permission callback, and disallows exactly the three managed-session tools.
2. Terminal message consumption reports the total tool count and per-tool
   summary, including an `Agent` invocation.
3. Existing model-usage, artifact, finalization, bootstrap, and build contracts
   continue to pass.

## Release Acceptance

Build and publish the Article TypeScript image with an immutable digest, update
the Server runtime image configuration, roll out Server, and create a fresh
Article task. Acceptance requires no `Using tool: Agent` entry in the managed
main session, at least one collected deliverable, a successful execution, and
the deployed task execution recording the new image digest.
