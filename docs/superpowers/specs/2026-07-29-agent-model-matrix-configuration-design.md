# Agent 模型矩阵与 Claude Code 配置设计

日期：2026-07-29
状态：设计已确认，待书面规格复核

## 1. 背景

当前三档 Agent 执行方案已经建立了稳定的产品档位、套餐准入、任务快照、固定任务费和独立 Designer 计费，但运行配置仍把每个档位锁死为单一 Provider 和单一模型：

- `cost_effective` 固定为 DeepSeek 单模型。
- `balanced` 固定为豆包单模型。
- `maximum_quality` 固定为 Kimi 单模型和合成的上下文/思考字段。
- Server 和 TypeScript Agent 都校验预期 Provider/Model，生产无法只通过配置切换模型。
- Claude Code 的 `default`、`opus`、`fable`、`sonnet`、`haiku` 五种模型角色没有独立配置。
- 上下文、思考、压缩和子 Agent 模型等 Claude Code 官方参数没有形成有类型、可冻结、可审计的配置。

本设计在保留三档产品语义的前提下，把 Provider 连接、Claude Code 模型矩阵和运行控制参数完全交给服务端部署配置。任务创建时冻结非敏感快照；重试使用冻结快照；账单使用稳定的 Profile ID 和规范化模型身份，不依赖 SKU 名称或运行时模型字符串推断。

## 2. 目标与非目标

### 2.1 目标

1. 保留三个稳定的用户选择：
   - `cost_effective`：性价比，Free 及以上可用。
   - `balanced`：平衡型，Pro 及以上可用。
   - `maximum_quality`：极致效果，Enterprise 可用。
2. 部署方可以为每档自由配置 Provider、五种 Claude Code 模型角色和已批准的 Claude Code 官方环境参数。
3. Server 和 Agent 不再硬编码某档必须使用 DeepSeek、豆包、Kimi 或 GLM。
4. 创建任务时冻结可重现的非敏感模型矩阵；重试不得漂移或自动降级。
5. 使用规范化 Provider/Model 身份完成 Token 成本核算；无法映射的使用量保持 `unreconciled`，不能按零成本处理。
6. 把极致效果任务固定费从平衡型的 `1.5x` 调整为 `3.0x`，覆盖 Kimi K3 等高成本模型的风险。
7. 为 DeepSeek V4、Kimi K3/K2.7 和 GLM-5.2 发布新的不可变 Provider 成本目录。
8. Studio、Miniapp 和三个 TypeScript Agent 运行镜像使用同一份动态能力契约。

### 2.2 非目标

- 不增加第四个用户可选档位。DeepSeek、豆包、Kimi 和 GLM 是档位背后的运行配置，不是新的产品档位。
- 不允许客户端提交 Provider、模型、Endpoint、价格或 Claude 控制参数。
- 不为旧 API、旧单模型配置或旧快照格式保留运行时兼容解析器。
- 不允许任意环境变量注入；只开放本规格列出的 Claude Code 官方变量。
- 不把 Endpoint、Secret 名称或 Secret 值写入任务、执行记录、账单或能力 API。
- 不改变图片生成、图片理解、视频理解或独立 Designer 的 Provider、模型、报价和扣费链路。
- 不引入启动时 Provider 网络探测，也不实现自动回退、自动换模型或熔断后降档。

## 3. 核心决策

### 3.1 固定产品档位，动态运行配置

三档 ID、展示名和最低套餐属于产品策略，由 Server 固定维护：

| Profile ID | 展示名 | 最低套餐 |
| --- | --- | --- |
| `cost_effective` | 性价比 | Free |
| `balanced` | 平衡型 | Pro |
| `maximum_quality` | 极致效果 | Enterprise |

Provider、模型矩阵和 Claude 参数不属于产品身份，由部署配置决定。代码只校验配置结构和可用性，不校验“某档应该是哪家模型”。

### 3.2 Provider 与 Profile 分离

`claude.providers` 只描述连接方式和凭据；`claude.execution_profiles` 只引用 Provider 并描述该档的 Claude Code 行为。多个 Profile 可以复用同一个 Provider，轮换 Token 或 Endpoint 时不需要改任务快照。

### 3.3 快照冻结行为，不冻结凭据

