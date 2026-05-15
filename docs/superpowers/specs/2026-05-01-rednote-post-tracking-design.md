# Seednote Post Tracking Design

## Goal

After a user marks a Seednote/SeedNote task as published, the system should automatically discover the published note from the account homepage, bind it to the local task, collect public engagement metrics once per day, and show the metrics and trends inside the task detail page.

The first release focuses on a minimal automatic loop:

- The user does not need to paste a note URL.
- Discovery starts the day after the task is marked as published.
- The system uses AI to match the local work to the public note.
- Metrics are collected from public pages only.
- Tracking stops automatically after a maximum tracking window or after engagement becomes stable.

## Scope

In scope:

- Seednote/SeedNote tasks only.
- Public homepage discovery after a task is marked as published.
- AI-based note matching using local task data and public homepage candidates.
- Binding the matched note URL and note ID to the local task tracking record.
- Daily public metric snapshots.
- Task detail analytics panel with latest metrics, deltas, trend chart, tracking status, and linked note.
- Automatic stop rules.
- Backend and frontend tests for the main paths.

Out of scope:

- Real exposure/view collection when it is not publicly available.
- Logged-in creator dashboard scraping.
- User confirmation UI for note binding.
- Account-level analytics dashboard.
- Historical bulk import of old SeedNote notes.
- Manual note URL entry as the primary first-release flow.

## Public Metrics

The first release collects public metrics that can be parsed from the homepage or note page:

- likes
- collects/favorites
- comments
- shares

`view_count` is included as a nullable field for future support. If public pages do not expose views or exposure, Studio displays "暂无公开数据" instead of inventing a value.

## Tracking Lifecycle

Each published SeedNote task has at most one tracking record.

Statuses:

- `waiting_discovery`: the task was marked as published and is waiting for homepage discovery.
- `tracking`: a public note was matched and metrics are being collected daily.
- `stopped`: collection ended because the max tracking window or low-growth rule was reached.
- `failed`: discovery or collection failed too many times, or discovery timed out.

Flow:

1. User marks a completed SeedNote task as published.
2. Backend creates or resumes a tracking record.
3. Backend schedules a discovery job 24 hours later.
4. Discovery fetches the channel profile page and extracts public note candidates.
5. AI compares candidates with the local task title, prompt, result summary, and generated assets.
6. If AI confidently identifies a match, backend stores `note_id`, `note_url`, match confidence, and match reason.
7. Backend immediately captures the first metric snapshot.
8. Backend schedules daily metric capture jobs.
9. Each capture stores a snapshot, computes growth, evaluates stop rules, and either stops or schedules the next capture.

## Data Model

### `seednote_post_trackings`

Tracks the relationship between a local task and a public SeedNote note.

Fields:

- `id`
- `task_id`
- `user_id`
- `channel_id`
- `status`
- `profile_url`
- `note_id`
- `note_url`
- `note_title`
- `note_cover_url`
- `published_marked_at`
- `discovered_at`
- `tracking_started_at`
- `tracking_stopped_at`
- `next_run_at`
- `last_run_at`
- `run_count`
- `consecutive_low_growth_count`
- `failure_count`
- `discovery_attempt_count`
- `match_confidence`
- `match_reason`
- `stop_reason`
- `last_error`
- `created_at`
- `updated_at`

Indexes and constraints:

- Unique index on `task_id`.
- Index on `user_id`.
- Index on `channel_id`.
- Index on `status`.
- Index on `next_run_at`.

### `seednote_metric_snapshots`

Stores one public metric snapshot per tracked note per day.

Fields:

- `id`
- `tracking_id`
- `task_id`
- `captured_at`
- `captured_date`
- `like_count`
- `collect_count`
- `comment_count`
- `share_count`
- `view_count` nullable
- `raw_data`
- `created_at`

Indexes and constraints:

- Unique index on `(tracking_id, captured_date)`.
- Index on `task_id`.
- Index on `captured_at`.

`raw_data` stores normalized parse details useful for debugging, not full private or sensitive page content.

## Repository Layer

Add repository interfaces following the existing repository package style:

- `SeednoteTrackingRepository`
- `SeednoteMetricRepository`

Core tracking repository methods:

- `Create(ctx, tracking)`
- `FindByTaskID(ctx, taskID)`
- `FindByID(ctx, id)`
- `FindDue(ctx, now, limit)`
- `Update(ctx, tracking)`
- `UpdateStatus(ctx, id, status)`

Core metric repository methods:

