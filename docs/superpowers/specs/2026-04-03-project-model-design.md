# Project Model Design Spec

**Date**: 2026-04-03
**Status**: Draft
**Replaces**: UserConfig-based configuration system

> **Naming update (2026-06):** the entity this spec introduced was originally called
> `Channel` (a WeChat/Seednote account). It has since been renamed to `Project`
> throughout the codebase, DB (`channels` → `projects`, `channel_id` → `project_id`),
> API, and UI. This document is rewritten in the current terminology; the design
> rationale and data model are otherwise unchanged.

## Context

Anban 智能创作助手 Online Service currently uses `UserConfig` with a unique constraint on `(user_id, scope)` to store platform credentials and content style settings. This limits users to one configuration per content type (article/seednote). In practice, a user may manage multiple WeChat public accounts (e.g., a food blog and a tech blog) or multiple Seednote accounts.

This redesign introduces **Project** — a first-class entity representing a single platform account. All tasks and plans belong to a Project, providing clear ownership, filtering, and statistics per account.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Entity name | `Project` | No ambiguity with User (login account). Code-clean: `ProjectID`, `ListProjects`. Frontend displays as "项目管理" or "我的账号". |
| Project scope | Single platform per project | One Project = one platform (WeChat public account / Seednote account). Simple isolation. |
| Credential changes | No impact on history | Changing Project credentials does not affect completed tasks or running plans. Next execution uses new credentials. |
| Statistics | Computed at query time | No denormalized counters. Query tasks by project_id for counts, success rate, last activity. |

## Data Model

### Project (new table, replaces `user_configs`)

```
projects
├── id              char(36)     PK, UUID
├── user_id         char(36)     FK → users.id, indexed
├── platform        varchar(20)  "article" / "seednote"
├── name            varchar(100) "美食公众号"
├── avatar_url      varchar(500) nullable
├── description     text         nullable, 项目简介
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
  - idx_projects_user_id (user_id)
  - idx_projects_user_platform (user_id, platform)
  - unique: (user_id, name) — same user can't have duplicate project names
```

### Task changes

```
ALTER TABLE tasks ADD COLUMN project_id char(36) AFTER user_id;
CREATE INDEX idx_tasks_project_id ON tasks(project_id);
```

### Plan changes

```
ALTER TABLE plans ADD COLUMN project_id char(36) AFTER user_id;
CREATE INDEX idx_plans_project_id ON plans(project_id);
```

### Model code (`server/model/project.go`)

```go
type Project struct {
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
    ProjectStatusActive   = "active"
    ProjectStatusArchived = "archived"
)

const (
    PlatformArticle = "article"
    PlatformSeednote = "seednote"
)
```

## API Endpoints

All endpoints require JWT authentication. All return `{code, msg, data}`.

