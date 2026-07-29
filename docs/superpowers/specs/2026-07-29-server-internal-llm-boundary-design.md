# Server 内部大模型边界设计

## 目标

删除具有误导性的 `writing` 模型路由，将内容推理统一收归托管的 Claude Code Agent 工作流。同时保留一个明确由 Server 内部使用的大模型路由，用于在任务和 Agent 执行尚未创建前必须同步完成的意图解析。

## 当前问题

`model_routes.writing` 已不再负责撰写微信公众号文章正文。文章生成已经由 Article Agent 和 `content-writing` Skill 负责，`write_article` 等生成式写作 MCP 工具此前也已删除。

但该路由目前仍被当作通用文本模型，用于多个彼此无关的操作：

- 创建任务前的 AI 入口意图解析；
- 创建 Seednote 项目时的画像信息补全；
- 独立的爆款笔记分析；
- Seednote 发布后的笔记匹配；
- Live Slicer 的主题识别、无效句检测、分段和主题内容补全。

因此，路由名称和 Studio 中的文案都不能准确表达它的用途。当前设计还会让用户级“写作模型”覆盖影响 Server 的控制决策。此外，部分现有 MCP 工具执行的是生成式工作流推理，这些逻辑应由正在运行的 Claude Code Agent 及其 Skills 负责。

## 架构决策

系统采用两个明确的执行边界：

1. 托管的 Claude Code Agent 执行模型负责内容创作、内容分析、工作流决策、修改迭代和结构化创作产物。
2. Server 自有的 `model_routes.server_internal` 路由只负责必须在 Agent 执行创建前同步完成的 Server 决策。

`server_internal` 初始且唯一的调用方是 AI 入口意图解析。它负责把用户的自然语言请求转换成经过校验的任务创建参数，不得撰写内容、选择其他 Agent 执行配置或执行后续工作流步骤。

不保留 `model_routes.writing` 的兼容别名或回退路径。配置中包含旧键时，Server 必须在启动校验阶段失败。

## 配置

语义模型路由调整为：

```yaml
model_routes:
  server_internal:
    provider: "moonshot"
    model: "kimi-k2.7-code"
    timeout: 5m
```

模型供应商注册表仍由图片理解、视频理解和图片生成等能力共享。只要其他已配置路由仍使用 Moonshot，删除 `writing` 路由就不意味着删除 `MOONSHOT_API_KEY`。

运行时配置暴露 `ServerInternal` 文本模型配置，不再暴露 `Writing`。启动时构造 `serverInternalLLMClient`，并且只注入 `AIEntryService`。

AI 入口不得读取用户模型设置。`server_internal` 缺失或无效时，AI 入口意图解析不可用，并返回稳定的配置错误；不得回退到用户模型、理解类路由或新建的 Agent 执行。

## 调用方迁移

### 微信公众号文章写作

该流程不需要改变。Article Agent 继续使用冻结的执行配置生成大纲、Markdown 正文、修改稿、SEO 产物和审阅产物。MCP 继续负责项目与资源访问、确定性渲染、图片操作、进度更新和发布。

### Seednote 项目画像设置

项目设置保留供应商直接解析得到的画像字段，并删除 `ProjectHandler` 中同步调用大模型进行补全的逻辑。Claude Agent 可以在后续任务执行中解释已存储的项目画像和来源材料。项目创建不得为了修饰画像元数据而启动隐藏的 Agent 执行。

项目设置中的图片分析只能使用专用的 `image_understanding` 路由。删除当前从图片理解回退到文本模型的路径。

### 爆款分析

删除 Server 独立调用大模型生成分析结果的路径。`viral_analysis` 继续作为用户可见的任务类型，但改为普通托管任务，并明确映射到 Seednote Agent 和现有的 Seednote 爆款分析 Skill。该任务必须携带明确的 Agent 执行配置，创建标准 `task_execution`，并通过 Agent 终态模型用量完成结算。证据提取、推理、校验和结果文件均由 Agent 负责。

实现时删除重复的 Server 端生成式分析流程，不保留两套结果系统。切换后，任务准入、状态、执行配置选择、计费和 Studio 导航都必须使用标准任务生命周期。新请求不得写入 `viral_analyses` 记录，也不得进入旧的独立分析 Worker。

### 直播切片

Live Slicer Agent 读取 TingWu 转写结果，并直接生成目前由 Server 大模型工具返回的语义产物：

- 无效句；
- 主题候选；
- 连贯的分段范围；
- 补全后的主题脚本。

Agent/Skill 契约负责定义并自检这些 JSON 结构。确定性 MCP 工具负责校验索引，并把通过校验的语义产物转换成切片时间和 ffmpeg 计划。

### Seednote 发布追踪

发布后的追踪流程不得再让通用大模型猜测哪个公开笔记对应当前任务。当供应商返回稳定的外部笔记 ID 或 URL 时，发布流程必须持久化该标识；后续追踪只依据该标识进行确定性关联。

如果发布结果不包含稳定标识，追踪流程必须记录明确的未关联或未解析状态。未知标识不得视为匹配成功，也不得触发隐藏的 Agent 执行。

## 删除 MCP 工具

删除下列生成式 MCP 工具，将其推理职责迁移到 Claude Agent 工作流：

- `recognize_live_subjects`；
- `recognize_live_invalid_sentences`；
- `recognize_live_segments`；
- `complete_live_subject`。

