# Agent Profile 统一环境变量配置设计

日期：2026-07-30
状态：方案 A 已确认，待书面规格复核

## 1. 背景

当前 `claude.execution_profiles` 同时使用 `models`、`claude`、Provider 连接配置和运行时转换代码表达 Claude Code 环境。相同事实分散在 YAML、Go 类型、快照、Bootstrap 以及 Go/TypeScript Agent 校验器中，新增或调整 Claude 官方环境变量时需要修改多层映射。

本设计将三个产品档位正式改名为 `effective`、`balanced`、`quality`，并让每个 Profile 的 `envs` 成为 Claude Code 环境变量的唯一配置来源。旧 ID 和旧配置结构直接删除，不保留运行时兼容分支。

## 2. 目标与非目标

### 2.1 目标

1. Profile ID 固定为 `effective`、`balanced`、`quality`。
2. 每个 Profile 使用完整 `envs` 配置 Provider 连接、五角色模型和 Claude Code 控制参数。
3. 删除 `claude.providers`、Profile `models` 和 Profile `claude`。
4. 使用严格白名单、值域校验和敏感信息隔离，禁止任意环境变量注入。
5. 任务冻结除凭据外的完整 Profile 环境；重试不因当前配置变化而切换模型、Endpoint 或推理参数。
6. 正式迁移任务、计划和执行记录中的旧 Profile ID，并发布只使用新 ID 的不可变账单目录。

### 2.2 非目标

- 不改变三档的展示名、套餐权限或价格策略。
- 不改变图片生成、图片理解、视频理解和独立 Designer 的 Provider 或计费链路。
- 不允许客户端提交环境变量、Provider、模型、Endpoint 或价格。
- 不把 API Key、Token 或 Secret 引用写入任务、执行记录、账单或能力 API。
- 不兼容 `cost_effective`、`maximum_quality`、`models`、`claude` 或 `claude.providers`。

## 3. 配置契约

### 3.1 完整结构

```yaml
claude:
  execution_profiles:
    effective:
      description: "适合日常创作和批量任务，成本最低"
      provider: "deepseek"
      envs:
        ANTHROPIC_BASE_URL: "${ANBAN_DEEPSEEK_ANTHROPIC_BASE_URL:-https://api.deepseek.com/anthropic}"
        ANTHROPIC_AUTH_TOKEN: "${ANBAN_DEEPSEEK_API_KEY}"
        ANTHROPIC_MODEL: "deepseek-v4-flash"
        ANTHROPIC_DEFAULT_OPUS_MODEL: "deepseek-v4-pro"
        ANTHROPIC_DEFAULT_FABLE_MODEL: "deepseek-v4-flash"
        ANTHROPIC_DEFAULT_SONNET_MODEL: "deepseek-v4-pro"
        ANTHROPIC_DEFAULT_HAIKU_MODEL: "deepseek-v4-flash"
        CLAUDE_CODE_EFFORT_LEVEL: "medium"
        CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "false"
        CLAUDE_CODE_MAX_CONTEXT_TOKENS: "1048576"
        CLAUDE_CODE_MAX_OUTPUT_TOKENS: "393216"
        MAX_THINKING_TOKENS: "0"
        CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING: "false"
        CLAUDE_CODE_DISABLE_THINKING: "false"
        CLAUDE_CODE_AUTO_COMPACT_WINDOW: "1048576"
        CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "80"
        CLAUDE_CODE_DISABLE_1M_CONTEXT: "false"
        CLAUDE_CODE_SUBAGENT_MODEL: "deepseek-v4-flash"
        ENABLE_TOOL_SEARCH: "true"
      model_usage_aliases:
        deepseek-v4-flash: "deepseek-v4-flash"
        deepseek-v4-pro: "deepseek-v4-pro"

    balanced:
      description: "兼顾复杂任务质量、速度和成本"
      provider: "zhipu"
      envs:
        ANTHROPIC_BASE_URL: "${ANBAN_ZHIPU_ANTHROPIC_BASE_URL:-https://open.bigmodel.cn/api/anthropic}"
        ANTHROPIC_AUTH_TOKEN: "${ANBAN_ZHIPU_API_KEY}"
        ANTHROPIC_MODEL: "glm-5.2"
        ANTHROPIC_DEFAULT_OPUS_MODEL: "glm-5.2"
        ANTHROPIC_DEFAULT_FABLE_MODEL: "glm-5.2"
        ANTHROPIC_DEFAULT_SONNET_MODEL: "glm-5.2"
        ANTHROPIC_DEFAULT_HAIKU_MODEL: "glm-5.2"
        CLAUDE_CODE_EFFORT_LEVEL: "high"
        CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "false"
        CLAUDE_CODE_MAX_CONTEXT_TOKENS: "1048576"
        CLAUDE_CODE_MAX_OUTPUT_TOKENS: "131072"
        MAX_THINKING_TOKENS: "0"
        CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING: "false"
        CLAUDE_CODE_DISABLE_THINKING: "false"
        CLAUDE_CODE_AUTO_COMPACT_WINDOW: "1048576"
        CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "80"
        CLAUDE_CODE_DISABLE_1M_CONTEXT: "false"
        CLAUDE_CODE_SUBAGENT_MODEL: "glm-5.2"
        ENABLE_TOOL_SEARCH: "true"
      model_usage_aliases:
        glm-5.2: "glm-5.2"

    quality:
      description: "旗舰模型与超长上下文，适合高难度任务"
      provider: "moonshot"
      envs:
        ANTHROPIC_BASE_URL: "${ANBAN_MOONSHOT_ANTHROPIC_BASE_URL:-https://api.moonshot.cn/anthropic}"
        ANTHROPIC_AUTH_TOKEN: "${ANBAN_MOONSHOT_API_KEY}"
        ANTHROPIC_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_OPUS_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_FABLE_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_SONNET_MODEL: "kimi-k3[1m]"
        ANTHROPIC_DEFAULT_HAIKU_MODEL: "kimi-k3[1m]"
        CLAUDE_CODE_EFFORT_LEVEL: "high"
        CLAUDE_CODE_ALWAYS_ENABLE_EFFORT: "true"
        CLAUDE_CODE_MAX_CONTEXT_TOKENS: "1048576"
        CLAUDE_CODE_MAX_OUTPUT_TOKENS: "131072"
        MAX_THINKING_TOKENS: "0"
        CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING: "false"
        CLAUDE_CODE_DISABLE_THINKING: "false"
        CLAUDE_CODE_AUTO_COMPACT_WINDOW: "1048576"
        CLAUDE_AUTOCOMPACT_PCT_OVERRIDE: "80"
        CLAUDE_CODE_DISABLE_1M_CONTEXT: "false"
        CLAUDE_CODE_SUBAGENT_MODEL: "kimi-k3[1m]"
        ENABLE_TOOL_SEARCH: "true"
      model_usage_aliases:
        kimi-k3: "kimi-k3"
        "kimi-k3[1m]": "kimi-k3"
```

