# Message-free start backport

Baseline: the published `@deepseek-ai/dsh-agent-loop@0.2.2-alpha.2` package.
`package.json`, `lib/index.js`, and all `lib/types/**/*.d.ts` were copied from
that release. This is not a build of the newer upstream package.

Source: the message-free startup behavior retained from the previous Anban
backport, applied to `packages/core/agent-loop/src/agent.ts`.

Only these upstream changes are applied:

- `startupRequested` and `startupIssued` state fields.
- The one-shot `ReactLoopAgent.start()` method, which wakes the existing driver
  without adding a user transcript message.
- The initial-step empty-message condition, which permits the requested
  message-free turn and consumes its pending startup request.
- Matching private fields and `start(): void` in `lib/types/agent.d.ts`.
- `TRANSPORT` turn failures retain their `Error.cause` chain in the durable
  `turn/end` diagnostic message, while preserving structured provider fields.

All other release JavaScript and declarations remain unchanged. Package version
and dependency metadata match the published `0.2.2-alpha.2` graph.

The alpha.2 package includes its published runtime and declaration files; those
and every other alpha.2 release change remain intact.

The runtime file contains the minimal Anban startup and transport-diagnostic
extensions; the surrounding implementation is from the published alpha.2
package. The alpha.2 package metadata remains from alpha.2.

## Verification

From the repository root, using Node and runtime-dsh dependencies:

```sh
node --test runtime-dsh/vendor/@deepseek-ai/dsh-agent-loop/backport.test.js
```

The tests use the real released Cordis, AgentRegistry, session, prompt, and tool
services with a deterministic in-process LLM adapter. They check a single model
request without a user transcript row, duplicate calls before/after completion,
cancellation both before dispatch and during streaming, a normal follow-up
after cancellation, and preservation of a transport cause chain with provider
status. `start()` remains one-shot after cancellation, matching the upstream
implementation. Repeated calls do not retry the cancelled startup.

The test fixture selects the installed override by default and verifies the
backported startup contract against the published alpha.2 implementation.

The bridge calls `start()` for a newly created idle Agent. Starting during
maintenance or using start as a general resume operation is outside this
backport's protocol contract; existing wake, cancellation, and maintenance
semantics are intentionally not changed. Replace this local package with a
published release once its message-free startup contract passes these tests and
the end-to-end Project Creator smoke.
