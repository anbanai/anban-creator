# Anban Skill 接口与治理规范 v0.1

> 本规范定义 Anban `harness/skills/` 中 Skill 的目录、触发、输入、输出、运行边界和质量要求。
> 它借鉴 Easel 的渐进式加载和文件化产物合同，但以 Anban 的 Server / Harness / MCP 分层为准。
> 当前状态：draft。新增或修改 Skill 时以本规范和仓库 `AGENTS.md` 为共同约束。

## 1. 设计目标

Anban Skill 是可组合的领域工作流说明。它告诉 Agent 如何完成研究、策划、创作、审核或分析，不能替代 Server 的业务状态机。

Skill 应满足以下目标：

1. **可路由**：Agent 能从简短的 `description` 判断何时加载。
2. **可组合**：多个 Skill 可以在一个 Agent 中按职责串联，不重复拥有同一领域规则。
3. **可恢复**：中间产物和失败原因写入任务工作区，任务可以从明确阶段继续。
4. **可审计**：下游读取文件化产物和 Server 返回的数据，不依赖对话记忆、日志或猜测。
5. **可托管**：同一 Skill 能在 Claude、Codex 和 Anban 托管运行时中使用，不假设 OpenClaw 或持久化本地工作区。

## 2. 所有权边界

这是 Anban Skill 设计的第一原则：**非确定性的内容判断放在 Skill/Agent，确定性的业务事实和外部副作用放在 Server**。

| 层 | 负责 | 不负责 |
|---|---|---|
| Server | 身份、项目和任务状态、Profile 快照、持久化、计费、调度、幂等、重试、外部发布、最终 readiness、对账 | 文章观点、标题创意、视觉概念等非确定性生成 |
| Agent | 读取 Skill、编排阶段、选择创作路径、组织 MCP 调用、写入任务产物、做语义审核和降级判断 | 自行维护另一套发布状态或替 Server 确认外部事实 |
| Skill | 领域方法、输入输出合同、质量标准、相邻职责边界 | 任务生命周期、平台阶段、账单、用户身份、长期调度 |
| MCP | 认证、校验、调用一个原子能力、编码结果 | 编排多个业务步骤、决定最终发布状态、通过日志推断成功 |
| Agent Pack | 声明绑定、运行时、artifact/delivery 合同、额度和 UI renderer | 把普通工作流步骤搬进 MCP handler 或 runtime adapter |

以下事实永远以 Server 为准：发布成功、草稿已创建、媒体已上传、任务 readiness、任务完成、计费结算。Agent 日志、旧版结果文件和本地存在的 HTML 都不能证明外部平台已经收到请求。

## 3. 目录与分发结构

Skill 的唯一分发源是 `harness/`：

```text
harness/
├── skills/
│   └── <skill-id>/
│       ├── SKILL.md              必须：触发说明和执行流程
│       ├── references/           可选：按需加载的领域知识和合同
│       ├── scripts/              可选：确定性本地处理脚本
│       ├── assets/               可选：模板、字体或固定资源
│       └── tests/                可选：轻量输入/输出样例
├── packs/<pack-id>/agent-pack.yaml  绑定与产物合同
├── agents/                       Agent 编排入口
└── .claude-plugin/、.codex-plugin/  原生宿主清单
```

约束：

- Skill 目录使用小写 kebab-case，入口文件必须精确命名为 `SKILL.md`。
- `name` 最长 64 个字符，只能使用小写字母、数字和连字符；不能以连字符开头或结尾，也不能连续使用连字符；不得包含 XML 标签或 `anthropic` / `claude` 保留词。
- Skill 目录内不新增 `README.md`；说明内容放在 `SKILL.md` 或 `references/`，仓库级分发说明放在 `harness/` 外部文档。
- `SKILL.md` 是流程目录，不是领域百科；细节放在 `references/`，只保留一层引用。
- Skill 脚本可以处理本地文件或媒体，但不得编写自定义 HTTP/MCP 客户端，也不得偷偷创建另一套持久化状态。
- Pack YAML 是产物路径和交付角色的权威来源；Skill 不得声明与 Pack 冲突的必需产物。
- `harness/` 变更必须同步更新 Claude 和 Codex 两份插件清单，并按仓库规则递增版本号。
- 上游第三方 Skill 可保留其许可证和必要文件，但新增 Anban Skill 遵循本规范。

### 工作流分层

Skill 可以按下面的逻辑层归类。层级用于路由、职责和审计，不要求把 `layer` 添加到每个 Skill 的 frontmatter；Pack 或仓库目录负责维护归类。