任务快照冻结 Profile ID、Provider ID、协议、五角色模型、Claude 控制参数和用量别名。运行时再通过 Provider ID 获取当前 Endpoint 和 Secret。这样既能保证模型行为不漂移，也能安全轮换凭据。

### 3.4 Profile 和模型成本必须同时可解析

一个 Profile 只有在以下条件全部满足时才可用：

- Profile 配置存在且结构合法。
- 引用的 Provider 存在，协议为 `anthropic`，HTTPS Endpoint 和 Secret 可解析。
- 五个模型角色全部为非空字符串。
- 所有可能出现在终端用量中的模型名称都有规范化别名。
- 每个规范化 Provider/Model 都存在于当前 Provider 成本目录。
- Claude 控制参数通过类型和范围校验。

缺少 Secret、Provider 或成本映射只会把受影响 Profile 标记为不可用，Server 仍可服务其他 Profile。YAML 结构错误、未知字段或显式非法值会使 Server 启动失败。

## 4. 配置契约

### 4.1 完整结构

```yaml
claude:
  providers:
    deepseek:
      protocol: "anthropic"
      base_url: "${ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL:-https://api.deepseek.com/anthropic}"
      auth_token: "${ANBAN_DEEPSEEK_API_KEY}"
    moonshot:
      protocol: "anthropic"
      base_url: "${ANBAN_MOONSHOT_ANTHROPIC_BASE_URL:-https://api.moonshot.cn/anthropic}"
      auth_token: "${ANBAN_MOONSHOT_API_KEY}"
    zhipu:
      protocol: "anthropic"
      base_url: "${ANBAN_ZHIPU_ANTHROPIC_BASE_URL:-https://open.bigmodel.cn/api/anthropic}"
      auth_token: "${ANBAN_ZHIPU_API_KEY}"
    volcengine_ark:
      protocol: "anthropic"
      base_url: "${ANBAN_DOUBAO_AGENT_BASE_URL:-https://ark.cn-beijing.volces.com/api/compatible}"
      auth_token: "${ANBAN_DOUBAO_AGENT_API_KEY}"

  execution_profiles:
    cost_effective:
      provider: "deepseek"
      description: "适合日常创作和批量任务，成本最低"
      models:
        default: "deepseek-v4-flash"
        opus: "deepseek-v4-pro"
        fable: "deepseek-v4-flash"
        sonnet: "deepseek-v4-pro"
        haiku: "deepseek-v4-flash"
      model_usage_aliases:
        deepseek-v4-flash: "deepseek-v4-flash"
        deepseek-v4-pro: "deepseek-v4-pro"
      claude:
        effort_level: "medium"
        max_context_tokens: 1048576
        max_output_tokens: 393216
        auto_compact_window: 1048576

    balanced:
      provider: "zhipu"
      description: "兼顾复杂任务质量、速度和成本"
      models:
        default: "glm-5.2"
        opus: "glm-5.2"
        fable: "glm-5.2"
        sonnet: "glm-5.2"
        haiku: "glm-5.2"
      model_usage_aliases:
        glm-5.2: "glm-5.2"
      claude:
        effort_level: "high"
        max_context_tokens: 1048576
        max_output_tokens: 131072
        auto_compact_window: 1048576

    maximum_quality:
      provider: "moonshot"
      description: "旗舰模型与超长上下文，适合高难度任务"
      models:
        default: "kimi-k3[1m]"
        opus: "kimi-k3[1m]"
        fable: "kimi-k3[1m]"
        sonnet: "kimi-k3[1m]"
        haiku: "kimi-k3[1m]"
      model_usage_aliases:
        kimi-k3: "kimi-k3"
        "kimi-k3[1m]": "kimi-k3"
      claude:
        effort_level: "high"
        always_enable_effort: true
        max_context_tokens: 1048576
        max_output_tokens: 131072
        auto_compact_window: 1048576
        subagent_model: "kimi-k3[1m]"
```

这只是部署示例，不代表三个档位必须使用这些 Provider 或模型。生产配置可以把任一档切换到其他已配置的 Anthropic-compatible Provider，只要模型用量别名和成本目录完整。

### 4.2 Kimi K2.7 示例