- `Create(ctx, snapshot)`
- `UpsertByTrackingAndDate(ctx, snapshot)`
- `FindByTaskID(ctx, taskID)`
- `FindLatestByTrackingID(ctx, trackingID)`
- `FindPreviousByTrackingID(ctx, trackingID, capturedAt)`

## Service Layer

Add `SeednoteTrackingService` for orchestration. Platform parsing and HTTP fetching stay outside this service.

Methods:

- `EnsureTrackingForPublishedTask(ctx, userID, taskID)`
- `DiscoverPublishedNote(ctx, trackingID)`
- `CaptureMetrics(ctx, trackingID)`
- `EvaluateStop(ctx, trackingID)`
- `GetTaskAnalytics(ctx, userID, taskID)`
- `StopTracking(ctx, userID, taskID, reason)`

Behavior:

- `EnsureTrackingForPublishedTask` is called when a SeedNote task is marked as published.
- Existing tracking is reused instead of duplicated.
- Discovery only considers tasks owned by the current user.
- Captures are idempotent by `(tracking_id, captured_date)`.
- A tracking record is not failed after a single transient error.

## Platform Layer

Extend `server/platform/seednote.go` with SeedNote-specific public parsing capabilities:

- `FetchProfilePosts(ctx, profileURL)`
- `FetchPostMetrics(ctx, noteURL)`
- `ExtractNoteID(url)`
- `NormalizeSeednoteMetricCount(text)`

The platform layer owns:

- URL extraction and normalization.
- Short-link redirect safety.
- Browser-like request headers.
- Public page parsing.
- Chinese metric count parsing, including `1.2万`, `3千`, comma-separated numbers, and plain numbers.

The platform layer does not own:

- Tracking lifecycle.
- AI matching decisions.
- Stop rules.
- Database writes.

## Scheduling

Add Asynq task types:

- `seednote:discover`
- `seednote:capture_metrics`

Scheduling rules:

- When a SeedNote task is marked as published, enqueue `seednote:discover` with a 24-hour delay.
- If discovery succeeds, enqueue metric capture immediately.
- After each successful capture, enqueue the next capture with a 24-hour delay unless tracking should stop.
- If discovery cannot confidently match a note, keep status `waiting_discovery` and retry the next day.
- Discovery retries for at most 7 days.
- Collection failures increment `failure_count`.
- Five consecutive business-level failures set status to `failed`.

Asynq still handles transient retry/backoff for job execution failures.

## AI Matching

AI is used only during discovery.

Input includes:

- Local task title.
- Local task prompt.
- Task result summary when available.
- Generated file names and accessible image URLs when available.
- Channel profile URL.
- Homepage candidate notes, including title, URL, note ID, cover URL, public metrics, and visible publish time when parsed.

Output must be strict JSON:

```json
{
  "matched": true,
  "note_url": "https://www.xiaohongshu.com/explore/...",
  "note_id": "...",
  "confidence": 0.92,
  "reason": "标题关键词、主题和封面内容一致"
}
```

Binding rules:

- Bind only when `matched=true`.
- Bind only when `confidence >= 0.8`.
- The selected `note_url` or `note_id` must exist in the candidate list.
- Low confidence, invalid JSON, or no match leaves the record in `waiting_discovery`.
- Discovery times out after 7 daily attempts.

After binding, AI is not rerun for routine metric capture.

## Stop Rules

Default maximum tracking period: 14 days.

Low-growth rule:

- Compute total daily growth as:

  `likes + collects + comments + shares`

- If total growth is less than `3` for `3` consecutive captures, stop tracking.

Stop reasons:

- `max_duration_reached`
- `low_growth`
- `discovery_timeout`
- `too_many_failures`
- `manual_stop`

These constants should be represented in the model layer so backend and frontend can share stable status semantics.

## API

Add:

`GET /api/v1/tasks/:id/seednote-analytics`

Returns analytics for the task owner.

Response shape:

```json
{
  "tracking": {
    "status": "tracking",
    "note_url": "https://www.xiaohongshu.com/explore/...",
    "note_title": "...",
    "note_cover_url": "...",
    "discovered_at": "...",
    "last_run_at": "...",
    "next_run_at": "...",
    "run_count": 5,
    "stop_reason": ""
  },
  "latest": {
    "like_count": 120,
    "collect_count": 45,
    "comment_count": 8,
    "share_count": 3,
    "view_count": null,
    "captured_at": "..."
  },
  "deltas": {
    "like_count": 12,
    "collect_count": 4,
    "comment_count": 1,
    "share_count": 0
  },
  "series": [
    {
      "captured_at": "...",
      "like_count": 80,
      "collect_count": 30,
      "comment_count": 4,
      "share_count": 1,
      "view_count": null
    }
  ]
}
```

