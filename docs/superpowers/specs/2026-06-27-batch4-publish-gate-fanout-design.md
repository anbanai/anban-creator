# Batch 4 设计：自动发布人工审核门 + 同选题多账号/多风格分发

- **日期**：2026-06-27
- **状态**：草案（draft）—— 待用户 review 并拍板 Part B 的业务决策
- **来源**：全方位优化路线图 Batch 4（`/Users/medivh/.claude/plans/lazy-wibbling-pixel.md`）
- **前置**：本设计**不依赖**进行中的 `Task.Overrides` 重构能否编译（纯设计文档）。实现时需按 Overrides 新模型读取署名/风格（`resolved.Byline` 等），见各 Part 注记。

---

## 背景与目标

路线图验证式探查确认两个真实业务缺口：

1. **自动发布无人工审核门**：`project.enable_publishing` 开启即直接落草稿，无「发布前需人工确认」选项（品牌安全缺口）。
2. **同选题多账号/多风格分发缺失**：单任务绑定单 project，无法一个选题 → N 个账号（各带不同三维风格）并行产出。

本设计把两者拆成两个独立 Part：**Part A 可直接落地**（低风险、依附既有 workflow 基础设施）；**Part B 是架构级改动**，需先定业务策略再分阶段实现。

---

## Part A —— 自动发布人工审核门（可直接实现）

### 现状（已核实锚点）

| 环节 | 位置 |
|---|---|
| 开关字段 | `ProjectConfig.EnablePublishing bool`（`server/model/project.go:13`）+ `Project.GetEnablePublishing()`（:88） |
| 自动发布触发 | `HandleExecution` 完成后条件分支（`server/service/task_execution.go:299-322`），关键判断 `:302` `if s.publishingSvc != nil && project != nil && project.GetEnablePublishing()` |
| Agent 已发布探测 | `wasPublishedByAgent(result.LogText)`（`:303`）—— 若 agent 执行中已调 publish，则跳过重复发布 |
| 草稿落库 | `autoPublishWithData()`（`:665`）goroutine，调用 `publishingSvc` → `app/draft.CreateDraft()`（`app/draft/service.go:86`） |
| 既有 workflow 阶段 | `WorkflowStagePublishOptional = "publish_optional"`（`server/service/workflow_status.go:24`），标签「发布草稿」（:149）；另有 `WorkflowStageReview = "review"`「质量复盘」 |
| Task 发布字段 | `Published bool` / `PublishedAt *time.Time` / `WorkflowStatus *string`（`server/model/task.go:131-133`） |

**关键认知**：当前所谓「发布」=在微信账号**草稿箱**创建草稿（`draft/add`），并非推送给粉丝。草稿落箱即占用账号草稿额度、出现在账号草稿列表。审核门的价值 = **在内容触碰微信账号之前**让人审一遍（质量/合规/品牌口径），并阻止 agent 执行中自动 publish。

### 数据模型改动

1. `ProjectConfig` 新增：
   ```go
   RequirePublishApproval bool `json:"require_publish_approval"`
   ```
   + `Project.GetRequirePublishApproval()` getter（镜像 `GetEnablePublishing`）。
   - 语义：`EnablePublishing=true` 且 `RequirePublishApproval=true` → 进审核门；`RequirePublishApproval=false` → 维持现状（自动落草稿）。二者正交，老项目零行为变化。

2. `Task` 新增发布审核态字段（**复用而非新表**）：
   ```go
   PublishApprovalState  string  `gorm:"type:varchar(20);default:''" json:"publish_approval_state,omitempty"`
   PendingDraftArticles  datatypes.JSONType[[]DraftArticleInput] `gorm:"type:json" json:"pending_draft_articles,omitempty"`
   ```
   - `PublishApprovalState` 取值：`""`（不适用/未走门）| `"pending"`（待审核）| `"approved"`（已放行→已落草稿）| `"rejected"`（已驳回）。
   - `PendingDraftArticles`：审核门触发时，把本该立即发布的 `[]DraftArticleInput`（标题/作者/digest/content/thumb…）冻结存盘，等放行时取出落草稿。这样 task 完成后 workspace 可正常清理，不依赖临时文件。

### 服务流程改动

在 `HandleExecution` 的发布分支（`task_execution.go:299-322`）插入门控：

```
if enable_publishing:
    articles = extractArticleDraftFromWorkspace(...)   # 既有
    if project.GetRequirePublishApproval() && !wasPublishedByAgent:
        # 进审核门：冻结草稿数据，置 pending，不发草稿
        task.PublishApprovalState = "pending"
        task.PendingDraftArticles = articles
        repo.Save(task)
        markStage(publish_optional, status="pending", label="待发布审核")
        notify(user, "task ready for publish approval")   # 复用 SSE/WS
    else:
        # 维持现状：autoPublishWithData goroutine
```

