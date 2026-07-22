---
name: seednote-visual-design
description: 'Use when creating seednote visual content including covers, content pages, and tail pages. Also use when user mentions ''种草笔记图片'', ''封面生成'', ''内容图'', ''尾图'', ''图片规划'', or when the seednote pipeline calls for image generation. Generates cover (封面), content pages (内容图), and tail pages (尾图) for Seednote (种草笔记) posts with 3:4 ratio design norms.'
---

# 种草笔记图片生成

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## 硬性纪律（违反视为流程失败，会被 SubagentStop 机械闸门拦截）

- **禁止跳过 image-plan.md 直接调 generate_image**
- **禁止 prompt 中省略「必须出现文字」字段**（封面是主文案，内容图是 2-4 条短句，尾图是 1-2 条文案）
- **每次调用 generate_image 后必须把实际 prompt + provider + model + revised_prompt 追加到 `$DIR/image-prompts.md`**
- **最后必须跑 Step 6 质量验证，写入 `$DIR/image-review.md`**
- **图片内所有可见文字必须是简体中文**（用户使用中文时），禁止英文/拼音/乱码/伪词
- **prompt 中文字必须用全角引号「」或书名号《》包裹**，让模型识别为"文字内容"而非视觉描述

---

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `generate_image` (project_id, prompt, image_type, output_path, task_id, ref_image_paths, verify_with_vision, verification_prompt) | 生成、落盘、登记并核验单张图片。每次都传 `verify_with_vision=true` 和当页动态 `verification_prompt`；**`task_id=$TASK_ID` 必传**，服务端据此把图片登记为 task_file，`list_task_files` 立即可见 |
| `upload_image` (project_id, file_path) | 上传图片到微信素材库 |

---

## 平台 Gotcha

种草笔记图片是 **3:4 竖版**，强视觉驱动。封面决定点击率，内容图决定完读率，尾图决定互动率。三类图片目标不同，prompt 构建方式也不同。

---

## Seednote 视觉方法论

本 skill 的目标是把内容蒸馏成大模型可执行的高质量图像指令，再通过 `generate_image` MCP 生成图片。流程不追求“凑齐文件”，而是让每张图都有明确的信息职责和审美秩序。

1. **内容蒸馏**：从 `$DIR/content.md` 提取主题、卖点、情绪、证据、关键短句、目标受众和每页承载的信息密度。
2. **视觉策略**：先确定统一色彩、画面主体、标题层级、信息密度、镜头/场景方向和内容页节奏，再写入 `$DIR/image-plan.md`。
3. **社交图文视觉原则**：用 `editorial 信息层级` 安排主标题、辅助信息、证据点和视觉主体；用 `Swiss/magazine 秩序感` 控制留白、对齐、分组和对比；用 `图文节奏` 保证封面负责点击，内容图负责理解，尾图负责收束。
4. **Prompt 蓝图**：每张图都明确角色、可见文案、视觉主体、构图层级、风格延续和验收标准；prompt 只描述要得到的画面效果和内容关系。
5. **生成记录**：每次调用 `generate_image` 后，把实际 prompt、`provider`、`model`、`output_path`、返回字段和修订信息追加到 `$DIR/image-prompts.md`。
6. **质量复盘**：逐图写 `$DIR/image-review.md`，检查主题相关度、文字准确性、移动端可读性、风格一致性、文件可访问性和 `seednote_image_mode` 一致性。

如果图片 API 返回 `error`、超时，或服务端视觉核验因配置/网络不可用而返回 `verification.passed=false`，记录 `provider`、`model`、`output_path`、原始 `error`/`verification.notes` 和`下一步建议` 到 `$DIR/image-review.md`，写入 `$DIR/failure-state.json`，并停止在图片阶段。`verification.notes` 包含 `verification call failed:` 表示图片已经生成、但核验运行依赖失败；必须原样记录其后的计费/配置/网络错误，保留返回的图片资产并立即停止，不得误写成图片 API 超时，不得再次调用 `generate_image`。禁止用 prompt 质量、文件存在、尺寸或 MIME 代替视觉核验。

`failure-state.json` 必须是结构化可恢复失败态：