Optional follow-up endpoints:

- `POST /api/v1/tasks/:id/seednote-analytics/retry-discovery`
- `POST /api/v1/tasks/:id/seednote-analytics/stop`

The first release can omit the frontend controls for these endpoints if schedule-driven tracking is enough.

## Frontend

Show a SeedNote analytics panel only when:

- `task.type === 'seednote'`
- `task.published === true`

Add:

- `studio/src/types/seednote-analytics.ts`
- `studio/src/lib/api/seednote-analytics.ts` or equivalent method on `tasksApi`
- `studio/src/components/tasks/SeednoteAnalyticsPanel.tsx`

`TaskDetailPage.tsx` should only load and embed the panel to avoid growing the page further.

Panel content:

- Tracking status.
- Bound note title, cover, and external link.
- Latest likes, collects, comments, shares, and explicit empty-state display for unavailable views/exposure.
- Delta from previous capture.
- Recharts line chart for daily trend.
- Last capture time, next capture time, run count, and stop reason.

Status copy:

- `waiting_discovery`: 明天将从账号主页自动识别这篇笔记
- `tracking`: 正在每日采集公开数据
- `stopped`: 数据变化已趋缓，已停止自动采集
- `failed`: 暂时无法识别或采集这篇笔记

UX rules:

- Do not render an empty chart when no snapshots exist.
- Display `view_count=null` as "暂无公开数据".
- Open the SeedNote note link in a new tab.
- Show a user-friendly error summary without exposing internal parser details.

## Error Handling

Discovery:

- Missing channel profile URL: fail the tracking record with a clear `last_error`.
- Homepage fetch failure: increment `failure_count` and retry later.
- No candidates: remain in `waiting_discovery` until discovery timeout.
- AI unavailable or invalid JSON: log warning, remain in `waiting_discovery`, retry next day.
- AI selects a non-candidate note: reject the match and retry next day.

Metric capture:

- Missing bound note URL: mark failed because tracking is inconsistent.
- Partial metric parse: save available metrics; missing public fields become zero or null.
- Complete page parse failure: increment `failure_count`.
- Duplicate daily capture: update existing snapshot or no-op idempotently.

## Testing

Backend tests:

- Homepage note list parsing extracts titles, URLs, note IDs, covers, and visible public metrics.
- Single-note metric parsing handles likes, collects, comments, shares, and nullable views.
- Count parsing supports `1.2万`, `3千`, comma-separated numbers, and plain numbers.
- Note ID extraction and URL normalization are stable.
- AI match parsing handles success, low confidence, no match, selected non-candidate URL, and invalid JSON.
- Stop rules cover max duration, low growth, discovery timeout, and repeated failures.
- Repository tests cover create, lookup, update, snapshot uniqueness, and latest/series retrieval.
- Service tests cover mark-published tracking creation, discovery success, discovery retry, capture scheduling, idempotent daily capture, and stop evaluation.

Frontend tests or build checks:

- Non-SeedNote or unpublished tasks do not show the analytics panel.
- Each tracking status displays the correct copy.
- Empty snapshots do not render an empty chart.
- `view_count=null` displays "暂无公开数据".
- Trend data maps correctly into chart series.

## Operational Notes

Public scraping is brittle by nature. Treat page structure changes as expected:

- Keep parsing logic centralized in the SeedNote platform provider.
- Store normalized parse details in `raw_data`.
- Do not fail an entire capture when one metric is missing.
- Add structured logs with `tracking_id`, `task_id`, `channel_id`, and `note_id`.
- Avoid logging full page contents or credentials.

Rate and scope controls:

- One capture per tracked task per day.
- SeedNote tasks only.
- Maximum 14-day tracking window.
- Five consecutive business-level failures before failing a tracking record.
- Future work can add channel-level concurrency and per-domain throttling.

## Implementation Order

1. Add models and AutoMigrate entries.
2. Add repositories and repository tests.
3. Extend SeedNote platform parsing and tests.
4. Add tracking service with fake platform and fake AI tests.
5. Add Asynq task types and handlers.
6. Hook SeedNote published marking into tracking creation.
7. Add analytics API endpoint and handler tests.
8. Add frontend types, API client, analytics panel, and TaskDetail integration.
9. Run backend and frontend verification.
