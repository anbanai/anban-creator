# 选题去重功能设计

## Context

当前 AI Agent 在生成选题时，只能通过 `list_drafts` 和 `list_published` 查看微信公众号平台上已有的草稿和已发布文章，无法查看系统内部 Task 数据库中已创建的选题。这导致重复选题（同一 Channel 内多次创建相似任务）。

**目标**: 新增 `list_topics` MCP 工具，让 AI Agent 在选题前能查看当前 Channel 已有的所有选题文本列表，自行判断避免重复。

## 设计

### 1. 新增 Repository 方法

在 `TaskRepository` 接口和实现中新增 `FindTopicsByChannelID`:

**文件**: `server/repository/repository.go`, `server/repository/task.go`

```go
// FindTopicsByChannelID returns all distinct topic texts for a channel,
// ordered by creation time descending.
FindTopicsByChannelID(ctx context.Context, channelID string) ([]string, error)
```

SQL 查询: `SELECT DISTINCT topic FROM tasks WHERE channel_id = ? ORDER BY created_at DESC`

### 2. 新增 Service 方法

在 `TaskService` 中新增 `ListTopics`:

**文件**: `server/service/task.go`

```go
func (s *TaskService) ListTopics(ctx context.Context, channelID string) ([]string, error)
```

直接调用 `repo.Tasks().FindTopicsByChannelID(ctx, channelID)`。

### 3. 新增 MCP 工具 `list_topics`

**文件**: `server/mcp/tools.go`

工具定义:
- 名称: `list_topics`
- 参数: `channel_id` (必填)
- 返回: `{"topics": ["选题1", "选题2", ...], "count": N}`

Handler 获取 userID（验证 channel 归属），调用 `TaskSvc.ListTopics`。

注册在 `registerTaskTools` 中，与 `list_tasks`、`create_task` 等放在一起。

### 4. 更新 Skill 定义

在以下 5 个 skill 中，选题步骤前加入 `list_topics` 调用指令:

| Skill 文件 | 修改内容 |
|-----------|---------|
| `plugin/skills/topic-research/SKILL.md` | 步骤 0 中增加 `list_topics(channel_id)` 调用 |
| `plugin/skills/rednote-research/SKILL.md` | 新增选题前检查已有选题的步骤 |
| `plugin/skills/article/SKILL.md` | 步骤 1 增加 `list_topics(channel_id)` 调用 |
| `plugin/skills/xls/SKILL.md` | 步骤 1 增加 `list_topics(channel_id)` 调用 |
| `plugin/skills/rednote/SKILL.md` | 步骤 3 前（或步骤 1 末尾）增加 `list_topics(channel_id)` 调用 |

更新方式：在已有的 `list_drafts`/`list_published` 检查之后，追加 `list_topics(channel_id)` 调用，注明"选题应同时避开这些已有任务选题"。

## 涉及文件

1. `server/repository/repository.go` — 添加接口方法
2. `server/repository/task.go` — 添加实现
3. `server/service/task.go` — 添加 `ListTopics` 方法
4. `server/mcp/tools.go` — 添加 `list_topics` 工具注册和 handler
5. `plugin/skills/topic-research/SKILL.md` — 更新步骤 0
6. `plugin/skills/rednote-research/SKILL.md` — 添加选题前检查步骤
7. `plugin/skills/article/SKILL.md` — 更新步骤 1
8. `plugin/skills/xls/SKILL.md` — 更新步骤 1
9. `plugin/skills/rednote/SKILL.md` — 更新步骤 1 或步骤 3 前

## 验证方式

1. 启动 server，通过 MCP 调用 `list_topics(channel_id=...)`，确认返回已有选题列表
2. 创建几个同 channel 的 task，再次调用确认列表更新
3. 观察 AI Agent 执行任务时是否正确调用 `list_topics` 并避开已有选题
4. 运行 `go test ./server/...` 确保无回归