```json
{"version":"1.0","status":"recoverable_failure","stage":"image_generation","error_code":"vision_verification_unavailable","message":"<原始错误摘要>","resume_from":"image_generation"}
```

---

<!-- seednote-reference-contract:start -->
## 多参考素材自动决策流程

1. 先读取用户统一提示词、项目资料、`.anban-creator/input-attachments/index.json` 和可选的 `errors.json`，写出 `request-analysis.json` 与 `request-analysis.md`。此阶段不得先分析图片。
2. 遍历 `index.json` 中每张可用图片。针对已完成的需求分析和该图片的可选 `instruction`，动态编写该图片独有的 `analyze_image` prompt；每张可用图片都必须分析，单张最多 3 次理解尝试。`errors.json` 中的条目必须记为 `analysis_failed`；若它是产品身份、Logo、包装、型号或核心结构的唯一证据则停止任务，其他素材能可靠补足时才可继续并记录依据。
3. 写出 `reference-analysis.json` 与 `reference-analysis.md`，记录可见事实、不确定性、需求支持点、可参考维度、必须保持、必须避免、不可推出结论，并完成同产品/系列/型号、新旧包装、角度、事实图/氛围图、Logo/文字/颜色/结构冲突分析。
4. 写出 `image-plan.md`。对每张输出图独立决定使用 0、1 或多张附件，记录附件编号、每张用途、保持项、禁止项。不得把所有素材传给所有页面；超过服务端返回的数量上限时按当页相关性排序选择子集。
5. 写出 `image-prompts.md`。调用 `generate_image` 时只传当前输出图相关的原始路径，数组顺序必须与 prompt 中“参考图 1、参考图 2”一致。不得传分析后的截图、拼图或转码替代原图。
6. 每张输出都在调用 `generate_image` 时传入动态编写的 `verification_prompt` 和 `verify_with_vision=true`，原子核验产品身份、结构、颜色、Logo、包装、文字、虚构部件、版本融合、禁止内容、页面职责和文字可读性；把服务端返回的 `verification` 原样记录到 `image-review.md`。`analyze_image` 只用于理解输入参考图，不再作为生成后核验的第二条调用链。
7. 核验不通过时自动调整参考组合/顺序、生成 prompt、保持项/禁止项、构图复杂度或核验 prompt。每张输出图最多 3 次生成尝试，初次生成计入。不得请求用户决定。
8. 写出 `reference-usage-summary.json`。关键事实无法保证时任务失败；非关键氛围或轻微构图问题可保留并记录 warning。
<!-- seednote-reference-contract:end -->

## 参考素材追踪产物与失败策略

每次运行都必须归档以下 8 个产物；即使任务失败，也不得删除已经写出的文件：

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

`reference-usage-summary.json` 使用以下结构；`status` 使用 `used`、`excluded` 或 `analysis_failed`，模型字段记录服务端实际返回值：

```json
{
  "version": "1.0",
  "inputs": [
    {
      "attachment_index": 1,
      "file_name": "attachment_01_front.png",
      "url": "https://example.invalid/front.png",
      "instruction": "保持包装和 Logo",
      "status": "used",
      "decision_summary": "正面图是产品身份和包装文字的主要证据",
      "analysis_attempts": 1,
      "warnings": []
    }
  ],
  "outputs": [
    {
      "file_name": "cover.png",
      "references": [{ "attachment_index": 1, "purpose": "保持产品身份、包装和 Logo" }],
      "generation_attempts": 2,
      "verification": { "passed": true, "score": "high", "missing_entities": [], "notes": "产品身份、包装和当页文字核验通过" },
      "provider": "openai",
      "model": "gpt-image-2",
      "selection_reason": "reference_compatible_fallback"
    }
  ],
  "warnings": [],
  "model_fallback_reason": "首选模型的参考图上限不足，服务端选择了兼容模型"
}
```

执行预算固定为：每张输入图最多 3 次理解尝试；每张输出图最多 3 次生成尝试，首次生成计入。不得向用户发起中途确认，也不得把参考素材选择、模型回退或重试决策转交给用户。

