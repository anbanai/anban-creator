# OpenAI Generated Image Download Retry Design

## Problem

The OpenAI-compatible image endpoint can successfully generate an image and
return a remote URL, while the subsequent server-side download fails because
of a transient transport error or a temporary upstream HTTP response. The
generation API and the URL download use different HTTP clients. The OpenAI SDK
retries eligible generation API failures, but `wechat.DownloadFile` currently
makes one Resty request and does not retry.

The failure is surfaced as `url_download_error`, so Designer marks the
generation as failed even though the provider has already created and charged
for the image. Retrying the generation API would risk duplicate charges and a
different image.

## Scope

Add bounded retries only when `OpenAIProvider` downloads a generated image URL.
Do not change the retry behavior of existing `wechat.DownloadFile` callers such
as WeChat material processing or generic remote-image downloads.

The Studio API, persisted Designer status values, billing behavior, user-facing
error message, and plugin assets remain unchanged.

## Selected Approach

Add a private `OpenAIProvider` download helper that owns retry policy and calls
a context-aware sibling of `wechat.DownloadFile`. Keep the existing
`wechat.DownloadFile(url)` signature as a compatibility wrapper with its
current one-attempt behavior.

Thread the existing generation `context.Context` through OpenAI response
conversion so both single-image and batch-image URL paths call the same helper.
The helper retries only the remote download; it never calls `Images.Generate`,
`Images.Edit`, or either streaming generation endpoint again.

## Data Flow

1. The OpenAI SDK performs the image generation or edit request.
2. The provider response returns base64 image data or a remote URL.
3. Base64 responses continue through the existing local decode path.
4. URL responses call `downloadGeneratedImage(ctx, rawURL)`.
5. The helper performs at most three download attempts within one 120-second
   download-phase deadline.
6. A successful download returns the temporary file path to the existing
   Designer persistence pipeline.
7. An exhausted or non-retryable failure remains a `GenerateError` with code
   `url_download_error`.

## Retry Policy

- Maximum attempts: three total, including the initial attempt.
- Backoff before retries: one second, then two seconds.
- Per-attempt timeout: retain the existing 60-second download timeout.
- Total download-phase deadline: 120 seconds, including attempts and backoff.
- Stop immediately when the caller context or download-phase context ends.

Retry these failures:

- Transport failures without a valid HTTP response, including connection
  reset, unexpected EOF, temporary DNS/TLS failure, and transport timeout.
- HTTP 408.
- HTTP 429.
- HTTP 500 through 599.

Do not retry other 4xx responses, including 400, 401, 403, and 404. These are
treated as deterministic request, authorization, expiry, or missing-resource
failures.

## Download Boundary

Add `wechat.DownloadFileContext(ctx, url)` for callers that need cancellation.
It uses the same Resty configuration, browser-compatible headers, temporary
file naming, 60-second timeout, cleanup rules, and `DownloadError` diagnostics
as the current function. `DownloadFile(url)` delegates to it with a background
context and remains a single-attempt operation.

The OpenAI helper owns attempt counting, classification, backoff, and the
120-second phase deadline. This keeps provider-specific policy out of shared
download behavior.

## Error Handling And Observability

Each retry warning records only operational diagnostics:

- URL host, not the full generated URL.
- Current attempt and maximum attempts.
- HTTP status code when available.
- Download elapsed time.
- Planned retry delay.
- Underlying transport error.

The final failure log retains the existing
`openai: failed to download generated image URL` message and adds the total
attempt count. Existing response metadata such as content type, content length,
server, Cloudflare ray, redirect location, and bounded body preview remains
available through `DownloadError`.

The user-facing error remains `图片已生成，但保存到作品库失败，请稍后重试`.
`RevisedPrompt` is not included as a download-failure hint because it is
unrelated to transport diagnostics.

Every failed attempt must remove its partial temporary file before retrying.
Successful temporary files continue to be owned and cleaned up by the existing
image processing pipeline.

## Tests

Add focused tests for these behaviors:

- `DownloadFileContext` stops when its context is canceled.
- Existing `DownloadFile` retains browser-compatible headers and one-attempt
  semantics.
- A generated-image download succeeds when a first 503 is followed by a valid
  image response, with exactly two requests.
- A truncated first response that produces unexpected EOF is retried and then
  succeeds.
- A 403 response is not retried and remains `url_download_error`.
- Repeated retryable failures stop after three attempts and emit the final
  attempt count and existing download diagnostics.
- An external cancellation or the 120-second download-phase deadline prevents
  additional attempts.
- Single-image and batch-image URL responses use the same retry helper.

Run fresh verification:

```bash
go test ./app/wechat ./app/image -count=1
go test ./... -count=1
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Studio tests and builds are not required because the Studio code and API
contract do not change.

## Acceptance Criteria

- A transient generated-image URL download failure can recover without
  repeating the provider generation request.
- Deterministic 4xx failures are attempted once.
- Download retries never exceed three attempts or the 120-second phase budget.
- Cancellation stops retry waits and in-flight context-aware downloads.
- Partial files are not retained after failed attempts.
- Final failures preserve structured diagnostics while user-visible errors do
  not expose provider URLs or internal details.
- Existing non-OpenAI `DownloadFile` callers retain their current behavior.

## Non-Goals

- Retrying OpenAI image generation or edit requests beyond the SDK policy.
- Adding user-configurable retry settings.
- Changing Designer polling, billing, persistence, or failure reversal.
- Adding global retries to all remote image downloads.
- Changing Studio UI or plugin assets.
