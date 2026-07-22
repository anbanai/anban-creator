# Contributing

Thanks for improving the Anban Creator Claude Code plugin.

## Development Setup

Work from the repository root:

```bash
go test ./server/agent ./server/mcp
```

For broader changes, run:

```bash
go test ./...
```

Avoid `go build ./server` and `go build ./agent` from the repository root. Use explicit output paths instead:

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

## Plugin Rules

- Keep all plugin runtime content in `plugins/`.
- Maintain one shared `skills/` tree; keep host-specific behavior in native manifests, Agents, MCP files, Hooks, and installers.
- Do not add `tools` or `mcpServers` to plugin agent frontmatter unless a contract test and developer note explain why.
- Do not print API keys or authorization headers. Check only whether a sensitive value exists.
- Keep `SKILL.md` files concise; move long references, examples, and rubrics into `references/`.
- Use hook command exec form (`command` plus `args`) when paths include plugin environment placeholders.

## Versioning

When changing plugin distribution assets, bump `.claude-plugin/plugin.json` in the same change. Use a patch bump for documentation, hook shape, compatibility, and workflow-contract fixes.

Update `CHANGELOG.md` with a short entry describing user-visible changes and maintenance fixes.

## Pull Request Checklist

- The changed surface has focused tests.
- Relevant `server/agent` or `server/mcp` contract tests pass.
- Plugin manifests and changelog are updated when distribution assets change.
- No generated output, logs, screenshots, or docs contain secrets.