关键失败包括：唯一产品身份、Logo、包装、型号或核心结构证据不可用；身份或结构幻觉；冲突版本融合；出现禁止内容；页面无法履行职责。遇到关键失败时停止在当前阶段，但必须保留已生成文件和 trace artifacts，记录失败阶段和下一步建议，后续从失败阶段恢复。非关键氛围或轻微构图问题只记录 warning，不得把它升级成需要用户中途决策的阻塞。

---

## 视觉风格设计原则

风格无固定预设，每次根据账号定位和内容动态设计：

**三个维度定调**：
1. **账号定位** — 知识干货型（专业简洁，结构感强）/ 生活美学型（温暖氛围，情绪感强）/ 娱乐趣味型（活泼鲜艳，夸张对比）
2. **内容主题** — 美食/旅行/家居/时尚各有视觉惯例，参考主题的典型配色和构图
3. **目标受众** — 年龄层、消费力影响配色（年轻用户偏饱和鲜艳；成熟用户偏质感低饱和）

**封面、内容图与尾图的一致性**：
- 封面、内容图和尾图均不预设是否使用参考素材。每页根据 `image-plan.md` 独立选择 0、1 或多张原图；没有相关参考时使用纯文生图。项目级品牌参考图仍可作为旧数据来源，但不得覆盖本次输入附件中更具体、更新的产品事实。
- 参考素材用于约束产品事实、品牌要素、结构、包装、颜色、角度或氛围中的相关维度，不得把某张素材的全部画面元素无差别复制到每一页
- 没有相关参考素材的页面通过共享文本风格块（配色/字体/批注/色调）延续调性，并保持独立视觉主体、场景和构图

**中文内容硬约束**：
- 用户使用中文时，图片内所有可见文字必须使用简体中文；禁止英文翻译、拼音、乱码、伪词和中英混排
- 每张图必须围绕 `content.md` 和 `image-plan.md` 的当页主题，不得把尾图预告、其他季节或无关茶类提前画进内容页
- 健康养生类主题只能表达生活方式建议和传统茶文化语境，禁止承诺治疗、治愈或绝对功效

---

## 封面设计规范

见 [references/cover.md](references/cover.md)

---

## 内容图设计规范

见 [references/content.md](references/content.md)

---

## 尾图设计规范

见 [references/tail.md](references/tail.md)

---

## 图片内容规划流程

调用本技能时，按以下流程完成从内容分析到图片生成的完整链路。**调用方只需提供 content.md，本技能内部完成全部规划与生成。**

### 输入

- `$DIR/content.md`：包含标题、正文、话题标签的完整内容文件
- 账号定位信息（如已知）
- 改写模式信息（如适用：`style-only` / `medium` / `tight`）
- `$DIR/viral-template.json`（仅复刻模式，如适用）：读取 `cover_template`、`do_not_copy`、`recommended_clone_depth`

### 步骤 1：内容蒸馏

读取 `content.md`，提取：
- 核心主题和内容类型（干货/情感/测评/教程/...）
- 全部信息点（具体数据、方法、结论、场景描述）
- 正文总字数和段落数
- 目标受众和内容调性
- 用户锁定字段：若 `content.md` 标明用户指定封面标题、正文或标签，图片文案必须优先使用这些字段
- 可直接入图的关键短句、证据点、情绪钩子和每页信息密度

### 步骤 2：信息点提取与分组

将提取的信息点按主题相关性分组，**每组对应一张内容图，组数即内容图张数（上限 3 张）**：
- 每组 2-4 个信息点
- 确保组间不重叠
- 组数参考阈值：信息点 ≤4 → 1 张、5-8 → 2 张、≥9 → 3 张（以可读性为先，宁少勿挤）
- 标记每组的关键词和推荐布局类型（参考 [references/content.md](references/content.md) 的 6 种布局模式）
- 优先级排序：最重要的信息 → 首张内容图

### 步骤 3：视觉策略与布局选择

**图片构成严格按结构化运行控制 `seednote_image_mode` 执行**。缺失时按 `cover_content`。四种组合：

