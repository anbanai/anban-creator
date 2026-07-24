# Fixed Plugin MCP Endpoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every official Anban plugin host use the fixed MCP endpoint `https://creator.anbanai.com/mcp` while exposing only API-key configuration to users.

**Architecture:** Treat the full MCP URL as an endpoint, never as a Base URL that consumers extend. Claude stores the literal endpoint in `.mcp.json`; Codex registration and Agent TOMLs store the same literal endpoint. Parent-repository contract tests enforce the child plugin contents and all user-facing connection guides omit endpoint configuration, while Miniapp's separate REST API base remains unchanged.

**Tech Stack:** Claude Code plugin JSON, Codex TOML, Markdown Skills/docs, Go filesystem contract tests, React/TypeScript, Vue/Uniapp, Bun.

---

## File Map

- `server/agent/naming_contract_test.go`: canonical fixed-endpoint and Claude user-config contract.
- `server/agent/claude_plugin_best_practices_test.go`: official Claude manifest validation.
- `server/agent/designer_contract_test.go`: Agent diagnostics contract without URL configuration.
- `server/agent/runtime_policy_test.go`: ad hoc MCP client example independent of removed environment variables.
- `plugins/.claude-plugin/plugin.json`: Claude API-key-only installation schema and version.
- `plugins/.claude-plugin/marketplace.json`: Claude marketplace release version.
- `plugins/.codex-plugin/plugin.json`: Codex release version.
- `plugins/.mcp.json`: fixed Claude MCP endpoint.
- `plugins/install/agents-registration.toml`: fixed Codex MCP endpoint.
- `plugins/agents/*.{md,toml}`: fixed Codex endpoint and key-only diagnostics.
- `plugins/skills/anban-setup/SKILL.md`: key/auth/connectivity diagnostics only.
- `plugins/{README.md,CODEX.md,docs/*.md,CHANGELOG.md}`: fixed-endpoint documentation and release notes.
- `studio/src/components/connect/{ClaudeGuide,CodexGuide}.tsx`: key-only hosted-plugin setup guidance.
- `miniapp/src/pages/connect/{claude-code,codex}.vue`: key-only hosted-plugin setup guidance.
- `miniapp/scripts/parity-test.mjs`: Miniapp connection-guide regression checks while preserving the REST API base contract.

### Task 1: Add Failing Fixed-Endpoint Contracts

**Files:**
- Modify: `server/agent/naming_contract_test.go`
- Modify: `server/agent/claude_plugin_best_practices_test.go`
- Modify: `server/agent/designer_contract_test.go`
- Modify: `miniapp/scripts/parity-test.mjs`

- [ ] **Step 1: Make the naming contract require API-key-only Claude configuration**

Change `assertClaudePluginUserConfig` so it requires exactly one `userConfig`
entry named `api_key`. Remove the `api_url` default assertion and fail when any
second install option is present:

```go
if len(object.UserConfig) != 1 {
	t.Fatalf("%s userConfig has %d entries, want only api_key", path, len(object.UserConfig))
}
if _, ok := object.UserConfig["api_url"]; ok {
	t.Fatalf("%s must not expose api_url as an install option", path)
}
```

Replace the `.mcp.json` interpolation assertion with:

```go
assertFileContains(t, filepath.Join(root, "plugins", ".mcp.json"), `"url": "https://creator.anbanai.com/mcp"`)
assertFileNotContains(t, filepath.Join(root, "plugins", ".mcp.json"), "user_config.api_url")
```

- [ ] **Step 2: Make the best-practices test reject endpoint user configuration**

In `TestClaudeCodePluginManifestMatchesOfficialBestPracticeFields`, retain the
required sensitive `api_key` assertion, require `len(manifest.UserConfig) == 1`,
and remove the old `api_url` default assertion.

- [ ] **Step 3: Add one cross-surface fixed-endpoint contract**