Kimi 使用开放平台，而不是 Kimi Code 订阅端点。切换到 K2.7 时只需修改 Profile 配置：

```yaml
maximum_quality:
  provider: "moonshot"
  description: "Kimi K2.7 Code 长思考任务"
  models:
    default: "kimi-k2.7-code"
    opus: "kimi-k2.7-code"
    fable: "kimi-k2.7-code"
    sonnet: "kimi-k2.7-code"
    haiku: "kimi-k2.7-code"
  model_usage_aliases:
    kimi-k2.7-code: "kimi-k2.7-code"
    kimi-k2.7-code-highspeed: "kimi-k2.7-code-highspeed"
  claude:
    disable_adaptive_thinking: true
    max_thinking_tokens: 32768
    max_context_tokens: 262144
    auto_compact_window: 262144
    subagent_model: "kimi-k2.7-code"
```

K2.7 Code 官方要求显式开启思考。若部署方需要高速版，应把实际使用的五角色模型或 `subagent_model` 改为 `kimi-k2.7-code-highspeed`，不能只增加一个未使用的别名。

### 4.3 五角色模型映射

五个字段全部必填，并一对一生成 Claude Code 环境变量：

| 配置字段 | 环境变量 |
| --- | --- |
| `models.default` | `ANTHROPIC_MODEL` |
| `models.opus` | `ANTHROPIC_DEFAULT_OPUS_MODEL` |
| `models.fable` | `ANTHROPIC_DEFAULT_FABLE_MODEL` |
| `models.sonnet` | `ANTHROPIC_DEFAULT_SONNET_MODEL` |
| `models.haiku` | `ANTHROPIC_DEFAULT_HAIKU_MODEL` |

Provider 连接生成：

| Provider 字段 | 环境变量 |
| --- | --- |
| `base_url` | `ANTHROPIC_BASE_URL` |
| `auth_token` | `ANTHROPIC_AUTH_TOKEN` |

不生成 `ANTHROPIC_API_KEY`，避免与 `ANTHROPIC_AUTH_TOKEN` 冲突。冻结 Profile 环境变量的优先级高于进程环境和 Montage 环境，后者不能覆盖 Profile 的 Provider、模型或 Claude 控制参数。

### 4.4 Claude Code 官方控制参数

只支持以下有类型字段：

| 配置字段 | 类型 | Claude Code 环境变量 |
| --- | --- | --- |
| `effort_level` | `low \| medium \| high \| max` | `CLAUDE_CODE_EFFORT_LEVEL` |
| `always_enable_effort` | boolean | `CLAUDE_CODE_ALWAYS_ENABLE_EFFORT` |
| `max_context_tokens` | positive integer | `CLAUDE_CODE_MAX_CONTEXT_TOKENS` |
| `max_output_tokens` | positive integer | `CLAUDE_CODE_MAX_OUTPUT_TOKENS` |
| `max_thinking_tokens` | non-negative integer | `MAX_THINKING_TOKENS` |
| `disable_adaptive_thinking` | boolean | `CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING` |
| `disable_thinking` | boolean | `CLAUDE_CODE_DISABLE_THINKING` |
| `auto_compact_window` | positive integer | `CLAUDE_CODE_AUTO_COMPACT_WINDOW` |
| `autocompact_pct_override` | integer `1..100` | `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` |
| `disable_1m_context` | boolean | `CLAUDE_CODE_DISABLE_1M_CONTEXT` |
| `subagent_model` | non-empty string | `CLAUDE_CODE_SUBAGENT_MODEL` |
| `enable_tool_search` | boolean | `ENABLE_TOOL_SEARCH` |

字段采用可空类型。省略或显式 `null` 都表示“不设置”，不会报错，也不会生成对应环境变量。以下语义必须保留：

- `max_thinking_tokens: 0` 是显式关闭思考，不等于省略。
- `enable_tool_search: false` 必须生成 `ENABLE_TOOL_SEARCH=false`。
- 其他 boolean 的 `false` 同样必须生成字符串 `false`，不能因 Go/TypeScript 零值而丢失。
- 空字符串是非法值，不等于省略。
- `subagent_model` 若设置，必须有用量别名和 Provider 成本映射。
- 不支持 SDK-only 的 `max_turns`、`max_budget_usd`，也不允许 `extra_env` 或任意键值注入。

