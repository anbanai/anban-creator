---
name: article
description: 微信公众号图文文章全自动创作。用户提到"写文章"、"写一篇"、"发文章"、"公众号文章"、"推文"时使用此 skill。
---

# /article 微信公众号文章创作命令

## 强制执行声明

**你正在执行微信公众号文章创作任务。你必须使用工具（MCP 工具、Write、Bash、TaskCreate 等）完成完整的创作流水线。**

**禁止直接用文字回答用户的主题问题。** 你不是在回答问题，你是在创作一篇微信公众号文章。如果你直接输出文字回答而没有使用任何工具，说明你理解错了任务。

用户输入 `/article` 后面的内容是创作主题，不是让你回答的问题。

---

## 必须执行的步骤

按顺序执行以下步骤。每一步都必须调用对应的工具，不能跳过。

### Phase 1: 信息收集

### 步骤 1：获取频道信息与工作目录

**频道选择（必须先完成，再调用频道 API）：**

- 检查 `$ANBANWRITER_DEFAULT_CHANNEL` 环境变量，非空则直接使用
- 否则调用 `list_channels(platform="article")`，**仅根据** `name`、`positioning`、`keywords` 语义匹配或让用户选择 → `$CHANNEL_ID`
- **⚠️ 禁止基于 API 可用性选择频道**：不要对多个频道调用 `get_channel_profile`/`list_published_articles` 来评估哪个"可用"。频道选择仅依据 `list_channels` 返回的 `name`、`positioning`、`keywords`。选定频道后，即使后续 API 调用返回错误也不得切换到其他频道

**频道选定后，仅对 `$CHANNEL_ID` 调用：**

- `get_channel_profile(channel_id="$CHANNEL_ID", scope="article")` → 获取账号定位、受众、写作风格
- `list_drafts(channel_id="$CHANNEL_ID")` 和 `list_published_articles(channel_id="$CHANNEL_ID")` → 已有文章标题（如返回错误可忽略，用空列表继续）
- `prepare_workspace(content_type="articles", task_id=TASK_ID)` → 工作目录路径 `$DIR`
- Bash 执行 `mkdir -p "$DIR"` 创建目录

### 步骤 2：选题研究

使用 `topic-research` skill：
- 结合账号关键词和用户需求搜索热门话题
- 生成文章大纲
- 创建 `$DIR/context-brief.md`，记录用户原始需求、频道定位、关键词、目标受众、历史文章避重结论、选题理由，以及每个 `##` 章节的上下文锚点
- 保存为 `$DIR/01-research.md`、`$DIR/02-outline.md`、`$DIR/context-brief.md`

### Phase 2: 内容创作

### 步骤 3：撰写文章

使用 `content-writing` skill：
- 基于大纲和 `$DIR/context-brief.md` 输出 Markdown 格式文章
- 每个 `##` 章节必须绑定 `context-brief.md` 中至少 1 个上下文锚点
- 每个章节必须包含具体素材（案例、场景、比喻、数据、人物、冲突或操作细节）
- **写作时不需要插入配图占位符**（配图由步骤 7 专门处理）
- 保存为 `$DIR/03-article.md`

### 步骤 4：AI 去痕与合规检查

使用 `content-writing` skill：
- 先执行 AI 去痕（`gentle` 模式）
- 再执行违禁词合规检查
- 创建 `$DIR/content-quality-report.md`，检查用户需求覆盖、账号定位一致性、历史文章差异、章节实质内容、研究结论引用、AI 套话风险
- 任一检查项不通过时，必须回到步骤 3/4 重写；不得进入 SEO、视觉或发布阶段
- 保存为 `$DIR/04-article-final.md` 和 `$DIR/content-quality-report.md`

### Phase 3: SEO 与视觉

### 步骤 5：SEO 优化

使用 `seo-optimization` skill：
- 优化标题、关键词、摘要
- 将优化后的标题和摘要保存为 `$DIR/seo-result.md`，供发布前总验收和草稿发布使用

### 步骤 6：封面设计与配图规划

使用 `article-visual-design` skill：
- **6a: 三维风格分析** — 基于账号定位 + 内容主题 + 受众确定视觉风格（不使用 writer YAML 的 `cover_style`/`cover_prompt`）
- **6b: 生成封面** — 从零构建封面 prompt，调用 `generate_image(channel_id="$CHANNEL_ID", prompt=封面提示词, image_type="cover", output_path="$DIR/cover.png", task_id="$TASK_ID", size="2.35:1")`
- 调用 `upload_image(channel_id="$CHANNEL_ID", file_path="$DIR/cover.png")` → 获取 `media_id`
- **6c: 创建配图规划** — 逐章分析文章，创建 `$DIR/image-plan.md`；每张图必须包含 `chapter_title`、`core_point`、`source_excerpt`、`visual_subject`、`composition_type`、`prompt_strategy`
- 记录 `$VISUAL_STYLE`、`$COLOR_PALETTE`、`$COVER_PATH`

