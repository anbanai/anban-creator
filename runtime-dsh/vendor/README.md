# Vendored DeepSeek Harness SDK

These four packages are vendored from the published DeepSeek Harness
`0.2.2-alpha.2` packages. The published SDK provides the current session
resume/wait protocol; Anban adds a small compatibility extension for the
interactive `session/start` and `session/answer` requests used by Studio.
`session/start.initialContext` injects the Server snapshot into that logical
session before its first turn. `session.question` announces a question only
after its answer handler is registered, so Studio can submit immediately.
The client must preserve the initialization capabilities advertising these
extensions so the Bridge can validate support before starting a session.
The upstream package remains the source of truth for all other runtime code.

Only the package runtime JavaScript and declaration files are tracked. Build
metadata and source maps are intentionally omitted. The package versions and
lockfile stay at `0.2.2-alpha.2` so the complete dependency graph is upgraded
together.

The npm dependencies in `runtime-dsh/package.json` and
`runtime-dsh/package-lock.json` deliberately point to these local packages.
Do not replace the vendored files or remove the compatibility extension unless
the interactive protocol contract tests and deployed startup smoke continue to
pass.

`npm run test:startup` checks this handshake and starts all three assistant
presets with the real SDK against local MCP and model fixtures. CI runs it
after the unit tests; it requires the checked-out `harness` submodule.

The runtime also applies `scripts/patch-dsh-dependencies.mjs` during image build
and before local tests. It is a pinned, layout-checked patch to the published
`0.2.2-alpha.2` packages: `@deepseek-ai/dsh-llm` keeps the sanitized
`Error.cause` chain for `TRANSPORT` diagnostics, and
`@deepseek-ai/dsh-llm-deepseek` preserves truncated tool arguments so
ToolRuntime can return `INVALID_ARGS` without running a tool body or ending the
interactive session. The script fails closed if either upstream package layout
changes.