删除旧的通用 `claude.env` 配置入口。Claude Code 运行参数只能通过 Profile 内的有类型 `claude` 对象配置；进程环境中的同名控制变量不得覆盖任务冻结值。此前位于 `claude.env` 的配置必须迁移到每个需要它的 Profile，未列入本表的旧键直接删除。

### 4.5 旧字段删除

删除旧 Profile 字段及其所有解析、验证、API 和测试：

```text
model_id
context_window
reasoning_effort
thinking_required
base_url        # 从 Profile 移到 Provider
auth_token      # 从 Profile 移到 Provider
```

删除 Server 和 Agent 中用于固定匹配 Provider/Model 的 `EXPECTED_PROFILES` 或等价常量。业务档位只固定 ID、展示名和最低套餐。

## 5. 配置校验与可用性

### 5.1 启动失败条件

以下情况属于配置契约错误，Server 必须启动失败并输出精确字段路径：

- `claude`、`providers`、`execution_profiles` 或嵌套对象出现未知字段。
- 出现三个稳定 ID 之外的 Profile ID。
- Provider `protocol` 不是 `anthropic`。
- Profile 的 `models` 缺少任一必填角色或角色值为空。
- Claude 参数类型错误、显式空字符串、非法枚举或越界。
- `model_usage_aliases` 目标为空、包含非法 Provider 前缀或同一原始模型存在冲突映射。
- Profile 快照 schema 构造后无法通过自身校验。

### 5.2 Profile 不可用条件

以下运行依赖缺失只影响对应 Profile，Server 继续启动：

- 稳定 Profile 块未配置。
- Profile 引用的 Provider 不存在。
- Provider Endpoint 缺失、不是合法 HTTPS URL，或环境变量展开后为空。
- Provider Secret 缺失或环境变量展开后为空。
- 五角色模型或 `subagent_model` 没有完整的用量别名。
- 规范化模型没有当前 Provider 成本目录条目。

能力 API 返回稳定的三个档位，并为不可用项设置 `available: false` 和结构化 `unavailable_reason`。启动阶段不调用 Provider API；网络、鉴权或限流错误只在执行时报告。

### 5.3 精确错误码

服务端错误使用以下稳定代码：

```text
agent_profile_not_found
agent_profile_unavailable
agent_profile_access_denied
agent_profile_snapshot_invalid
agent_profile_snapshot_conflict
agent_provider_unavailable
agent_model_cost_unmapped
billing_profile_sku_not_found
```

不得把上述错误静默转换为其他 Profile，也不得只返回不含错误码的泛化 500。

## 6. 快照、指纹与执行语义

### 6.1 Task 快照 schema

新快照使用明确版本：

```json
{
  "schema_version": 2,
  "profile_id": "maximum_quality",
  "display_name": "极致效果",
  "provider": "moonshot",
  "protocol": "anthropic",
  "models": {
    "default": "kimi-k3[1m]",
    "opus": "kimi-k3[1m]",
    "fable": "kimi-k3[1m]",
    "sonnet": "kimi-k3[1m]",
    "haiku": "kimi-k3[1m]"
  },
  "claude": {
    "effort_level": "high",
    "always_enable_effort": true,
    "max_context_tokens": 1048576,
    "max_output_tokens": 131072,
    "auto_compact_window": 1048576,
    "subagent_model": "kimi-k3[1m]"
  },
  "model_usage_aliases": {
    "kimi-k3": "kimi-k3",
    "kimi-k3[1m]": "kimi-k3"
  }
}
```

快照不得包含 Endpoint、Secret 环境变量名、Secret 引用或 Secret 值。

### 6.2 指纹

Server 对快照执行确定性规范化 JSON 序列化，并计算 SHA-256，保存为小写十六进制 `agent_profile_fingerprint`。对象键排序、空对象表达和 `null` 处理必须跨 Go 与 TypeScript 一致；TypeScript Agent 不自行重算业务配置，只校验 Server 下发的快照和指纹契约。

### 6.3 创建、重试、克隆与计划

