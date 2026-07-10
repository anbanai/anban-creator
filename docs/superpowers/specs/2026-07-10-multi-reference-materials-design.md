# 多参考素材驱动的种草图文设计

- 日期：2026-07-10
- 状态：已完成产品设计确认，待实现计划
- 首个完整落地场景：种草笔记（Seednote）
- 共享能力：Studio 参考素材输入、任务/计划附件契约、执行器素材落盘与可追溯展示

## 1. 背景

目前 Studio 首页已经允许一次上传多个附件，任务也能通过 `Task.InputAttachments` 持久化附件，执行器会把非续跑附件落盘到 `.anban-creator/input-attachments/` 并生成 `index.json`。图片生成工具已经支持单张和多张参考图，图片理解工具也允许 Agent 自己提供分析 Prompt。

现有链路仍存在以下缺口：

1. Studio 只显示简单附件标签，没有缩略图和逐图说明。
2. 手动创建任务和创建计划没有复用同一套通用输入附件能力。
3. AI Entry 会把第一张图片隐式写入 `ReferenceImageURL`，错误地赋予第一张图“唯一参考图”语义。
4. Seednote Agent 只知道账号级单参考图，没有“先理解需求、再逐图理解、最后按输出页选择参考素材”的流程。
5. `seednote-visual-design` 及其内容图规范明确禁止内容图、尾图使用参考图，与多参考素材需求冲突。
6. 运行过程缺少结构化的参考素材决策和最终使用摘要。

本设计让用户只负责第一次输入。任务开始后，是否使用参考素材、使用哪张、参考什么、排除什么、如何重试，全部由 Agent 和 Skill 自动决定。

## 2. 目标

### 2.1 产品目标

用户在一次提交中提供：

- 统一创作需求；
- 多张参考图片；
- 每张图片的可选自然语言说明。

系统自动完成：

- 需求理解；
- 逐图分析；
- 跨图关系判断；
- 每张输出图的参考素材选择；
- 原图驱动的图片生成；
- 生成后视觉核验；
- 不合格结果的自动修正和重试；
- 最终参考使用摘要。

### 2.2 体验目标

- 不向用户展示 `auto / required / excluded` 等内部策略。
- 不要求用户绑定封面图、内容图或尾图。
- 任务运行中没有确认或选择步骤。
- Studio 的首页、手动任务和计划入口复用一个参考素材组件。
- 留空单图说明时仍能自动理解图片。
- 优先保证结果质量，允许增加图片理解调用、积分消耗和执行时间。

### 2.3 技术目标

- 延续现有 `EntryAttachment`、`Task.InputAttachments` 和执行器落盘能力。
- 计划保存参考素材，并在每次触发时复制独立任务快照。
- Agent 对图片理解 Prompt、生成 Prompt 和核验 Prompt 均采用动态编写。
- 不在 Skill 中硬编码供应商参考图数量；由服务端暴露当前模型能力。
- 本地、Docker、Kubernetes 执行保持一致。

## 3. 非目标

- 本期不要求把所有文章、朋友圈、电商和视频 Agent 都改造成同一套参考素材推理流程。
- 本期完整更新 Seednote Agent/Skill；共享组件和数据契约允许其他 Agent 后续直接接入。
- 本期不增加人工审核节点、运行中编辑策略或成本优先模式。
- 本期不移除项目级品牌参考图和旧版 `reference_image_url` 数据库字段。
- 本期不把模型隐藏推理过程展示给用户，只展示结构化事实、决策结果和质量记录。

## 4. 产品体验与 Studio UX

### 4.1 统一组件

新增通用组件：

```text
ReferenceMaterialInput
```

首期直接复用于：

- `DashboardPage` 首页 AI 创作入口；
- `TasksPage` 手动创建任务；
- `PlansPage` 创建和编辑计划。

组件统一负责：

- 多文件选择与拖拽；
- 上传、进度和单文件失败重试；
- 图片缩略图；
- 非图片附件的通用文件卡片，确保 Dashboard 现有附件能力不回退；
- 单文件可选说明；
- 删除和继续追加；
- 数量、类型、大小校验；
- 输出统一的附件数组。

组件通过属性接收允许的文件类型和上限。Seednote 任务与 Seednote 计划启用图片参考素材；Dashboard 保留现有图片、音频、视频和文档附件能力。视频专用的结构化参考输入不在本期被替换。

### 4.2 图片卡片

每张图片显示：

- 缩略图；
- 文件名；
- 上传进度或失败状态；
- “说明（可选）”输入框；
- 删除按钮。

说明框提示：

