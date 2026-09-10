# Montage Video Cover Design

## Goal

Every managed Montage task produces a video cover through the Anban image MCP
workflow. The cover uses the same user-selected aspect ratio as the video and,
when the cover contains the user, uses only the portrait reference selected in
Studio and supplied by the system.

## Authoritative Inputs

Studio collects the video aspect ratio and portrait selection when the user
creates the task. Server converts those selections into concise natural-language
instructions in the first Cloud user message, for example:

```text
Video aspect ratio: 9:16
Portrait reference: use system-provided portrait reference image 1
```

The first Cloud user message is the Agent's execution contract for these values.
The Agent must not rediscover or override the video aspect ratio from
`montage-input.json`, project image settings, Skill defaults, or provider
recommendations. `montage-input.json` continues to carry the existing Montage
brief, assets, preferences, and limits, but it is not the normative source for
the cover ratio.

The server may persist or snapshot the selected values for validation, billing,
and replay safety. Such copies are not independent configuration sources and
must be derived from the same Studio selection.

## Agent Data Flow

At the start of the managed run, the Montage Agent parses the first user message
once and freezes the video aspect ratio. It passes that exact value to both:

1. The OpenMontage adapter manifest and video render request.
2. The `video-cover-design` workflow and its `generate_image` call.

The Agent does not ask the user to repeat parameters. Missing optional creative
choices are resolved from the task brief, project positioning, source media,
and deterministic Skill defaults. A missing or invalid required ratio is a
structured task failure rather than an interactive question.

## Portrait Reference Flow

Portrait references come from Studio-backed system assets. The first user
message identifies the selected reference semantically; the managed runtime
materializes or otherwise exposes the corresponding system asset to the Agent.

The Skill must never look for `assets/my-face.png`, create `config.md`, or ask
the user to place a portrait inside the Skill directory. If the brief requires
the user's identity and no usable system portrait exists, the Agent writes a
structured failure diagnosis and stops. If a person is not required, it may
select a non-portrait composition automatically.

## Cover Workflow

After OpenMontage has produced the final video and before delivery validation,
the Agent invokes `video-cover-design` with all known context at once: the video
brief and content summary, final title or title evidence, frozen aspect ratio,
selected system portrait, relevant system assets, project positioning, and any
explicit visual preferences from the first user message.

The Skill:

1. Chooses a suitable composition from its style references without multi-round
   questioning.
2. Builds a concrete generation prompt as an internal audit artifact, not as
   the user-facing deliverable.
3. Calls Anban MCP `generate_image` with `project_id`, `task_id`,
   `image_type="cover"`, `output_path="output/cover.png"`, the exact video
   aspect ratio, and ordered system reference paths when applicable.
4. Calls `analyze_image` to check identity, subject, composition, title, and
   aspect ratio. It retries only within a fixed creative retry budget.
5. Writes the cover, plan, prompt audit, and quality result to explicit
   `output/` paths. On terminal failure it preserves existing artifacts and
   writes a structured diagnosis without asking the user for help mid-run.

## Delivery Contract

`output/cover.png` is a required Montage artifact, not an optional
`delivery_targets` switch. The Montage delivery manifest includes the cover,
and task completion requires the final video, cover, project manifest, and
delivery manifest to be registered as task files.

The existing boundary that routes creative video production through OpenMontage
is narrowed for this post-production asset: video production remains inside the
OpenMontage adapter and provider registry, while cover generation uses the Anban
image MCP workflow required by `video-cover-design`.

## Server And Billing Support

The server passes the Studio-selected ratio and portrait semantics into
`BuildUserPrompt` for Montage tasks. It validates that the selected ratio is
supported by both the Montage and image generation paths, makes the system
portrait resolvable by MCP without relying on an Agent-local arbitrary path,
and freezes the image capability and billing SKU required for the cover call.

There is no server-side creative orchestration: MCP handlers remain atomic.
The Montage Agent and Skill own sequencing, retries, quality gates, and the
decision to stop after a structured failure.

## Distribution And Tests

The Pack-owned Claude and Codex Montage Agent sources both load
`video-cover-design`; generated native agents, DSH presets, and catalogs are
refreshed from the Pack. The Montage Pack declares `output/cover.png` as a
required artifact and delivery item.

Tests cover:

- natural-language aspect ratio and portrait semantics in the first Cloud user
  message;
- Agent instructions using the first message as the sole ratio source;
- the Skill's zero-interaction, system-reference, MCP generation contract;
- Montage image-ratio validation, capability/billing admission, and system
  reference resolution;
- required cover artifact validation and Pack distribution consistency.

Both native plugin manifest versions receive the required patch bump because
the change updates Agent and Skill assets under `plugins/`.