| 层 | 典型工作 | 允许的结果 |
|---|---|---|
| `general` | Profile、写作者、模板、共享质量规则 | 读取/整理配置，生成可复用参考，不产生外部副作用 |
| `discover` | 热点、资料、历史内容、竞品和素材研究 | 带来源、时间和缺失字段的研究文件 |
| `plan` | 选题、定位、内容策略、大纲、视觉计划、排期建议 | 决策文件和 brief，不直接改 Server 排期 |
| `produce` | 文章、笔记、图片、音视频和 HTML 生成 | 任务 `output/` 中的草稿、媒体和中间产物 |
| `review` | 内容质量、合规、风格、人设、视觉和 readiness 证据 | 审核报告、warning 或 blocked 结论 |
| `attribute` | 数据快照、发布后分析、复盘和策略建议 | 有样本范围和置信度说明的分析快照 |

“发布”不是一个由 Skill 自主拥有的层。发布前准备可以由 `review` 或 `general` Skill 完成；实际草稿创建、正式发布、排期执行和结果归因由 Server 及其显式 MCP 能力负责。

## 4. 渐进式加载与 Frontmatter

Skill 采用三层加载：

| 层 | 内容 | 加载时机 |
|---|---|---|
| Metadata | `name`、`description` 以及宿主允许的少量控制字段 | Skill 路由时常驻 |
| Instructions | `SKILL.md` 主体 | Skill 被选中后 |
| Resources | `references/`、`scripts/`、`assets/` | 执行到相关步骤时按需读取 |

推荐的最小头部：

```yaml
---
name: example-skill
description: Use when a managed Anban task needs research, planning, content production, or review of file-backed artifacts.
---
```

Frontmatter 规则：

- `name` 必须与目录名一致，最长 64 个字符，只能使用小写字母、数字和连字符；不能以连字符开头或结尾，也不能连续使用连字符；不得包含 XML 标签或 `anthropic` / `claude` 保留词。
- `description` 最长 1024 个字符，不得包含 XML 标签；使用第三人称或仓库认可的 `Use when` / `Use for` / `Use as` 激活句式，同时说明 Skill 的能力和触发场景；只写路由所需的内容，不总结完整执行流程。
- `user-invocable`、`disable-model-invocation`、`context: fork` 只有在宿主契约需要时使用；`context` 只能取 `fork`。
- 不在 Skill 头部放版本、实现来源、平台密钥、普通 `metadata.trigger` 或过长的工具清单；来源信息放仓库文档，运行约束放正文。
- 不为 Skill 预批准 `AskUserQuestion`。托管 Agent 是零交互流程；缺少输入时按本 Skill 的失败或降级合同处理。
- `SKILL.md` 主体控制在 500 行以内；超过 100 行时在顶部添加目录。超出部分拆到 `references/`，引用保持一层深度。

## 5. SKILL.md 标准结构

每个 Skill 至少应有以下内容，顺序可以按任务复杂度调整：

```markdown
---
name: <skill-id>
description: <能力 + 触发场景 + 相邻边界>
---

# <Skill 名称>

## 适用范围与边界
说明负责什么，不负责什么，和相邻 Skill 如何分工。

## 输入
列出必需输入、可选输入、Profile/任务上下文和数据来源。

## 执行流程
只写 Agent 应执行的步骤；复杂领域规则引用 references/。

## 输出合同
列出每个文件的路径、格式、必要字段和状态含义。

## 失败与降级
区分必需能力失败、可恢复失败、数据不足和质量 warning；说明 resume_from。

## 质量检查
列出完成前必须验证的事实、哈希、字段或质量门槛。
```

好的 Skill 描述方法和判断标准，不把每个 MCP 参数、平台规则或具体案例复制到主文件中。

## 6. 输入合同

Skill 应优先消费结构化运行上下文和 Server 返回值。常见输入包括：

- `project_id`、`task_id`、当前执行身份和平台/任务类型；由托管上下文或 Server 提供。
- 用户原始请求和结构化运行控制；不得从目录名、日志或自然语言猜测一个缺失的硬开关。
- `get_project_profile` 返回的已解析 Profile 快照。任务级配置覆盖项目级配置时，使用 Server 返回的 resolved 值。
- 当前任务的输入素材索引、相对路径和前序产物路径。
- 外部研究、历史标题、草稿、已发布内容等 Server/MCP 返回的数据及其时间、来源和 stale 状态。

输入规则：