- 新任务创建接口强制要求 `execution_profile`，客户端只提交 Profile ID。
- Server 校验套餐、Profile 可用性、SKU 和余额后，在同一创建流程中冻结 Quote、Profile 快照和指纹。
- 重试读取原任务快照和指纹，只从当前 Provider Registry 解析连接信息。当前 Profile 配置变化不能改变重试模型。
- 若冻结 Provider 已删除、协议不再可用或 Secret 缺失，重试返回 `agent_provider_unavailable`，不能回退。
- 克隆保留来源任务的 Profile ID，但使用当前生产配置生成新任务的新快照。克隆不是原任务重试。
- 计划保存 Profile ID；每次计划生成任务时使用当时配置生成新快照。
- 已存在计划迁移为 `balanced`。计划执行期间不可重新选择或覆盖 Profile。
- 任务字段、快照中的 Profile ID 和执行记录不一致时返回 `agent_profile_snapshot_conflict`。

### 6.4 Task Execution 记录

`task_executions` 保存实际启动的冻结配置：

```text
execution_profile
provider
model_matrix JSON
claude_controls JSON
profile_fingerprint
```

删除中间版本的单模型字段：

```text
model_id
protocol
reasoning_effort
context_window
```

协议是 Profile 快照的一部分；执行记录无需再以独立列重复保存。Provider 保留为独立列以支持运维筛选，完整矩阵和控制参数使用 JSON 保存。

## 7. Server 到 TypeScript Agent 的运行契约

Bootstrap 响应包含：

```text
profile_id
provider
protocol
models
claude
model_usage_aliases
profile_fingerprint
runtime_env
```

`runtime_env` 由 Server 在请求时组合：

1. 从任务快照读取模型矩阵和 Claude 控制参数。
2. 用快照中的 Provider ID 查询当前 Provider 连接。
3. 解析 Endpoint 和 Secret。
4. 只生成本规格列出的 Provider、模型和 Claude Code 环境变量。

TypeScript Agent 必须：

- 接受任意合法 Provider ID 和模型字符串，不保留固定档位/模型表。
- 严格校验 Bootstrap schema、指纹格式、HTTPS Endpoint 和 allowlist。
- 以冻结的 `runtime_env` 覆盖进程环境和 Montage 环境中的同名变量。
- 不记录或上报 `ANTHROPIC_AUTH_TOKEN`。
- 将 SDK 终端 `modelUsage` 的每个原始模型名通过冻结别名解析为 `provider/model`。
- 任一用量无法映射时保留可映射项，同时把本次成本状态标记为 `unreconciled` 并上报诊断。
- 三个 Agent 镜像使用相同的 Agent TypeScript 包、Claude Agent SDK 和 Claude Code 版本；禁止某个镜像单独漂移版本。

## 8. 能力 API 与用户界面

### 8.1 能力 API

认证接口保持：

```text
GET /api/v1/agent/execution-profiles
```

单项响应改为动态矩阵契约：

```json
{
  "id": "maximum_quality",
  "display_name": "极致效果",
  "description": "旗舰模型与超长上下文，适合高难度任务",
  "min_tier": "enterprise",
  "available": true,
  "provider": "moonshot",
  "protocol": "anthropic",
  "models": {
    "default": "kimi-k3[1m]",
    "opus": "kimi-k3[1m]",
    "fable": "kimi-k3[1m]",
    "sonnet": "kimi-k3[1m]",
    "haiku": "kimi-k3[1m]"
  },
  "claude": {
    "effort_level": "high",
    "max_context_tokens": 1048576
  }
}
```

不可用项仍返回已配置的非敏感字段和 `unavailable_reason`。API 删除单数 `model_id`；前端不能再从旧 `model_name/model_id` 假设一个档位只有一个模型。API 不返回 Endpoint、Secret 引用、Secret 值或完整 `runtime_env`。

### 8.2 Studio 与 Miniapp

- 创建任务和创建计划仍展示三档选择器。
- 每档突出显示默认模型和 Provider，并可展开查看五种角色；若五种角色相同，可紧凑显示为“全部角色：模型名”。
- 不满足套餐或配置不可用的档位禁用，并显示服务端返回的原因。
- 默认选择当前用户可用档位中固定任务费最低的档位。
- 价格预览只使用 `task_type + execution_profile` 查询 Billing Catalog。
- 表单 payload 只包含 Profile ID，不回传 Provider、模型或 Claude 参数。
- 任务详情展示任务冻结时的 Provider、五角色模型和已设置的 Claude 参数，而不是当前能力目录。
- 克隆对话框默认选中来源 Profile ID，但显示当前能力目录和新价格。
- Avatar Popover 已有钱包模块保持不变，继续使用共享 `['billing', 'wallet']` Query。

