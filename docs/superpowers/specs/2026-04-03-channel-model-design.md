# Channel Model Design Spec

**Date**: 2026-04-03
**Status**: Draft
**Replaces**: UserConfig-based configuration system

## Context

Anban 智能创作助手 Online Service currently uses `UserConfig` with a unique constraint on `(user_id, scope)` to store platform credentials and content style settings. This limits users to one configuration per content type (article/xls/seednote). In practice, a user may manage multiple WeChat public accounts (e.g., a food blog and a tech blog) or multiple Seednote accounts.

This redesign introduces **Channel** — a first-class entity representing a single platform account. All tasks and plans belong to a Channel, providing clear ownership, filtering, and statistics per account.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Entity name | `Channel` | No ambiguity with User (login account). Code-clean: `ChannelID`, `ListChannels`. Frontend displays as "频道管理" or "我的账号". |
| Channel scope | Single platform per channel | One Channel = one platform (WeChat public account / Seednote account / WeChat newspic account). Simple isolation. |
| Credential changes | No impact on history | Changing Channel credentials does not affect completed tasks or running plans. Next execution uses new credentials. |
| Statistics | Computed at query time | No denormalized counters. Query tasks by channel_id for counts, success rate, last activity. |

## Data Model

### Channel (new table, replaces `user_configs`)

```
channels
├── id              char(36)     PK, UUID
├── user_id         char(36)     FK → users.id, indexed
├── platform        varchar(20)  "article" / "xls" / "seednote"
├── name            varchar(100) "美食公众号"
├── avatar_url      varchar(500) nullable
├── description     text         nullable, 频道简介
├── wechat_app_id   varchar(100) nullable
├── wechat_secret   varchar(200) nullable
├── keywords        text         nullable
├── positioning     text         nullable
├── style           varchar(50)  nullable
├── theme           varchar(50)  nullable
├── author          varchar(50)  nullable
├── image_api_config json        nullable
├── status          varchar(20)  "active" / "archived", default "active"
├── created_at      timestamp
└── updated_at      timestamp

Indexes:
  - idx_channels_user_id (user_id)
  - idx_channels_user_platform (user_id, platform)
  - unique: (user_id, name) — same user can't have duplicate channel names
```

### Task changes

```
ALTER TABLE tasks ADD COLUMN channel_id char(36) AFTER user_id;
CREATE INDEX idx_tasks_channel_id ON tasks(channel_id);
```

### Plan changes

```
ALTER TABLE plans ADD COLUMN channel_id char(36) AFTER user_id;
CREATE INDEX idx_plans_channel_id ON plans(channel_id);
```

### Model code (`server/model/channel.go`)

```go
type Channel struct {
    ID             string    `gorm:"type:char(36);primaryKey" json:"id"`
    UserID         string    `gorm:"type:char(36);index;not null" json:"user_id"`
    Platform       string    `gorm:"type:varchar(20);not null" json:"platform"`
    Name           string    `gorm:"type:varchar(100);not null" json:"name"`
    AvatarURL      string    `gorm:"type:varchar(500)" json:"avatar_url"`
    Description    string    `gorm:"type:text" json:"description"`
    WechatAppID    string    `gorm:"type:varchar(100)" json:"wechat_app_id"`
    WechatSecret   string    `gorm:"type:varchar(200)" json:"-"` // hidden from API
    Keywords       string    `gorm:"type:text" json:"keywords"`
    Positioning    string    `gorm:"type:text" json:"positioning"`
    Style          string    `gorm:"type:varchar(50)" json:"style"`
    Theme          string    `gorm:"type:varchar(50)" json:"theme"`
    Author         string    `gorm:"type:varchar(50)" json:"author"`
    ImageAPIConfig string    `gorm:"type:json" json:"image_api_config"`
    Status         string    `gorm:"type:varchar(20);default:active" json:"status"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

`WechatSecret` uses `json:"-"` — never exposed to API responses. Only returned in explicit "get credential" endpoints if needed.

### Constants (`server/model/constants.go` additions)

```go
const (
    ChannelStatusActive   = "active"
    ChannelStatusArchived = "archived"
)

const (
    PlatformArticle = "article"
    PlatformXLS     = "xls"
    PlatformSeednote = "seednote"
)
```

## API Endpoints

All endpoints require JWT authentication. All return `{code, msg, data}`.