Add a table-driven test in `naming_contract_test.go` that checks `.mcp.json`,
`install/agents-registration.toml`, and every `agents/*.toml` file for the exact
full endpoint. It must also scan distributed plugin text for the obsolete
`ANBAN_API_URL`, `user_config.api_url`, and `https://api.creator.anbanai.com/mcp`
forms. Scope the scan to plugin-owned connection configuration so unrelated
REST API URLs are not rejected.

- [ ] **Step 4: Update diagnostic contract expectations**

Remove `ANBAN_API_URL` from the required Designer Agent terms and add it to the
forbidden terms. Keep `ANBAN_API_KEY`, `ANBAN_DEFAULT_PROJECT`, MCP tool
availability, and stop-on-failure requirements unchanged.

- [ ] **Step 5: Add Miniapp guide regression assertions**

Extend `miniapp/scripts/parity-test.mjs` with `assertContains` checks proving
both connection guides contain `ANBAN_API_KEY`, plus file-content assertions
that reject `ANBAN_API_URL` and `api.creator.anbanai.com` in those two guide
files. Do not change the existing assertion that
`src/api/api-base.ts` contains `https://api.creator.anbanai.com/api/v1`.

- [ ] **Step 6: Run tests and confirm the expected red state**

Run:

```bash
go test ./server/agent -run 'TestAnbanCreatorNamingContract|TestClaudeCodePluginManifestMatchesOfficialBestPracticeFields|TestDesignerAgentKeepsMCPAndSkillContract' -count=1
cd miniapp && bun run test
```

Expected: both commands fail because the current plugin and Miniapp guides
still expose `api_url` / `ANBAN_API_URL` and use the old host.

### Task 2: Update The Canonical Plugin Distribution

**Files:**
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.claude-plugin/marketplace.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `plugins/.mcp.json`
- Modify: `plugins/install/agents-registration.toml`
- Modify: `plugins/agents/article.toml`
- Modify: `plugins/agents/designer.md`
- Modify: `plugins/agents/designer.toml`
- Modify: `plugins/agents/ecommerce.md`
- Modify: `plugins/agents/ecommerce.toml`
- Modify: `plugins/agents/live-slicer.toml`
- Modify: `plugins/agents/moments.toml`
- Modify: `plugins/agents/montage.toml`
- Modify: `plugins/agents/seednote.toml`
- Modify: `plugins/skills/anban-setup/SKILL.md`
- Modify: `plugins/README.md`
- Modify: `plugins/CODEX.md`
- Modify: `plugins/docs/codex-installation.md`
- Modify: `plugins/docs/plugin-development.md`
- Modify: `plugins/CHANGELOG.md`

- [ ] **Step 1: Make Claude installation API-key-only**

Delete `userConfig.api_url` from `.claude-plugin/plugin.json` and set
`.mcp.json` to:

```json
"url": "https://creator.anbanai.com/mcp"
```

Keep the existing bearer header and 900000 ms timeout unchanged.

- [ ] **Step 2: Fix every Codex MCP declaration**

Change `install/agents-registration.toml` and every distributed Agent TOML from
the `ANBAN_API_URL` fallback expression to:

```toml
url = "https://creator.anbanai.com/mcp"
bearer_token_env_var = "ANBAN_API_KEY"
```

- [ ] **Step 3: Remove URL diagnostics and override instructions**

In Designer and Ecommerce Agent instructions, diagnose only API-key presence,
default-project presence, and inherited MCP tool availability. In
`anban-setup`, delete Claude `api_url`, Codex `ANBAN_API_URL`, alternate-server,
and URL-change FAQ guidance. Keep `list_projects`, auth errors, connectivity
errors, and secret-redaction guidance.

- [ ] **Step 4: Rewrite plugin documentation around the fixed endpoint**

State that the official plugin connects automatically to
`https://creator.anbanai.com/mcp` and that users configure only the key. Remove
self-host/local override instructions and Base URL interpolation from README,
Codex installation, developer notes, and `CODEX.md`.

- [ ] **Step 5: Publish plugin version 4.0.6 consistently**

Set `4.0.6` in both native manifests and Claude marketplace metadata. Add a
`4.0.6` changelog entry explaining that the official MCP endpoint is fixed and
the install-time URL option was removed.