### 步骤 7：配图设计与生成

使用 `article-visual-design` skill：

- **7a: 生成内容配图** — 按 `$DIR/image-plan.md` 逐章生成，所有配图使用 `ref_image_path="$DIR/cover.png"` 保持风格一致
- **7b: 质量验证** — 对照 `image-plan.md` 和 `images.json` 检查文件完整性、风格一致性、视觉多样性、内容关联性、审计字段完整性
- **7c: 保存结果** — 覆盖写回 `$DIR/04-article-final.md`，保存 `$DIR/images.json`；每条记录包含 `chapter_title`、`composition_type`、`visual_subject`、`prompt_source_excerpt`、`ref_image_path`、`image_type`、`quality_status`

### Phase 4: 组装发布

### 步骤 8：HTML 转换

使用 `content-writing` skill：
- 读取已插入 CDN 图片链接的 `$DIR/04-article-final.md`
- 将文件内容作为 `markdown` 参数传给 `convert_markdown(channel_id="$CHANNEL_ID", markdown=文章全文, theme=可选主题)`
- `$DIR/images.json` 仅作为审计记录，`convert_markdown` 不会读取该文件
- 保存为 `$DIR/05-article.html`

### 步骤 9：发布前总验收

创建 `$DIR/final-review.md`，检查内容质量、视觉质量、SEO、合规、HTML、草稿字段。

任一硬性项失败时停止发布：内容不贴题、缺少封面 `media_id`、章节缺图、未使用 `ref_image_path`、SEO 标题/摘要缺失、HTML 转换失败。

### 步骤 10：草稿发布

使用 `article-publishing` skill：
- 从 `$DIR/seo-result.md` 读取优化后的标题和摘要
- 创建 `draft.json`（title 使用 SEO 优化标题，digest 使用 SEO 优化摘要）
- 仅当 `$DIR/final-review.md` 全部通过时，发布到草稿箱：`publish_draft`

---

## 质量标准

- 文章至少 3 个二级标题，结构清晰
- **上下文锚定**：`context-brief.md` 存在，每个 `##` 章节至少绑定 1 个上下文锚点
- **内容质量闸门**：`content-quality-report.md` 全部通过后才能进入 SEO 与视觉阶段
- 封面图必须成功生成并上传，视觉风格与账号定位匹配
- **配图规划**：`image-plan.md` 在生成配图前创建
- **配图与内容关联**：每个配图提示词必须包含对应章节的具体概念
- **参考链一致**：所有内容配图使用 `ref_image_path="$DIR/cover.png"`
- **视觉审计完整**：`images.json` 记录 ref_image_path、composition_type、chapter_title 等审计字段
- **图文并茂**：每个 `##` 章节至少一张配图
- **视觉多样性**：3 张以上配图使用 3 种以上不同构图类型
- 无明显 AI 痕迹，无违禁词
- **发布前总验收**：`final-review.md` 全部通过后才能创建草稿
- 草稿使用 SEO 优化后的标题和摘要

---

## 任务追踪要求

流程启动时用 TaskCreate 创建任务列表，每个步骤对应一个任务。开始前 `TaskUpdate status → in_progress`，完成后 `TaskUpdate status → completed`。报告进度示例：`[3/10] 文章撰写完成 → $DIR/03-article.md`

---

## 子技能调用顺序

| 步骤 | 调用技能 | 产出 |
|------|----------|------|
| 1 | 直接 MCP 调用 | `$CHANNEL_ID`, `$DIR` |
| 2 | `topic-research` | `01-research.md`, `02-outline.md`, `context-brief.md` |
| 3 | `content-writing` | `03-article.md` |
| 4 | `content-writing` | `04-article-final.md`, `content-quality-report.md` |
| 5 | `seo-optimization` | `seo-result.md` |
| 6 | `article-visual-design` | `cover.png`, `image-plan.md` |
| 7 | `article-visual-design` | `images.json`, 更新 `04-article-final.md` |
| 8 | `content-writing` | `05-article.html` |
| 9 | 直接检查 | `final-review.md` |
| 10 | `article-publishing` | 微信草稿箱 |
