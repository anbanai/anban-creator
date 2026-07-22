# Changelog

All notable changes to the Anban Creator Claude Code plugin are documented here.

This project follows semantic versioning for the plugin package. Patch releases cover documentation, compatibility, hook, and workflow contract fixes that do not change the public agent or MCP behavior.

## [2.12.0] - 2026-07-22

### Changed

- Flattened the canonical plugin distribution from `plugins/anban/` to `plugins/`.
- Updated Claude Code, Codex, Docker, desktop, and repository installation paths to use the new plugin root.
- Renamed the minimal managed runtime from `creator-agent-content` to `creator-agent-article`; Seednote and Montage remain dedicated dependency images.

## [2.11.0] - 2026-07-21

### Changed

- Consolidated the Claude Code and Codex distributions under `plugins/` with one shared Skill tree.
- Kept native manifests, Agent formats, MCP authentication, Hooks, and Codex subagent installation as thin host adapters.
- Moved repository, Docker, desktop packaging, runtime discovery, tests, and maintenance scripts to the unified plugin root.

### Removed

- Removed the standalone `claudecode` and `codex` submodules and their duplicated Skill copies.

## [2.10.72] - 2026-07-21

### Removed

- Removed the `videocreator` and `videoeditor` Agents, their quality gates, and the five dedicated video editing and overlay Skills.

## [2.10.71] - 2026-07-21

### Changed

- Restored official Agent `skills:` declarations for specialized capabilities and removed duplicated runtime Skill-loading boilerplate.
- Made `videocreator` own its MCP planning and generation workflow directly.

### Removed

- Removed the unused Seedance 2.0 Skill OS distribution and its nested reference, example, schema, eval, and script assets.

## [2.10.70] - 2026-07-21

### Removed

- Removed the duplicate top-level `seednote` Skill and `/anban:seednote` command; `anban:seednote` Agent is now the only top-level Seednote workflow entry and delegates to isolated phase Skills.

## [2.10.69] - 2026-07-21

### Fixed

- Added explicit file-backed contracts for Seednote research, analysis, writing, and visual phases.
- Folded Seednote-specific de-AI editing into `seednote-writing` so the managed workflow no longer loads the large general Humanizer Skill between writing and image generation.

## [2.10.68] - 2026-07-21

### Removed

- Removed the unused `dreamina-video` compatibility Skill; video generation requests use the `videocreator` workflow directly.

## [2.10.67] - 2026-07-21

### Fixed

- Removed ignored plugin-Agent `permissionMode` declarations; managed zero-interaction enforcement remains owned by the Agent SDK runtime policy.

## [2.10.66] - 2026-07-21

### Changed

- Moved internal Agent, prompt, and Hook engineering documents to the parent repository so the distributed plugin contains only plugin-owned development documentation.

## [2.10.65] - 2026-07-21

### Changed

- Replaced dynamic video credit estimates with immutable fixed-SKU selection and durable-output settlement contracts for video generation workflows.

## [2.10.64] - 2026-07-17

### Changed

- Removed legacy workspace archive tool and kept managed task deliverables in canonical `output/` for server-side artifact collection.
- Required `task_id` for workspace preparation and removed standalone local workspace fallback behavior.

## [2.10.63] - 2026-07-17

### Changed

- Declared official agent-level `permissionMode: dontAsk` and a deterministic zero-interaction fallback contract for every managed Claude Code workflow.
- Added full-run managed preauthorization for ordinary Montage creative gates while preserving checkpoints, decision logs, and hard operational blockers.

## [2.10.62] - 2026-07-16

### Changed

- Renamed the Montage runtime environment contract to `env`/`env_keys` and allowed upstream-defined environment variable names without a provider allowlist.

## [2.10.61] - 2026-07-15

### Fixed

- Replaced tool-incapable completion prompt hooks with deterministic command gates, removed per-task whole-workflow feedback, and ran Seednote/video gates for managed main agents.
- Moved valid final feedback and Seednote title finalization into tool-capable Agent stages, made feedback/template writes idempotent, and finalized titles before visuals.
- Added verified Seednote archive staging with candidate reservations, atomic publication, recoverable failures, and source retention.

### Added

- Added GPT-5.6 prompt guidance, the Agent/Skill optimization audit, and the final Hook lifecycle implementation record.

## [2.10.60] - 2026-07-14

### Changed

- Removed execution-time credit-balance gates from video workflow contracts so accepted tasks continue while operation usage is recorded.