> 可选：告诉 AI 这张图是什么，或哪些内容需要保留、忽略。留空也会自动理解。

说明既可以是描述，也可以是要求，例如：

- 正面产品图，包装和 Logo 要准确；
- 只参考配色，不要照搬构图；
- 这是使用场景，可以参考氛围；
- 旧包装，不要出现在生成结果中；
- 内部结构图，用于表现方便清洗。

不提供策略开关、用途下拉框、页面绑定和生成前确认。

### 4.3 上传规则

- Seednote 首次输入最多 16 张图片。
- 单文件大小和格式沿用现有 AI Entry 安全校验，避免同类入口出现不同标准。
- 单图说明最多 1000 个 Unicode 字符。
- 上传进行中时禁止提交。
- 单张失败不清空其他文件。
- 用户只能在首次提交前追加、删除和重试。

上传区域显示提示：

> AI 会根据创作需求逐张理解参考素材，并自动核验生成结果。参考素材越多，执行时间和积分消耗可能越高。

计划页面增加：

> 每次计划执行都会根据当次主题重新分析参考素材，并产生相应的执行消耗。

### 4.4 任务页

运行中通过现有 `update_task_progress` 展示：

- 理解创作需求；
- 分析参考素材 `3/8`；
- 判断图片关系；
- 规划参考策略；
- 生成和核验具体输出图；
- 自动修正具体输出图。

所有状态均为只读，不包含确认按钮。

任务完成后显示：

- 初始参考素材及用户说明；
- 每张素材是否使用、排除或分析失败；
- 每张输出图使用了哪些素材、具体参考什么；
- 自动重试次数与核验结果；
- 警告和降级原因。

## 5. 通用数据模型

### 5.1 服务端附件模型

继续使用 `model.EntryAttachment`，新增：

```go
Instruction string `json:"instruction,omitempty"`
```

完整语义：

- `Type`：附件类型；
- `URL` / `Text`：附件来源；
- `FileName` / `ContentType` / `Size`：展示和校验信息；
- `UploadID` / `Key`：上传归属与存储定位；
- `Role`：内部角色，普通参考素材 UI 不暴露；
- `Instruction`：用户对单个附件的可选自然语言说明。

不新增用户可见的策略字段。

### 5.2 Studio 类型

把目前仅位于 AI Entry API 内的附件接口提取为共享类型，例如：

```ts
export interface InputAttachment {
  type: InputAttachmentType
  url?: string
  text?: string
  file_name?: string
  content_type?: string
  size?: number
  role?: string
  upload_id?: string
  key?: string
  instruction?: string
}
```

Dashboard、TasksPage、PlansPage 和 `ReferenceMaterialInput` 使用同一类型。

### 5.3 Task

`CreateTaskRequest` 增加：

```json
{
  "input_attachments": []
}
```

服务端 `createTaskRequest`、`CreateManualParams` 和 Task 响应统一使用 `[]EntryAttachment`。`Task.InputAttachments` 已存在，继续作为任务级快照存储。

### 5.4 Plan

`Plan` 增加：

```go
InputAttachments datatypes.JSONType[[]EntryAttachment] `gorm:"type:json" json:"input_attachments"`
```

创建计划接受 `input_attachments`。

更新计划必须区分：

- 字段省略：保持原附件；
- 空数组：清空附件；
- 非空数组：完整替换。

Go 请求和 Service 参数使用指针切片或等价的显式可选结构，不能用普通切片丢失 omitted 与 empty 的区别。

### 5.5 上传目的

首期继续复用现有 `ai_entry_attachment` direct-upload purpose 和 pending-upload 完成机制，避免新增上传迁移。附件验证逻辑从 AI Entry Handler 中提取为共享能力，供 AI Entry、任务和计划入口调用。

共享校验包括：

- 类型识别；
- URL 合法性；
- 上传归属；
- pending upload 完成；
- 大小限制；
- 字段去空格；
- `instruction` 长度；
- 附件数量。

## 6. API 与兼容性

### 6.1 AI Entry

继续接受：

```json
{
  "text": "统一创作需求",
  "attachments": []
}
```

Seednote 分支删除以下隐式行为：

```go
params.ReferenceImageURL = firstImageAttachmentURL(req.Attachments)
```

Seednote 上传顺序只用于稳定编号，第一张图片不具有更高语义优先级。Article 和 Moments 不在本期完成参考素材推理改造；为避免现有功能回退，它们暂时保留旧版首图兼容行为，直到各自 Agent/Skill 接入通用附件决策流程后再移除。

### 6.2 手动任务