放行/驳回新方法（`server/service/publishing.go` 或 `task_publish_approval.go`）：
- `ApprovePublish(ctx, taskID) error`：owner 校验 → 状态须 `pending` → 取 `PendingDraftArticles` → 调既有 `publishingSvc.PublishDraft` 落草稿 → 置 `approved` + `Published=true` + `PublishedAt` → 清空 `PendingDraftArticles` → 通知。
- `RejectPublish(ctx, taskID, reason) error`：owner 校验 → 置 `rejected` → 清空 `PendingDraftArticles` → 通知。

### API

```
POST /api/v1/tasks/:id/publish-approve     → 200 {published:true}
POST /api/v1/tasks/:id/publish-reject      body {reason}  → 200 {state:"rejected"}
```
（鉴权复用 `requireOwnership` 模式；handler 风格对齐既有 `Cancel`/`Retry`。）

### Studio UI

- `TaskDetailPage`：当 `task.publish_approval_state === "pending"` 时，展示「待发布审核」卡（预览标题/digest/封面缩略图 + 「放行发布 / 驳回」按钮）。
- 复用既有 `fetchPreviewHTML` 渲染内容预览。
- `ProjectEditDialog`：在 `enable_publishing` 开关下增加子开关「发布前需人工审核」（默认关）。
- 列表 Badge：pending 审核态加一个「待审核发布」徽章。

### 边界与交互

- **Agent 执行中已发布**（`wasPublishedByAgent`）：跳过门控，直接 `approved`（agent 已落草稿，无需再审）。
- **重试**：重试产生的新任务是全新计费任务，按其自身 project 配置决定是否走门控（与 `task_retry.go` 一致：克隆 Overrides，project 一致 → 门控行为一致）。
- **与 Part B 的交互**：fanout 下每个子任务独立走各自 project 的门控。
- **草稿额度**：驳回 = 不落草稿，不消耗额度；放行才落。这是门控的额外收益。
- **DB 迁移**：新列均有 `default:''`/`type:json`，GORM AutoMigrate 兼容；老 task 行 `publish_approval_state=''`（不适用）。

### 实现工作量

小-中。改动面：`model/project.go`、`model/task.go`（2 字段+迁移）、`service/task_execution.go`（门控分支）、新 `service/task_publish_approval.go`、2 个 handler+路由、Studio 卡片+开关。无并发/计费新语义，风险低。

---

## Part B —— 同选题多账号/多风格分发（架构级，需分阶段）

### 现状架构（探查结论）

当前架构**本质单 project 作用域**，fanout 触及多处 BLOCKING 摩擦：

| 组件 | 现状 | fanout 摩擦 | 严重度 |
|---|---|---|---|
| Project | 1 project = 1 微信账号 + 完整三维风格（`project.go` VisualStyle/WriterKey/Theme/Byline/PersonaAvatar/MaxConcurrentTasks） | ✅ 完美契合（每账号一个 project） | 无 |
| `Task.ProjectID` | 单 FK（`task.go:78`），`CreateManual` 强制单 project（`service/task.go:227-251`） | ❌ 1:1 硬绑定 | **BLOCKING** |
| Plan 派生 | `Plan.ProjectID` 单（`plan.go:16`），`CreateFromPlan` 每 trigger 造 1 task（`service/task.go:438-474`） | ❌ 非多 project | **BLOCKING** |
| 选题池 | `ClaimForTask` 每 task 原子认领 1 题（`service/topic_pool.go:85-96`，`SELECT FOR UPDATE` + `unused→used`） | ❌ 防重复消费不变式与 fanout 冲突 | **BLOCKING** |
| 计费 | `DeductForTaskWithAmount` 每 task 扣（`credit.go:526-557`） | ⚠️ N task = N× 成本 | **业务决策** |
| 并发 | 每 project Redis 槽（`redis_pubsub.go:19-37` Lua 原子） | ⚠️ fanout 跨多 project，按各自额度排队 | 中 |

> 选题池防重复消费不变式见 memory `project_topic_pool_anti_double_consume_invariant`：服务端预认领写进 `task.Prompt` + skill 见 `about: <X>` 禁 claim，**改 executor prompt 必须同步改 6 个 research skill**。fanout 必须尊重此不变式。

### 三种实现路径（含取舍）

