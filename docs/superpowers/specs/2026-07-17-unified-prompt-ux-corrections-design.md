# Unified Prompt UX Corrections Design

## Goal

Make every prompt-entry surface use the same attachment UX and OSS-backed identity contract while allowing task and plan forms to keep their single authoritative footer action.

## Decisions

- `AgentPromptInput` owns attachment selection, thumbnails, drag routing, rejection feedback, and preview everywhere.
- The component exposes `submitMode="inline" | "external"`. External mode hides the inline arrow and does not submit on Enter; the surrounding form footer remains authoritative.
- The global drag router still chooses the active composer, but its visible overlay is positioned over that composer's actual input surface instead of the viewport center.
- Attachments render as a horizontal tile strip. Images use the controller's local object URL; other files use type icons. Remove, retry, and instruction editing remain available without expanding the composer into rows.
- Image preview is a viewport-filling dark lightbox with close/download actions, bounded previous/next navigation, and bottom zoom controls. Non-image previews retain their existing safe renderers inside the same full-screen shell.
- General prompt attachments are limited to five. Media files use the existing 50 MiB server limit, documents/text use 25 MiB, Designer images use 10 MiB, and Resume files use 25 MiB.
- Create/update task and plan endpoints, clone, and Resume enforce five server-side. Resume accepts stable `input_attachments` identities, verifies and finalizes them, then reads the finalized OSS objects into the existing resume workspace flow. Multipart file uploads are removed from the Studio path.

## Validation

- Component tests defend external submit mode, tile thumbnails, target-aligned drag overlay, full-screen preview, and policy labels.
- Page tests defend one submit action in task/plan dialogs and five-file policies on every prompt surface.
- Handler and service tests defend five-file validation, Resume upload identity authorization, immutable finalization, and workspace persistence.
- Browser QA covers task and plan dialogs, the shared Resume composer, five-file admission, attachment tiles, and full-screen preview on desktop and mobile. Drag alignment is defended by the component test's explicit prompt-surface geometry.
