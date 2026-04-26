# 积分体系文档

## 1. 体系概览

Anban 创作助手采用**钱包模式**管理积分：

- **余额字段**: `users.credits_balance` — 规范余额（原子更新）
- **交易账本**: `credit_transactions` 表 — 追加式审计日志，每笔交易记录 `BalanceAfter` 快照

所有积分操作均在数据库事务中完成，使用 SQL 原子操作（`WHERE credits_balance >= ?`）防止负余额和竞态条件。

## 2. 积分获取

### 2.1 每日签到

- **奖励**: 默认 1024 积分/天（可配置 `credits.daily_sign_in`）
- **限制**: 每用户每天限 1 次，使用 `FindTodaySignIn` 检查当日记录
- **防重**: 签到检查和余额调整在同一个数据库事务中完成，防止并发签到
- **API**: `POST /api/v1/credits/sign-in`（JWT 认证）

### 2.2 管理员充值

- **方式**: 通过管理后台 API 或扫码联系客服人工充值
- **API**: `POST /api/v1/admin/credits/grant`（`X-Admin-API-Key` 认证）
- **金额**: 正整数，无上限

### 2.3 充值套餐

| 套餐 | 价格 | 积分 | 单价 |
|------|------|------|------|
| 基础 | 10 元 | 10,000 | 1.0 元/千积分 |
| 标准 | 50 元 | 55,000 | 0.91 元/千积分 |
| 专业 | 100 元 | 120,000 | 0.83 元/千积分 |

> 充值流程：前端展示套餐价格 → 用户扫码联系客服 → 客服通过 Admin API 充值

### 2.4 任务失败退还

- **触发条件**: 任务永久失败（超过最大重试次数）、任务卡死（无心跳超过 5 分钟）、任务创建回滚
- **退款金额**: 等于该任务原始扣费金额
- **防重**: `RefundForTask` 先检查是否已有 `task_refund` 记录，防止双重退款
- **幂等**: 多次调用 `RefundForTask` 是安全的，第二次调用会跳过

### 2.5 任务取消退还

- **触发条件**: 用户主动取消 `pending` 或 `running` 状态的任务
- **退款金额**: 等于该任务原始扣费金额
- **幂等**: 与失败退还共享防重逻辑

## 3. 积分消费

### 3.1 任务创建（预扣费）

| 任务类型 | 默认价格 | 说明 |
|---------|---------|------|
| `article` | 4,000 | 公众号文章 |
| `xls` | 3,200 | 小绿书图片帖 |
| `rednote` | 3,200 | 小红书图文 |

- **扣费时机**: 创建任务时预扣（`CreateManual` / `CreateFromPlan`）
- **批量扣费**: 支持一次创建 1-5 个任务，总费用 = 单价 x 数量
- **不足处理**: 返回 HTTP 402（`insufficient_credits`），不创建任何任务
- **回滚**: 若部分任务创建失败，自动退还未创建任务的积分

### 3.2 MCP 操作（实时扣费）

| 操作类型 | 默认价格 | 说明 |
|---------|---------|------|
| `image_gen` | 80 | 图片生成（单张） |
| `image_upload` | 40 | 图片上传（微信 CDN） |
| `article_write` | 400 | 文章写作 |
| `convert` | 160 | Markdown → 微信 HTML 转换 |
| `humanize` | 120 | AI 痕迹去除 |
| `topic_research` | 80 | 选题研究 |
| `seo` | 80 | SEO 优化 |
| `draft_publish` | 40 | 草稿发布 |
| `outline` | 80 | 大纲生成 |

- **扣费时机**: 每次操作前预扣（先扣费后执行）
- **不足处理**: 返回错误，不执行操作
- **批量**: `image_gen` 支持按数量批量扣费（`count` 参数）

## 4. 退款机制

### 4.1 自动退款场景

| 场景 | 触发位置 | 退款类型 | 交易描述 |
|------|---------|---------|---------|
| 任务重试耗尽 | `task_execution.go` | `task_refund` | 任务失败退还 |
| 限流重试耗尽 | `task_execution.go` | `task_refund` | 任务失败退还 |
| 任务卡死（5 分钟无心跳） | `plan_checker.go` | `task_refund` | 任务失败退还 |
| 任务创建失败回滚 | `task.go` | `task_refund` | 任务失败退还 |
| 用户取消任务 | `task.go` | `task_refund` | 任务取消退还 |

### 4.2 防重退款