| `seednote_image_mode` | 应生成文件 | 总数 |
|---|---|---|
| `cover_only` | cover.png | 1 |
| `cover_content` | cover.png + image_01.png … image_0N.png（1~3 张） | 2~4 |
| `cover_tail` | cover.png + tail.png | 2 |
| `full` | cover.png + image_01.png … image_0N.png（1~3 张）+ tail.png | 3~5 |

> **禁止规则（与上表同等优先级）**：未包含尾图的模式（`cover_only` / `cover_content`）禁止生成 `tail.png`——`image-plan.md` 不得包含 `## tail` 节，步骤 5 不得执行尾图生成，步骤 6 质量验证跳过尾图项，最终产物不含尾图。未包含内容图的模式（`cover_only` / `cover_tail`）禁止生成 `image_0N.png`。

**内容图张数按信息点自适应（1~3 张）**：N 由步骤 2 的信息点分组决定，每张承载 2-4 个信息点，最多 3 张（image_01.png、image_02.png、image_03.png）。布局模式参考 [references/content.md](references/content.md) 的 6 种布局。

**image-plan.md 必须在「计划图片数量」字段写入实际生成的总张数**（1~5），机械闸门按此校验。

每张图在规划阶段先定义视觉策略：统一色彩、画面主体、标题层级、构图层级、信息密度和图文节奏。社交图文视觉原则用于组织画面：`editorial 信息层级` 让标题、辅助信息、证据点和主体互不抢戏；`Swiss/magazine 秩序感` 用留白、对齐、分组和对比提高移动端可读性；`图文节奏` 让封面、内容图和尾图各自完成不同传播任务。

### 步骤 4：生成 image-plan.md 与 Prompt 蓝图

按以下模板生成 `$DIR/image-plan.md`：

```markdown
# 图片内容规划

## 总体策略

- 主题方向: {从 content.md 提取的核心主题}
- 内容类型: {干货/情感/测评/教程/...}
- 目标受众: {从 content.md 提取}
- 内容调性: {从 content.md 判断}
- 图片内容定位: {图片要传达什么}
- 计划图片数量: N 张

---

## cover 封面

- 钩子: （≤10 字，从标题提取最吸引人的点）
- 辅助信息: （≤15 字，补充封面信息）
- 必须出现文字: （1-2 行简体中文主文案，优先用用户指定封面标题）
- 视觉主体: （必须能直接看出主题的实物/场景）
- 禁止元素: （英文、错别字、无关品类、医疗功效承诺等）
- 验收标准: （0.5 秒内能读懂主题 + 主体与标题一致）

---

## image_01 [内容] 主题：{第一组信息点主题}

- 信息点1: （8-15 字）
- 信息点2: （8-15 字）
- 推荐布局: {编号清单/对比双栏/步骤流程/标注图解/数据卡片/Q&A 对话}
- 必须出现文字: （2-4 条简体中文短句，必须来自信息点）
- 视觉主体: （当页知识对应的实物/过程/对比对象）
- 禁止元素: （英文、伪词、无关主题、误导性参数）
- 验收标准: （文字准确 + 主体准确 + 与其他页面不重复）

（按步骤 2 分组重复 image_02、最多到 image_03；每组独立填写信息点 / 推荐布局 / 必须出现文字 / 视觉主体）

---

## tail [尾部] 类型：{follow|comment|traffic}（**仅当 `seednote_image_mode` 包含尾图时添加本节；不含尾图则整节省略**）

匹配依据：{根据内容类型自动判断——知识干货→follow, 测评对比→comment, 种草推荐→traffic}
- 内容点: （见 tail.md 规范，根据类型填充）
```

### 步骤 5：图片生成

按 image-plan.md 逐一生成：

