# Montage Environment Configuration Design

## Goal

Make Montage runtime environment configuration follow upstream OpenMontage changes without requiring an Anban code release for every new environment variable.

## Contract

- The only YAML field is `montage.env`.
- `env` accepts arbitrary environment variable names and string values. Anban does not maintain a provider-key allowlist.
- Empty values are not injected into a task process.
- Configured variables are injected only into Montage tasks.
- Secret values remain process-only. MCP profiles expose a redacted `env` map whose values indicate whether each variable is configured.
- The old `provider_env` name is removed without a compatibility path because this is a new project.

## Data Flow

The server loads `montage.env`, validates environment variable names structurally, and passes the map into each Montage execution option. Local execution injects the configured map directly. Docker execution adds it to the Montage agent container, whose Claude subprocess inherits the container environment. Kubernetes execution includes it in the authenticated bootstrap response, and the standalone agent injects that explicit map into the Claude subprocess only for a Montage task. No path discovers keys by scanning the host or container environment.

The MCP profile and the Claude Code, OpenClaw, and Codex workflow contracts use `env` and `env_keys`. They never expose environment variable values.

## Safety

Removing the provider allowlist must not broaden task scope. Environment injection remains gated by `task.Type == montage`. Names are rejected only when they are not valid process environment names, such as an empty name or one containing `=` or a NUL byte. Empty values remain excluded. Platform-owned identity, project, submodule-path, and Claude runtime variables take precedence when the same key is configured in Montage. Secrets are not written to task files, workspace manifests, logs, or MCP responses.

## Testing

Regression tests use a synthetic future upstream key such as `NEW_PROVIDER_TOKEN` to prove that configuration, local/Docker/Kubernetes execution, standalone-agent forwarding, and redaction do not depend on a known-key list. Existing tests continue to prove that non-Montage tasks receive no Montage environment and that secret values do not appear in generated files or profile JSON.

Distribution parity tests and full Go tests verify that all three plugin copies use the new contract. Each affected plugin manifest receives a patch version bump.