## [2.10.59] - 2026-07-14

### Changed

- Preserved exact `generate_image` verification dependency errors instead of relabeling billing or configuration failures as image API timeouts.
- Stopped Seednote image retries immediately when vision verification is operationally unavailable, and prohibited bypass retries with `verify_with_vision=false`.

## [2.10.58] - 2026-07-14

### Changed

- Made Agent-Reach an optional enhancement for original Seednote research: unavailable Xiaohongshu backends now fall back to the task topic, topic pool, project profile, and title deduplication without fabricating external trend evidence.
- Kept recoverable research failures only for replicate tasks whose required external source content cannot be resolved.

## [2.10.57] - 2026-07-14

### Changed

- Replaced the customized Humanizer with the byte-identical `blader/humanizer` v2.8.2 skill and moved Seednote, Article, and ecommerce constraints into their owning workflows.
- Made managed Seednote execution use Agent SDK MCP readiness, SDK lifecycle hooks, atomic image verification, and structured recoverable failure states instead of Bash-side MCP clients or unverified visual fallbacks.
- Injected managed MCP and completion-gate contracts for Seednote, including atomic `generate_image` vision verification, structured recoverable failures, and complete trace-artifact validation.

## [2.10.56] - 2026-07-14

### Changed

- Required Agent-Reach channel status to be `ok` before Seednote research invokes the selected backend, and separated managed-runtime packaging failures from local installation guidance.

## [2.10.55] - 2026-07-10

### Changed

- Documented Montage provider-env, tool-policy, and pipeline-default runtime files for upstream OpenMontage execution.

## [2.10.54] - 2026-07-09

### Changed

- Clarified that Montage agents prefer `$ANBAN_MONTAGE_SUBMODULE_PATH` for production runtime resolution.

## [2.10.53] - 2026-07-09

### Changed

- Renamed the Anban Montage agent and skill distribution to the canonical Montage product name.

## [2.10.52] - 2026-07-09

### Added

- Added Montage agent and skill distribution contracts for Claude Code, Codex, and OpenClaw plugins.

## [2.10.51] - 2026-07-09

### Changed

- Strengthened WeChat article cover strategy gates around reader relevance, information scent, three-concept review, click promise proof, viral audit enforcement, and anti-generic examples across agent and skill distributions.

## [2.10.50] - 2026-07-08

### Changed

- Renamed cover scoring references to the WeChat cover quality scorecard across article visual design skill docs.

## [2.10.49] - 2026-07-08

### Changed

- Strengthened WeChat article cover quality gates with title/digest alignment, thumbnail readability, anti-generic constraints, screenshot-derived failure cases, and final review / viral audit enforcement.

## [2.10.48] - 2026-07-08

### Changed

- Clarified videocreator native video-understanding contracts so reference videos preserve deep intent, meaning boundaries, and business subtext.

## [2.10.47] - 2026-07-08

### Changed

- Treat Seednote image API and quality failures as recoverable image-stage failures with explicit provider, model, output path, error, and next-step records.

## [2.10.45] - 2026-07-08

### Changed

- Clarified videoeditor delivery gates so rendered MP4 output and CapCut draft packages are validated as separate acceptable delivery modes.

## [2.10.44] - 2026-07-08

### Changed

- Renamed video generation profile references from `video.*` to `videocreator.*` across distributed agent and skill contract docs.

## [2.10.43] - 2026-07-08

### Changed

- Fully separated video creator/editor contracts so Studio/API/Skill docs use `video_creator_input`, `video_creator_config`, `video_editor_input`, and `video_editor_config` instead of a unified video intake.

## [2.10.42] - 2026-07-08

### Changed

- Synchronized the Seednote visual methodology contract across Claude and Codex distributions, including content distillation, visual strategy, Prompt blueprints, generation records, quality review, and recoverable image API failure reporting.
- Kept `videocreator` scoped to Seedance generation by removing post-production draft tooling from the creator agent.

## [2.10.41] - 2026-07-08

### Changed

- Split the unified video agent into dedicated `videocreator` and `videoeditor` agents with separate hooks, quality gates, and feedback identities.
- Refined the Seednote visual workflow into a model-image-first methodology with content distillation, visual strategy, Prompt blueprints, generation records, quality review, and recoverable image API failure reporting.

## [2.10.40] - 2026-07-08

### Changed