1. **逐页选参考素材**：先按 `image-plan.md` 为封面、每张内容图和尾图分别确定 0、1 或多张附件，只保留能服务当前页面职责的原始路径
2. **封面**：使用 [references/cover.md](references/cover.md) 的 Prompt 模板生成，并传入封面计划选中的原始路径子集
3. **内容图**：使用 [references/content.md](references/content.md) 的 Prompt 模板逐张生成（1~3 张），传入当前页选中的原始路径子集以及对应信息点和布局；没有相关参考时纯文生图；始终保证不同实景背景和构图角度
4. **尾图（仅当 `seednote_image_mode` 包含尾图时）**：使用 [references/tail.md](references/tail.md) 的 Prompt 模板单独生成，并仅传尾图相关的原始路径子集；不含尾图则跳过
5. **原子生成与核验**：每次调用 `generate_image` 都传 `verify_with_vision=true` 和当前页面独有的 `verification_prompt`；把实际 prompt、核验 prompt、`image_type`、请求 `size`、实际 `width`/`height`、`output_path`、`ref_image_paths`、返回的 `provider`、`model`、`selection_reason`、`response_type`、`revised_prompt`、`output_mime`、`verification` 追加写入 `$DIR/image-prompts.md`
6. **失败记录**：如果 `generate_image` 返回 `error`、超时，或 `verification` 缺失/因运行依赖不可用而失败，立即写入 `$DIR/image-review.md` 和 `$DIR/failure-state.json`，保留已有产物并停止；必须逐字保留工具返回的错误分类，不得把计费或配置错误改写成超时。只有核验服务正常且内容质量未通过时，才按下一步重试预算自动修订

### 步骤 6：质量验证

生成后写入 `$DIR/image-review.md`，逐张打分并给出结论：
- [ ] 文件存在且可访问，真实 MIME 与文件扩展名一致
- [ ] 封面 0.5 秒内能读出主题，主文案与用户指定标题一致
- [ ] 每张图主题相关度 ≥4/5，与 image-plan.md 当页主题一致
- [ ] 图片内文字为简体中文，无英文、拼音、乱码、伪词或错别字
- [ ] 茶类/产品/数字参数准确，不出现误导性内容（例如"10 秒出汤"不得写成"焖泡10秒"）
- [ ] 封面、内容图（、尾图，仅当生成）视觉风格一致，内容图之间有视觉多样性

每张生成图片都要根据当页职责和参考素材用途动态编写 `verification_prompt`，并在同一次 `generate_image` 调用中核验；不得复用固定核验 prompt。只有服务端返回 `verification.passed=true` 才算通过。内容质量未通过时自动调整参考组合/顺序、生成 prompt、保持项/禁止项、构图复杂度或核验 prompt，并覆盖同一个 `output_path` 重试；核验服务不可用属于运行依赖失败，立即写 `failure-state.json` 并停止，不消耗后续图片生成。任何情况下都不得改用 `verify_with_vision=false` 绕过核验。每张输出图最多 3 次生成尝试，初次生成计入；该预算只用于核验服务正常时的内容质量修订，不适用于计费、配置、网络或其他运行依赖错误。耗尽后按关键失败与 warning 策略处理，不得请求用户决定。交付前检查目录，仅保留 `image-plan.md` 中列出的 N 张图（cover/image_01..03/tail），删除多余候选或旧版文件。

---

## 常见失败与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 风格不一致 | 共享风格块描述过弱/被忽略 | 强化「风格延续：{style}」块（明确配色/字体/批注/色调），重申禁用元素 |
| 封面文字渲染错误/缺失 | prompt 文字约束力不够 | 检查 prompt 是否用「」包裹 required_text；缩短到 ≤15 字；明确字号占图宽 12-15% 和位置 |
| 出现英文/拼音/乱码 | 未显式独立禁止 | prompt 末尾追加独立「禁止项」段，明确「禁止任何英文/拼音/乱码/伪词」 |
| 内容图信息点错乱 | 模型把多条短句合并或乱序 | 每条短句单独用「」包裹并编号，明确「按列表顺序，禁止合并/拆分/修改任何字符」 |
| 内容图信息点模糊 | prompt 中信息点描述过于抽象 | 使用 image-plan 中的具体数据/场景作为视觉主体 |
| 尾图与正文调性断裂 | 尾图 prompt 未沿用统一风格或引用了无关素材 | 沿用共享「风格延续：{style}」块，并只传 `image-plan.md` 为尾图选中的相关原图 |
| 茶类识别错误 | 视觉主体不具体 | 明确茶类外观、茶干、花材、茶汤颜色和器具 |