任务创建端点接受并校验 `input_attachments`，随后传入 `CreateManualParams.InputAttachments`。批量 quantity 创建时，每个任务获得相同内容的独立 JSON 快照。

### 6.3 计划

计划创建和更新端点接受 `input_attachments`。每次 `CreateFromPlan`：

1. 解析本次实际 Prompt，包括计划 Prompt、标题或话题池结果；
2. 将计划当前附件复制到新任务；
3. 新任务后续独立运行；
4. 修改计划不影响已创建任务。

计划每次运行都根据当次实际主题重新分析原始图片，不永久复用上一次任务的“图片用途”判断。

### 6.4 旧数据

- 保留项目级品牌参考图及 `skip_reference_image` 语义。
- 保留旧任务和旧计划的 `reference_image_url`。
- 新 Seednote Studio 多参考素材只写 `input_attachments`，不再写第一张图片为 `reference_image_url`；Article/Moments 在各自 Agent 接入前保留旧兼容路径。
- 缺少 `instruction` 的旧附件按空说明处理。
- Agent 明确区分项目品牌参考图、旧版任务参考图和新版任务输入素材。

## 7. 执行器素材落盘

执行器继续落盘到：

```text
.anban-creator/input-attachments/
```

`MaterializedInputAttachment` 和 `index.json` 增加：

- `instruction`；
- `upload_id`，用于稳定追溯；
- 下载状态或独立失败记录。

成功素材的索引示例：

```json
[
  {
    "index": 1,
    "upload_id": "upload-123",
    "type": "image",
    "file_name": "product-front.png",
    "content_type": "image/png",
    "path": ".anban-creator/input-attachments/01-product-front.png",
    "instruction": "正面产品图，包装和 Logo 要准确"
  }
]
```

下载失败不能静默消失。执行器写入独立的失败清单，例如：

```text
.anban-creator/input-attachments/errors.json
```

其中保留原始附件编号、文件名、URL/存储标识和错误。成功附件编号不得因前一张失败而重新排序。

本地、Docker、Kubernetes 执行器使用同一落盘函数和索引契约。

## 8. Agent / Skill 自动执行流水线

### 8.1 总原则

Skill 固定工作阶段、质量标准和产物契约，不固定图片理解 Prompt 的具体文本。

Agent 根据本次需求动态编写：

- 原始图片的分析 Prompt；
- 输出图片的生成 Prompt；
- 生成结果的核验 Prompt。

### 8.2 阶段一：需求理解

Agent 读取：

- 用户统一提示词；
- 项目画像和账号配置；
- 任务类型和图片构成；
- 单图用户说明；
- 计划任务的当次实际主题。

输出：

```text
request-analysis.json
request-analysis.md
```

至少包含：

- 创作目标；
- 受众；
- 核心卖点；
- 产品一致性要求；
- 视觉方向；
- 需要从图片确认的问题；
- 禁止虚构和禁止出现的内容。

此阶段只建立第一版任务理解，不假装已经看懂图片。

### 8.3 阶段二：逐图动态分析

Agent 遍历 `index.json`，为每张图片单独编写 `analyze_image` Prompt。Prompt 必须围绕本次任务的问题，而不是复用统一固定模板。

Skill 只约束结果需要覆盖：

- 客观视觉事实；
- 主体、结构、颜色、材质和视角；
- 可见 Logo、文字和数字；
- 遮挡、模糊和不确定性；
- 对本次卖点的支持；
- 可参考的维度；
- 必须保持和必须避免的内容；
- 不能由该图片推出的结论。

默认分析所有可用图片。

输出：

```text
reference-analysis.json
reference-analysis.md
```

记录必须引用稳定的 `attachment_index` 和工作区路径，不能只用文件名。

### 8.4 阶段三：跨图关系分析

Agent 判断：

- 同一产品、同系列或不同型号；
- 新旧包装；
- 正面、侧面、内部和细节角度；
- 产品事实图与场景氛围图；
- Logo、文字、颜色和结构冲突；
- 产品身份锚点；
- 应排除的图片。

图片事实可以校正第一版需求理解，因此 `request-analysis.*` 可以在此阶段更新。

### 8.5 阶段四：每页参考计划

Agent 结合需求、图片分析、文案和 Seednote 页面职责生成：

```text
image-plan.md
```

每张输出图明确：

- 是否使用参考素材；
- 使用的附件编号；
- 每张分别参考什么；
- 必须保持什么；
- 不能照搬或不能出现什么。

允许每张输出图使用 0、1 或多张参考图。封面、内容图和尾图没有固定“必须使用”或“禁止使用”规则。

