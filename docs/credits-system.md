# 积分体系文档

## 1. 体系概览

Anban 智能创作助手采用**钱包模式**管理积分：

- **余额字段**: `users.credits_balance` — 规范余额（原子更新）
- **交易账本**: `credit_transactions` 表 — 追加式审计日志，每笔交易记录 `BalanceAfter` 快照

所有积分操作均在数据库事务中完成，使用 SQL 原子操作（`WHERE credits_balance >= ?`）防止负余额和竞态条件。

## 2. 计费规则

| 场景 | 调度费（任务费） | 模型调用费 |
|------|----------------|-----------|
| **Web/Plan 创建任务** | 基础服务费/模块套餐/视频动态预估 | 按 `model_prices` + `billing` 的真实用量另计 |
| **直接 MCP + 平台模型** | 无 | 按真实用量或明确固定价扣费 |
| **直接 MCP + BYOK** | 无 | 免费 |

- **图片上传和草稿发布**：完全免费
- **BYOK（自带模型）**：所有操作免费
- **Managed key（Agent 任务执行）**：任务创建先扣基础服务费；执行中产生的写作、图片理解、生图、视频理解、视频生成等 MCP 操作按实际用量写入独立交易
- **最终费用口径**：`任务总费用 = 任务基础服务费 + MCP/模型/媒体操作费 - 退款`

## 3. 积分获取

### 3.1 每日签到

- **奖励**: 默认 100 积分/天（可配置 `credits.daily_sign_in`）
- **限制**: 每用户每天限 1 次
- **API**: `POST /api/v1/credits/sign-in`（JWT 认证）

### 3.2 管理员充值

通过 Admin API 为指定用户充值积分，详见 [管理员充值接口](#34-管理员充值接口)。

### 3.3 充值套餐

| 套餐 | 价格 | 积分 | 单价 |
|------|------|------|------|
| 基础 | 10 元 | 10,000 | 1.0 元/千积分 |
| 标准 | 50 元 | 52,000 | 0.96 元/千积分 |
| 专业 | 100 元 | 110,000 | 0.91 元/千积分 |

### 3.4 任务失败/取消退还

- **任务失败**（重试耗尽/卡死）→ 全额退还任务基础服务费
- **用户取消任务** → 全额退还任务基础服务费
- **防重**: 多次退款调用是幂等的，自动跳过重复退款

## 4. 积分消费

### 4.1 任务创建（Web 端）

| 任务类型 | 默认基础服务费 | 说明 |
|---------|---------|------|
| `article` | 4,000 | 公众号文章 |
| `seednote` | 3,600 | 种草笔记图文 |
| `viral_analysis` | 1,200 | 爆文拆解 |

- `task_costs` 只表示基础服务费，不包含后续 MCP/模型/媒体操作费
- 批量创建：基础服务费 = 单价 x 数量；强目标模式按配置倍率计入基础服务费
- 电商出图走模块套餐费：`Σ（模块单价 x 数量）`
- 视频生成走服务端动态预估并按视频生成配置结算
- 失败/取消退款只退相应任务基础服务费；已成功发生的独立 MCP 操作交易不自动合并到基础费里

### 4.2 MCP 直接调用（按模型定价）

通过 Claude Code / OpenClaw 等 MCP 客户端调用工具时，平台模型按实际使用的模型定价：

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

> 以上旧按次表仅用于理解历史口径。当前生产配置优先使用 `model_prices` 的 token/image/video 用量价格；缺少 usage 的少数兼容路径才回退到明确固定价。最终扣费由 `billing.credits_per_cny`、等级倍率和用户专属倍率共同计算。

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
  daily_sign_in: 100           # 每日签到奖励
  register_bonus: 1000         # 注册奖励
  invite_reward: 1000          # 邀请奖励
  task_costs:                  # 任务基础服务费，不含 MCP/模型/媒体操作费
    article: 4000
    seednote: 3600
    viral_analysis: 1200
  admin_api_key: ""            # 管理员充值 API Key

recharge_tiers:
  - key: basic
    label: 基础包
    price_cny: 10
    credits: 10000
    bonus_credits: 0
    enabled: true
  - key: standard
    label: 标准包
    price_cny: 50
    credits: 52000
    bonus_credits: 2000
    enabled: true
  - key: pro
    label: 进阶包
    price_cny: 100
    credits: 110000
    bonus_credits: 10000
    enabled: true

model_prices:
  currency_rates:
    USD: {to_cny: 7.20}
    CNY: {to_cny: 1.0}
  token_models:
    moonshot/kimi-k2.7-code:
      currency: USD
      unit: 1000000
      cached_input: 0.19
      input: 0.95
      output: 4.00
  image_generation:
    volcengine_ark/doubao-seedream-5-0-260128:
      pricing_type: per_image
      currency: CNY
      unit: image
      price: 0.22
    wangcai_openai/gpt-image-2:
      pricing_type: openai_image_usage
      currency: USD
      unit: 1000000
      require_usage: true
      text_input: 5.00
      text_cached_input: 1.25
      image_input: 8.00
      image_cached_input: 2.00
      image_output: 30.00
      estimate_table:
        "1024x1024": {low: 0.006, medium: 0.053, high: 0.211}

billing:
  credits_per_cny: 1600
  tier_multipliers:
    free: 1.35
    pro: 1.20
    enterprise: 1.05
  default_user_multiplier: 1.00
  minimum_charge_credits: 1
```

> **注意**：GPT Image 2、图片理解、视频理解、写作非流式工具等真实用量路径必须返回 usage 才能按真实成本结算；生成成功但没有 required usage 会失败或走明确固定价 fallback。历史账单会保存价格快照，后续调价不会改写旧账单。

## 7. 业务规则

| 规则 | 说明 |
|------|------|
| 基础费不含一切 | Web/Plan 创建任务先扣基础服务费，执行中的 MCP/模型/媒体操作另记独立交易 |
| MCP 按模型定价 | MCP 工具按 `model_prices` 的真实成本和 usage 扣费，或在兼容路径使用明确固定价 |
| BYOK 全免 | 用户自带模型（BYOK）所有操作免费 |
| 图片上传/发布免费 | 图片上传和草稿发布不扣费 |
| 签到限制 | 每用户每天限签 1 次 |
| 负余额防护 | SQL CAS 操作确保余额不会变为负数 |
| 取消/失败退还 | 任务取消或失败自动退还任务基础服务费，幂等防重 |