### 复刻模式适配

当提供改写模式和 `$DIR/viral-template.json` 时：
- `style-only`：只参考 `cover_template` 的风格方向、信息层级和色彩倾向，完全重做具体构图
- `medium`：参考源笔记的信息结构重新设计内容图主题，但替换视觉主体、场景和版式
- `tight`：仅在 `recommended_clone_depth=tight` 且 `do_not_copy` 风险低时参考图片张数和各页主题关键词；不得复用源图人物姿势、图标组合、文字框位置或可识别构图。**图片构成仍以 `seednote_image_mode` 为准；若源笔记超过模式允许张数，按信息点优先级合并到该模式允许范围内，并在 image-plan.md 记录合并理由**

无论哪种模式，`do_not_copy` 中列出的元素都必须写入 `image-plan.md` 的风险提示，并在生成 prompt 时显式避开。若模板 `confidence=low` 或视觉证据不足，按 `style-only` 处理。

---

## 图片生成方式

通过 MCP 工具调用。**每次 `generate_image` 都必须传 `task_id=$TASK_ID`、`project_id=$PROJECT_ID`、`verify_with_vision=true` 和动态 `verification_prompt`**。服务端在一次调用中完成生成、登记和核验；`$TASK_ID`/`$PROJECT_ID` 由 seednote agent 一次解析、全程复用。

1. **规划参考子集**：读取 `image-plan.md`，为每张输出图确定 0、1 或多张相关原图；超过服务端上限时按当页相关性截取
2. **生成封面（单张）**：调用 `generate_image`，`image_type` 设为 `"cover"`，只传封面相关的 `ref_image_paths`，并用封面职责生成独有 `verification_prompt`
3. **逐张生成内容图（image_01.png 起，张数由信息点分组决定）**：逐张调用 `generate_image`，每张使用 `image-plan.md` 中对应的信息点构造 prompt 和核验 prompt，并只传当前页相关的 `ref_image_paths`；没有相关参考时纯文生图
4. **单独生成尾图（仅当 `seednote_image_mode` 包含尾图时）**：调用 `generate_image`，只传尾图相关的 `ref_image_paths` 和尾图核验 prompt；没有相关参考时纯文生图；不含尾图则跳过
5. **保持原图与顺序**：不得传截图、拼图或转码替代文件；`ref_image_paths` 顺序与 prompt 中的参考图编号完全一致
6. **带风格描述**：在 prompt 中加入风格描述（如"手绘感，暖色调，小清新"）

**调用示例（封面，务必带上 task_id）**：

```
generate_image(project_id=$PROJECT_ID, task_id=$TASK_ID, prompt=<封面提示词>, image_type="cover", output_path="$DIR/cover.png", verify_with_vision=true, verification_prompt=<封面动态核验提示词>)
```

内容图、尾图同理，逐张调用时只替换 `image_type` 与 `output_path`（如 `$DIR/image_01.png`、`$DIR/tail.png`），`task_id=$TASK_ID` 每张都必须带。`$DIR` 由 `prepare_workspace(task_id=$TASK_ID)` 返回，任务模式下为相对路径 `output`，故 output_path 形如 `output/cover.png`，服务端可直接登记为 task_file。

**关键规则**：封面、内容图和尾图均不预设是否使用参考素材。每页根据 `image-plan.md` 独立选择 0、1 或多张原图；没有相关参考时使用纯文生图。项目级品牌参考图仍可作为旧数据来源，但不得覆盖本次输入附件中更具体、更新的产品事实。 每张图都使用独立 prompt、独立参考子集和独立核验结论；参考某张原图不等于复用它的全部场景或版式。

### 春季花茶/白茶回归示例

当输入包含"春季｜百花复苏，宜饮花茶/白茶"、"春日饮茶指南"时，`image-plan.md` 至少包含：
- 封面必须出现：`春日饮茶指南`、`花茶+白茶`
- 内容图必须包含：`茉莉花茶`、`白牡丹白茶`、`85-90°C`、`10秒出汤`
- 禁止出现：英文标注、"焖泡10秒"、夏季主题提前出现在封面或内容图、非茶相关主体