### 8.6 阶段五：原图驱动生成

图片分析摘要不能替代原始图片。调用 `generate_image` 时：

- 使用 `ref_image_path` / `ref_image_paths` 传入当前输出页真正需要的原始文件；
- 保证参考图数组顺序与 Prompt 中“参考图 1、2……”一致；
- Prompt 明确每张图片的用途和禁止事项；
- 不把所有输入图无脑传给所有输出图；
- 超出模型能力时按相关性选择子集，不得简单截取前 N 张。

每次调用的真实 Prompt、参考路径、provider、model、输出路径和 revised prompt 写入：

```text
image-prompts.md
```

### 8.7 阶段六：动态核验

每张生成图使用：

```text
verify_with_vision: true
```

`verification_prompt` 由 Agent 根据当页职责、原图分析、保持项和禁止项动态编写。至少检查：

- 产品身份；
- 结构和颜色；
- Logo、包装和文字；
- 不存在部件的虚构；
- 不同版本错误融合；
- 禁止内容；
- 当页文案和视觉职责；
- 页面文字可读性。

结果写入：

```text
image-review.md
```

### 8.8 阶段七：自动修正

每张输出图最多 3 次生成尝试，包括首次生成和最多 2 次修正。Agent 可调整：

- 参考素材组合和顺序；
- 生成 Prompt；
- 保持项和禁止项；
- 构图复杂度；
- 核验 Prompt。

全程不询问用户。

## 9. Agent 与 Skill 修改范围

### 9.1 Seednote Agent

`claudecode/agents/seednote.md` 及对应分发版本负责：

- 需求理解；
- 参考素材遍历与动态分析；
- 跨图关系判断；
- 调用视觉 Skill；
- 自动生成、核验和重试；
- 保存结构化决策产物；
- 更新只读任务进度。

删除任何运行中让用户选择项目或参考图的指令。Studio 任务使用传入的项目；直接调用缺少项目时由 Agent 语义匹配并使用确定性排序选择。

### 9.2 Seednote Visual Design Skill

更新：

- `claudecode/skills/seednote-visual-design/SKILL.md`
- `claudecode/skills/seednote-visual-design/references/content.md`
- 对应 `codex`、`openclaw` 分发文件

删除或改写：

- 内容图不传 `ref_image_path`；
- 默认不使用参考图；
- 尾图不传参考图。

新规则：

> 是否使用参考图，由 Agent 根据当前页面职责、参考素材分析和产品一致性要求决定。

### 9.3 分发一致性

仓库已有 Seednote Skill 契约测试覆盖 `claudecode`、`codex`、`openclaw`。实现时同步更新三套分发，不允许只修改一套。

## 10. 模型能力与自动路由

服务端向 Agent 暴露本次任务实际图片模型能力：

```json
{
  "provider": "volcengine",
  "model": "seedream-5-pro",
  "supports_reference": true,
  "max_reference_images": 10
}
```

能力来自当前模型路由配置，不在 Skill 中硬编码。

处理规则：

1. 当前输出图不需要参考图时，可以继续使用当前模型。
2. 当前输出图必须依赖参考图、首选模型又不支持时，系统自动选择已配置的质量最优兼容模型。
3. 自动切换不询问用户，但在任务记录中显示实际模型和原因。
4. 没有可用兼容模型时进入可恢复失败状态，不能静默纯文生图。

用户显式模型选择是首选，不是允许牺牲关键产品真实性的绝对约束。

## 11. 冲突、失败与降级

### 11.1 优先级

冲突处理优先级：

1. 用户统一提示词的明确要求；
2. 单图说明；
3. 多图一致的客观事实；
4. 清晰度高、遮挡少的素材；
5. 项目级品牌资料；
6. 无法验证的推测。

用户要求不能把图片中不存在的事实变成“已被图片证明”的事实。

### 11.2 素材下载失败

执行器自动重试。仍失败时：

- 非关键氛围图可以跳过并警告；
- 产品身份、Logo、包装、核心结构的唯一证据失败时，任务停止；
- 其他图片能可靠补足时可以继续。

### 11.3 图片理解失败

每张图片最多 3 次理解尝试。不可解析结果不等于图片无价值。

- 非关键氛围图失败：可跳过并警告；
- 关键产品图失败：任务停止；
- 其他素材足以补足：继续并记录依据。

### 11.4 生成重试耗尽

必须失败：

- 产品身份错误；
- 产品结构虚构；
- 新旧版本错误融合；
- 明确禁止内容出现；
- 核心 Logo、包装或型号错误；
- 输出图不能承担基本页面职责。