**路径 1：Fanout 父任务（parent → N children）**
- 新增「分发任务」：父任务记录选题 + target project 列表，执行时为每个 target 造一个子任务（各带目标 project 的三维风格）。
- 选题认领：父任务认领一次，子任务复用同一选题文本（不重复 claim）。
- 优点：语义清晰、选题天然共享、可汇总展示（父卡聚合 N 个产出）。
- 缺点：引入父子任务模型（新表/新字段、级联状态、级联取消/重试）、计费模型需重新定义（按父？按子？）、UI/时间线/选题池多处要懂父子关系。**工作量大**。

**路径 2：多 project 任务创建（N 个独立 task 共享选题）**
- 创建期：前端选 1 选题 + N 个 target project → 后端循环 `CreateManual` 造 N 个**独立** task（每个自带同一选题文本，**不走选题池 claim**，避免重复消费）。
- 计费：N × 每 task 成本（透明，用户创建前看到总价）。
- 优点：**零数据模型改动**，复用全部现有单 task 链路（执行/进度/取消/重试/门控），最快落地。
- 缺点：选题「共享」靠创建期复制文本（非运行时 claim），需确保不走选题池的 task 不触发 claim（见下「选题策略」）；N 个 task 在列表里分散，无聚合视图。

**路径 3：Plan 级 `TargetProjectIDs []string`**
- Plan 增 `TargetProjectIDs`（可空=现状单 project；非空=fanout）。`CreateFromPlan` 每 trigger 按 target 列表造 N task。
- 适合**定时**多账号分发。
- 缺点：与路径 1/2 的选题、计费问题相同，且定时场景的失败/部分失败处理更复杂。

### 推荐：分阶段（路径 2 先行 → 路径 3 → 视需要路径 1）

1. **阶段一（路径 2，小-中）**：Studio 创建对话框支持「分发到多个账号」——选 1 选题 + N project → 后端循环造 N 独立 task，创建前展示 N× 总价预估。**零模型改动**，立刻满足「一选题多账号」核心诉求。
2. **阶段二（路径 3，中）**：Plan 支持 `TargetProjectIDs`，定时多账号分发。
3. **阶段三（路径 1，大，按需）**：若用户要父子聚合视图/统一取消，再引入 fanout 父任务模型。

### 选题策略（必须尊重防重复消费不变式）

- 阶段一/二的 fanout task **直接以选题文本入 prompt，不调 `ClaimForTask`**（避免 N 次 claim 触发防重复消费守卫）。选题池只服务「单 task 自动选题」场景。
- 若选题来自选题池：fanout 创建时 claim **一次**，把同一选题文本复制给 N 个 task（认领 1 次、产出 N 份），符合「claim once, reference N」语义。需在 `CreateManual` 增一个 `skipTopicClaim bool` 或 fanout 专用入口。

### 待用户拍板的业务决策（Part B 阻塞项）

1. **计费**：N 账号 fanout = N× 成本（每账号独立生成）？还是有「分发折扣」？（推荐 N×，透明展示总价。）
2. **选题共享语义**：同一选题文本发到 N 账号是否合规（避免各账号内容完全雷同被平台判同质）？是否需为每账号做差异化改写（额外成本）？
3. **聚合视图**：是否需要父子聚合（路径 1），还是 N 个独立 task 即可（路径 2）？
4. **定时 fanout**：Plan 级多账号是否在范围内（阶段二）？

---

## 分阶段交付建议

| 阶段 | 内容 | 风险 | 前置 |
|---|---|---|---|
| **4A** | 自动发布审核门（完整实现） | 低 | 无（独立于 Overrides 重构） |
| **4B-1** | 多账号分发·创建期循环（路径 2） | 中 | 用户拍板计费 + 选题策略 |
| **4B-2** | Plan 级 `TargetProjectIDs`（路径 3） | 中 | 4B-1 落地 |
| **4B-3** | 父子 fanout 聚合（路径 1，可选） | 高 | 用户确认需聚合视图 |

建议先实现 **4A**（自包含、低风险、立即可用），Part B 待用户回答上述 4 个业务决策后再启动。

---

## 待用户 review 的问题

1. Part A 审核门：草稿数据冻结在 `Task.PendingDraftArticles`（JSON 列）的方案可接受吗？还是更倾向保留 workspace 临时文件到审核完成？（推荐前者，避免 workspace 清理竞态。）
2. Part B 计费：N× 成本 + 创建前展示总价，OK？
3. Part B 选题共享：同题多账号是否需要差异化改写？
4. Part B 阶段范围：先做创建期 fanout（4B-1）够不够，还是要含定时（4B-2）？