### 8.3 Designer 隔离

- Designer 页面不显示三档模型选择器。
- Designer 请求 schema 不增加 `execution_profile`。
- Designer 服务不读取 Agent Provider/Profile Registry。
- Designer 图片 SKU、Provider 成本和独立扣费保持现状。
- 契约测试必须证明 Agent 新配置和新固定任务价不会改变 Designer 报价或扣费。

## 9. Billing 设计

### 9.1 固定任务费

发布不可变目录：

```text
retail-2026-07-29-v6
```

倍率调整为：

| Profile | 相对平衡型倍率 |
| --- | ---: |
| `cost_effective` | `0.8x` |
| `balanced` | `1.0x` |
| `maximum_quality` | `3.0x` |

任务标价：

| 任务类型 | 性价比 | 平衡型 | 极致效果 |
| --- | ---: | ---: | ---: |
| Article | 4,800 | 6,000 | 18,000 |
| Seednote | 4,000 | 5,000 | 15,000 |
| Moments | 2,400 | 3,000 | 9,000 |
| Ecommerce | 2,400 | 3,000 | 9,000 |
| Montage | 1,600 | 2,000 | 6,000 |

再显式应用现有套餐折扣：Free 0%、Pro 10%、Enterprise 20%。极致效果仅 Enterprise 可创建，因此用户实际支付价为：

| 任务类型 | Enterprise 极致效果价 |
| --- | ---: |
| Article | 14,400 |
| Seednote | 12,000 |
| Moments | 7,200 |
| Ecommerce | 7,200 |
| Montage | 4,800 |

所有整数价格直接写入不可变目录，不在 Quote 请求时做浮点乘法。SKU 使用结构化 `execution_profile` 字段匹配，不能解析 SKU ID 或借用 `route`。

### 9.2 Provider 成本目录

发布：

```text
provider-cost-2026-07-29-v4
```

新增或更新以下规范化模型。价格均为每 1,000,000 Token：

| 规范化模型 | 币种 | 缓存命中输入 | 缓存未命中/创建 | 输出 |
| --- | --- | ---: | ---: | ---: |
| `deepseek/deepseek-v4-flash` | USD | 0.0028 | 0.14 | 0.28 |
| `deepseek/deepseek-v4-pro` | USD | 0.003625 | 0.435 | 0.87 |
| `moonshot/kimi-k3` | CNY | 2.00 | 20.00 | 100.00 |
| `moonshot/kimi-k2.7-code` | CNY | 1.30 | 6.50 | 27.00 |
| `moonshot/kimi-k2.7-code-highspeed` | CNY | 2.60 | 13.00 | 54.00 |
| `zhipu/glm-5.2` | CNY | 2.00 | 8.00 | 28.00 |

成本 schema 继续分别保存 `input`、`cache_read_input`、`cache_creation_input` 和 `output`。上表“缓存未命中/创建”写入 `input` 与 `cache_creation_input`；缓存命中写入 `cache_read_input`。

每个条目包含官方证据 URL、价格快照日期和 `effective_at`。本目录不得删除仍可能出现在历史执行或 K2.7 切换配置中的模型。

### 9.3 成本归集

- Profile 的运行模型字符串先通过冻结 `model_usage_aliases` 规范化，再查 Provider 成本目录。
- 同一任务可产生多个模型的终端用量，所有已对账模型成本相加形成实际 Agent 运行成本。
- 缺少别名、成本条目或终端用量时，执行成本状态为 `unreconciled`，不可写入零成本事实。
- 固定任务费与实际 Provider 成本是两条不同账务事实；固定费不会随运行时 Token 动态追扣用户积分。
- 图片及其他 MCP 操作费仍作为任务内明细累计展示，不被三档任务固定费吸收。

## 10. 数据库迁移

由于现有 Profile 迁移尚未进入生产，直接修订：