可以保留并警告：

- 产品事实正确但氛围不够理想；
- 构图稍弱但内容和文字准确；
- 非核心装饰与计划轻微不同。

失败任务保留已生成文件和分析记录，支持修复模型、额度、网络或配置后从相同阶段继续。

## 12. 成本

- 默认逐张分析所有参考图片。
- 默认核验每张生成图片。
- 不提供省积分模式或跳过分析开关。
- `analyze_image`、生成和视觉核验继续使用现有 operation billing。
- TaskDetail 积分明细显示真实消耗。
- 计划每次运行重新分析并独立计费。

## 13. 可追溯产物

任务必须归档：

```text
request-analysis.json
request-analysis.md
reference-analysis.json
reference-analysis.md
image-plan.md
image-prompts.md
image-review.md
reference-usage-summary.json
```

`reference-usage-summary.json` 至少包含：

- 输入素材编号；
- `used`、`excluded`、`analysis_failed` 状态；
- 使用或排除摘要；
- 每张输出图使用的附件和用途；
- 尝试次数；
- 核验结果；
- 警告。

TaskDetail 默认渲染易读摘要。完整技术产物放在折叠的执行记录或任务文件区域。

## 14. 测试设计

### 14.1 Studio

覆盖：

- Dashboard、TasksPage、PlansPage 复用同一组件；
- Dashboard 非图片附件能力不回退；
- 多选、拖拽、缩略图、说明、追加、删除、失败重试；
- 上传中禁止提交；
- 16 张限制；
- 创建和编辑计划恢复附件；
- omitted、清空、替换三种计划编辑行为。

### 14.2 服务端

覆盖：

- `instruction` 规范化和 1000 字限制；
- 三个入口共享附件校验；
- 手动任务持久化；
- 计划持久化；
- 计划触发复制独立快照；
- 编辑计划不影响历史任务；
- 批量任务各自获得快照；
- Seednote AI Entry 不再把第一张图写入 `reference_image_url`，Article/Moments 旧行为暂时兼容；
- 旧数据兼容。

### 14.3 执行器

覆盖：

- 稳定编号和路径；
- `index.json` 包含说明和上传标识；
- 下载失败写入错误清单；
- 前序失败不改变后续附件编号；
- 本地、Docker、Kubernetes 行为一致。

### 14.4 Agent / Skill 契约

覆盖：

- 需求分析先于图片分析；
- 每张图片由 Agent 动态编写分析 Prompt；
- 每张可用输入图调用 `analyze_image`；
- 内容图和尾图不再固定禁用参考图；
- 每张输出图只传相关素材子集；
- 动态核验和自动重试；
- 不要求用户中途选择；
- `claudecode`、`codex`、`openclaw` 分发一致。

### 14.5 端到端验收场景

1. 多张产品角度图，封面选择正面图。
2. 内部结构图只用于对应卖点页。
3. 场景图只参考氛围，不复制其产品。
4. 旧包装被自动排除。
5. 全部单图说明为空时仍能完成理解。
6. 某张输出图无需参考素材时自动使用纯文生图。
7. 冲突素材不被错误融合。
8. 计划针对不同运行主题重新制定参考策略。
9. 关键图片分析失败时停止，不虚构结果。
10. 全流程不存在中间用户确认。

## 15. 实施边界和依赖顺序

实现计划应按以下依赖顺序拆分：

1. 共享附件类型、服务端模型和校验；
2. Task/Plan API 与计划快照；
3. 执行器索引和失败记录；
4. Studio `ReferenceMaterialInput` 及三个入口接入；
5. 模型能力暴露和兼容模型自动路由；
6. Seednote Agent/Skill 三套分发更新；
7. TaskDetail 素材和使用摘要；
8. 契约测试、集成测试和端到端验证。

各阶段保持旧任务可运行。任何数据库变更通过现有自动迁移体系加入，不删除旧字段。

## 16. 完成标准

本功能完成必须同时满足：

- 用户可在首页、手动任务和 Seednote 计划入口一次上传多张图片并填写可选说明；
- 计划触发任务获得稳定附件快照；
- Agent 先理解需求，再动态编写逐图分析 Prompt；
- 每张输出图由 Agent 自动选择 0、1 或多张原图；
- 生成后自动核验并在预算内重试；
- 关键事实无法保证时明确失败，不静默伪造；
- TaskDetail 可查看输入素材和参考使用摘要；
- 旧任务、旧计划和项目品牌参考图继续工作；
- 所有相关测试通过；
- 整个运行过程不要求用户做中间决策。
