# anbanwriter for Codex

Professional **WeChat** and **Seednote (种草笔记)** content creation toolkit for OpenAI Codex — MCP server with AI-powered writing, visual design, and multi-platform publishing. This is the Codex-native port of [`claudecode/`](https://github.com/anbanai/anbanwriter-claudecode), connecting to the same `anbanwriter` MCP server.

## What you get

- **18 auto-discovered skills** (SKILL.md format): content writing, WeChat article assembly, Seednote viral analysis, live video slicing, line-art coloring, short-video cover replication, portrait pose variants, SEO, and more.
- **5 native Codex subagents**: end-to-end orchestrators (10,000+ word pipelines each) for the workflows above.
- **MCP integration**: connects to the anbanwriter HTTP MCP server for channel management, image generation, WeChat publishing, TingWu transcription, and FFmpeg-driven clip assembly.
- **Lifecycle hooks**: `SubagentStop` (per-agent delivery summary) + `Stop` (generic QA gate).

## Prerequisites

- **Codex CLI** installed (`codex --version` works)
- **`ffmpeg` + `ffprobe`** (only required for `live-slicer` / `live-slice`)
- **`jq`** (used by some skill-side validation steps)
- **An anbanwriter account** at https://creator.anbanai.com — grab your API Key

## Installation

### 1. Install the plugin

From a local clone:

```bash
codex plugin marketplace add ./codex
codex plugin install anbanwriter
```

### 2. Install the subagents

Codex plugins cannot bundle subagents (open limitation — see `CODEX.md`). The five subagents live in `codex/agents/*.toml` and must be copied to `~/.codex/agents/`:

```bash
bash codex/install/install-subagents.sh
```

The script is idempotent and does three things:
1. **Copies `agents/*.toml` to `~/.codex/agents/`**, substituting `__PLUGIN_ROOT__` with the discovered plugin install path (so each `[[skills.config]]` path resolves correctly).
2. **Merges `install/agents-registration.toml` into `~/.codex/config.toml`** — adds `[features] multi_agent = true`, `[agents] max_threads = 6`, and one `[agents.<name>]` block per subagent. Existing tables are preserved (no overwrites).
3. **Prints next-step reminders** (env var setup, restart Codex, verify).

### 3. Set API credentials

Add to `~/.zshrc` (or `~/.bashrc`):

```sh
export ANBANWRITER_API_KEY="<your-api-key-from-creator.anbanai.com>"
# Optional overrides:
# export ANBANWRITER_API_URL="https://api.creator.anbanai.com"  # default
# export ANBANWRITER_DEFAULT_CHANNEL="<channel-id>"             # skip list_channels
```

Then `source ~/.zshrc` (or restart your terminal).

### 4. Restart Codex

Quit and relaunch Codex so it picks up the new subagents, MCP server, and skills.

## Verification

After restart, run:

```
/skills
```

Expected: ~18 anbanwriter skills listed (article, content-writing, seednote, line-art-coloring, etc.) with no "some skills omitted" warning.

```
/agents
```

Expected: 5 subagents listed (wechatarticle, seednote, designer, live-slicer, short-video-studio) with their nicknames.

```
$init
```

Expected: the init skill runs `list_channels` and returns your configured channels. If it fails, your `ANBANWRITER_API_KEY` is missing or invalid.

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
| `seednote` | Topic/viral analysis → Markdown note (title + body + hashtags) → cover + 3-8 content images + tail image → archive |
| `designer` | Per-lineart `colored_NN.png` + Color Bible + consistency report (PASS/MINOR/FAIL per entity) + manual-review flags |
| `live-slicer` | metadata.json + audio.mp3 + cover.jpg + TingWu analysis + filtered sentences + clip plan + exported MP4s + CapCut drafts + transcript.md + summary.md |
| `short-video-studio` | Routes between `short-video-cover` (replicate a reference cover) and `portrait-pose-variants` (N identity-locked pose variants) based on input |

## Troubleshooting

### `/agents` shows nothing

- Confirm `~/.codex/agents/*.toml` exists (5 files).
- Confirm `~/.codex/config.toml` contains `[agents.wechatarticle]` etc.
- Confirm `[features] multi_agent = true` is in `config.toml`.
- Fully restart Codex (not just reload).

### Subagent fails with "MCP tool not found"

- `echo $ANBANWRITER_API_KEY` — should print your key.
- Confirm `~/.codex/config.toml` has `[mcp_servers.anbanwriter]` (the install script does not add this; set it manually if you skipped plugin install):
  ```toml
  [mcp_servers.anbanwriter]
  url = "${ANBANWRITER_API_URL:-https://api.creator.anbanai.com}/mcp"
  bearer_token_env_var = "ANBANWRITER_API_KEY"
  ```

### Skill paths in subagent TOMLs are wrong

The install script substitutes `__PLUGIN_ROOT__` based on `~/.codex/plugins/cache/...`. If you installed the plugin to a non-default location, set `ANBANWRITER_PLUGIN_ROOT` and re-run:

```bash
ANBANWRITER_PLUGIN_ROOT=/custom/path bash codex/install/install-subagents.sh
```

### Hooks don't fire

Plugin-bundled hooks require explicit user approval in Codex. Check Codex's plugin trust UI; if hooks are still blocked, the same delivery summaries will run via the prompts embedded in each subagent's `developer_instructions` (so quality reports still happen — just less reliably).

## Differences from the Claude Code plugin

See [CODEX.md](./CODEX.md#codex-vs-claude-code-differences) for the full mapping table. Summary:

- Subagents only run when explicitly invoked (`use the X subagent`).
- Subagents declare their own MCP servers and skills (no inheritance).
- No `TaskCompleted` event — `Stop` matcher `"*"` replaces it.
- Plugin hooks require explicit trust review.
- Skills, themes, writers, MCP tool names, and content pipelines are byte-identical to `claudecode/`.

## License

MIT — see [LICENSE](./LICENSE).
