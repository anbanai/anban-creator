# Task 1 Implementation Report

## Outcome

Implemented explicit, immutable execution identity for managed Bootstrap and runtime execution.

## Changes

- Added `execution_id` to the server `AgentBootstrapResponse` and TypeScript `BootstrapResponse`/`BootstrapIdentity` contracts.
- Added runtime contract version `1` and required `X-Anban-Agent-Contract-Version: 1` on Bootstrap requests; the server rejects missing or mismatched versions with HTTP 426.
- Bootstrap validation now rejects missing or mismatched response execution IDs before Agent Pack validation, file materialization, or Claude startup.
- Trusted Bootstrap failure identities now require the explicit execution ID and validate it alongside JWT claims.
- Managed `runClaude`, query options, completion hooks, and the default Reporter now derive identity from the validated Bootstrap response rather than an optional process-environment fallback.
- `buildExecutionEnvironment` now injects only non-secret `ANBAN_TASK_ID`, `ANBAN_EXECUTION_ID`, and `ANBAN_TASK_TYPE` identity values in addition to the project ID; API keys and API URLs remain excluded.
- Completion Hook environment receives the same identity values and scrubs `ANBAN_API_KEY`/`ANBAN_API_URL`.
- Local compatibility Bootstrap responses now carry `execution_id` and local Claude calls use the response identity.
- Added regression tests for response identity validation, request contract header, identity environment variables, server header enforcement, and server response identity.

## Verification

- `go test ./server/service ./server/handler`: PASS
- `cd agent-ts && bun run typecheck`: PASS
- `cd agent-ts && bun test test/runner.test.ts test/main.test.ts`: PASS (46 tests)
- `cd agent-ts && bun test test/bootstrap.test.ts test/runner.test.ts test/main.test.ts`: 73 pass, 1 pre-existing catalog fixture failure because the isolated worktree's `harness` submodule is empty (`ENOENT harness/agent-pack-catalog.json`)
- `git diff --check`: PASS

## Concerns

The `harness` submodule must be initialized in CI or the checkout before the full Bootstrap test file can pass. No production behavior depends on that checkout state.