1. 必需输入缺失时写结构化失败产物或按 Skill 合同降级，不把推断值当成用户配置。
2. Profile 是增强项，不是所有 Skill 的前提；没有 Profile 时只能使用明确的用户输入和可验证数据，并记录 `profile_missing` 或等价 warning。
3. 数据字段缺失时保留缺失事实，不补造热度、互动率、发布成功或平台规则结论。
4. Agent 必须使用 MCP 工具访问 Server 能力，禁止在 Skill 或 Agent 中创建自定义 HTTP 客户端。

## 7. 产物与上下游合同

### 7.1 工作区

托管 runtime 已创建任务私有工作区和 `output/`。Skill 只写 Pack 合同允许的 `output/<filename>` 路径：

- 不创建、发现、移动或重命名 `output/`。
- 不使用 `$DIR`、`.task-context`、当前目录名或持久化宿主目录推导任务身份。
- 不把临时文件写到交付目录；必要的缓存使用 runtime 允许的临时位置，并在结束前清理。
- 所有交付路径使用 POSIX 相对路径，并以 `output/` 开头。

### 7.2 文件化输出

跨阶段传递使用任务文件，不依赖对话上下文：

- 文本内容使用 Markdown 或 HTML；结构化事实使用 JSON。
- JSON 必须有稳定的 `schema_version`，状态字段使用有限枚举。
- 每个文件应说明来源、数据时间、缺失字段和审核状态（适用时）。
- 失败也要留下 `output/failure-state.json` 或该 Pack 规定的失败产物，至少包含 `version`、`status`、`stage`、稳定 `error_code`、脱敏 `message` 和 `resume_from`。
- `output/draft.json`、`delivery-manifest.json` 等交付包只描述产物路径、摘要、哈希和 readiness 证据，不复制整篇内容。

### 7.3 产物角色

Pack `artifacts` 描述执行结果，`delivery` 描述交付给 Studio/用户的文件。Skill 不得用“文件存在”代替质量通过，也不得把 warning 写成 success。

推荐的输出说明：

| 字段 | 含义 |
|---|---|
| `path` | Pack 合同中的 `output/` 相对路径 |
| `role` | `final`、`analysis`、`review`、`image`、`failure_state` 等稳定角色 |
| `status` | `ready`、`blocked`、`warning`、`failed` 等有限状态 |
| `evidence_paths` | 支撑 readiness 的审核或扫描文件路径 |
| `content_sha256` | 需要防止审阅结果与成品漂移时记录的原始字节哈希 |

上下游 Skill 通过明确文件路径消费结果。可使用一个轻量的摘要/索引文件，但索引只放路由信息、结论摘要和路径，不能复制大段正文。

## 8. MCP 使用与重试

### 允许的调用模式

- 读取项目、Profile、历史内容和能力注册表。
- 请求一个图像、媒体、渲染或分析能力并保存返回结果。
- 上传一个已经生成并审核通过的媒体文件。
- 提交进度或完成元数据，但不把这些调用当作外部发布证据。

### 禁止的调用模式

- 在 MCP handler 中串联多个业务步骤或条件决定下一个能力。
- 用 Agent 日志、HTTP 访问日志、本地文件或“调用没有报错”推断微信已收到请求。
- Skill 直接更新任务状态机、账单、排期、发布状态或 Profile 持久化。
- 用自定义 HTTP、Node、Python 客户端绕过已注册的 MCP 能力。
- 为了掩盖认证/执行身份错误而更换 Provider、Prompt、比例或工具反复尝试。

### 重试原则

- 传输失败、明确可重试的 provider 暂时错误，按具体 Skill 的有限次数重试；每次重试原因必须可审计。
- 外部副作用、发布、草稿创建和结算的重试、幂等键、`ambiguous` 处理由 Server 负责。
- `execution_identity_required`、`execution_identity_mismatch` 等身份错误不因创作参数变化而重试；保留产物并写 warning/失败状态。
- 工具不可用时不得伪造空列表、成功 URL、热度数据或审核通过。

## 9. 发布与安全

Skill 可以生成发布前内容、质量报告和显式交互草稿请求，但不能自行宣布正式发布成功。

- 自动创建或正式发布公众号内容必须通过 Server finalizer；Server 的状态机、幂等键和 provider evidence 是唯一事实来源。
- 交互式草稿创建可以作为独立 MCP 能力，但仍须返回 Server 记录的结果。
- 发布前 Skill 应检查内容完整性、格式、合规和敏感信息；检查结果只能作为 readiness 证据，不能绕过 Server 策略。
- 不把 API Key、Cookie、内部 URL、完整环境变量、绝对宿主路径写入产物、日志、截图或错误消息。
- 平台规则必须来自版本化 Server 资源或 Skill reference，并记录规则版本；不要硬编码易过期的“流量池/限流”断言。
- 公开内容出站前使用仓库认可的内容安全扫描能力；硬阻断与放行由明确的安全合同决定。

