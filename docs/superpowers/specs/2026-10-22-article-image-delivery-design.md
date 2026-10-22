# Article Image Delivery Design

## Intent

Ensure generated article images appear in the final article preview and survive copy/paste into the WeChat editor, whether a task uses automatic delivery or user-assisted publishing. The frozen task delivery decision chooses where article images are hosted; it must not decide whether accepted images are inserted into the article.

## Problem

`TaskImageOperationsService.Upload` rejects user-assisted WeChat tasks. The article Agent and visual-design Skill currently require a successful WeChat upload and `wechat_url` before inserting an image. Thus user-assisted tasks can generate and review image files but have no eligible URL to place in the article. The layout renderer skips empty image URLs, and the audit can omit empty image-only slots, so final HTML can silently lose images.

## Goals

- Render every planned, accepted body image into `05-article.html` and the Studio preview for both delivery modes.
- Route automatic tasks through the existing WeChat material upload and retain `media_id` plus `wechat_url` for Server publication.
- Route user-assisted tasks through Anban-managed durable public image hosting and retain a distinct platform URL for article rendering and manual copy.
- Keep image destination selection, persistence, URL validation, and final delivery evidence Server-owned.
- Make a missing, stale, invalid, or unrendered expected image a visible blocked delivery condition, never a silently complete article.
- Preserve the task's frozen delivery decision throughout retries and execution.

## Non-Goals

- Changing who creates or formally publishes a WeChat draft. Only the Server finalizer does that for automatic tasks.
- Enabling WeChat credentials or automatic delivery for users who did not configure them.
- Changing cover-image selection or cover upload UX in this change; body-image rendering is the acceptance target.
- Retaining compatibility with ambiguous/untyped image URLs or legacy image-delivery behavior.

## Product Behavior

1. At task admission, the Server freezes the effective WeChat delivery decision on the task, as it does today.
2. The Agent generates and independently reviews each planned body image. A failed or unavailable image is never inserted.
3. For automatic delivery, the Server uploads an accepted image to WeChat and persists its `media_id` and `wechat_url` on the current execution's TaskFile.
4. For user-assisted delivery, the Server publishes an accepted image to Anban-managed durable HTTPS hosting and persists a distinct platform image URL on that TaskFile. This publication intentionally makes only reviewed, selected article images publicly readable. Internal API preview URLs and expiring download signatures are not valid article URLs.
5. The image-operation result identifies the selected destination and returns a typed render URL. The Agent writes that URL to the matching `images.json` record and `layout_plan` slot, then renders the article.
6. `render_template` emits a real `<img>` for each populated image slot and reports each expected slot, including missing-image failures. It does not silently omit a planned slot.
7. Before delivery, the Server validates that every expected body-image URL in the final HTML is backed by a delivered image TaskFile in the same task execution and matches that task's frozen delivery mode. Automatic-mode publication continues to validate WeChat URLs and media IDs. User-assisted output validates platform URLs without treating them as WeChat material evidence.
8. Studio preview and copy use the final HTML with the selected durable HTTPS URLs. The preview exposes broken or missing images; a copy-success toast is shown only after the article-image contract is satisfied.

## Data and Ownership

- Keep `wechat_url` and `media_id` exclusively for WeChat-owned material.
- Persist a separately named platform-hosted URL on the TaskFile for user-assisted article delivery. It is public by design and must be backed by Anban storage/CDN policy; do not expose the TaskFile's private object URL or task-scoped preview endpoint as a substitute.
- The Server selects the destination from the frozen task snapshot and writes the URL metadata. Agent output is evidence/input for validation, not authority to claim an upload succeeded.
- Keep image bytes and typed URL metadata associated with the current execution so retries and superseded files cannot satisfy the current article's image contract.
- Tell the user before manual delivery that the selected article images receive public, stable URLs. Do not expose unselected, failed, reference, or intermediate images through the public hosting path.

## Failure Behavior

- A WeChat upload failure in automatic mode blocks publication readiness; it does not fall back to a platform URL because automatic publishing requires WeChat material evidence.
- A platform-hosting failure in user-assisted mode blocks article readiness; it does not leave an image file that is silently omitted from HTML.
- A rejected/unavailable image is reported as a failed or unavailable slot and is excluded from HTML. The article may continue rendering, but readiness is blocked while a required planned slot is unresolved.
- Missing URL, URL/type mismatch, wrong execution ownership, duplicate slot assignment, or an HTML image absent from the render audit blocks readiness with a specific evidence code.
- Platform-hosted image URLs do not expire or disappear during normal task cleanup or a later task execution. They remain available until the user explicitly deletes the corresponding article asset or deletes the account; deletion is surfaced as potentially breaking external articles that still reference the URL.

## Acceptance Criteria

- In automatic mode, an accepted image is uploaded to WeChat, visible in platform task management, rendered in final HTML with its WeChat URL, and remains eligible for automatic draft creation under existing Server rules.
- In user-assisted mode, an accepted image is available in platform task management, has a durable public platform URL distinct from `wechat_url`, renders in final HTML and Studio preview, and is present after copying into the WeChat Official Accounts web editor in the latest stable desktop Chrome.
- The user-assisted Agent flow no longer attempts a prohibited WeChat material upload or treats the absence of `wechat_url` as a reason to omit an otherwise accepted image.
- Tests prove empty or unresolved planned slots cannot produce a ready article and that mode/URL mismatches are rejected.
- No Agent, Skill, MCP handler, or Studio code creates an automatic WeChat draft or formally publishes an article.

## Constraints and Open Implementation Decisions

- Use existing storage/CDN facilities where they can provide a durable, externally readable HTTPS URL. If current storage cannot support that property, add a Server-owned article-image hosting capability rather than leaking private object locations.
- The default product call is durable public hosting until explicit asset/account deletion; normal task cleanup and superseding executions must preserve URLs already included in an article artifact.
- Update both native plugin manifest versions if the Agent or Skill contract changes, following the repository's patch-bump convention.
- Verify the user-assisted copy flow against the supported WeChat editor in an end-to-end environment; unit tests alone cannot prove that the editor imports the remote images.
