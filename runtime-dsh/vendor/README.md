# Vendored DeepSeek Harness SDK

These three packages are built from DeepSeek Harness commit
`8c2ae0d1ebf0e43cbe6fdebaa41495ac8676efba` and are temporarily vendored here
because the published `0.2.1-alpha.2` packages do not contain the interactive
`session/start` and `session/answer` protocol. The upstream commit is the
source of truth; this copy should be removed once an equivalent released SDK
is available and the DSH bridge has been verified against it.

Only the package runtime JavaScript and declaration files are tracked. Build
metadata and source maps are intentionally omitted. The package versions stay
at `0.2.1-alpha.2` so their peer dependency graph remains compatible with the
other DSH packages installed by the runtime image.

The npm dependencies in `runtime-dsh/package.json` and
`runtime-dsh/package-lock.json` deliberately point to these local packages.
Do not replace them with registry versions unless the interactive protocol
contract tests continue to pass.
