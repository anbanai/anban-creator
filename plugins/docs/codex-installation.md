# Anban Creator for Codex

Professional **WeChat** and **Seednote (种草笔记)** content creation toolkit for OpenAI Codex. This Codex adapter lives in the same `plugins/` source as the Claude Code plugin and uses the shared Skill tree.

## What you get

- **29 auto-discovered skills** (SKILL.md format): content writing, WeChat article assembly, Seednote viral analysis, Agent-Reach-backed Xiaohongshu research, WeChat Moments packages, live video slicing, line-art coloring, short-video cover replication, portrait pose variants, SEO, e-commerce product imagery, and more.
- **7 native Codex subagents**: end-to-end orchestrators for the workflows above.
- **MCP integration**: connects to the anban-creator HTTP MCP server for project management, image generation, WeChat publishing, TingWu transcription, and FFmpeg-driven clip assembly.
- **Embedded completion checks**: every TOML subagent performs its own delivery validation and final quality summary.

## Prerequisites

- **Codex CLI** installed (`codex --version` works)
- **`ffmpeg` + `ffprobe`** (required for `live-slicer` and `live-slice`)
- **`jq`** (used by some skill-side validation steps)
- **An Anban Creator account** at https://creator.anbanai.com — grab your API Key

## Installation

### 1. Install the plugin

From a local clone:

```bash
codex plugin marketplace add ./plugins
codex plugin install anban
```

### 2. Install the subagents

Codex plugins cannot bundle subagents (open limitation — see `../CODEX.md`). The seven subagents live in `agents/*.toml` and must be copied to `~/.codex/agents/`:

```bash
bash plugins/install/install-subagents.sh
```

The script is idempotent and does three things:
1. **Copies `agents/*.toml` to `~/.codex/agents/`**, substituting `__PLUGIN_ROOT__` with the discovered plugin install path (so each `[[skills.config]]` path resolves correctly).
2. **Merges `install/agents-registration.toml` into `~/.codex/config.toml`** — adds the `creator` MCP server, `[features] multi_agent = true`, `[agents] max_threads = 7`, and one `[agents.<name>]` block per subagent. Existing tables are preserved (no overwrites).
3. **Prints next-step reminders** (env var setup, restart Codex, verify).

### 3. Set API credentials

Add to `~/.zshrc` (or `~/.bashrc`):

```sh
export ANBAN_API_KEY="<your-api-key-from-creator.anbanai.com>"
# Optional overrides:
# export ANBAN_API_URL="https://api.creator.anbanai.com"  # default
# export ANBAN_DEFAULT_PROJECT="<project-id>"             # skip list_projects
```

Then `source ~/.zshrc` (or restart your terminal).

### 4. Restart Codex

Quit and relaunch Codex so it picks up the new subagents, MCP server, and skills.

## Verification

After restart, run:

```
/skills
```

Expected: Anban Creator skills listed (article, content-writing, seednote, ecommerce, live-slice, line-art-coloring, etc.) with no "some skills omitted" warning.

```
/agents
```

Expected: 7 subagents listed (wechatarticle, seednote, moments, montage, designer, live-slicer, ecommerce) with their nicknames.

```
$anban-setup
```

Expected: the anban-setup skill runs `list_projects` and returns your configured projects. If it fails, your `ANBAN_API_KEY` is missing or invalid.

## Usage

### Implicit (skills auto-load)

Just ask in natural language:

```
写一篇关于 SwiftUI 与 Jetpack Compose 对比的文章
```

Codex's main agent detects the request, loads `article`, `content-writing`, `topic-research`, etc. on demand. No subagent is spawned.

### Explicit (full pipeline via subagent)

For end-to-end runs that produce a complete publishable artifact, delegate to a subagent:

```
use the wechatarticle subagent to write a 3000-word article about Rust ownership
```

```
delegate to designer: colorize the line art at /path/to/lineart/ using a warm summer palette
```

```
use the live-slicer subagent on /path/to/live.mp4 — pull 5 high-density clips
```

The subagent runs autonomously, writes intermediate artifacts to `$DIR/*.md`, calls MCP tools, and emits a delivery summary on completion. **You cannot interrupt mid-run** (zero-interaction contract).

### Hybrid

If you only want one capability (e.g. cover generation without writing the article), invoke the skill directly:

```
using the article-visual-design skill, generate a 2.35:1 cover for the article at ./draft.md
```

## Capabilities by subagent

| Subagent | What it produces |
|----------|-----------------|
| `wechatarticle` | Researched outline → final Markdown → WeChat-safe HTML → uploaded cover + content images → published draft |
| `seednote` | Topic/viral analysis → Markdown note (title + body + hashtags) → image-plan/runtime mode output → delivery validation → `$DIR` delivery |
| `designer` | Per-lineart `colored_NN.png` + Color Bible + consistency report (PASS/MINOR/FAIL per entity) + manual-review flags |
| `live-slicer` | metadata.json + audio.mp3 + cover.jpg + TingWu analysis + filtered sentences + clip plan + exported MP4s + CapCut drafts + transcript.md + summary.md |
| `ecommerce` | Product Bible (analyze product photos) → selling points (FABE) → asset plan → anchor-first generation with provider-adaptive ref strategy + vision self-check → compliance (广告法极限词) → delivery validation → `$DIR` manifest delivery |

## Troubleshooting

### `/agents` shows nothing

- Confirm `~/.codex/agents/*.toml` contains the 7 Anban subagents.
- Confirm `~/.codex/config.toml` contains `[agents.wechatarticle]` etc.
- Confirm `[features] multi_agent = true` is in `config.toml`.
- Fully restart Codex (not just reload).

### Subagent fails with "MCP tool not found"

- `test -n "$ANBAN_API_KEY"` — should exit successfully without printing the key.
- Confirm `~/.codex/config.toml` has `[mcp_servers.creator]` (the subagent installer adds it; set it manually only if you skipped that step):
  ```toml
  [mcp_servers.creator]
  url = "https://api.creator.anbanai.com/mcp"
  bearer_token_env_var = "ANBAN_API_KEY"
  ```

### Skill paths in subagent TOMLs are wrong

The install script substitutes `__PLUGIN_ROOT__` based on `~/.codex/plugins/cache/...`. If you installed the plugin to a non-default location, set `ANBAN_PLUGIN_ROOT` and re-run:

```bash
ANBAN_PLUGIN_ROOT=/custom/path bash plugins/install/install-subagents.sh
```

### Completion checks

The current Codex plugin manifest does not accept bundled Hooks. Delivery summaries therefore live directly in each subagent's `developer_instructions`; no separate Hook approval is required.

## Differences from the Claude Code plugin

See [CODEX.md](./CODEX.md#codex-vs-claude-code-differences) for the full mapping table. Summary:

- Subagents only run when explicitly invoked (`use the X subagent`).
- Subagents declare their own MCP servers and skills (no inheritance).
- Claude uses lifecycle Hooks; Codex subagents keep equivalent completion checks in their own instructions.
- Skills, themes, writers, MCP tool names, and content pipelines come from the same unified plugin tree.

## Skill Upstream Index

The canonical source/copy ledger for Anban skills lives in
[`../README.md`](../README.md#skill-上游来源与批量更新索引). Use that table before bulk-updating
shared Skills: it records which Skills embed or adapt open-source work, which ones only borrow structure, and which ones
are Anban-original workflows. Apply the repository Claude Skills maintenance checklist under `../../../docs/claude/`
before shipping Skill or Agent changes.

## License

MIT — see [LICENSE](./LICENSE).
