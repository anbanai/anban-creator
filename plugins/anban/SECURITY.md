# Security Policy

## Supported Versions

Security fixes are accepted for the latest `2.10.x` Claude Code plugin release. Older plugin builds may continue to work, but fixes should be made against the current release line first.

## Reporting a Vulnerability

Please report suspected vulnerabilities privately by email:

- `hello@anban.ai`

Do not open a public GitHub issue for secrets, credential exposure, authentication bypasses, unsafe hook behavior, or MCP access-control concerns.

When possible, include:

- Plugin version from `.claude-plugin/plugin.json`
- Claude Code version and operating system
- A short reproduction path
- The affected file, command, agent, skill, hook, or MCP tool
- Whether credentials, draft content, media files, or project data may be exposed

## Secret Handling

Never include API keys, bearer tokens, cookies, private draft URLs, or full MCP authorization headers in issues, logs, screenshots, fixtures, or generated artifacts. The plugin declares `api_key` as a sensitive Claude Code `userConfig` value; diagnostics should only report whether it is present.

## Plugin Updates

Plugin releases must update `.claude-plugin/plugin.json` and `CHANGELOG.md` together.
