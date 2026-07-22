# agent-reach Examples

### Case 1: Agent-Reach unavailable

- Input: `agent-reach doctor --json` is missing, returns no Xiaohongshu backend, or returns `status=warn` with a repairable `active_backend`.
- Recommended path: In a managed runtime, report missing CLI as a packaging fault. Otherwise stop external research and report the exact doctor `status`, `active_backend`, and `message` without invoking the candidate backend.
- Artifacts: task report with `data_source=agent-reach`, `channel_status`, `active_backend=unavailable`, and `fallback_reason`.
- Quality gate: Do not fabricate trending notes, comments, URLs, `feed_id`, or `xsec_token`.

### Case 2: Original topic research

- Input: Seednote original mode needs real notes for “春季花茶”.
- Recommended path: Run doctor, require `status=ok`, use only the active backend command family, collect search/feed results, and pass them to `seednote-research`.
- Artifacts: `topic-analysis.md` with `data_source=agent-reach`, active backend, missing fields, and candidate scoring evidence.
- Quality gate: Backend order and fallback belong to Agent-Reach; do not call backend tools directly by preference.

### Case 3: Source note retrieval

- Input: Replicate mode receives a Xiaohongshu URL or note id.
- Recommended path: Resolve source details through the active Agent-Reach backend and extract `feed_id` / `xsec_token` only from real backend output or URLs.
- Artifacts: `source-note.md`, raw source details, `token_source`, `missing_fields`, and `fallback_reason`.
- Quality gate: Never construct `xsec_token`; stop after the backend retry path fails.