```text
server/migrations/20260728_agent_execution_profiles.sql
```

最终字段：

```text
tasks.execution_profile
tasks.agent_profile_snapshot
tasks.agent_profile_fingerprint
plans.execution_profile
task_executions.execution_profile
task_executions.provider
task_executions.model_matrix
task_executions.claude_controls
task_executions.profile_fingerprint
billing_skus.execution_profile
billing_quotes.agent_profile_snapshot
```

迁移顺序：

1. 以 nullable 方式增加新列。
2. 历史任务和计划统一标记为 `balanced`。
3. 历史任务使用切换前真实运行配置 `volcengine_ark/doubao-seed-evolving` 生成 schema v2 五角色快照；所有角色均为该模型，Claude 控制对象为空。
4. 以相同规范化算法生成快照指纹。
5. 从任务快照回填已有 `task_executions` 的 Profile、Provider、模型矩阵、Claude 控制和指纹。
6. 增加必要索引并把业务必填列改为 non-null。
7. 删除中间单模型执行列。

迁移 SQL 中的历史快照是旧行为的事实记录，不读取部署时最新 Profile。应用启动前置检查应验证最终列而不是已删除的中间列。

## 11. 受影响范围

主要后端：

```text
server/config/config.go
server/config.example.yaml
server/service/agent_profiles.go
server/service/agent_bootstrap_profile.go
server/model/task.go
server/model/task_execution.go
server/model/billing_wallet.go
server/handler/agent_profiles.go
server/migrations/20260728_agent_execution_profiles.sql
server/billing/products.yaml
server/billing/costs.yaml
```

主要 TypeScript Agent：

```text
agent-ts/src/bootstrap.ts
agent-ts/src/runner.ts
agent-ts/src/types.ts
agent-ts/test/bootstrap.test.ts
agent-ts/test/runner.test.ts
```

主要前端：

```text
studio/src/types/agent-profile.ts
studio/src/types/task.ts
studio/src/lib/schemas.ts
studio/src/lib/api/agent-profiles.ts
studio/src/components/tasks/ExecutionProfileSelector.tsx
studio/src/pages/TaskDetailPage.tsx
miniapp/src/types/agent-profile.ts
miniapp/src/types/task.ts
miniapp/src/pages/tasks/create.vue
miniapp/src/pages/plans/create.vue
```

测试应根据实际依赖扩展到任务创建、克隆、计划、Quote、调度、Docker/Kubernetes Bootstrap 和 Miniapp parity contract。

## 12. 构建与部署

本次运行契约同时影响 Server、Studio/Miniapp 客户端和 TypeScript Agent。生产部署需要重新构建：

```text
Server 镜像
Studio 镜像
Miniapp 构建产物（若独立发布）
creator-agent-article
creator-agent-seednote
creator-agent-montage
```

三个 Agent 镜像都必须重建，因为它们共享更新后的 `agent-ts` Bootstrap 和环境变量契约。只重建 Server 会导致旧 Agent 拒绝新的模型矩阵响应。

部署顺序：

1. 在 Secret 管理中准备 Provider API Key。
2. 在 Server 配置中部署 Provider、Profile 和 Claude 参数。
3. 发布新 Provider 成本目录和固定价格目录。
4. 构建、推送三个不可变 Agent 镜像并记录 digest。
5. 更新 `ANBAN_AGENT_IMAGE_ARTICLE`、`ANBAN_AGENT_IMAGE_SEEDNOTE`、`ANBAN_AGENT_IMAGE_MONTAGE`。
6. 执行一次性数据库迁移。
7. 部署 Server，再部署 Studio/Miniapp。
8. 用三档各创建一个任务进行运行时验收。

配置中的可选 Claude 参数不填写不会阻止启动，也不会产生空环境变量。Provider Secret、被 Profile 引用的模型别名和成本映射属于可用性依赖；缺失时对应档位会明确不可用。

## 13. 测试与验收

### 13.1 Go

覆盖：

