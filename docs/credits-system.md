# 积分体系文档

## 1. 体系概览

Anban 智能创作助手采用**钱包模式**管理积分：

- **余额字段**: `users.credits_balance` — 规范余额（原子更新）
- **交易账本**: `credit_transactions` 表 — 追加式审计日志，每笔交易记录 `BalanceAfter` 快照

所有积分操作均在数据库事务中完成，使用 SQL 原子操作（`WHERE credits_balance >= ?`）防止负余额和竞态条件。

## 2. 计费规则

| 场景 | 创建时扣费 | 执行/操作扣费 |
|------|------------|---------------|
| **Web/Plan 云端任务** | 基础任务服务费 + Claude Code 运行预留 | Claude Code 运行真实成本 + MCP/模型/媒体真实用量 |
| **Web 本机任务** | 基础任务服务费 | 不收平台 Claude Code 运行费；平台 MCP/媒体/模型仍按用量 |
| **直接 MCP + 平台模型** | 无 | 按真实用量或明确固定价扣费 |
| **直接 MCP + BYOK** | 无 | 免费 |

- **图片上传和草稿发布**：完全免费
- **BYOK（自带模型）**：不收平台模型操作费；基础任务服务费仍正常收取
- **Managed key（Agent 任务执行）**：任务创建先扣基础服务费和 Claude Code 运行预留；执行完成后按 Claude Code SDK 返回的 `total_cost_usd` 或 token usage 多退少补；执行中产生的图片理解、生图、视频理解、视频生成等 MCP 操作按实际用量写入独立交易
- **写作生成**：已迁移到 Skills，不再作为 MCP 写作生成工具暴露；若 Skills 侧使用平台模型，也必须写入独立模型用量交易
- **最终费用口径**：`任务总费用 = 任务基础服务费 + Claude Code 运行真实成本 + MCP/模型/媒体操作费 - 退款`

## 3. 积分获取

### 3.1 每日签到

- **奖励**: 默认 100 积分/天（可配置 `credits.daily_sign_in`）
- **限制**: 每用户每天限 1 次
- **API**: `POST /api/v1/credits/sign-in`（JWT 认证）

### 3.2 管理员充值

