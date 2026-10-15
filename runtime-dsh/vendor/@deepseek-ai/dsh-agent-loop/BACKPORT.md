# Message-free start backport

Baseline: the published `@deepseek-ai/dsh-agent-loop@0.2.1-alpha.4` package.
`package.json`, `lib/index.js`, and all `lib/types/**/*.d.ts` were copied from
that release. This is not a build of the newer upstream package.

Source: DeepSeek Harness commit
`8c2ae0d1ebf0e43cbe6fdebaa41495ac8676efba`, the same upstream revision recorded
in `runtime-dsh/vendor/README.md`, specifically
`packages/core/agent-loop/src/agent.ts`.

Only these upstream changes are applied:

- `startupRequested` and `startupIssued` state fields.
- The one-shot `ReactLoopAgent.start()` method, which wakes the existing driver
  without adding a user transcript message.
- The initial-step empty-message condition, which permits the requested
  message-free turn and consumes its pending startup request.
- Matching private fields and `start(): void` in `lib/types/agent.d.ts`.

All other release JavaScript and declarations remain unchanged. Package version
and dependency metadata remain unchanged to preserve the published graph.

The alpha.4 package also includes its published locale files; those and every
other alpha.4 runtime/declaration change remain intact.

Published alpha.4 baseline SHA-256 values (before the backport):

```text
cf57e3ecd487ac5c2553455aaa5cef2c6fb41e6bd61be5aa455675d1d7193033  lib/index.js
66131a86c5f024fabb77e99103f3f07cdca5818cbce7d6aaecb058197c66589e  lib/types/agent.d.ts
```

These two alpha.4 runtime files contain the minimal Anban startup extension;
the surrounding implementation is from the published alpha.4 package. The
alpha.4 package metadata and locale files remain from alpha.4.

## Verification

From the repository root, using Node and runtime-dsh dependencies:

```sh
node --test runtime-dsh/vendor/@deepseek-ai/dsh-agent-loop/backport.test.js
```

The tests use the real released Cordis, AgentRegistry, session, prompt, and tool
services with a deterministic in-process LLM adapter. They check a single model
request without a user transcript row, duplicate calls before/after completion,
cancellation both before dispatch and during streaming, and a normal follow-up
after cancellation. `start()` remains one-shot after cancellation, matching the
upstream implementation. Repeated calls do not retry the cancelled startup.

Before installing this local override, `DSH_BACKPORT_BASELINE=1` selects the
installed registry package and demonstrates failure because `agent.start` is
undefined. After installation that mode selects the installed override instead.

The bridge calls `start()` for a newly created idle Agent. Starting during
maintenance or using start as a general resume operation is outside this
backport's protocol contract; existing wake, cancellation, and maintenance
semantics are intentionally not changed. Replace this local package with a
published release once its message-free startup contract passes these tests and
the end-to-end Project Creator smoke.