同步删除这些工具的 Handler、Server 大模型 Service 方法、Schema、计费钩子、测试、Agent 指令和工具列表断言。不保留废弃工具名、别名或回退调用。

保留原子化或确定性的 MCP 能力，包括：

- 项目、资源、历史记录和任务状态访问；
- TingWu 上传、任务创建和结果查询；
- 用于校验时间并生成 ffmpeg 计划的 `build_live_clip_plan` 和 `build_live_subject_clip_plan`；
- 切片清单构建；
- 文章模板渲染；
- 图片理解和图片生成；
- 发布和持久化任务文件注册。

确定性切片计划工具必须接收 Agent 生成的语义 JSON，并拒绝错误的索引、范围或来源标识；不得再调用其他模型尝试修复。

## 用户模型配置清理

删除用户级文本模型覆盖。剩余的 Server 模型属于内部基础设施，不是用户可选择的写作能力。清理范围包括：

- 模型配置 API DTO 和 Service 方法中的文本模型字段；
- `TextUserConfig`、`UserModelConfig.TextConfigJSON`，以及删除 `user_model_configs.text_config_json` 的前向数据库迁移；
- Studio 中的文本模型表单状态、校验、请求、文案和测试；
- `GetEffectiveWritingConfig`、文本代理辅助方法，以及所有用户文本配置回退逻辑。

保留用户级图片模型配置及其现有 API 行为。Studio 中的对应区域只描述剩余的图片生成覆盖，不再展示笼统的“MCP 模型配置”入口。

## Service 清理

删除 `WritingService` 中已经失效的文本生成状态，包括默认文本客户端、超时、用户模型配置依赖和未使用的解析器。仅在其余能力仍具备内聚性的前提下保留或重命名该 Service；剩余能力包括确定性的微信 HTML 渲染，以及专用的图片和视频理解客户端。

通用的 OpenAI 兼容客户端抽象继续供 `server_internal`、图片理解和视频理解使用。其名称和注释必须表达通用模型客户端含义，不得继续描述为写作专用能力。

## 失败语义

- 未配置 `server_internal` 客户端时，AI 入口必须在创建任务前返回稳定的“配置不可用”结果。
- AI 意图解析结果必须通过严格 JSON 解析和现有任务字段校验。可以使用同一个 `server_internal` 模型进行次数受限的修复，但不得切换到其他路由。
- 未通过 Skill 自检的 Claude Agent 语义产物继续留在当前 Agent 的修正循环中。
- 确定性 MCP 校验失败时，根据所属 Agent 契约终止任务或将任务标记为可恢复；Server 不得调用其他模型修复产物。
- 缺失发布标识时必须保持明确的未解析状态，绝不能转化为猜测匹配结果。

## 数据与计费

Server 内部的意图解析请求继续记录符合现有供应商成本体系的 provider、model 和 usage 证据。该操作属于内部解析，不是文章写作 SKU，也不是用户可选择的 Agent 配置。

Claude Agent 负责的分析和写作成本继续计入 Agent 终态模型用量。删除语义 MCP 模型调用时，必须同时删除对应的独立操作成本归因，避免同一段推理重复计费。

爆款分析切换后必须使用唯一的标准任务准入和终态结算路径。历史记录继续可读，但新任务不得进入已删除的独立生成流程。

## 插件与契约更新

更新插件规范源中的 Agent/Skill 指令，使其不再调用已删除的 MCP 工具，而是直接创建所需的语义产物。由于这些变更会影响运行时插件资产，必须在同一变更中升级两个原生清单的版本号：

- `plugins/.claude-plugin/plugin.json`；
- `plugins/.codex-plugin/plugin.json`。

更新 MCP 工具列表和边界测试，证明已删除工具不再存在，同时保留的工具仍保持原子能力边界。

## 测试

新增或更新针对以下行为的测试：

- 解析并派生 `model_routes.server_internal`；
- 启动时拒绝 `model_routes.writing`；
- AI 入口只使用 Server 内部客户端，不读取用户覆盖，也不回退到其他模型；
- 删除 Seednote 画像的大模型补全和文本模型回退；
- 项目图片分析必须使用 `image_understanding`；
- `viral_analysis` 明确映射到托管的 Seednote Agent、要求执行配置，并进入标准任务生命周期；
- 根据外部标识进行确定性发布追踪，并支持明确的未解析状态；
- 4 个直播语义 MCP 工具均不存在；
- Live Slicer Agent/Skill 产物满足确定性切片计划 Schema；
- 删除用户文本模型 DTO、持久化字段、Studio 控件和请求；
- 保留用户图片模型配置；
- Agent 终态用量取代已删除的语义 MCP 用量归因；
- Claude 和 Codex 插件清单版本保持同步。

先运行 Server 配置、Service、Handler、MCP 和插件契约的针对性测试。完成前运行 `go test ./...`，将两个 Go 二进制构建到 `/tmp`，使用 Bun 运行完整 Studio 测试和生产构建，并执行 `git diff --check`。

## 非目标

- 不改变三个可选 Agent 执行配置及其供应商。
- 不允许用户在 Studio 中编辑 `server_internal`。
- 不使用 `server_internal` 作为图片或视频理解的回退路由。
- 不在项目设置或发布追踪期间启动隐藏的 Agent 执行。
- 不保留旧 `writing` 配置键、已删除 MCP 名称或独立的生成式工作流。