### Project CRUD

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/projects` | List user's projects. Query: `?status=active`, `?platform=article` |
| POST | `/api/v1/projects` | Create project. Body: `{platform, name, wechat_app_id?, ...}` |
| GET | `/api/v1/projects/:id` | Get project detail + stats |
| PUT | `/api/v1/projects/:id` | Update project fields |
| PATCH | `/api/v1/projects/:id/archive` | Archive project (status → archived) |
| PATCH | `/api/v1/projects/:id/restore` | Restore project (status → active) |
| DELETE | `/api/v1/projects/:id` | Delete project (only if no tasks reference it) |

### Project detail response

```json
{
  "code": 0,
  "msg": "ok",
  "data": {
    "project": { ... },
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

- `POST /api/v1/tasks` body now requires `project_id` (string, required). `type` is inferred from `project.platform`.
- `POST /api/v1/plans` body now requires `project_id` (string, required).
- `GET /api/v1/tasks` supports `?project_id=` filter.
- `GET /api/v1/timeline` supports `?project_id=` filter.

### Backward compatibility

The old `/api/v1/configs` endpoints remain functional during transition. They map internally to Project queries using `(user_id, platform)` as the lookup key. These endpoints are deprecated and will be removed in a future version.

## Repository Layer

### ProjectRepository interface

```go
type ProjectRepository interface {
    Create(ctx context.Context, project *model.Project) error
    FindByID(ctx context.Context, id string) (*model.Project, error)
    ListByUserID(ctx context.Context, userID string, opts ListOptions) ([]*model.Project, error)
    FindByUserAndPlatform(ctx context.Context, userID, platform string) ([]*model.Project, error)
    Update(ctx context.Context, project *model.Project) error
    UpdateStatus(ctx context.Context, id, status string) error
    Delete(ctx context.Context, id string) error
    GetStats(ctx context.Context, projectID string) (*ProjectStats, error)
}

type ProjectStats struct {
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

Add `Projects() ProjectRepository` to the main `Repository` interface.

## Service Layer

### ProjectService

```go
type ProjectService struct {
    repo   repository.Repository
    logger *zerolog.Logger
}

func (s *ProjectService) Create(ctx, userID, req) (*model.Project, error)
func (s *ProjectService) Get(ctx, userID, projectID) (*model.Project, *ProjectStats, error)
func (s *ProjectService) List(ctx, userID, opts) ([]*model.Project, error)
func (s *ProjectService) Update(ctx, userID, projectID, req) (*model.Project, error)
func (s *ProjectService) Archive(ctx, userID, projectID) error
func (s *ProjectService) Restore(ctx, userID, projectID) error
func (s *ProjectService) Delete(ctx, userID, projectID) error
```

All methods verify `project.UserID == userID` for ownership.

### TaskService changes

- `HandleExecution` changes from:
  ```go
  cfg, _ := s.repo.UserConfigs().FindByUserAndScope(ctx, userID, task.Type)
  ```
  to:
  ```go
  project, _ := s.repo.Projects().FindByID(ctx, task.ProjectID)
  ```
- The loaded `Project` replaces `UserConfig` in `ExecutionOptions`.
- `CreateTask` requires `project_id`. Validates project exists, belongs to user, is active. Sets `task.Type = project.Platform`.

## Agent Layer Changes

### BuildAppConfig (mcp_server.go)

Signature changes from `BuildAppConfig(uc *model.UserConfig)` to `BuildAppConfig(ch *model.Project)`. Field mapping is identical since Project has all the same fields as UserConfig (plus new ones).

### CreateMCPTools (mcp_server.go)

Changes from `CreateMCPTools(workDir string, userConfig *model.UserConfig, ...)` to `CreateMCPTools(workDir string, project *model.Project, ...)`.

### Executor (executor.go)

`ExecutionOptions` struct changes:
- Remove `UserConfig *model.UserConfig`
- Add `Project *model.Project`

### Prompts (prompts.go)

`GetSystemPrompt` changes from accepting `(taskType string, uc *model.UserConfig)` to `(taskType string, ch *model.Project)`. Field references (`uc.Name`, `uc.Keywords`, etc.) become `ch.Name`, `ch.Keywords`.

### MCP HTTP endpoint (mcp/mcp.go)

Currently loads configs via `UserConfigs().ListByUserID`. Changes to `Projects().ListByUserID(userID, ListOptions{Status: ProjectStatusActive})`.

## Frontend Design

### New page: ProjectsPage (`/projects`)

- **Grid layout**: Cards with avatar, name, platform badge, description, task count
- **Create**: Modal with platform selector → form with platform-specific fields
- **Edit**: Same modal, pre-filled
- **Archive/Restore**: Context menu or button on card
- **Stats**: Inline on card (task count, last activity)

### Updated: TaskDetailPage

- Shows Project name + avatar at top of task detail
- Project links to `/projects/:id`

### Updated: TasksPage

- Project filter dropdown in toolbar (shows active projects grouped by platform)
- Each task row shows project avatar + name

### Updated: PlansPage

- Project filter dropdown
- Plan creation modal requires project selection (replaces type selection)

### Updated: CreateTaskModal / CreatePlanModal

- First field: Project selector (grouped by platform: 公众号 / 种草笔记)
- Selecting a project auto-sets the type/platform
- Shows project name and avatar in the selector

### Navigation

- Add "项目管理" to sidebar/topnav
- Settings page: remove content config tabs (moved to project management)

## Data Migration

### Step 1: Create projects table (AutoMigrate)

GORM AutoMigrate adds the new table.

### Step 2: Migrate user_configs → projects

```sql
INSERT INTO projects (id, user_id, platform, name, wechat_app_id, wechat_secret,
    keywords, positioning, style, theme, author, image_api_config, status, created_at, updated_at)
SELECT
    UUID() as id,
    user_id,
    scope as platform,
    COALESCE(name, CONCAT(scope, '项目')) as name,
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

### Step 3: Add project_id to tasks/plans

AutoMigrate adds the column (nullable initially).

### Step 4: Backfill project_id

For each task, find the project where `project.user_id = task.user_id AND project.platform = task.type`:
```sql
UPDATE tasks t
JOIN projects c ON c.user_id = t.user_id AND c.platform = t.type
SET t.project_id = c.id;
```

Same for plans.

### Step 5: Make project_id NOT NULL

After backfill verification, add NOT NULL constraint.

## Files to Create

| File | Purpose |
|------|---------|
| `server/model/project.go` | Project model |
| `server/repository/project.go` | ProjectRepository interface + implementation |
| `server/service/project.go` | ProjectService business logic |
| `server/handler/project.go` | Project CRUD handlers |
| `web/src/pages/ProjectsPage.tsx` | Project management page |
| `web/src/components/ProjectSelector.tsx` | Reusable project picker component |
| `web/src/components/ProjectCard.tsx` | Project card component |

## Files to Modify

| File | Changes |
|------|---------|
| `server/model/model.go` | Add Project to AutoMigrate; add unique index; add migration logic |
| `server/model/task.go` | Add ProjectID field |
| `server/model/plan.go` | Add ProjectID field |
| `server/model/constants.go` | Add Project/Platform constants |
| `server/repository/repository.go` | Add Projects() to Repository interface |
| `server/repository/task.go` | Update queries to support project_id filter |
| `server/repository/plan.go` | Update queries to support project_id filter |
| `server/service/task.go` | Load Project instead of UserConfig; require project_id on create |
| `server/service/plan.go` | Load Project; require project_id on create |
| `server/agent/mcp_server.go` | BuildAppConfig takes Project; CreateMCPTools takes Project |
| `server/agent/executor.go` | ExecutionOptions uses Project instead of UserConfig |
| `server/agent/prompts.go` | GetSystemPrompt uses Project fields |
| `server/handler/task.go` | Create requires project_id; list supports project_id filter |
| `server/handler/plan.go` | Same as task handler changes |
| `server/handler/config.go` | Deprecate: map to Project internally |
| `server/handler/timeline.go` | Support project_id filter |
| `server/mcp/mcp.go` | Load projects instead of user_configs |
| `server/router/router.go` | Register project routes |
| `server/main.go` | Wire ProjectService |
| `web/src/lib/api.ts` | Add Project types + API methods; update Task/Plan types |
| `web/src/pages/TasksPage.tsx` | Project filter + project info on task rows |
| `web/src/pages/PlansPage.tsx` | Project filter + project in create modal |
| `web/src/pages/TaskDetailPage.tsx` | Show project info |
| `web/src/pages/SettingsPage.tsx` | Remove content config tabs |

## Verification

```bash
# Backend
go build ./server/...
go vet ./server/...
go test -v ./server/...

# Manual API testing
# Create project
curl -X POST /api/v1/projects -d '{"platform":"article","name":"测试公众号"}'

# List projects
curl /api/v1/projects

# Create task with project_id
curl -X POST /api/v1/tasks -d '{"project_id":"...","topic":"测试"}'

# Filter tasks by project
curl '/api/v1/tasks?project_id=...'

# Timeline with project filter
curl '/api/v1/timeline?from=2026-01-01&to=2026-12-31&project_id=...'

# Frontend
cd web && npm run dev
# Navigate to /projects — verify CRUD, stats display
# Create task — verify project selector, platform auto-set
# Task list — verify project filter works
```
