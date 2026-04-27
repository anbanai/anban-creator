# 积分体系文档

## 1. 体系概览

Anban 智能创作助手采用**钱包模式**管理积分：

- **余额字段**: `users.credits_balance` — 规范余额（原子更新）
- **交易账本**: `credit_transactions` 表 — 追加式审计日志，每笔交易记录 `BalanceAfter` 快照

所有积分操作均在数据库事务中完成，使用 SQL 原子操作（`WHERE credits_balance >= ?`）防止负余额和竞态条件。

## 2. 计费规则

| 场景 | 调度费（任务费） | 模型调用费 |
|------|----------------|-----------|
| **Web/Plan 创建任务** | 3,200/4,000（固定，含一切） | 包含在内，不额外扣费 |
| **直接 MCP + 平台模型** | 无 | 按模型定价扣费 |
| **直接 MCP + BYOK** | 无 | 免费 |

- **图片上传和草稿发布**：完全免费
- **BYOK（自带模型）**：所有操作免费
- **Managed key（Agent 任务执行）**：所有操作免费（已含在任务费中）

## 3. 积分获取

### 3.1 每日签到

- **奖励**: 默认 1024 积分/天（可配置 `credits.daily_sign_in`）
- **限制**: 每用户每天限 1 次
- **API**: `POST /api/v1/credits/sign-in`（JWT 认证）

### 3.2 管理员充值

通过 Admin API 为指定用户充值积分，详见 [管理员充值接口](#34-管理员充值接口)。

### 3.3 充值套餐

| 套餐 | 价格 | 积分 | 单价 |
|------|------|------|------|
| 基础 | 10 元 | 10,000 | 1.0 元/千积分 |
| 标准 | 50 元 | 55,000 | 0.91 元/千积分 |
| 专业 | 100 元 | 120,000 | 0.83 元/千积分 |

### 3.4 任务失败/取消退还

- **任务失败**（重试耗尽/卡死）→ 全额退还任务费
- **用户取消任务** → 全额退还任务费
- **防重**: 多次退款调用是幂等的，自动跳过重复退款

## 4. 积分消费

### 4.1 任务创建（Web 端）

| 任务类型 | 默认价格 | 说明 |
|---------|---------|------|
| `article` | 4,000 | 公众号文章 |
| `xls` | 3,200 | 小绿书图片帖 |
| `rednote` | 3,200 | 小红书图文 |

- 任务费包含该任务的所有操作（写作、生图、转换、发布等），不再额外扣费
- 批量创建：总价 = 单价 x 数量

### 4.2 MCP 直接调用（按模型定价）

直接通过 Claude Code / OpenClaw 等 MCP 客户端调用工具时，按实际使用的模型定价：

| 操作 | 模型 | 积分/次 |
|------|------|---------|
| 图片生成 | volcengine/doubao-seedream | 50/张 |
| 图片生成 | openai/dall-e-3 | 120/张 |
| 图片生成 | gemini/gemini-2.0-flash | 80/张 |
| 文章写作 | glm-5-turbo | 300 |
| 文章写作 | glm-5.1 | 400 |
| 格式转换 | glm-5-turbo | 100 |
| 格式转换 | glm-5.1 | 160 |
| 文章润色 | glm-5-turbo | 80 |
| 文章润色 | glm-5.1 | 120 |
| 选题研究 | glm-5-turbo | 60 |
| 选题研究 | glm-5.1 | 80 |
| SEO 优化 | glm-5-turbo | 60 |
| SEO 优化 | glm-5.1 | 80 |
| 大纲生成 | glm-5-turbo | 60 |
| 大纲生成 | glm-5.1 | 80 |

> 以上为默认定价，全部通过 `config.yaml` 的 `credits.model_costs` 配置，可随时调整。

### 4.3 管理员充值接口

管理员可以通过 API 直接给用户充值积分。

**配置**：在 `config.yaml` 中设置 `credits.admin_api_key`：

```yaml
credits:
  admin_api_key: "your-random-api-key-here"  # 生成: openssl rand -hex 32
```

**请求示例**：

```bash
# 给用户充值 10000 积分
curl -X POST https://your-domain.com/api/v1/admin/credits/grant \
  -H "X-Admin-API-Key: your-random-api-key-here" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "目标用户ID",
    "amount": 10000,
    "description": "充值100元"
  }'
```

**请求参数**：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user_id` | string | 是 | 目标用户 ID |
| `amount` | int | 是 | 充值积分数量（正整数） |
| `description` | string | 否 | 充值说明，默认"管理员充值" |

**响应**：

```json
{"code": 0, "data": {"granted": true}}
```

**错误响应**：

| 状态码 | 说明 |
|--------|------|
| 401 | Admin API Key 未配置或传入了错误的 Key |
| 400 | 缺少 user_id 或 amount 不合法 |

**典型场景**：
1. 用户扫码付款后，调用此接口自动到账
2. 活动奖励、补偿积分等批量操作
3. 接入客服系统，客服确认收款后一键充值

## 5. API 端点

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| `GET` | `/api/v1/credits/balance` | JWT | 查询余额 |
| `GET` | `/api/v1/credits/pricing` | JWT | 查询定价信息 |
| `GET` | `/api/v1/credits/sign-in/status` | JWT | 签到状态 |
| `POST` | `/api/v1/credits/sign-in` | JWT | 每日签到 |
| `GET` | `/api/v1/credits/transactions` | JWT | 交易记录（分页） |
| `POST` | `/api/v1/admin/credits/grant` | Admin API Key | 管理员充值 |

## 6. 配置

```yaml
credits:
  daily_sign_in: 1024          # 每日签到奖励
  register_bonus: 4096         # 注册奖励
  invite_reward: 2048          # 邀请奖励
  task_costs:                  # Web 端任务创建费用
    article: 4000
    xls: 3200
    rednote: 3200
  model_costs:                 # MCP 直接调用的按模型定价
    image_gen:
      "volcengine/doubao-seedream-5-0-260128": 50
      "openai/dall-e-3": 120
      "gemini/gemini-2.0-flash": 80
    article_write:
      "glm-5-turbo": 300
      "glm-5.1": 400
    convert:
      "glm-5-turbo": 100
      "glm-5.1": 160
    humanize:
      "glm-5-turbo": 80
      "glm-5.1": 120
    topic_research:
      "glm-5-turbo": 60
      "glm-5.1": 80
    seo:
      "glm-5-turbo": 60
      "glm-5.1": 80
    outline:
      "glm-5-turbo": 60
      "glm-5.1": 80
  admin_api_key: ""            # 管理员充值 API Key
```

> **注意**：`model_costs` 的 key（如 `"glm-5.1"`）必须与 `writing.model` 或用户 BYOK 配置的模型名完全匹配，否则该模型将按免费处理。

## 7. 业务规则

| 规则 | 说明 |
|------|------|
| 任务费包含一切 | Web/Plan 创建任务只扣一次任务费，执行中不再扣费 |
| MCP 按模型定价 | 直接调用 MCP 工具时按实际使用的模型定价扣费 |
| BYOK 全免 | 用户自带模型（BYOK）所有操作免费 |
| 图片上传/发布免费 | 图片上传和草稿发布不扣费 |
| 签到限制 | 每用户每天限签 1 次 |
| 负余额防护 | SQL CAS 操作确保余额不会变为负数 |
| 取消/失败退还 | 任务取消或失败自动退还任务费，幂等防重 |