## 10. Profile、证据与不确定性

Profile 用来约束定位、受众、风格、偏好和历史记忆，不是凭空补齐事实的授权。Skill 应：

1. 区分项目级 Profile、任务级覆盖和本次用户明确输入。
2. 记录关键结论所用的证据路径、来源时间和数据完整性。
3. 在样本不足时输出“证据不足/低置信度/待观察”，不把经验规则表述成平台算法保证。
4. 诊断类 Skill 使用“问题 → 证据 → 建议”结构；建议默认是 advisory，不自动改写项目定位或发布策略。
5. 策略、复盘和评分结果如需跨任务复用，应由 Server 版本化持久化；Skill 不维护隐式全局文件。

## 11. 任务生命周期与交互

- 只有顶层 Agent 可以维护平台任务阶段；Skill、子 Agent 和脚本不得调用或模拟 Anban 任务生命周期接口。
- Skill 不声明百分比，也不把“公众号草稿”或“正式发布”当作 Agent 阶段。
- 托管执行默认零交互。缺少选择时按任务输入、项目配置、Server 默认和能力注册表的既定优先级处理，并把采用的回退写入产物。
- 无法在安全和能力边界内继续时，写结构化失败态并结束当前执行；不得停在模糊的半成功状态。
- 质量不通过通常回到产生该问题的步骤修订；不可修订的外部能力失败则保留已有产物并标记 `blocked`。

## 12. 测试与验收

新增或修改 Skill 至少检查：

- frontmatter 可被 Claude/Codex 路由，`description` 写清能力、触发和边界。
- 主文件不超过 500 行，引用的每个 `references/` 文件存在且引用路径有效。
- 没有自定义 MCP/HTTP 客户端、Secrets、宿主工作区推导或 Skill 自有生命周期调用。
- 所有输出路径都落在 Pack 声明的 `output/` 合同中，JSON 字段、状态和失败恢复信息稳定。
- 在没有 Profile、没有外部数据、MCP 失败和部分媒体失败时，行为符合降级合同。
- 运行与修改范围匹配的验证：`make agent-pack-check`、`make agent-pack-generate`，以及 `cd server && go test ./agent/...` 或相关包的定向测试；涉及完整插件合同时运行 `cd server && go test ./...`。

Skill 变更应同时更新 Pack 版本、相关 Agent 声明和契约测试；只改文档或 references 时仍需确认没有陈旧路径和冲突规则。

## 13. 反模式与替代方案

| 反模式 | 问题 | Anban 做法 |
|---|---|---|
| `outputs/<主题>` 作为永久资产库 | 和托管任务隔离、对象存储及 Pack 交付冲突 | 使用任务私有 `output/`，由 Server/Studio 管理归档 |
| Skill 直接调用微信或平台网页发布 | 绕过幂等、审计和恢复 | 生成交付包，交由 Server finalizer 或显式 MCP 能力 |
| 本地 JSON 调度器/发布日志 | Agent 文件不是业务事实 | 使用 Server 计划、队列、发布记录和 Analytics 模型 |
| 一个万能 `manifest` 复制全部上下文 | 容易漂移、膨胀且泄露内容 | Pack 约束产物；索引只传路径和摘要 |
| 把经验规则写成“限流/流量池”事实 | 规则不可验证且容易过期 | 使用可观察信号、数据来源和不确定性说明 |
| SKILL.md 复制所有平台、模板和 API 细节 | 路由上下文膨胀、规则重复 | 主文件写流程，领域知识放 references/，Server 提供动态资源 |
| 用“文件已生成”判断任务成功 | 产物可能未审核、未上传或不可发布 | readiness 由 Pack 合同、质量证据和 Server 最终状态共同决定 |

## 14. Easel 规范的取舍

Anban 直接借鉴：

- Metadata / Instructions / Resources 三层渐进式加载。
- Skill 主文件精简，领域知识放 `references/`。
- 明确输入、输出、Profile 缺失时的降级策略。
- 以文件传递上下游产物，索引只保留路径和摘要。
- 用清晰的相邻 Skill 边界避免重复触发。

Anban 不直接采用：

- OpenClaw 命令和同步脚本。
- `outputs/<主题>/` 作为跨任务资产根目录。
- Skill 自有发布器、调度器、评论操作和本地发布日志。
- 把所有平台合规规则硬编码进 Skill。
- 用本地文件或 Agent 日志证明 Server/微信已经完成外部操作。