示例值用于说明完整结构，生产模型和参数由部署方控制。所有值使用字符串，确保 YAML、快照、Bootstrap 和进程环境具有相同语义。布尔值必须写为 `"true"` 或 `"false"`，整数必须使用十进制字符串。

### 3.2 环境变量白名单

只允许以下 19 个变量：

| 类别 | 环境变量 |
| --- | --- |
| Provider | `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN` |
| 模型 | `ANTHROPIC_MODEL`、`ANTHROPIC_DEFAULT_OPUS_MODEL`、`ANTHROPIC_DEFAULT_FABLE_MODEL`、`ANTHROPIC_DEFAULT_SONNET_MODEL`、`ANTHROPIC_DEFAULT_HAIKU_MODEL` |
| 推理 | `CLAUDE_CODE_EFFORT_LEVEL`、`CLAUDE_CODE_ALWAYS_ENABLE_EFFORT`、`MAX_THINKING_TOKENS`、`CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING`、`CLAUDE_CODE_DISABLE_THINKING` |
| Token/上下文 | `CLAUDE_CODE_MAX_CONTEXT_TOKENS`、`CLAUDE_CODE_MAX_OUTPUT_TOKENS`、`CLAUDE_CODE_AUTO_COMPACT_WINDOW`、`CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`、`CLAUDE_CODE_DISABLE_1M_CONTEXT` |
| Agent/工具 | `CLAUDE_CODE_SUBAGENT_MODEL`、`ENABLE_TOOL_SEARCH` |

未知键、空键、包含 NUL/换行的值、单值超过 16 KiB 或总量超过 32 KiB均使 Server 启动失败。五个模型变量、Base URL 和 Auth Token 必填；其余控制变量可以省略，省略表示不向 Claude Code 注入且不会报错。

值域规则沿用 Claude Code 官方参数语义：

- `CLAUDE_CODE_EFFORT_LEVEL`：`low`、`medium`、`high`、`max`。
- 五个 boolean：只接受 `true` 或 `false`。
- Context、Output、Auto Compact：正整数。
- `MAX_THINKING_TOKENS`：非负整数，`0` 是显式值。
- `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`：`1..100`。
- Base URL：HTTPS、无凭据、无查询和 Fragment。
- Subagent 模型和五角色模型必须存在于 `model_usage_aliases`，其规范化 Provider/Model 必须存在于成本目录。

## 4. 运行时与安全

### 4.1 配置解析