- [ ] **Step 6: Verify the plugin tree contains no obsolete connection contract**

Run:

```bash
rg -n 'api_url|ANBAN_API_URL|api\.creator\.anbanai\.com/mcp|\$\{.*URL.*\}/mcp' plugins --hidden --glob '!**/.git/**'
```

Expected: no matches.

- [ ] **Step 7: Commit the child repository change**

From `plugins/`, stage only the listed plugin files and commit:

```bash
git commit -m "fix: use fixed creator MCP endpoint"
```

Record the resulting child SHA for the parent gitlink.

### Task 3: Align Product Connection Guides

**Files:**
- Modify: `studio/src/components/connect/ClaudeGuide.tsx`
- Modify: `studio/src/components/connect/CodexGuide.tsx`
- Modify: `miniapp/src/pages/connect/claude-code.vue`
- Modify: `miniapp/src/pages/connect/codex.vue`
- Modify: `server/agent/runtime_policy_test.go`

- [ ] **Step 1: Remove Studio endpoint configuration guidance**

Delete the paragraphs offering `ANBAN_API_URL` and self-host/local service
addresses. Keep the API-key instructions and state that the official endpoint
is configured by the plugin.

- [ ] **Step 2: Remove Miniapp endpoint snippets**

Remove `ANBAN_API_URL` from the Claude settings JSON. Remove the Codex endpoint
paragraph, copy block, and `envUrlSnippet`; keep only `ANBAN_API_KEY`.

- [ ] **Step 3: Decouple the runtime policy example from the removed variable**

Change the blocked curl example to a literal non-production MCP URL such as:

```go
ToolInput: map[string]any{"command": `curl -s "https://server.example.com/mcp"`},
```

The policy behavior and assertions remain unchanged.

- [ ] **Step 4: Run targeted tests to reach green**

Run:

```bash
go test ./server/agent -run 'TestAnbanCreatorNamingContract|TestClaudeCodePluginManifestMatchesOfficialBestPracticeFields|TestDesignerAgentKeepsMCPAndSkillContract|TestManagedAgentRuntimePolicyBlocksAdHocMCPClients' -count=1
cd miniapp && bun run test
```

Expected: PASS.

### Task 4: Full Verification And Parent Commit

**Files:**
- Modify: `plugins` gitlink in the parent repository
- Modify: files from Tasks 1 and 3
- Add: `docs/superpowers/plans/2026-07-24-fixed-plugin-mcp-endpoint.md`

- [ ] **Step 1: Verify all plugin and host contracts**

Run:

```bash
go test ./server/agent -count=1
go test ./server/mcp -count=1
go test ./... -count=1
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test && bun run build
cd miniapp && bun run test && bun run type-check
```

Expected: every command exits 0 with no test failures or build errors.

- [ ] **Step 2: Audit endpoint and REST URL separation**

Run:

```bash
rg -n 'api_url|ANBAN_API_URL|api\.creator\.anbanai\.com/mcp' plugins studio/src/components/connect miniapp/src/pages/connect
rg -n 'https://api\.creator\.anbanai\.com/api/v1' miniapp/src/api/api-base.ts miniapp/scripts/parity-test.mjs
```

Expected: the first command has no matches; the second finds the intentional
Miniapp REST API base and its parity assertion.

- [ ] **Step 3: Inspect child and parent diffs**

Run `git -C plugins status --short`, `git -C plugins show --stat --oneline HEAD`,
`git status --short`, `git diff --check`, and `git diff --submodule=log`. Confirm
that the unrelated untracked timeout plan remains untouched.

- [ ] **Step 4: Commit the parent repository change**

Stage only the parent tests, connection guides, implementation plan, and
`plugins` gitlink. Commit:

```bash
git commit -m "fix: pin official plugin MCP endpoint"
```

- [ ] **Step 5: Verify final repository state**

Confirm the parent points to the new child SHA, the child worktree is clean,
and only pre-existing unrelated user files remain untracked or modified.
