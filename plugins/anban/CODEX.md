# CODEX.md

This file provides guidance to OpenAI Codex (codex CLI / IDE) when working with code in this repository.

## Project Overview

This directory is the unified Anban plugin source for Claude Code and Codex. Codex uses the shared workflows below through native TOML subagents and Codex-specific adapters:

- **WeChat Official Account articles** (微信公众号图文)
- **SeedNote posts** (种草笔记)
- **Moments posts** (朋友圈)
- **Live video slicing** (直播切片)
- **Line art coloring** (线稿上色)
- **E-commerce product imagery** (电商出图：主图/详情/封面/分享/SKU，多产品图输入保一致)

Both hosts connect to the same `anban-creator` MCP server. Skills, themes, writers, layouts, and content contracts exist once in this directory.

## Architecture

The plugin follows Codex's **Skill + Subagent + MCP** model:

- **Skills** (`skills/`) — the canonical shared Skill tree auto-discovered by both hosts
- **Subagents** (`agents/`) — seven TOML files installed to `~/.codex/agents/` and registered in `~/.codex/config.toml` (Codex plugins cannot bundle subagents directly — see GitHub issue #18988)
- **MCP server** (`install/agents-registration.toml`) — installed into Codex config with `ANBAN_API_KEY`; each TOML subagent also declares its MCP dependency
- **Completion checks** — embedded in TOML subagent instructions because the current Codex plugin manifest does not accept bundled Hooks

### Subagents (`agents/`)

| Subagent | Triggers | Pipeline |
|----------|----------|----------|
| `wechatarticle` | "写文章", "发文章", "公众号文章" | Research → Write → De-AI → SEO → Cover → Illustrations → HTML → Draft |
| `seednote` | "种草笔记", "种草", "复刻", "仿写" | Research → Viral analysis (replicate) → Content → image-plan/runtime mode output → Compliance → Delivery validation → `$DIR` delivery |
| `live-slicer` | "直播切片", "剪直播", "听悟" | ffmpeg prep → TingWu transcription → Invalid sentence filter → Segment/subject planning → Batch cuts/concat → CapCut export → Report |
| `designer` | "上色", "填色", "线稿", "color consistency", "designer" | Init → Progressive coloring → Full audit → Best-effort correction/backtracking → Report with `needs_img2img` where strict line preservation is impossible |
| `ecommerce` | "电商出图", "商品图", "主图", "详情页", "商详", "SKU图" | Product Bible → Selling points → Asset plan → Provider-adaptive generation → Vision self-check → Delivery validation → `$DIR` delivery |

**Codex-specific behavior**:
- Subagents only spawn when the user **explicitly** asks ("use the wechatarticle subagent to ...", "delegate to X"). Codex does not auto-spawn subagents.
- Each subagent declares its own `[mcp_servers.creator]` and `[[skills.config]]` — it does **not** inherit MCP servers or skills from the parent session.
- Multi-agent mode requires `[features] multi_agent = true` in `~/.codex/config.toml` (the install script adds this automatically).
- `[agents] max_threads = 7` bounds concurrent subagent execution.

### Skills (`skills/`)

Each shared skill has a `SKILL.md` with YAML frontmatter (`name` + `description`). Codex loads skills based on prompt matching and subagent declarations; there is no second Codex Skill copy.

Key skill groups:
- **Content**: `content-writing`, `topic-research`, `seo-optimization`
- **WeChat article**: `article`, `article-visual-design`, `article-publishing`
- **SeedNote**: `seednote`, `seednote-research`, `seednote-viral-analysis`, `seednote-writing`, `seednote-visual-design`
- **Live slicing**: `live-slice`, `capcut-draft`
- **Design**: `line-art-coloring`
- **Media and design**: `short-video-cover`, `portrait-pose-variants`, `capcut-draft`
- **Setup**: `anban-setup` (first-time API Key setup and connectivity verification; Codex-specific — does not auto-write `~/.codex/config.toml`, documents manual setup steps instead)
- **Config**: `config` (project-level runtime configuration: writer, theme, image provider, positioning)

### MCP Server

The installer registers `https://api.creator.anbanai.com/mcp` with Bearer token auth via `ANBAN_API_KEY`. Key MCP tools:

- `list_projects`, `get_project_profile`, `list_drafts`, `list_published_articles`, `list_project_titles`
- `prepare_workspace`
- `render_template`, `convert_markdown`
- `generate_image`, `upload_image`, `download_image`, `compress_image`, `analyze_image`
- `publish_draft` (WeChat draft box)
- `get_feed_detail` (SeedNote source note fetching)
- `upload_live_audio`, `create_live_analysis_task`, `query_live_analysis_task`, `recognize_live_invalid_sentences`, `recognize_live_segments`, `build_live_clip_plan`, `build_live_subject_clip_plan`, `build_live_clip_manifest`, `recognize_live_subjects`, `complete_live_subject`
- `prepare_file_upload`

### Themes (Server-managed)

Themes define visual styling for article排版. Themes are managed server-side via the MCP server's `convert_markdown` tool. Each project has a configured theme applied automatically during Markdown-to-WeChat-HTML conversion.

### Writers (`skills/writers/`)

YAML files defining **writing** styles (the writer dimension only). Each has `name`, `english_name`, `writing_prompt` (required), plus optional `core_beliefs`, `title_formulas`, `quote_templates`. Writers **do not** carry visual identity — image visual style is an orthogonal dimension configured per project/task (resolved at runtime as the `visual_style` field; see `article-visual-design` skill). Built-in styles: `dan-koe`, `cultural-depth`, `casual-science`.

### Completion Checks

The current Codex plugin manifest does not accept bundled Hooks. Each TOML subagent therefore owns its delivery validation and final quality summary in `developer_instructions`. `hooks/hooks.json` is the Claude Code adapter and is not a Codex fallback.

## Key Conventions

- **Zero user interaction**: All subagents run autonomously. Decisions are recorded in `$DIR/*.md` files, never by asking the user.
- **Workspace isolation**: Each creation task calls `prepare_workspace` MCP tool to obtain the canonical workspace path, then creates the directory locally with `mkdir -p`. Every artifact remains there; server `task_files`, `execution_id`, and OSS storage own their persistence and version boundaries.
- **File naming**: Subagents use numbered prefixes (`01-research.md`, `02-outline.md`...) or semantic names (`cover.png`, `content.md`, `image-plan.md`).
- **Image reference chain**: First image establishes visual style; subsequent images use the first as reference to maintain consistency. For line-art coloring, current `generate_image` is best-effort reference-image generation, not a guaranteed line-preserving colorize tool.
- **Skill references**: Subagents invoke skills via `using the <skill-name> skill` phrasing, not the Skill tool.
- **Content is Chinese**: All generated content targets Chinese social media platforms. Prohibited words lists (违禁词) are in `references/prohibited-words.md`.
- **Live media dependency**: `live-slicer` and `live-slice` require local `ffmpeg` and `ffprobe`; TingWu provides transcription.
- **Subagent invocation**: Codex subagents do NOT auto-spawn. To run a full pipeline, the user must explicitly invoke: "use the wechatarticle subagent to write an article about X" or "delegate to designer: colorize line art at /path".

## Modifying This Plugin

- **Adding a new skill**: Create `skills/<name>/SKILL.md` with YAML frontmatter `name` + `description`. Add `references/` for detailed guides. Codex auto-discovers skills via the plugin's `skills` manifest field.
- **Adding a new subagent**: Create `agents/<name>.toml` with required fields (`name`, `description`, `developer_instructions`) and optional `[mcp_servers.*]` / `[[skills.config]]` sections. Update `install/agents-registration.toml` to add the `[agents.<name>]` block. Re-run `install/install-subagents.sh`.
- **Adding a new theme**: Themes are managed server-side. Contact the server admin to add new themes.
- **Adding a new writer style**: Add `skills/writers/<name>.yaml` with required `name`, `english_name`, `writing_prompt`.

## Codex vs Claude Code Differences

| Aspect | Claude Code adapter | Codex adapter |
|--------|------------------------------|------------------|
| Plugin manifest | `.claude-plugin/plugin.json` | `.codex-plugin/plugin.json` (camelCase fields) |
| Subagent format | `agents/*.md` with YAML frontmatter | `~/.codex/agents/*.toml` (TOML) + registration in `~/.codex/config.toml` |
| Subagent auto-spawn | Not applicable (parent calls subagent) | Never — must be explicit (`use the X subagent`) |
| Skills inheritance | Skills inherit from parent session | Skills MUST be declared per-subagent via `[[skills.config]]` |
| Lifecycle checks | `hooks/hooks.json` plus managed runtime Hooks | Embedded in each TOML subagent instruction |
| Bundled Hooks | Supported by Claude Code manifest | Not accepted by the current Codex plugin manifest |
| MCP server list | `mcpServers` in frontmatter | `[mcp_servers.X]` table in TOML |
| Tools allowlist | `tools:` frontmatter field | `sandbox_mode` field (read-only / workspace-write / danger-full-access) |
| Model override | `model: inherit` | Omit `model` field to inherit parent session |
| Max turns | `maxTurns: 300` | No direct equivalent — subagents run to completion or until the user cancels (optionally bounded by Codex's global `job_max_runtime_seconds`, which this plugin does not set) |

## Installation

See [docs/codex-installation.md](docs/codex-installation.md) for end-user installation instructions.