- YAML unknown-field、类型、范围和空字符串校验。
- 三个稳定档位和 Free/Pro/Enterprise 权限矩阵。
- 任意合法 Provider/Model 组合，不存在固定 Provider/Model 身份断言。
- 五角色模型到 Claude 环境变量的精确映射。
- 所有可选 Claude 参数的省略、`null`、`false` 和 `0` 语义。
- Secret、Provider、模型别名和成本映射缺失时的 fail-closed 可用性。
- 快照规范化、指纹稳定性和 Secret 隔离。
- 重试不漂移、Provider 删除后失败、克隆重新解析、计划按次冻结。
- Profile/SKU 精确匹配、3.0x 价格矩阵和套餐折扣。
- 新 Provider 成本的 Decimal 计算和多模型成本求和。
- 未映射用量保持 `unreconciled`。
- 数据迁移、历史快照、指纹、非空约束和中间列删除。
- Designer API、SKU、Quote 和扣费隔离。

命令：

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

### 13.2 TypeScript Agent

覆盖：

- 动态 Provider 和五角色矩阵，不依赖预期档位常量。
- Bootstrap schema 和 Profile 指纹校验。
- 严格环境 allowlist 与冻结环境最高优先级。
- 所有可选 Claude 参数的 omission/`null`/`false`/`0` 行为。
- 模型用量别名、多模型终端用量和 `unreconciled` 诊断。
- Token 不进入日志、结果、快照或错误正文。
- 三个 Agent 镜像使用一致的 SDK/Claude Code 版本。

命令以 `agent-ts/package.json` 的实际脚本为准，至少执行：

```bash
cd agent-ts && bun run test
cd agent-ts && bun run typecheck
cd agent-ts && bun run build
cd agent-ts && npm audit --omit=dev
```

### 13.3 Studio

覆盖：

- 三档能力目录、套餐禁用和配置不可用状态。
- Provider、默认模型和五角色模型的动态展示。
- 新价格预览和提交 payload 只含 Profile ID。
- 克隆当前配置语义和任务详情冻结快照展示。
- Avatar 钱包模块不回归。
- Designer 不出现组合档位。

命令：

```bash
cd studio && bun run test
cd studio && bun run build
```

### 13.4 Miniapp

覆盖：

- 创建、克隆和计划的 Profile 传递。
- 能力目录、权限禁用和价格预览。
- 新动态类型与 Studio/API parity contract。
- Designer 隔离。

命令：

```bash
cd miniapp && npm run test
cd miniapp && npm run type-check
```

### 13.5 运行镜像验收

对 `creator-agent-article`、`creator-agent-seednote` 和 `creator-agent-montage` 分别执行：

- 镜像构建。
- Bootstrap 契约 smoke test。
- Profile 环境变量优先级检查。
- Secret 不落盘/不输出检查。
- 使用示例 DeepSeek、GLM 和 Kimi Profile 的最小运行请求。

若本机没有 Docker，代码与静态测试结果和“未执行镜像运行验收”必须分开报告，不能把 Dockerfile 检查表述为镜像已验证。

### 13.6 最终验收条件

只有以下条件全部满足，才允许合并并推送 `main`：

1. 完整 diff review 无阻断问题。
2. 数据库迁移、API 契约、运行配置、账单冻结和 Designer 隔离均有测试。
3. Go、Agent TS、Studio 和 Miniapp 对应命令通过。
4. 三个 Agent 镜像运行验收通过，或明确记录由生产环境补做的唯一剩余部署验证。
5. 新目录 ID 和价格均已核对，历史目录未被修改。
6. `origin/main` 包含最终提交，且工作区中用户无关文件未被改动。

## 14. 官方依据

- Claude Code 设置与环境变量：<https://code.claude.com/docs/en/settings>
- Kimi Claude Code 开放平台配置：<https://platform.kimi.com/docs/guide/claude-code-kimi>
- Kimi K3 价格：<https://platform.kimi.com/docs/pricing/chat-k3>
- Kimi K2.7 Code 价格：<https://platform.kimi.com/docs/pricing/chat-k27-code>
- DeepSeek 模型与价格：<https://api-docs.deepseek.com/quick_start/pricing/>
- GLM-5.2 模型：<https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2>
- GLM Anthropic-compatible API：<https://docs.bigmodel.cn/cn/guide/develop/claude/introduction>

这些资料用于提供可部署示例和 Provider 成本快照。生产模型选择仍由部署方控制；模型价格变化通过新的不可变 Provider 成本目录发布，不覆盖本次目录。