通过 Admin API 为指定用户充值积分，详见 [管理员充值接口](#44-管理员充值接口)。

### 3.3 充值套餐

| 套餐 | 价格 | 积分 | 单价 |
|------|------|------|------|
| 基础 | 10 元 | 10,000 | 1.0 元/千积分 |
| 标准 | 50 元 | 52,000 | 0.96 元/千积分 |
| 专业 | 100 元 | 110,000 | 0.91 元/千积分 |

### 3.4 任务失败/取消退还

- **任务失败**（重试耗尽/卡死）→ 全额退还任务基础服务费
- **用户取消任务** → 未开始执行时退还任务基础服务费和 Claude Code 运行预留；已进入执行的任务等最终执行结果回传后按真实 Claude Code usage 结算运行预留
- **Claude usage 缺失** → 全额退还 Claude Code 运行预留
- **防重**: 多次退款调用是幂等的，自动跳过重复退款

## 4. 积分消费

### 4.1 任务创建（Web 端）

| 任务类型 | 默认基础服务费 | 默认 Claude Code 运行预留 | 说明 |
|---------|---------------|--------------------------|------|
| `article` | 4,000 | 4,000 | 公众号文章 |
| `seednote` | 3,600 | 3,600 | 种草笔记图文 |
| `ecommerce` | 3,000 | 3,000 | 电商素材任务基础费 |
| `video` | 2,000 | 2,000 | 视频任务基础费 |
| `viral_analysis` | 1,200 | 1,200 | 爆文拆解 |

- `task_costs` 只表示基础服务费，不包含 Claude Code runtime 和后续 MCP/模型/媒体操作费
- `agent_runtime_reserve` 表示云端 Claude Code agent run 的预留额度；执行完成后按官方返回的真实成本多退少补
- 本机运行使用用户自己的 Claude Code 环境，不预扣平台 Claude Code 运行费
- 批量创建：基础服务费和运行预留均按数量计入；强目标模式按配置倍率计入基础服务费和运行预留
- 电商出图创建只扣 `task_costs.ecommerce`；所选模块只影响后续图片生成/理解操作用量
- 视频任务创建只扣 `task_costs.video`；调用 `video_gen` MCP 时再按服务端估算和实际参数独立结算
- 失败/取消退款只退相应任务基础服务费；已成功发生的 Claude Code runtime 与独立 MCP 操作交易不自动合并到基础费里

### 4.2 Claude Code Agent Runtime（按官方结果结算）

云端任务完成后，服务端读取 Claude Code/Agent SDK 最终结果中的 `total_cost_usd`、`usage`、`num_turns`、`session_id`、`duration_api_ms` 等字段，并生成独立运行成本账单。

| 交易类型 | 金额方向 | 说明 |
|----------|----------|------|
| `agent_runtime_reserve` | 扣款 | 创建云端任务时预扣运行额度 |
| `agent_runtime` | 扣款或 0 元标记 | 执行完成后的真实 Claude Code 运行成本；若预留已覆盖成本，则记录 0 元审计标记 |
| `agent_runtime_refund` | 退款 | 真实成本低于预留或没有 usage 时退回差额 |

结算规则：

1. 优先使用 SDK 返回的 `total_cost_usd`：

   ```text
   credits = ceil(total_cost_usd x USD_TO_CNY x credits_per_cny x tier_multiplier x user_billing_multiplier)
   ```

2. 如果 `total_cost_usd` 缺失但有 token usage，则按 `model_prices.token_models` 计算，支持 `input_tokens`、`output_tokens`、`cache_read_input_tokens`、`cache_creation_input_tokens`。未配置 `cache_creation_input` 单价时回退到普通 input 单价，并在账单 metadata 保存价格快照。
3. 如果真实成本低于预留，退还差额。
4. 如果真实成本高于预留且余额足够，补扣差额。
5. 如果真实成本高于预留且余额不足，任务设置为 `billing_status=payment_required`，记录 `billing_shortfall_credits`，保留结果但限制发布、下载等交付动作；用户充值或管理员加款后自动尝试补扣并解锁。
6. BYOK/local 自带 Claude 运行不收平台 `agent_runtime`，运行预留会退回；基础任务服务费和平台 MCP 媒体/模型费用不受影响。

### 4.3 MCP / Skills 模型操作（按模型定价）

通过 Claude Code / OpenClaw / Codex 等客户端调用平台能力时，平台模型按实际使用的模型定价。当前 MCP 直接保留媒体、理解、转换、评分等工具；文章写作、选题研究、SEO 优化、大纲生成等生成式写作流程迁移到 Skills，不再作为 MCP 写作生成工具暴露。

| 操作 | 模型 | 积分/次 |
|------|------|---------|
| 图片生成 | volcengine/doubao-seedream | 50/张 |
| 图片生成 | openai/dall-e-3 | 120/张 |
| 图片生成 | gemini/gemini-2.0-flash | 80/张 |
| 文章润色 | glm-5-turbo | 80 |
| 文章润色 | glm-5.1 | 120 |

> 以上旧按次表仅用于理解历史口径。当前生产配置优先使用 `model_prices` 的 token/image/video 用量价格；缺少 usage 的少数兼容路径才回退到明确固定价。`convert_markdown` / `render_template` 当前为本地确定性转换，不扣模型操作费；未来如果接入 LLM 排版，再接入同一 usage 计费函数。写作 Skills 如果调用平台模型，必须按同一套 `model_prices` 公式记录模型操作交易，而不是恢复旧 MCP 写作生成工具。最终扣费由 `billing.credits_per_cny`、等级倍率和用户专属倍率共同计算。

### 4.4 管理员充值接口

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
    ecommerce: 3000
    video: 2000
    viral_analysis: 1200
  agent_runtime_reserve:       # 云端 Claude Code 运行预留，执行完成后按真实成本多退少补
    article: 4000
    seednote: 3600
    ecommerce: 3000
    video: 2000
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
      cache_read_input: 0.19
      cache_creation_input: 0.95
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

> **注意**：GPT Image 2、图片理解、视频理解、Skills 侧平台写作模型等真实用量路径必须返回 usage 才能按真实成本结算；生成成功但没有 required usage 会失败或走明确固定价 fallback。历史账单会保存价格快照，后续调价不会改写旧账单。

## 7. 业务规则

| 规则 | 说明 |
|------|------|
| 基础费不含一切 | Web/Plan 创建任务先扣基础服务费，执行中的 Claude Code runtime、MCP/模型/媒体操作另记独立交易 |
| Runtime 预留 | 云端任务创建预扣 `agent_runtime_reserve`，完成后按 SDK `total_cost_usd` 或 token usage 多退少补 |
| 交付锁定 | 真实 runtime 成本超过预留且余额不足时设置 `payment_required`，充值补扣后解锁交付 |
| MCP 按模型定价 | MCP 工具按 `model_prices` 的真实成本和 usage 扣费，或在兼容路径使用明确固定价 |
| BYOK 模型免费 | 用户自带模型不收平台模型操作费；基础任务服务费照常收取 |
| 图片上传/发布免费 | 图片上传和草稿发布不扣费 |
| 签到限制 | 每用户每天限签 1 次 |
| 负余额防护 | SQL CAS 操作确保余额不会变为负数 |
| 取消/失败退还 | 任务取消或失败自动退还任务基础服务费，幂等防重 |
