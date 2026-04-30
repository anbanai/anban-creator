# Rednote Profile Analysis Design

## Goal

When a user creates a Xiaohongshu account in Studio, the account homepage field accepts either a clean URL or copied share text. The backend extracts the Xiaohongshu URL, fetches the public profile, selects the best-performing visible posts, and uses AI to generate useful account metadata: positioning, keywords, and visual style.

For WeChat article and WeChat Xiaolvshu accounts, the account homepage option is disabled for now.

## Scope

In scope:

- Xiaohongshu only.
- Accept share text containing `xhslink.com`, `xiaohongshu.com`, `www.xiaohongshu.com`, or `m.xiaohongshu.com` URLs.
- Resolve short links safely.
- Fetch visible public profile data and visible post metadata.
- Sort posts by available engagement metrics and choose the best few as AI samples.
- Use AI analysis as an enhancement, with graceful fallback to scraped profile data if AI is not configured or returns invalid output.
- Auto-fill Studio form fields: name, avatar, positioning, keywords, and visual style.
- Hide the account homepage field for article and xls platforms.

Out of scope:

- Persisting sampled posts in the database.
- Adding database columns.
- Supporting WeChat profile scraping.
- Deep crawling individual Xiaohongshu posts beyond data available from the profile page.
- Blocking account creation when AI analysis fails.

## Architecture

The feature uses the existing `POST /api/v1/channels/fetch-profile` endpoint and extends the Xiaohongshu provider.

`server/platform/rednote.go` remains responsible for URL extraction, redirect resolution, HTTP fetch, and HTML/embedded-data parsing. Its returned `PlatformProfile.RawData` includes normalized fields for the resolved profile URL, source text, visible post samples, and selected top posts.

The handler enriches Xiaohongshu profiles by calling the existing OpenAI-compatible `service.LLMClient` path when available. The AI response is parsed as strict JSON and merged into the profile response. If the call fails, the handler logs a warning and returns the scraped data.

Studio uses platform config to render only the fields that apply. Since article and xls no longer expose `profile_url`, their account homepage input is hidden. Xiaohongshu keeps it and lets users paste either a URL or share text.

## Data Flow

1. User selects 小红书 and pastes share text into the account homepage field.
2. Frontend calls `fetchProfile(platform, inputText, ...)`.
3. Backend validates the platform.
4. For Xiaohongshu, backend extracts the first supported URL from arbitrary text.
5. Provider resolves short links and fetches the resulting profile page.
6. Provider parses:
   - nickname
   - avatar
   - bio/description
   - visible post title or summary
   - visible engagement counts when present
7. Provider sorts visible posts by engagement score descending.
8. Handler sends profile plus top posts to AI analysis.
9. Handler merges analysis fields into `PlatformProfile`.
10. Frontend auto-fills the form.

## Post Ranking

Each visible post gets an engagement score from available metrics:

- like count
- collect count
- comment count
- share count

Counts support common Chinese formats such as `1.2万`, `3千`, and plain numbers. Missing metrics count as zero. If all scores are zero, the original page order is preserved and the first few posts are used.

Default sample size: top 5.

## AI Output

The AI analysis returns JSON with these fields:

```json
{
  "positioning": "账号定位，80 字以内",
  "keywords": ["关键词1", "关键词2", "关键词3"],
  "style": "视觉风格描述，120 字以内",
  "content_summary": "内容方向摘要，120 字以内"
}
```

The backend validates and normalizes:

- `positioning` replaces scraped bio only when non-empty.
- `keywords` becomes a comma-separated string for existing `Channel.Keywords`.
- `style` fills existing `Channel.Style`.
- `content_summary` is returned inside `raw_data.analysis` for transparency.

## Error Handling

- Missing supported URL in share text: `400` with a clear message.
- Unsupported platform auto-fetch: no profile URL field is shown in Studio, but backend still returns a validation error if called.
- Xiaohongshu fetch failure: `500` with existing error shape.
- AI unavailable or invalid JSON: logged as warning; response still succeeds with scraped profile fields.
- Redirect targets are restricted to Xiaohongshu allowed hosts.

## Testing

Backend tests cover:

- URL extraction from share text.
- Rejection when share text has no supported URL.
- Short-link redirect resolution still works.
- Post metric parsing and ranking.
- Profile parsing includes top posts in `RawData`.
- AI analysis JSON parsing and merge behavior.
- AI failure fallback behavior.

Frontend tests or type-safe build cover:

- Article and xls platform configs no longer expose `profile_url`.
- Xiaohongshu profile input accepts arbitrary text and fetch button enables when a supported URL exists inside it.
- Auto-fill applies keywords and style when returned.