Server 使用严格 YAML 解码。`claude` 下只接受 `execution_profiles` 和现有执行器基础设施字段；Profile 只接受 `description`、`provider`、`envs`、`model_usage_aliases`。删除 Provider registry 和 typed Claude controls 的解析、默认值及转换代码。

`provider` 是稳定的计费身份。连接地址、Token 和模型全部来自同一个 Profile 的 `envs`，不会再与另一个 Provider 配置合并。

### 4.2 快照 schema v3

任务创建时生成 schema v3 快照：

```json
{
  "schema_version": 3,
  "profile_id": "effective",
  "display_name": "性价比",
  "provider": "deepseek",
  "protocol": "anthropic",
  "envs": {
    "ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic",
    "ANTHROPIC_MODEL": "deepseek-v4-flash"
  },
  "model_usage_aliases": {
    "deepseek-v4-flash": "deepseek-v4-flash"
  }
}
```

快照包含除 `ANTHROPIC_AUTH_TOKEN` 外的所有已配置白名单变量。Fingerprint 对排序后的脱敏 `envs` 和别名计算，不能包含 Token。

Bootstrap 处理任务时：

1. 校验 schema v3 快照和 Fingerprint。
2. 从当前同 ID Profile 读取 `ANTHROPIC_AUTH_TOKEN`。
3. 要求当前 Profile 的 `provider` 与冻结 Provider 相同。
4. 将当前 Token 与冻结 `envs` 合并。
5. 运行 Go 或 TypeScript Agent 前再次执行同一白名单和值域校验。

Token 缺失时 Profile 在能力目录显示不可用，任务创建和执行均 fail closed。Token 可以轮换；模型、Endpoint 和控制参数不能在重试时漂移。Bootstrap API、日志、任务快照和数据库均不得返回 Token。

## 5. 产品、API 与计费

Server 固定产品元数据：

| Profile ID | 展示名 | 最低套餐 |
| --- | --- | --- |
| `effective` | 性价比 | Free |
| `balanced` | 平衡型 | Pro |
| `quality` | 极致效果 | Enterprise |

能力 API、Quote、任务、计划、克隆、重试、Studio 和 Miniapp 只接受新 ID。旧 ID 返回结构化的 `invalid_agent_execution_profile`，不做映射或回退。

发布新不可变账单目录 `retail-2026-07-30-v7`，其中 Agent SKU 的 `execution_profile` 使用新 ID；SKU 字符串本身不用于解析 Profile。历史 `billing_skus`、已消费 Quote、Charge 和 Wallet Entry 不修改，继续保留当时的目录与 Profile 事实，但不再参与新任务准入。维护窗口开始后使旧目录尚未消费的 Quote 失效，恢复流量后只允许新目录生成 Quote。

固定任务价格和 Provider Token 成本不因本次结构调整改变。终端使用量从冻结 `envs` 的模型变量和 `model_usage_aliases` 归一化；未知模型保持 `unreconciled`。

Designer API、图片 SKU 和图片 Provider 不读取 `claude.execution_profiles`。

## 6. 数据库迁移

MySQL DDL 会隐式提交，因此本次切换不能伪装成一个跨 DDL 和数据回填的大事务。新增：

- `server/migrations/20260730_agent_profile_envs_expand.sql`
- `server/migrations/20260730_agent_profile_envs_expire_quotes.sql`
- `server/migrations/20260730_agent_profile_envs_contract.sql`
- 使用生产 Server 相同 `AgentProfileFingerprint` 实现的一次性 Go backfill 命令

迁移分四阶段执行：

1. **预检**：在任务、计划和执行表中只允许 `cost_effective`、`balanced`、`maximum_quality` 三种现有 Agent Profile 值；记录各表行数、空值、未知值和快照 schema 分布，任一异常立即中止。历史账单表单独统计但不作为旧 ID 清零对象。
2. **Expand DDL**：给 `task_executions` 增加可空 `profile_envs` JSON；保留旧 `model_matrix` 和 `claude_controls`，不提前破坏当前数据。
3. **Go 批量回填**：使用短事务和主键游标执行以下转换，每批可安全重试：
   - `tasks.execution_profile`、`plans.execution_profile` 和 `task_executions.execution_profile` 中 `cost_effective` -> `effective`、`maximum_quality` -> `quality`。
   - `tasks.agent_profile_snapshot` 从 schema v2 转换为 schema v3；从旧 `models` 和 `claude` 生成字符串 `envs`，更新 `profile_id`，删除旧字段和敏感值。若历史 Provider 与当前同档 Provider 不同，运维必须通过可重复的 `--legacy-provider-base-url provider=https://endpoint` 参数提供当时的非敏感 Endpoint；迁移禁止猜测或改写历史 Provider/模型。
   - 使用应用层规范化函数重新计算 `tasks.agent_profile_fingerprint`，不得使用与 Go 序列化顺序可能不一致的 SQL JSON 哈希。
   - 从任务的 schema v3 快照回填 `task_executions.profile_envs` 和 `profile_fingerprint`，并保持 Provider 与关联任务一致。