### Channel CRUD

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/channels` | List user's channels. Query: `?status=active`, `?platform=article` |
| POST | `/api/v1/channels` | Create channel. Body: `{platform, name, wechat_app_id?, ...}` |
| GET | `/api/v1/channels/:id` | Get channel detail + stats |
| PUT | `/api/v1/channels/:id` | Update channel fields |
| PATCH | `/api/v1/channels/:id/archive` | Archive channel (status → archived) |
| PATCH | `/api/v1/channels/:id/restore` | Restore channel (status → active) |
| DELETE | `/api/v1/channels/:id` | Delete channel (only if no tasks reference it) |

### Channel detail response

```json
{
  "code": 0,
  "msg": "ok",
  "data": {
    "channel": { ... },
    "stats": {
      "total_tasks": 42,
      "completed_tasks": 38,
      "failed_tasks": 2,
      "running_tasks": 1,
      "pending_tasks": 1,
      "success_rate": 0.90,
      "last_activity_at": "2026-04-02T10:30:00Z"
    }
  }
}
```

### Task/Plan API changes

- `POST /api/v1/tasks` body now requires `channel_id` (string, required). `type` is inferred from `channel.platform`.
- `POST /api/v1/plans` body now requires `channel_id` (string, required).
- `GET /api/v1/tasks` supports `?channel_id=` filter.
- `GET /api/v1/timeline` supports `?channel_id=` filter.

### Backward compatibility

The old `/api/v1/configs` endpoints remain functional during transition. They map internally to Channel queries using `(user_id, platform)` as the lookup key. These endpoints are deprecated and will be removed in a future version.

## Repository Layer

### ChannelRepository interface

```go
type ChannelRepository interface {
    Create(ctx context.Context, channel *model.Channel) error
    FindByID(ctx context.Context, id string) (*model.Channel, error)
    ListByUserID(ctx context.Context, userID string, opts ListOptions) ([]*model.Channel, error)
    FindByUserAndPlatform(ctx context.Context, userID, platform string) ([]*model.Channel, error)
    Update(ctx context.Context, channel *model.Channel) error
    UpdateStatus(ctx context.Context, id, status string) error
    Delete(ctx context.Context, id string) error
    GetStats(ctx context.Context, channelID string) (*ChannelStats, error)
}

type ChannelStats struct {
    TotalTasks     int64
    CompletedTasks int64
    FailedTasks    int64
    RunningTasks   int64
    PendingTasks   int64
    SuccessRate    float64
    LastActivityAt *time.Time
}

type ListOptions struct {
    Status   string // filter by status
    Platform string // filter by platform
}
```

Add `Channels() ChannelRepository` to the main `Repository` interface.

## Service Layer

### ChannelService

```go
type ChannelService struct {
    repo   repository.Repository
    logger *zerolog.Logger
}

func (s *ChannelService) Create(ctx, userID, req) (*model.Channel, error)
func (s *ChannelService) Get(ctx, userID, channelID) (*model.Channel, *ChannelStats, error)
func (s *ChannelService) List(ctx, userID, opts) ([]*model.Channel, error)
func (s *ChannelService) Update(ctx, userID, channelID, req) (*model.Channel, error)
func (s *ChannelService) Archive(ctx, userID, channelID) error
func (s *ChannelService) Restore(ctx, userID, channelID) error
func (s *ChannelService) Delete(ctx, userID, channelID) error
```

All methods verify `channel.UserID == userID` for ownership.

### TaskService changes

- `HandleExecution` changes from:
  ```go
  cfg, _ := s.repo.UserConfigs().FindByUserAndScope(ctx, userID, task.Type)
  ```
  to:
  ```go
  channel, _ := s.repo.Channels().FindByID(ctx, task.ChannelID)
  ```
- The loaded `Channel` replaces `UserConfig` in `ExecutionOptions`.
- `CreateTask` requires `channel_id`. Validates channel exists, belongs to user, is active. Sets `task.Type = channel.Platform`.

## Agent Layer Changes

### BuildAppConfig (mcp_server.go)

Signature changes from `BuildAppConfig(uc *model.UserConfig)` to `BuildAppConfig(ch *model.Channel)`. Field mapping is identical since Channel has all the same fields as UserConfig (plus new ones).

### CreateMCPTools (mcp_server.go)

Changes from `CreateMCPTools(workDir string, userConfig *model.UserConfig, ...)` to `CreateMCPTools(workDir string, channel *model.Channel, ...)`.

### Executor (executor.go)

`ExecutionOptions` struct changes:
- Remove `UserConfig *model.UserConfig`
- Add `Channel *model.Channel`

### Prompts (prompts.go)

`GetSystemPrompt` changes from accepting `(taskType string, uc *model.UserConfig)` to `(taskType string, ch *model.Channel)`. Field references (`uc.Name`, `uc.Keywords`, etc.) become `ch.Name`, `ch.Keywords`.

### MCP HTTP endpoint (mcp/mcp.go)

Currently loads configs via `UserConfigs().ListByUserID`. Changes to `Channels().ListByUserID(userID, ListOptions{Status: ChannelStatusActive})`.

## Frontend Design

### New page: ChannelsPage (`/channels`)

- **Grid layout**: Cards with avatar, name, platform badge, description, task count
- **Create**: Modal with platform selector → form with platform-specific fields
- **Edit**: Same modal, pre-filled
- **Archive/Restore**: Context menu or button on card
- **Stats**: Inline on card (task count, last activity)

### Updated: TaskDetailPage

- Shows Channel name + avatar at top of task detail
- Channel links to `/channels/:id`

### Updated: TasksPage

- Channel filter dropdown in toolbar (shows active channels grouped by platform)
- Each task row shows channel avatar + name

### Updated: PlansPage

- Channel filter dropdown
- Plan creation modal requires channel selection (replaces type selection)

### Updated: CreateTaskModal / CreatePlanModal

- First field: Channel selector (grouped by platform: 公众号 / 种草笔记 / 小绿书)
- Selecting a channel auto-sets the type/platform
- Shows channel name and avatar in the selector

### Navigation

- Add "频道管理" to sidebar/topnav
- Settings page: remove content config tabs (moved to channel management)

## Data Migration

### Step 1: Create channels table (AutoMigrate)

GORM AutoMigrate adds the new table.

### Step 2: Migrate user_configs → channels

```sql
INSERT INTO channels (id, user_id, platform, name, wechat_app_id, wechat_secret,
    keywords, positioning, style, theme, author, image_api_config, status, created_at, updated_at)
