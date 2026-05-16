# Seednote Profile Analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Seednote account setup accepts share text, fetches profile and top visible posts, then AI-enriches positioning, keywords, and visual style while hiding account homepage for other platforms.

**Architecture:** Extend the existing channel profile fetch endpoint. Keep URL extraction, redirect safety, profile parsing, post ranking, and raw samples in `server/platform`; add optional AI enrichment via the existing `service.LLMClient`; update Studio to rely on platform config and fill returned enrichment fields.

**Tech Stack:** Go 1.26, Fiber v3, zerolog, OpenAI-compatible LLM client, React, TypeScript, React Hook Form, TanStack Query.

---

## File Structure

- `server/platform/provider.go`: extend `PlatformProfile` with `Keywords` and `Style`, and add a `SeednotePost` DTO.
- `server/platform/seednote.go`: extract Seednote URLs from arbitrary text, parse visible post metadata, normalize counts, rank top posts.
- `server/platform/seednote_test.go`: TDD coverage for URL extraction, post ranking, and profile raw data.
- `server/handler/channel.go`: accept share text for seednote, block auto-fetch for article, optionally enrich seednote profile through AI.
- `server/handler/channel_test.go`: handler-level tests for AI merge and fallback.
- `server/main.go`: inject the existing LLM client into `ChannelHandler`.
- `server/model/platform.go`: remove homepage fields and auto-fetch for article; keep seednote homepage with share-text wording.
- `studio/src/types/channel.ts`: expose `keywords` and `style` in `PlatformProfile`.
- `studio/src/pages/ChannelsPage.tsx`: hide profile input for non-seednote, enable fetch based on embedded supported URL, auto-fill keywords/style.

## Tasks

### Task 1: Seednote URL Extraction And Post Ranking

**Files:**
- Modify: `server/platform/provider.go`
- Modify: `server/platform/seednote.go`
- Test: `server/platform/seednote_test.go`

- [ ] Write failing tests for share-text URL extraction, unsupported text rejection, count parsing, top-post sorting, and `RawData.top_posts`.
- [ ] Run `go test ./server/platform -run 'TestSeednote' -count=1` and confirm the new tests fail.
- [ ] Implement URL extraction, `SeednotePost`, count parsing, and top-post selection with SSRF-safe allowed hosts.
- [ ] Run `go test ./server/platform -run 'TestSeednote' -count=1` and confirm it passes.

### Task 2: AI Analysis Service In Channel Handler

**Files:**
- Modify: `server/handler/channel.go`
- Modify: `server/main.go`
- Test: `server/handler/channel_test.go`

- [ ] Write failing handler tests using a fake `LLMClient`: AI JSON merges into profile; invalid AI output keeps scraped profile; article fetch is rejected.
- [ ] Run `go test ./server/handler -run 'TestChannelFetchProfile' -count=1` and confirm the new tests fail.
- [ ] Add optional LLM injection to `ChannelHandler`, build a bounded Chinese analysis prompt from profile and top posts, parse strict JSON, and merge non-empty fields.
- [ ] Wire `server/main.go` to provide the same configured LLM client to `ChannelHandler` when available.
- [ ] Run `go test ./server/handler -run 'TestChannelFetchProfile' -count=1` and confirm it passes.

### Task 3: Platform Config And Studio Form Behavior

**Files:**
- Modify: `server/model/platform.go`
- Modify: `studio/src/types/channel.ts`
- Modify: `studio/src/pages/ChannelsPage.tsx`

- [ ] Update backend platform config so article does not expose `profile_url` and does not support auto-fetch.
- [ ] Update frontend URL detection to support Seednote URLs embedded in share text.
- [ ] Render the profile input only when the current platform has a `profile_url` field.
- [ ] Auto-fill `keywords` and `style` from `fetchProfile`.
- [ ] Run `go test ./server/model ./server/platform ./server/handler -count=1` and `cd studio && bun run build`.

### Task 4: Full Verification

**Files:** no new files.

- [ ] Run `go test ./server/platform ./server/handler ./server/model -count=1`.
- [ ] Run `cd studio && bun run build`.
- [ ] Run `git diff -- server/platform/provider.go server/platform/seednote.go server/platform/seednote_test.go server/handler/channel.go server/handler/channel_test.go server/main.go server/model/platform.go studio/src/types/channel.ts studio/src/pages/ChannelsPage.tsx`.
- [ ] Confirm no unrelated files were modified by this work.