```sql
-- RefundForTask 查询已有退款记录
SELECT * FROM credit_transactions WHERE task_id = ? AND type = 'task_refund'
-- 存在则跳过，不存在才执行退款
```

### 4.3 无退款场景

- MCP 操作执行失败（已扣费不退还，因为实际消耗了 API 资源）
- 写作服务扣费后 LLM 调用失败（已扣费不退还）

### 4.4 取消费用政策

用户取消任务时，积分退还遵循以下规则：

| 费用类型 | 是否退还 | 原因 |
|---------|---------|------|
| 任务创建费（如 4000） | **全额退还** | 任务未完成，Agent 执行槽位可回收 |
| 操作费（如 image_gen、article_write） | **不退还** | API 调用已实际发生，平台已付出成本 |

> 前端取消确认弹窗会提示此政策，让用户在取消前了解费用影响。

## 5. API 端点

### HTTP API

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| `GET` | `/api/v1/credits/balance` | JWT | 查询余额 |
| `GET` | `/api/v1/credits/sign-in/status` | JWT | 签到状态 |
| `POST` | `/api/v1/credits/sign-in` | JWT | 每日签到 |
| `GET` | `/api/v1/credits/transactions` | JWT | 交易记录（分页） |
| `POST` | `/api/v1/admin/credits/grant` | Admin API Key | 管理员充值 |

### MCP 工具

| 工具名 | 说明 |
|--------|------|
| `get_credit_balance` | 查询当前用户余额 |

> 其他 MCP 工具（图片生成、写作、发布等）内部自动扣费，无需用户显式调用积分相关工具。

## 6. 数据模型

### users 表（余额字段）

```sql
credits_balance INT NOT NULL DEFAULT 0
```

### credit_transactions 表

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | uint | 自增主键 |
| `user_id` | char(36) | 用户 ID（索引） |
| `type` | varchar(20) | 交易类型常量 |
| `amount` | int | 正数=收入，负数=支出 |
| `balance_after` | int | 交易后余额快照 |
| `task_id` | char(36) NULL | 关联任务 ID（索引） |
| `description` | varchar(500) | 交易描述 |
| `created_at` | timestamp | 创建时间 |

**索引**:
- `idx_ct_user_created`: `(user_id, created_at)` — 按用户查询交易记录
- `idx_task_id`: `(task_id)` — 按任务查询扣费/退款记录

## 7. 并发安全

### 原子扣费

```sql
UPDATE users SET credits_balance = credits_balance - ?
WHERE id = ? AND credits_balance >= ?
```

使用 `WHERE credits_balance >= amount` 实现 CAS（Compare-And-Swap），防止并发扣费导致负余额。

### 事务保证

- 所有积分操作在数据库事务中完成
- 余额更新和交易记录创建在同一事务中，保证一致性
- MySQL 默认 REPEATABLE READ 隔离级别，事务内的读操作能看到自身写操作的结果

## 8. 配置

```yaml
credits:
  daily_sign_in: 1024          # 每日签到奖励
  task_costs:
    article: 4000              # 文章任务单价
    xls: 3200                  # 小绿书任务单价
    rednote: 3200              # 小红书任务单价
  operation_costs:
    image_gen: 80              # 图片生成
    image_upload: 40           # 图片上传
    article_write: 400         # 文章写作
    convert: 160               # 格式转换
    humanize: 120              # AI 痕迹去除
    topic_research: 80         # 选题研究
    seo: 80                    # SEO 优化
    draft_publish: 40          # 草稿发布
    outline: 80                # 大纲生成
  admin_api_key: ""            # 管理员充值 API Key
```

所有字段可通过环境变量覆盖：`ANBAN_SERVER_CREDITS_ADMIN_API_KEY`

## 9. 业务规则

| 规则 | 说明 |
|------|------|
| 计划任务也扣费 | `CreateFromPlan` 创建任务时扣费，积分不足跳过该次执行 |
| 签到限制 | 每用户每天限签 1 次（基于本地时区当日判断） |
| 负余额防护 | SQL CAS 操作确保余额不会变为负数 |
| 先扣费后执行 | 所有 MCP 操作先检查并扣除积分，再执行实际操作 |
| 取消退还任务费 | 用户取消任务退还创建费，执行中已消耗的操作费不退还 |
| 失败退还 | 任务失败（重试耗尽/卡死）自动退还积分，防双重退款 |
| 退款描述区分 | 取消退款显示"任务取消退还"，失败退款显示"任务失败退还" |
| 管理员充值 | 需通过 Admin API Key 认证，支持为任意用户充值 |
