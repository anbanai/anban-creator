# Vendored DeepSeek Harness SDK

These four packages are vendored from the published DeepSeek Harness
`0.2.1-alpha.4` packages. The published SDK provides the current session
resume/wait protocol; Anban adds a small compatibility extension for the
interactive `session/start` and `session/answer` requests used by Studio.
The upstream package remains the source of truth for all other runtime code.

Only the package runtime JavaScript and declaration files are tracked. Build
metadata and source maps are intentionally omitted. The package versions and
lockfile stay at `0.2.1-alpha.4` so the complete dependency graph is upgraded
together.

The npm dependencies in `runtime-dsh/package.json` and
`runtime-dsh/package-lock.json` deliberately point to these local packages.
Do not replace the vendored files or remove the compatibility extension unless
the interactive protocol contract tests and deployed startup smoke continue to
pass.

The runtime also applies `scripts/patch-dsh-dependencies.mjs` during image build
and before local tests. It is a pinned, layout-checked patch to the published
`@deepseek-ai/dsh-llm@0.2.1-alpha.4` failure normalizer so `TRANSPORT` errors
retain their sanitized `Error.cause` chain in the durable turn diagnostic. The
script fails closed if the upstream package layout changes.
