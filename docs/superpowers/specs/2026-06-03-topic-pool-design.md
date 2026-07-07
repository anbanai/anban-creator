# Topic Pool (选题池) Feature Design

## Context

AI-generated content tasks currently select topics in two ways: the user provides a prompt/topic hint, or the agent autonomously researches and generates a topic through the writing Skills workflow. There is no persistent topic queue — AI-generated topic candidates are discarded after each run.

This feature adds a user-managed "topic pool" (选题池) per channel. Users manually add topic text to the pool. When a plan triggers task creation, the system claims an unused topic from the pool instead of requiring AI research. When the pool is empty, the system falls back to the Skills-side topic research behavior.

## Data Model

New `topic_pool` table, managed via GORM AutoMigrate:

```go
type TopicPool struct {
    ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
    UserID    string     `gorm:"type:char(36);index;not null" json:"user_id"`
    ChannelID string     `gorm:"type:char(36);index;not null" json:"channel_id"`
    Topic     string     `gorm:"type:varchar(500);not null" json:"topic"`
    Status    string     `gorm:"type:varchar(20);default:'unused'" json:"status"` // unused, used
    TaskID    *string    `gorm:"type:char(36)" json:"task_id,omitempty"`
    UsedAt    *time.Time `json:"used_at,omitempty"`
    CreatedAt time.Time  `json:"created_at"`
    UpdatedAt time.Time  `json:"updated_at"`
}
```

- Scoped per channel (`channel_id`) and user (`user_id`)
- `Status`: `unused` (available for claiming) or `used` (already consumed by a task)
- `TaskID`: references the task that consumed this topic
- `UsedAt`: timestamp when the topic was claimed

## Backend Layers

### Repository (`server/repository/topic_pool.go`)

New `TopicPoolRepository` interface with methods:
- `Create(ctx, topic) error`
- `CreateBatch(ctx, topics) error`
- `FindByID(ctx, id) (*model.TopicPool, error)`
- `FindByChannel(ctx, channelID, status, offset, limit) ([]*model.TopicPool, int64, error)`
- `ClaimOne(ctx, userID, channelID) (*model.TopicPool, error)` — atomic: selects earliest `unused` topic, sets `status=used`, `used_at=now`, returns it
- `ResetStatus(ctx, userID, id) error` — resets a topic back to `unused`, clears `task_id` and `used_at`
- `Delete(ctx, userID, id) error`

Add to `Repository` facade interface in `repository.go` and `txRepository`.

### Service (`server/service/topic_pool.go`)

New `TopicPoolService` with methods:
- `Add(ctx, userID, channelID, topics []string) ([]*model.TopicPool, error)` — validates channel ownership, creates batch
- `List(ctx, userID, channelID, status string, offset, limit int) ([]*model.TopicPool, int64, error)` — validates channel ownership
- `Claim(ctx, userID, channelID) (string, error)` — calls repo `ClaimOne`, returns topic text or empty string if pool empty
- `Reset(ctx, userID, id) error` — validates ownership, resets status
- `Delete(ctx, userID, id) error` — validates ownership, deletes

### Integration Point: `CreateFromPlan`

In `server/service/task.go` → `CreateFromPlan`:

```
Current flow:
  prompt = plan.Prompt
  if prompt == "" { prompt = plan.Title }
  → create task with prompt

New flow:
  prompt = plan.Prompt
  if prompt == "" {
      prompt = plan.Title
  }
  if prompt == "" {
      // Try to claim from topic pool
      claimed, _ := topicPoolSvc.Claim(ctx, plan.UserID, plan.ChannelID)
      if claimed != "" {
          prompt = claimed
      }
  }
  → create task with prompt (empty = AI auto-research fallback)
```

This ensures topic pool is only consulted when both `plan.Prompt` and `plan.Title` are empty. If the pool has no available topics, the empty prompt triggers the existing AI auto-research path via `BuildUserPrompt`.

### Handler (`server/handler/topic_pool.go`)

New `TopicPoolHandler` with Fiber handlers:
- `List` — GET `/api/v1/channels/:channel_id/topics`
- `Create` — POST `/api/v1/channels/:channel_id/topics` (accepts `{topics: ["topic1", "topic2"]}` for batch add)
- `Delete` — DELETE `/api/v1/channels/:channel_id/topics/:id`
- `Reset` — PATCH `/api/v1/channels/:channel_id/topics/:id/reset`

All handlers validate JWT auth and channel ownership.

### MCP Tools (`server/mcp/topic_pool_tools.go`)

New MCP tools registered in `RegisterTools`:

1. **`claim_topic(channel_id)`** — Claims the next unused topic from the pool. Returns `{topic: "text", id: 123}` or `{topic: null}` if empty.
2. **`list_topics(channel_id, status?)`** — Lists pool contents with optional status filter.
3. **`add_topic(channel_id, topic)`** — Adds a single topic to the pool.

### Route Registration

Add to `server/router/router.go` under the JWT-protected group:

```go
if svc.TopicPoolHandler != nil {
    channels := api.Group("/channels/:channel_id")
    channels.Get("/topics", svc.TopicPoolHandler.List)
    channels.Post("/topics", svc.TopicPoolHandler.Create)
    channels.Delete("/topics/:id", svc.TopicPoolHandler.Delete)
    channels.Patch("/topics/:id/reset", svc.TopicPoolHandler.Reset)
}
```

### Wiring in `server/main.go`

1. Create `TopicPoolService` with repository dependency
2. Create `TopicPoolHandler` with service dependency
3. Pass `TopicPoolService` to `TaskService` for the `CreateFromPlan` integration
4. Add handler to `router.Services` struct
5. Register MCP tools with `SetServices`

## Studio Frontend

### API Client (`studio/src/lib/api/topic-pool.ts`)

```typescript
topicPoolApi = {
  list(channelId, params?: { status?: string; offset?: number; limit?: number }),
  create(channelId, data: { topics: string[] }),
  delete(channelId, topicId: number),
  reset(channelId, topicId: number),
}
```

Add to unified `api` object as `api.topicPool`.

### Types (`studio/src/types/topic-pool.ts`)

```typescript
type TopicPoolStatus = 'unused' | 'used'
interface TopicPool {
  id: number
  user_id: string
  channel_id: string
  topic: string
  status: TopicPoolStatus
  task_id?: string
  used_at?: string
  created_at: string
  updated_at: string
}
```

### UI Component

Add a "选题池" section to the existing channel management flow. Options:

- A tab or collapsible section within the channel detail view
- A dialog accessible from the channels page

Displays:
- Two tabs: "未使用" / "已使用"
- Text input for adding topics (textarea, one topic per line, batch add)
- Delete button per topic
- Reset button for used topics
- Status badge and linked task ID for used topics

## Verification

1. **Backend unit tests**: Repository CRUD, `ClaimOne` atomicity, `CreateFromPlan` integration
2. **API tests**: Handler endpoints with auth and ownership validation
3. **MCP tool tests**: `claim_topic` returns topic or null correctly
4. **Integration test**: Create plan with empty prompt → topic pool claim → task created with pool topic
5. **Fallback test**: Pool empty → `CreateFromPlan` → task with empty prompt → AI auto-research kicks in
6. **Studio**: Add topics → verify list updates → run plan → verify topic marked as used