- Added Stable Video Generation V2 guardrails to the video generation workflow and Seedance 2.0 Skill OS: `prepare_video_generation_inputs`, strict remake triggers, fail-closed user references, reference timelines, and multi-segment compose-before-delivery.

## [2.10.39] - 2026-07-08

### Changed

- Removed the experimental rendered-card visual route from Claude, Codex, and OpenClaw distributions so image workflows use the standard platform visual-design skills.
- Simplified the moments workflow contract around the three required text artifacts: `material-analysis.md`, `content.md`, and `quality-review.md`.

## [2.10.38] - 2026-07-07

### Changed

- Documented the earlier unified Studio video intake experiment, leaving business playbook, mode, subject, audience, and message decisions to the Seedance skills.
- Documented that video task creation charges only the base task fee; `video_gen` provider costs are deducted by MCP execution.

## [2.10.37] - 2026-07-07

### Changed

- Normalized distributed Skill descriptions to start with explicit invocation conditions so Claude can decide when to load them from frontmatter.
- Added contents sections to long Skill references and linked long top-level references directly from each Skill entrypoint for one-level progressive disclosure.

## [2.10.36] - 2026-07-07

### Changed

- Added the `moments` completion review hook so 朋友圈素材包 runs verify required artifacts, evidence boundaries, and feedback submission.

## [2.10.35] - 2026-07-07

### Changed

- Moved auxiliary skill README content into `references/` files so shipped skills follow Agent Skills progressive-disclosure packaging guidance.
- Added a mirrored `agent-reach` boundary skill and tightened plugin agent skill-path contracts so Seednote research uses real Agent-Reach backends without missing local skill references.

## [2.10.34] - 2026-07-07

### Added

- Added the independent `moments` Agent and mirrored `moments` skill for WeChat Moments / 朋友圈素材包 generation.
- Documented the Caihui public-method reference boundary and fixed required artifacts.

## [2.10.33] - 2026-07-07

### Changed

- Added an experimental rendered-card skill for Seednote/WeChat visual workflows.
- Documented the upstream adapter posture without vendoring external templates, scripts, validators, or assets.

## [2.10.32] - 2026-07-07

### Changed

- Expanded the video editing workflow around file-based video transcripts, local `anban video` rendering, overlay/subtitle QC, and no nested agent delegation.
- Added contract coverage that keeps the Claude Code plugin manifest version represented in this changelog.

## [2.10.31] - 2026-07-07

### Documentation

- Added a Skill upstream-source and batch-update index to the README, covering copied/adapted open-source skills, structural references, mirror synchronization, and validation commands.

## [2.10.30] - 2026-07-07

### Changed

- Added a human-readable Claude Code plugin display name for plugin UI surfaces.
- Removed `AskUserQuestion` from the humanizer skill's preapproved tools so autonomous writing pipelines stay zero-interaction by default.
- Added contract coverage for Claude Code plugin, agent, hook, and skill best-practice constraints.

## [2.10.27] - 2026-07-06

### Changed

- Moved WeChat article preflight ownership into skills and agents, with anti-diversion review, automatic adjustment loops, and visual prompt guards for QR/contact/link cues.
- Removed the `inspect_article` MCP preflight path from the article workflow.
- Routed Seednote external Xiaohongshu research through Agent-Reach doctor/backend selection, with Anban-managed OpenCLI/xiaohongshu-mcp install guidance removed from the main flow.

## [2.10.24] - 2026-07-06

### Documentation

- Added progressive `references/examples.md` case libraries for complex workflow skills, following Anthropic Skill disclosure patterns and GitHub high-star skill repository structures.

## [2.10.23] - 2026-07-06

### Changed

- Switched Claude Code hook commands that reference plugin paths to exec-form command declarations with `args`.
- Marked the bootstrap hook as asynchronous so Claude Code session startup is not blocked by background binary preparation.
- Updated SubagentStop hook matchers to use plugin-scoped Claude Code agent names.
- Updated agent diagnostics to test whether `ANBAN_API_KEY` exists without printing the secret value.
- Added GitHub health files for contribution and vulnerability reporting.
- Trimmed the line-art coloring skill entrypoint to stay under the official Claude Code Skill size guideline while keeping detailed guidance in `references/`.

### Documentation

- Clarified release, security, and plugin-maintenance practices for the Claude Code distribution.

## [2.10.22] - 2026-07-05

### Changed

- Previous Claude Code plugin release.