4. **验证与 Contract DDL**：Go backfill 使用 verify-only 模式逐行重算并核对 Fingerprint；SQL 在任务、计划和执行表断言旧 ID、schema v2、空 `profile_envs` 和快照内 `ANTHROPIC_AUTH_TOKEN` 数量均为零；随后把 `profile_envs` 设为非空，删除 `task_executions.model_matrix` 与 `task_executions.claude_controls`，重建只允许三个新 ID 的约束和索引。

Go backfill 在写入前后都验证快照和 Fingerprint，已是 schema v3 且结果一致的行直接跳过，因此可以从中断处继续。Expand DDL 和 Contract DDL 分别提供独立的前置/后置断言；由于不保留旧应用兼容，Contract DDL 完成后的回滚方式是恢复维护窗口备份，而不是重新启用旧 ID。

## 7. 上线顺序

本次是无兼容切换，必须在维护窗口发布：

1. 备份数据库并记录旧三档、任务、计划、执行、SKU、Quote 和快照数量。
2. 暂停新任务创建、计划调度、重试和 Agent 消费。
3. 确认没有 `starting`、`running` 或待结算执行；未完成任务先排空或明确取消。
4. 部署包含新配置解析器和迁移代码的 Server 镜像，但保持流量关闭。
5. 依次执行 Expand DDL、Go backfill、verify-only、旧 Quote 失效 SQL、结构断言和 Contract DDL；旧 Quote SQL 只把尚未消费且仍有效的旧目录 Quote 的 `expires_at` 收敛到当前时间，不修改价格、目录或快照。
6. 发布并导入 `retail-2026-07-30-v7`，挂载只含新 ID 和 `envs` 的生产配置与 Secret，启动 Server 并检查三档能力 API。
7. 更新并部署 Studio、Miniapp、Go Agent 和 TypeScript Agent 镜像。
8. 用三个套餐账号分别验证档位权限、Quote、创建、执行、重试、账单明细和累计扣费。
9. 恢复计划调度、Agent 消费和创建流量。

若任一新 Profile 不可用、迁移断言失败或能力 API 返回旧 ID，保持流量关闭并从数据库备份回滚；禁止临时启用旧 ID 兼容。

## 8. 测试与验收

### 8.1 Go

- 严格 YAML schema：拒绝 `providers`、`models`、`claude` 和未知 env。
- 三个新 ID 的权限矩阵；三个旧 ID 全部拒绝。
- env 必填项、可选项、类型、范围、大小和 URL 校验。
- schema v3 快照脱敏、稳定 Fingerprint 和深拷贝。
- Bootstrap 只从当前 Profile 注入 Token，其余值使用冻结快照。
- Token 轮换成功；Provider、模型、Endpoint 和控制参数漂移不影响重试。
- 所有模型和 Subagent 模型都有别名及成本映射。
- Expand/Contract SQL、可重试 Go backfill、约束、JSON 转换、旧 Quote 失效和零敏感值断言。
- Designer 隔离。

### 8.2 TypeScript Agent

- 只接受 schema v3 和三个新 ID。
- 拒绝旧 `models`、`claude`、旧 ID、未知 env 和缺失 Token。
- 将完整冻结环境原样传给 Claude Code SDK 进程。
- 终端 usage 仍按别名归一化并在异常时标记 `unreconciled`。

### 8.3 Studio 与 Miniapp

- 能力目录展示新 ID 对应的三档并按套餐禁用。
- 创建、克隆、计划和价格预览只提交新 ID。
- 任务明细展示冻结配置的非敏感摘要，不展示 Token。
- Designer 页面不出现 Agent Profile。

### 8.4 发布门禁

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd agent-ts && bun run test && bun run typecheck && bun run build
cd studio && bun run test && bun run build
cd miniapp && npm run test && npm run type-check
```

Docker 可用时必须构建并运行 Server、Article、Seednote、Montage 四个受影响镜像的 smoke test。生产上线前必须在数据库副本执行迁移和回滚演练。

## 9. 明确决策

- 正式 ID 是 `effective`、`balanced`、`quality`。
- `envs` 是 Claude Code 参数唯一配置来源。
- `provider` 和 `model_usage_aliases` 是计费元数据，不属于 Claude 环境变量，因此继续保留。
- `ANTHROPIC_AUTH_TOKEN` 只存在于部署配置和运行时内存，不进入冻结快照。
- 旧配置、旧 ID 和 schema v2 不保留运行时兼容。
- 本次改造不改变 Designer，也不改变已经确认的三档权限和价格策略。