SELECT
    UUID() as id,
    user_id,
    scope as platform,
    COALESCE(name, CONCAT(scope, '频道')) as name,
    wechat_app_id,
    wechat_secret,
    keywords,
    positioning,
    style,
    theme,
    author,
    image_api_config,
    'active' as status,
    created_at,
    updated_at
FROM user_configs;
```

### Step 3: Add channel_id to tasks/plans

AutoMigrate adds the column (nullable initially).

### Step 4: Backfill channel_id

For each task, find the channel where `channel.user_id = task.user_id AND channel.platform = task.type`:
```sql
UPDATE tasks t
JOIN channels c ON c.user_id = t.user_id AND c.platform = t.type
SET t.channel_id = c.id;
```

Same for plans.

### Step 5: Make channel_id NOT NULL

After backfill verification, add NOT NULL constraint.

## Files to Create

| File | Purpose |
|------|---------|
| `server/model/channel.go` | Channel model |
| `server/repository/channel.go` | ChannelRepository interface + implementation |
| `server/service/channel.go` | ChannelService business logic |
| `server/handler/channel.go` | Channel CRUD handlers |
| `web/src/pages/ChannelsPage.tsx` | Channel management page |
| `web/src/components/ChannelSelector.tsx` | Reusable channel picker component |
| `web/src/components/ChannelCard.tsx` | Channel card component |

## Files to Modify

| File | Changes |
|------|---------|
| `server/model/model.go` | Add Channel to AutoMigrate; add unique index; add migration logic |
| `server/model/task.go` | Add ChannelID field |
| `server/model/plan.go` | Add ChannelID field |
| `server/model/constants.go` | Add Channel/Platform constants |
| `server/repository/repository.go` | Add Channels() to Repository interface |
| `server/repository/task.go` | Update queries to support channel_id filter |
| `server/repository/plan.go` | Update queries to support channel_id filter |
| `server/service/task.go` | Load Channel instead of UserConfig; require channel_id on create |
| `server/service/plan.go` | Load Channel; require channel_id on create |
| `server/agent/mcp_server.go` | BuildAppConfig takes Channel; CreateMCPTools takes Channel |
| `server/agent/executor.go` | ExecutionOptions uses Channel instead of UserConfig |
| `server/agent/prompts.go` | GetSystemPrompt uses Channel fields |
| `server/handler/task.go` | Create requires channel_id; list supports channel_id filter |
| `server/handler/plan.go` | Same as task handler changes |
| `server/handler/config.go` | Deprecate: map to Channel internally |
| `server/handler/timeline.go` | Support channel_id filter |
| `server/mcp/mcp.go` | Load channels instead of user_configs |
| `server/router/router.go` | Register channel routes |
| `server/main.go` | Wire ChannelService |
| `web/src/lib/api.ts` | Add Channel types + API methods; update Task/Plan types |
| `web/src/pages/TasksPage.tsx` | Channel filter + channel info on task rows |
| `web/src/pages/PlansPage.tsx` | Channel filter + channel in create modal |
| `web/src/pages/TaskDetailPage.tsx` | Show channel info |
| `web/src/pages/SettingsPage.tsx` | Remove content config tabs |

## Verification

```bash
# Backend
go build ./server/...
go vet ./server/...
go test -v ./server/...

# Manual API testing
# Create channel
curl -X POST /api/v1/channels -d '{"platform":"article","name":"测试公众号"}'

# List channels
curl /api/v1/channels

# Create task with channel_id
curl -X POST /api/v1/tasks -d '{"channel_id":"...","topic":"测试"}'

# Filter tasks by channel
curl '/api/v1/tasks?channel_id=...'

# Timeline with channel filter
curl '/api/v1/timeline?from=2026-01-01&to=2026-12-31&channel_id=...'

# Frontend
cd web && npm run dev
# Navigate to /channels — verify CRUD, stats display
# Create task — verify channel selector, platform auto-set
# Task list — verify channel filter works
```
