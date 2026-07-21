---
name: article
description: 'Use when 微信公众号图文文章全自动创作。用户提到"写文章"、"写一篇"、"发文章"、"公众号文章"、"推文"时使用此 skill。'
---

# /article 微信公众号文章创作命令

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## 强制执行声明

**你正在执行微信公众号文章创作任务。你必须使用工具（MCP 工具、Write、Bash、TaskCreate 等）完成完整的创作流水线。**

**禁止直接用文字回答用户的主题问题。** 你不是在回答问题，你是在创作一篇微信公众号文章。如果你直接输出文字回答而没有使用任何工具，说明你理解错了任务。

用户输入 `/article` 后面的内容是创作主题，不是让你回答的问题。

---

## 图片运行控制前置（硬性）

公众号文章的封面图与正文配图由 user message 的结构化运行控制 `article_image_mode` 决定。若该键缺失，按 `cover_and_content` 兼容旧任务；不要扫描自然语言禁令来推断开关。所有质量标准、成功标准、发布前验证和失败判定都必须先判断图片模式：

- `cover_and_content`：封面和正文配图都开启，按完整视觉流程执行。
- `cover_only`：不得生成 `image-plan.md` / `images.json` / 正文 `<img>`；模板 `image_count.min` 不生效；不得把章节缺图、缺 `image-plan.md`、缺 `images.json` 判为失败。
- `content_only`：不得生成 `cover.png` / `cover-prompt.md`；草稿不带 `thumb_media_id`；不得把缺封面或缺 `media_id` 判为失败；正文图不得把 `ref_image_path` 指向不存在的 `$DIR/cover.png`。
- `text_only`：纯文字文章，不生成任何图片，`visual-rhythm-plan.md` 可存在但所有 `image_url=null`，草稿不带 `thumb_media_id`，`final-review.md` 记录「未生成封面，公众号后台可能不显示封面/需手动设置」。

**仅在对应图片模式开启该产物时**，封面、正文配图、vision 校验、正文图片互不相同等图片相关要求才是硬性项；关闭时跳过且不计为失败。
## 必须执行的步骤

按顺序执行以下步骤。每一步都必须调用对应的工具，不能跳过。

### Phase 1: 信息收集

### 步骤 1：获取项目信息与工作目录

**项目选择（必须先完成，再调用项目 API）：**

- 检查 `$ANBAN_DEFAULT_PROJECT` 环境变量，非空则直接使用
- 否则调用 `list_projects(platform="article")`，**仅根据** `name`、`positioning`、`keywords` 语义匹配或让用户选择 → `$PROJECT_ID`
- **⚠️ 禁止基于 API 可用性选择项目**：不要对多个项目调用 `get_project_profile`/`list_published_articles` 来评估哪个"可用"。项目选择仅依据 `list_projects` 返回的 `name`、`positioning`、`keywords`。选定项目后，即使后续 API 调用返回错误也不得切换到其他项目

**项目选定后，仅对 `$PROJECT_ID` 调用：**

- `get_project_profile(project_id="$PROJECT_ID", scope="article", task_id="$TASK_ID")` → 获取账号定位、受众、风格维度。**同时解析视觉维度的权威来源**：`$VISUAL_STYLE_CONFIGURED` = profile 的 `visual_style` 字段、`$VISUAL_STYLE_SOURCE` = `visual_style_source`（task / project）。`task_id` 让服务端按任务级覆盖解析（`task > project` 两层），不传则只拿到 project 级信息。**务必区分两个易混字段**：顶层 `author` 是公众号**署名**（步骤 10 发布时原样填入 `draft.json` 的 author，空则省略）；顶层 `writer` 是**写作风格资源 key**（驱动正文语气）。二者用途不同，**绝非署名、绝不混用**。写作风格头像/昵称只是 Studio 展示元数据，不会出现在 MCP profile 中。
- `list_drafts(project_id="$PROJECT_ID")` 和 `list_published_articles(project_id="$PROJECT_ID")` → 已有文章标题（如返回错误可忽略，用空列表继续）
- `prepare_workspace(content_type="articles", task_id=TASK_ID)` → 工作目录路径 `$DIR`
- Bash 执行 `mkdir -p "$DIR"` 创建目录

### 步骤 2：选题研究

using the topic-research skill 结合账号关键词和用户需求搜索热门话题，创作文章大纲。产出：
- `$DIR/01-research.md` — 选题分析和关键词
- `$DIR/02-outline.md` — 文章大纲（≥3 个二级标题）

### Phase 2: 内容创作

### 步骤 3：撰写文章

using the content-writing skill 基于账号定位和大纲输出 Markdown 格式文章。**写作时不需要插入配图占位符**（配图由步骤 7 专门处理）。产出：
- `$DIR/03-article.md` — 完整文章内容

### 步骤 4：AI 去痕与合规检查

using the content-writing skill 先执行 AI 去痕（`humanizer` skill，无强度档位），再执行公众号文章预检、导流风险检查、违禁词合规检查。这是自动流水线步骤：不得调用 `AskUserQuestion`；没有写作样本时按账号定位、上下文锚点和当前稿件语气直接改写。改写必须覆盖原文全部信息点、保持段落和字数量级、保留人称/情绪/具体细节，且不得引入新的违禁词或导流风险。审阅未通过代表内容待调整，必须自动回步骤 3/4 改写并重新预检。产出：
- `$DIR/04-article-final.md` — 检查后的文章
- `$DIR/content-quality-report.md` — 含导流风险、内容完整性、标题摘要一致性、互动合规、违禁词、AI 痕迹检查；无待调整项后才能进入 SEO 与视觉

### Phase 3: SEO 与视觉

### 步骤 5：SEO 优化

using the seo-optimization skill 优化标题、关键词、摘要。将优化后的标题和摘要保存为 `$DIR/seo-result.md`，供步骤 9/10 使用。

### 步骤 6：模板选择、节奏规划、封面生成（带视觉校验）、配图规划

using the article-visual-design skill 完成以下子步骤。详细规范见 `skills/article-visual-design/SKILL.md` 与 `skills/article-visual-design/references/{cover,content,rhythm}.md`。

#### 6a：选择文章类型模板（配置优先风格）

公众号"模板"由三个**正交**维度组成：图片视觉（`visual_style`）、写作者（`writer`）、排版样式（`theme`）。三者各自独立解析，互不推导——**写作风格绝不决定图片视觉**。

读取 `$DIR/03-article.md`（或 `04-article-final.md`），根据结构特征匹配模板（自动决策，不询问用户）：

| 特征 | 模板 | YAML 路径 |
|------|------|-----------|
| 含"3 个/5 种/N 条" + 并列项 | `listicle` | `templates/article/listicle.yaml` |
| 按步骤序号组织（步骤 1 / step N） | `tutorial` | `templates/article/tutorial.yaml` |
| 情节弧、场景、人物时间线 | `story-narrative` | `templates/article/story-narrative.yaml` |
| 其他（深度观点、评论、分析） | `long-form-essay`（默认） | `templates/article/long-form-essay.yaml` |

加载模板 YAML，提取 `rhythm`（slot 规则）、`image_count`（min/max）、`modules.preferred`（可用 layout module）、`composition_guidance`（构图指南）。记录 `$TEMPLATE_NAME` / `$TEMPLATE_PATH`。特征模糊时优先选 `long-form-essay`。

#### 6b：创建视觉节奏规划

读取 `$DIR/03-article.md`，对每个 `##` 章节分配 slot，创建 `$DIR/visual-rhythm-plan.md`：按模板 `rhythm` 规则把每个 `##` 映射到 `hero` / `section_opener` / `inline_detail` / `footer`；按 `composition_guidance` 为每个图 slot 选 `composition_type`（3+ 图时用 3+ 种构图，`listicle` 模板豁免）；从 `modules.preferred` 选 module；每个 slot 写 1 句 `chapter_anchor`。模板自检：hero 有且仅有 1 个、section_opener 数量符合模板、footer 符合必选/可选规则。模板格式和完整示例见 `skills/article-visual-design/references/rhythm.md`。

#### 6c：视觉风格确定（配置优先，分析兜底）

视觉风格**优先**取自步骤 1 已解析的任务字段（`task > project`）：
- 若 `$VISUAL_STYLE_CONFIGURED` 非空 → 它是**权威视觉锚点**。以它为 `$VISUAL_STYLE` 的核心，三维分析（账号定位 / 内容主题 / 受众）只做**补充细化**（配色、情绪、构图），**绝不可覆盖或偏离**配置的视觉方向。例如配置了"温暖自然的生活摄影"，就不得生成维多利亚木刻/黑白版画等冲突风格。
- 若 `$VISUAL_STYLE_CONFIGURED` 为空（所有层级都未配置视觉）→ 执行完整三维分析兜底。
- **不从 writer YAML 推视觉**（writer 仅决定文字风格，已不再携带任何视觉/封面字段）。

产出 `$VISUAL_STYLE`（含配置锚点 + 细化方向）/ `$COLOR_PALETTE` / `$MOOD` / `$VISUAL_STYLE_SOURCE`。映射关系见 `skills/article-visual-design/references/cover.md`。

#### 6d：生成封面（委托 article-cover-design skill，生成与上传原子化）

封面是全篇风格锚点，更是标题-摘要-正文-用户画像的点击承诺载体。**封面设计已独立成稿**——using the `article-cover-design` skill：硬编码官方比例（900×383 / 2.35:1）、中心安全区构图、受控文字策略、`cover_strategy`、三选一概念评审、`visual_quality_scorecard` 与 `cover_effectiveness_scorecard` 双评分卡把关。本步骤只交代与本流水线的衔接：

1. 从 `$DIR/context-brief.md`、`$DIR/seo-result.md`、digest 和 `$DIR/04-article-final.md` 提取最终标题、目标读者、读者痛点/任务、文章承诺、正文证据和最强视觉素材。
2. 交给 `article-cover-design` skill 先写 `cover_strategy`：`target_reader`、`reader_pain_or_job`、`article_promise`、`content_proof_points`、`click_trigger`、至少 3 个 `cover_concept_candidates`、`selected_cover_concept`。
3. 三选一概念评审必须先过 `generic_swap_test`、`promise_proof_test`、`audience_motivation_test`；任何“换到其他方法论文章也成立”的封面概念不得进入生成。
4. 调用 `generate_image`（**`upload_to_cdn=true` 让生成与上传原子化**：同一调用内完成生成→裁剪→校验→压缩→上传微信 CDN，直接返回 `media_id` + `wechat_url`）：
   ```
   generate_image(
     project_id=$PROJECT_ID,
     prompt=<article-cover-design skill 构建的封面提示词>,
     image_type="cover",
     output_path="$DIR/cover.png",
     task_id=$TASK_ID,
     size="21:9",
     verify_with_vision=true,
     verification_prompt=<公众号封面质量评分卡 + 封面有效性评分卡>,
     upload_to_cdn=true
   )
   ```
   - `size="21:9"` 是生成提示比（Volcengine 支持的最近比）；**服务端按 `platform=article + image_type=cover` 把成品精确裁到 900×383 并像素断言**——微信零裁剪，告别「需要手动裁剪的纯图」。
5. 校验不过 → 先改 `cover_strategy` / `selected_cover_concept` / `cover_hook` / `visual_metaphor` / `thumbnail_strategy`，再锐化 prompt 重试，最多 3 次；仍不过请求用户协助。vision JSON 类型不匹配、校验超时、缺 `cover_effectiveness_scorecard`、三个硬测试任一失败、或 `cover_effectiveness_scorecard.overall_pass=false` 时，不得手动 `upload_image` 后继续发布。
6. 从返回值取 `media_id`（发布草稿的 thumb）+ `wechat_url`（`$COVER_CDN_URL`，**仅供 thumb_media_id 来源识别**，不得复用为正文 `<img src>`）。若返回 `upload_error`（生成成功但上传失败），用 `upload_image(file_path="$DIR/cover.png")` 单独重传即可，**无需重新生成**；但只有校验通过的封面才允许重传。
7. 记录 `$COVER_PATH="$DIR/cover.png"`、`$COVER_MEDIA_ID`、`$COVER_CDN_URL`（供步骤 7/8/10 使用）。
8. **原子写 `$DIR/cover-prompt.md`**（先写 `$DIR/.cover-prompt.md.tmp` → `fsync` → `rename` 覆盖）：完整记录封面生成决策，必须含 `cover_strategy`、`cover_concept_candidates`、`selected_cover_concept`、`visual_quality_scorecard`、`cover_effectiveness_scorecard`、`generic_swap_test`、`promise_proof_test`、`audience_motivation_test` 和 vision 校验结果。仅有旧的 6 维 vision 全 high 不得通过。

详细推导链、评分卡模板、迭代策略见 `skills/article-cover-design/SKILL.md` 与 `skills/article-cover-design/references/cover-effectiveness.md`；三维风格方向参考见 `skills/article-visual-design/references/cover.md`。

#### 6e：创建配图内容规划（升级 schema）

按 `$DIR/visual-rhythm-plan.md` 中每个需要图的 slot，规划配图内容，写入 `$DIR/image-plan.md`。**新 schema 强制要求**：
- `visual_brief`：1-2 句白话"这张图必须画什么"
- `required_entities`：必须出现的具体物体列表（vision 校验依据）
- `must_match_excerpts`：章节中支撑这些实体的原句
- 沿用字段：`slot_id`、`section_index`、`chapter_title`、`core_point`、`composition_type`、`source_excerpt`、`prompt_strategy`

详细正反例和填写规范见 `skills/article-visual-design/references/content.md`。

**产出**：`$DIR/visual-rhythm-plan.md`, `$DIR/cover-prompt.md`, `$DIR/cover.png`, `media_id`, `$COVER_CDN_URL`, `$VISUAL_STYLE`/`$COLOR_PALETTE`/`$MOOD`/`$VISUAL_STYLE_SOURCE`, `$TEMPLATE_NAME`, `$DIR/image-plan.md`

### 步骤 7：配图生成（带 vision 校验循环）

using the article-visual-design skill 按 `$DIR/visual-rhythm-plan.md` 中 slot 顺序生成。每个需要图的 slot 执行：

#### 7a：构建 prompt 并生成（生成与上传原子化）

基于 image-plan.md 中对应 slot 的 `visual_brief` + `required_entities` + `composition_type` 叠加 `$VISUAL_STYLE` / `$COLOR_PALETTE` 构建 prompt，并构建 vision 校验 prompt（含 `visual_brief` + `required_entities`，详见 `skills/article-visual-design/references/content.md`）。调用（**`upload_to_cdn=true` 让生成与上传原子化**：校验通过才上传，返回值直接带 `wechat_url`/`media_id`）：

```
generate_image(
  project_id=$PROJECT_ID,
  prompt=<构建的 prompt>,
  image_type="content",
  output_path="$DIR/img_N.png",
  task_id=$TASK_ID,
  ref_image_path=<封面开关开启时 "$DIR/cover.png"；封面关时省略或链到首张已生成图>,
  size=<按 slot 固定：section_opener/信息图用 "4:3"，inline_detail 用 "1:1">,
  verify_with_vision=true,
  verification_prompt=<vision 校验 prompt>,
  upload_to_cdn=true
)
```

**关键**：公众号正文配图不依赖项目级/任务级 image ratio；每次 `generate_image` 必须显式传 `size`（section_opener/信息图用 `size="4:3"`，inline_detail 用 `size="1:1"`）。封面+配图均开启时，`ref_image_path` 用 `$DIR/cover.png`（**风格锚点/参考输入，不是把封面图当作正文图复用**）；封面关·配图开时不传 `ref_image_path` 或链到首张已生成图，严禁指向不存在的 `$DIR/cover.png`。每张正文图的 `<img src>` 必须是**该图独立生成并上 CDN 后得到的 `wechat_url`**——**严禁**把封面 `$COVER_CDN_URL` 直接填入正文任何 `<img src>`，也**严禁**多张正文图共用同一个 `wechat_url`；否则会触发服务端"正文全图相同"硬拦截导致发布失败。**不再有独立的批量 `upload_image` 阶段**——每张图生成的瞬间即上 CDN。

#### 7b：vision 校验与失败重试

解析 `generate_image(verify_with_vision=true)` 返回的 `verification` 字段。**字段名是服务端归一化后的 `passed`/`score`/`missing_entities`/`notes`/`raw`，不是 LLM 原始 JSON 的 `overall_pass`/`relevance_score`**（服务端已为你做归一化）。若 server 未自动校验，agent 单独调 `analyze_image` 并按 `article-visual-design` skill 的容错解析规则处理返回文本。失败重试策略：
- `passed=true` → 通过，继续下一 slot
- `passed=false` + `score` 为 medium/low → 用 `notes` 中的 `sharper_prompt_hint` 锐化 prompt 重试（最多 2 次，共 3 次尝试）
- 3 次仍失败 → 标记 `quality_status=failed`，继续后续 slot

**锐化 prompt 策略**：在 prompt 开头加 `MUST CONTAIN: ` + `verification.missing_entities` 列表；把 `visual_brief` 改写得更具体（加入材质、颜色、方位、数量）；加强主体权重 "MAIN SUBJECT: <具体物体>"。

#### 7c：记录并立即原子落盘（生成即持久化）

`generate_image(upload_to_cdn=true)` 返回时，`wechat_url`/`media_id` 已就绪（或返回 `upload_error`）。**无需调用 `upload_image`**——立即把这张图写回 `$DIR/images.json`，使中断最多丢失"正在生成的那一张"，已上 CDN 的全部安全。

- 从 `generate_image` 返回值取 `wechat_url` → 写入 `url` 字段；若返回 `upload_error`（生成成功但上传失败），记到 `upload_error` 字段并用 `upload_image(file_path="$DIR/img_N.png")` 单独重传（**不重新生成**），重传成功后再落盘。
- **原子写** `$DIR/images.json`：先写 `$DIR/.images.json.tmp` → `fsync` → `rename` 覆盖。**绝不要"攒齐所有图再一次性写"**——每张图返回即落盘。
- 每条记录必须含：`index`、`slot_id`、`section_index`、`image_type`、`chapter_title`、`composition_type`、`visual_brief`、`required_entities`、`must_match_excerpts`、`prompt`、`verification`（passed/score/missing_entities/notes/raw，**直接来自服务端返回值**）、`verification_audit`（attempt_count/sharper_prompt_history，**agent 维护**）、`ref_image_path`、`file_path`、`url`、`wechat_url`、`media_id`、`quality_status`。

#### 7d：插入到文章并回填 rhythm-plan

按 slot 的 `slot_id` + `section_index` 把 `![描述](CDN_URL)` 插入 `$DIR/04-article-final.md`：
- `section_opener`：紧跟 `## 章节标题` 之后
- `inline_detail`：在 `after_paragraph_index` 指定的段落之后
- `hero`：在文章开头（如有 hero module 文字，则在 hero module 之后）

把所有 CDN URL 回填到 `$DIR/visual-rhythm-plan.md` 的 `layout_plan` JSON 块中。

#### 7e：质量验证

生成完成后执行检查：
- [ ] **节奏完整性**：`visual-rhythm-plan.md` 中每个 `##` 都映射到一个 slot
- [ ] **模板一致性**：所选模板的 rhythm 规则被遵守（listicle 的 inline_detail 必须为空、tutorial 的 footer 必填等）
- [ ] **文件完整性**：所有图片文件存在且可访问
- [ ] **风格一致性**：封面+配图均开启时，`images.json` 中所有内容图 `ref_image_path="$DIR/cover.png"`；封面关·配图开时无 `ref_image_path` 或链首图，且不得指向不存在的 `$DIR/cover.png`
- [ ] **视觉多样性**：3+ 配图使用 3+ 种不同 `composition_type`（`listicle` 模板豁免）
- [ ] **Vision 校验通过率**：至少 80% 的内容图 `verification.passed=true`
- [ ] **审计完整性**：`images.json` 每条含 `visual_brief` / `required_entities` / `must_match_excerpts` / `verification` / `slot_id` / `section_index` / `wechat_url` / `media_id`
- [ ] **CDN 持久化**：`images.json` 每条都有非空 `wechat_url`（即每张图已上微信 CDN）
- [ ] **正文图片互不相同**：`images.json` 中所有内容图的 `wechat_url` 两两不同，且没有任何一张等于封面 `$COVER_CDN_URL`（封面只能用于 `thumb_media_id`，**不得复用为正文图**）；服务端 `publish_draft` 会硬拦截"正文 ≥2 图但唯一 URL==1"的草稿，配图失败时宁可缺图降级也不得用封面/他图顶替

未通过检查时按问题类型处理：单图失败降级、节奏/模板违规回步骤 6a/b、Vision 通过率 <80% 回步骤 6e。**超过一半章节配图失败则暂停流程请求用户协助**。

**产出**：更新后的 `$DIR/04-article-final.md`（含 CDN 图片链接）、`$DIR/images.json`、回填后的 `$DIR/visual-rhythm-plan.md`

### Phase 4: 组装发布

### 步骤 8：HTML 渲染（render_template）

using the content-writing skill 渲染 HTML。**不再使用 `convert_markdown` 自由发挥**，改用 `render_template` MCP 工具按节奏计划确定性渲染：

```
render_template(
  project_id=$PROJECT_ID,
  markdown=<$DIR/04-article-final.md 全文>,
  layout_plan=<$DIR/visual-rhythm-plan.md 中的 layout_plan JSON 块>,
  theme=<可选，默认用 project theme>
)
```

`render_template` 服务端按 `layout_plan` 中的 slot 顺序确定性渲染 HTML 骨架，图片占位符按 slot 位置精确插入，layout module 按 `module_vars` 渲染。返回 `{ html, slots_rendered }`。把返回的 `html` 字段保存为 `$DIR/05-article.html`，把 `slots_rendered` 写入 `$DIR/final-review.md` 作为渲染审计。

> **图片单一路径（避免重复 `<img>`）**：`render_template` 按 `layout_plan` 的 slot 自动注入配图 `![alt](image_url)`。若传入的 `markdown`（`04-article-final.md`）里已内联同一张图，服务端**按 URL 去重**——同一 URL 只渲染一个 `<img>`，**无需手动 Edit 清理重复 img**。配图位置以 `layout_plan` 为单一权威路径；`04-article-final.md` 的内联图仅作人工可读 markdown 产物。

> 注：`convert_markdown` 仅在 `render_template` 不可用的旧版 server 上作为兼容降级路径，新流水线主路径必须用 `render_template`。

产出：
- `$DIR/05-article.html`（含 CDN 图片 + 结构化 slot）

### 步骤 9：发布前总验收

创建 `$DIR/final-review.md`，汇总并判定以下硬性项（**图片开关守卫**：封面/配图相关项在对应开关关闭时跳过且不计为失败；纯文字文章时额外记录「未生成封面，公众号后台可能不显示封面/需手动设置」）：
- 内容质量：`content-quality-report.md` 全部通过，文章贴合用户需求、账号定位和上下文
- **导流风险**：无二维码、联系方式、外链 URL、跳小程序、其他公众号/服务号/视频号、进群、加微信、关注/点赞/留言/转发领资料、回复关键词或多重跳转交易；文章在当前页面提供完整信息
- **模板与节奏**：`visual-rhythm-plan.md` 存在；所选模板的 rhythm 规则被遵守；每个 `##` 章节映射到 slot；封面/配图开启时 `layout_plan` JSON 块的对应 `image_url` 已用 CDN URL 回填（关闭时对应 slot `image_url=null`）
- **配图内容贴切**（仅正文配图开启时）：`image-plan.md` 每张图含 `visual_brief` + `required_entities` + `must_match_excerpts`；`images.json` 中至少 80% 的内容图 `verification.passed=true`
- 视觉一致性（封面开关开启时）：封面存在且已上传获得 `media_id`；封面+配图均开启时所有内容图 `ref_image_path="$DIR/cover.png"`；封面关·配图开时内容图不传 `ref_image_path` 或链首图
- SEO：`seo-result.md` 包含优化后的标题和摘要
- 合规：违禁词和平台合规检查无高风险未处理项
- HTML：`05-article.html` 由 `render_template` 生成（记录在 `final-review.md` 的 `render_audit` 段），图片链接有效，内容未超过平台限制
- 草稿字段：title、digest、content 可从前序产物读取；`thumb_media_id` 仅封面开关开启时要求可读取

**审阅闭环**：任一项审阅未通过时标记为待调整，自动回到正文、标题摘要、互动诱因、视觉 prompt 或 HTML 渲染步骤修订，并重新写入 `final-review.md`。全部通过前不得调用 `publish_draft`。

### 步骤 10：草稿发布

using the article-publishing skill 创建 `draft.json` 并发布：
- `title`：步骤 5 优化后的标题（从 `$DIR/seo-result.md` 读取）
- `content`：步骤 8 的 HTML
- `digest`：步骤 5 优化后的摘要
- `thumb_media_id`：**仅当封面开关开启时**填步骤 6 的封面 `$COVER_MEDIA_ID`；封面开关关闭时一律不带 `thumb_media_id`，并在 `final-review.md` 记录提示
- `author`：**仅**取自步骤 1 `get_project_profile` 顶层 `author`（公众号署名，原样填入 `draft.json` 的 `author` 键；空则省略，**禁用** `writer` 顶替——见 article-publishing skill「作者字段来源」）

仅当 `$DIR/final-review.md` 所有硬性项通过时，调用 `publish_draft` 发布到草稿箱。产出：
- `$DIR/draft.json`

---

## 自动决策原则

**全程零用户交互**。所有决策点自动选择最优解（项目选择是唯一例外——多个项目无法匹配时需让用户选择）：

| 决策点 | 自动策略 |
|--------|----------|
| **选题方向** | 结合账号关键词 + 用户需求 + 历史文章去重，自动选 Top 1 |
| **文章结构** | 根据选题类型自动匹配结构模板（教程/清单/故事/分析） |
| **视觉模板** | 根据文章结构特征自动选 `templates/article/*.yaml`（listicle / tutorial / story-narrative / long-form-essay），模板定义节奏、配图数量、layout module |
| **视觉节奏** | 模板选定后，自动把每个 `##` 映射到 slot（hero / section_opener / inline_detail / footer），写入 `visual-rhythm-plan.md` |
| **配图内容贴切** | 每张图提取 `visual_brief` + `required_entities` + `must_match_excerpts`，生成后强制 vision 校验，失败锐化 prompt 重试 |
| **视觉风格** | **配置优先**：优先取自任务解析的 `visual_style` 字段（`get_project_profile` 的 `visual_style`/`visual_style_source`，按 `task > project` 解析）；配置为空时由账号定位+内容主题+受众三维分析兜底；**不使用 writer YAML 的 `cover_style`/`cover_prompt`**（writer 仅决定文字风格）。封面+配图均开启时配图通过 `ref_image_path="$DIR/cover.png"` 保持一致；封面关·配图开时不传 `ref_image_path` 或链首图 |
| **HTML 渲染** | 用 `render_template`（带 `layout_plan`）确定性渲染，不再用 `convert_markdown` 自由发挥 |
| **SEO 优化** | 自动提取关键词，生成标题/摘要/标签，结果用于草稿发布 |
| **AI 去痕** | 自动检测并移除 AI 写作模式（33 类，详见 `humanizer` skill） |
| **文章预检** | 自动检查导流风险、内容完整性、标题摘要一致性和互动合规；审阅未通过时自动调整并复审 |
| **错误处理** | 自动重试 + 降级，非关键步骤跳过继续 |

## 硬性规则（违反即发布失败或质量不达标）

- **禁止把封面 wechat_url 当作正文 img src**：封面 `$COVER_CDN_URL` 只能用于 `thumb_media_id`，正文每张图必须独立生成上 CDN。
- **禁止多张正文图共用同一 wechat_url**：服务端 `publish_draft` 会硬拦截"正文 ≥2 图但唯一 URL==1"的草稿。
- **正文全图相同时不得发布**：服务端发布前会做图片去重硬拦截；配图失败时宁可缺图降级，也不得用封面/他图顶替。
- **封面必须 vision 与有效性校验通过**才可作为 `thumb_media_id`（仅封面开关开启时）；缺 `cover_strategy`、缺 `cover_effectiveness_scorecard`、仅有旧的 6 维 vision 全 high、或 `cover_effectiveness_scorecard.overall_pass=false` 均不得发布。
- **超过一半章节配图失败**：暂停流程，请求用户协助（不得用降级顶替方式强行凑齐）。
- **Vision 校验通过率 < 80%**：回到步骤 6e 检查 prompt 构建逻辑，不得直接发布。
- **HTML 主路径必须用 `render_template`**（带 `layout_plan`）；`convert_markdown` 仅作旧版 server 兼容降级，不得作主渲染路径。
- **导流风险必须清零**：不得出现二维码、联系方式、外链 URL、扫码进群、加微信、关注/点赞/留言/转发领资料、回复关键词、跳小程序/其他账号或多重跳转交易。发现后自动调整，不作为任务失败。

## MCP 工具使用规则

- **必须使用 MCP 工具调用服务端接口**（如 `list_projects`、`generate_image`、`render_template` 等）
- **禁止编写 JavaScript/Node.js/Python 脚本或创建自定义 HTTP 客户端来调用 MCP 接口**
- **如果 MCP 工具不可用或调用失败，立即停止并报告错误**，不要尝试自行发现、探测或创建替代连接方式
- **`prepare_workspace(content_type="articles", task_id=$TASK_ID)` 是唯一工作目录工具**，返回 `$DIR` 后由 agent 本地创建目录。所有产物始终保留在 `$DIR`；任务完成前不得移动、复制或按标题重命名成果目录。`task_files`、`execution_id` 与 OSS 持久化由服务端维护各自的登记、执行和版本边界。

## 质量标准

- 有标题和清晰结构（至少 3 个二级标题）
- 字数符合用户要求或文章类型的合理长度
- 无明显 AI 痕迹，无违禁词
- 无导流风险，内容完整，标题摘要一致，互动诱因合规
- 有价值、有见地、语言自然
- **模板驱动节奏**（硬性要求）：从 `templates/article/*.yaml` 加载模板，不得临时编造节奏
- **节奏规划完整**（硬性要求）：`visual-rhythm-plan.md` 存在且每个 `##` 都映射到 slot
- 封面图必须成功生成并上传（硬性要求，**仅封面开关开启时**），**vision 校验和 `cover_effectiveness_scorecard` 均通过**
- **封面策略闸门**（硬性要求，**仅封面开关开启时**）：`cover-prompt.md` 必须含 `cover_strategy`（`target_reader` / `reader_pain_or_job` / `article_promise` / `content_proof_points` / `click_trigger` / `cover_concept_candidates` / `selected_cover_concept`）、`visual_quality_scorecard`、`cover_effectiveness_scorecard`；`generic_swap_test`、`promise_proof_test`、`audience_motivation_test` 必须全过
- **配置优先风格匹配**（硬性要求）：`$VISUAL_STYLE` 优先取自 `get_project_profile` 的 `visual_style` 字段，配置为空时三维分析兜底；不使用 writer YAML 的 `cover_style`/`cover_prompt`
- **配图内容贴切**（硬性要求）：`image-plan.md` 每张图含 `visual_brief` + `required_entities` + `must_match_excerpts`，prompt 必须引用章节具体物体/比喻/案例（非通用描述）
- **Vision 校验闭环**（硬性要求）：每张内容图经过 `verify_with_vision`（或单独 `analyze_image`）校验；至少 80% `verification.passed=true`
- **参考链一致**（硬性要求，**仅封面+配图均开启时**）：所有内容配图使用 `ref_image_path="$DIR/cover.png"` 保持视觉一致；封面关·配图开时不传或链首图
- **正文配图开启时的图文并茂**：正文配图开启时，每个 `##` 章节至少一张配图（按模板 rhythm 规则）；正文配图关闭时不得判失败
- **视觉多样性**（硬性要求）：3 张以上配图时使用 3 种以上不同构图类型（`listicle` 模板可豁免）
- **结构化渲染**（硬性要求）：HTML 由 `render_template`（带 `layout_plan`）生成，不得用 `convert_markdown` 自由发挥
- **正文图片互不相同**（硬性要求）：所有正文内容图 `wechat_url` 两两不同，且无一张复用封面 `$COVER_CDN_URL`
- **发布前总验收**（硬性要求）：`final-review.md` 全部通过后才能创建草稿；审阅未通过 / 待调整项必须自动修订并复审
- **爆款审计硬闸门**（硬性要求）：缺 `viral-audit.md` 不得发布；视觉停留维度必须读取 `cover_strategy` 和 `cover_effectiveness_scorecard`，不得只凭"风格统一"给高分
- 草稿使用 SEO 优化后的标题和摘要

### 平台合规检查

合规检查由 skill `content-writing` 执行，关键要点：
- **封面图**：人物五官完整、无马赛克/播放标记、画质清晰
- **标题**：准确反映内容、无省略号隐藏关键信息
- **内容**：语言文明、无低俗擦边、无暴力宣扬
- **反导流**：无二维码、联系方式、外链跳转、站外交易或福利诱导互动

---

## 风险与缓解措施

| 风险 | 缓解措施 |
|------|----------|
| **选题与历史文章重复** | 自动跳过重复选题，选择次优候选 |
| **文章结构不清晰** | 自动匹配结构模板，确保至少 3 个二级标题 |
| **封面生成失败** | 重试两次（不同 prompt 措辞），仍失败则请求用户协助 |
| **封面 vision 校验未通过** | 重试一次（锐化 prompt），仍失败请求用户协助；不得用未通过 vision 的封面发布 |
| **配图提示词设计质量差** | 提示词必须引用章节具体内容，使用 ref_image_path 保持风格一致 |
| **单张配图生成失败** | 重试一次（更换提示词），仍失败则标记该章节缺图，继续后续章节 |
| **超过一半章节配图失败** | 暂停流程，请求用户协助 |
| **正文图片全图相同 / 复用封面 URL** | 服务端 `publish_draft` 硬拦截；为每个 slot 独立 `generate_image(upload_to_cdn=true)`，不得用封面/他图顶替 |
| **AI 去痕过度** | 由 `humanizer` skill draft→audit→final 改写，保留人称/情绪/细节等人味 |
| **违禁词检测误报** | 记录疑似词，人工复核标记，不自动删除 |
| **HTML 渲染失败** | 检查 Markdown 格式和 `layout_plan` JSON，修复后用 `render_template` 重试 |
| **草稿创建失败** | 检查 draft.json 格式和 media_id 有效性 |

---

## 成功标准

- [ ] 工作目录创建成功，`$DIR` 路径有效
- [ ] `01-research.md` 包含选题分析和关键词
- [ ] `02-outline.md` 包含清晰的文章结构（≥3 个二级标题）
- [ ] `03-article.md` 包含完整文章内容
- [ ] `04-article-final.md` 无 AI 痕迹，无违禁词
- [ ] `seo-result.md` 包含优化后的标题和摘要
- [ ] **`visual-rhythm-plan.md` 存在**，记录所选模板、slot 分配表、`layout_plan` JSON
- [ ] 封面图 `$DIR/cover.png` 存在且可访问，**vision 校验和 `cover_effectiveness_scorecard` 均通过**
- [ ] 封面图已上传，获得有效 `media_id`
- [ ] `$DIR/cover-prompt.md` 存在，含 `2.35:1` 比例、视觉风格来源（配置锚点优先）、`cover_strategy`、`cover_concept_candidates`、`selected_cover_concept`、核心隐喻、`required_entities`、`visual_quality_scorecard`、`cover_effectiveness_scorecard`、vision 校验结果
- [ ] `image-plan.md` 存在，每张图含 `slot_id` + `section_index` + `chapter_title` + `core_point` + `composition_type` + `source_excerpt` + **`visual_brief` + `required_entities` + `must_match_excerpts`** + `prompt_strategy`
- [ ] `images.json` 每条记录含 `slot_id` + `section_index` + `chapter_title` + `composition_type` + **`visual_brief` + `required_entities` + `must_match_excerpts`** + `prompt` + **`verification`** + `ref_image_path` + `image_type` + `quality_status`
- [ ] 封面+配图均开启时，所有内容配图使用了 `ref_image_path="$DIR/cover.png"` 生成并记录；封面关·配图开时所有内容图未指向不存在的 `$DIR/cover.png`
- [ ] 所有正文内容图的 `wechat_url` 两两不同，且无一张复用封面 `$COVER_CDN_URL`
- [ ] **至少 80% 的内容图 `verification.passed=true`**
- [ ] `04-article-final.md` 中每个 `##` 章节都有 CDN 图片链接（按模板 rhythm 规则）
- [ ] 每个配图提示词包含对应章节的具体物体/比喻/案例（非通用描述）
- [ ] 3 张以上配图使用了 3 种以上不同构图类型（`listicle` 模板豁免）
- [ ] 所有章节配图生成并上传成功
- [ ] `images.json` 包含所有配图的 CDN 链接
- [ ] **`05-article.html` 由 `render_template` 生成**，`final-review.md` 中记录 `render_audit`
- [ ] `final-review.md` 全部通过，且 `cover_quality_gate` 同时读取 `visual_quality_scorecard` 与 `cover_effectiveness_scorecard`
- [ ] **缺 `viral-audit.md` 不得发布**；若生成则整体 ≥7.0 且视觉停留不得只凭"风格统一"给高分
- [ ] `draft.json` 使用了 SEO 优化后的标题和摘要
- [ ] 草稿创建成功，可通过公众号后台查看

---

## 错误处理

**非关键步骤失败**（SEO优化、AI去痕）：记录问题，使用降级方案继续，在最终报告中说明。

**配图步骤失败**（单张配图生成失败）：重试一次（锐化 prompt 后），仍失败则记录该章节缺少配图继续后续章节。如果超过一半章节配图失败，暂停流程请求用户协助。

**关键步骤失败**（封面生成、草稿创建）：暂停流程，分析原因，尝试重试一次，仍失败则请求用户协助。

**配置问题**：假定配置已正确设置，不要尝试验证配置。如果 MCP 工具因配置问题失败，直接报告错误信息并继续流程。

## 工作规范

### 文件组织

- 每篇文章使用独立目录（步骤 1 创建，变量 `$DIR`）
- 编号命名（01-research.md, 02-outline.md...）
- 使用标准格式：Markdown（.md）、JSON（.json）、HTML（.html）
- 图片统一保存在 `$DIR/` 下（cover.png, img_01.png 等）
- 质量报告统一保存在 `$DIR/visual-rhythm-plan.md`、`$DIR/cover-prompt.md`、`$DIR/image-plan.md`、`$DIR/final-review.md`

### 任务追踪

- 流程启动时用 TaskCreate 创建任务列表
- 每个任务对应一个流程步骤
- 开始前：`TaskUpdate status → in_progress`
- 完成后：`TaskUpdate status → completed`
- 设置依赖：每个任务 blockedBy 前一个任务
- 报告进度：`[3/10] 文章撰写完成 → $DIR/03-article.md (2,847字)`

## 最佳实践

1. **模板驱动视觉节奏**：步骤 6a 自动选模板，6b 生成 `visual-rhythm-plan.md` 把每个 `##` 映射到 slot，模板决定图片数量和位置
2. **视觉风格配置优先**：`$VISUAL_STYLE` 优先取自 `get_project_profile` 的 `visual_style` 字段（配置锚点），配置为空时由三维分析兜底；不使用 writer YAML 的 cover_style/cover_prompt
3. **配图内容三件套**：每张图必须有 `visual_brief`（具体画面）+ `required_entities`（必须物体）+ `must_match_excerpts`（章节原句）——这是 vision 校验的前提
4. **vision 校验闭环**：每张图生成后必须用 `verify_with_vision`（或单独 `analyze_image`）校验，失败锐化 prompt 重试，最多 3 次
5. **参考链保持风格一致**：封面+配图均开启时，所有内容配图使用 `ref_image_path="$DIR/cover.png"`（用封面防止风格漂移）；封面关·配图开时不传 `ref_image_path` 或链首图，严禁指向不存在的 `$DIR/cover.png`
6. **结构化 HTML 渲染**：步骤 8 用 `render_template`（带 `layout_plan`）确定性渲染，不用 `convert_markdown` 自由发挥
7. **正文图片各自独立**：每张正文图独立生成上 CDN，严禁复用封面 URL 或多图共用同一 URL
8. **审计记录可复盘**：`images.json` 必须记录 `verification`、`required_entities`、`slot_id`、`composition_type`、`chapter_title` 等字段
9. **发布前总验收**：`final-review.md` 全部通过后才能创建草稿
10. **SEO 结果回写**：优化后的标题和摘要用于最终草稿
11. **决策透明记录**：选题、模板选择、风格选择写入文件，便于追溯

配图设计流程、封面合规、违禁词检查等详见各 skill 文档。

---

## 红旗检查清单

流程中出现以下情况时需要特别关注：

- [ ] 文章缺少二级标题（<3 个）→ 需补充结构
- [ ] **`visual-rhythm-plan.md` 缺失** → 步骤 6b 必须创建，不得跳过
- [ ] **模板选择含糊** → 默认选 `long-form-essay`，避免临时编造
- [ ] **所选模板的 rhythm 规则被违反**（如 listicle 出现 inline_detail）→ 回到步骤 6a/b 重新规划
- [ ] 章节缺少配图（且模板要求该 slot 必填）→ 需在步骤 7 补充
- [ ] **`$VISUAL_STYLE_CONFIGURED` 非空但配图偏离配置方向**（如配置温暖自然却生成维多利亚木刻）→ 回到步骤 6c 重新收敛
- [ ] 封面 prompt 参考了 writer YAML 的 cover_prompt → 应从零构建
- [ ] **封面 vision 或有效性校验未通过**（含 `cover_effectiveness_scorecard.overall_pass=false`、三项硬测试任一失败、仅有旧的 6 维 vision 全 high）→ 先改 `cover_strategy` / `selected_cover_concept`，再重试
- [ ] `image-plan.md` 缺失或字段不完整 → 步骤 6e 必须按新 schema 创建
- [ ] **`visual_brief` 是抽象描述**（"商务场景"、"科技感"）→ 重写为具体画面
- [ ] **`required_entities` 是抽象词**（"美感"、"氛围"）→ 重写为可识别的物体
- [ ] **`must_match_excerpts` 是论点而非原句** → 从章节中摘真实段落
- [ ] 封面+配图均开启时，内容配图未使用 `ref_image_path="$DIR/cover.png"` → 风格不一致风险；封面关·配图开时，内容配图指向不存在的 `$DIR/cover.png` → 必须移除或链到首张已生成图
- [ ] **正文 `<img src>` 出现封面 `$COVER_CDN_URL`，或多张正文图共用同一 `wechat_url`** → 服务端 `publish_draft` 会拒绝发布；回步骤 7 为缺失 slot 独立生成，不得用封面/他图顶替
- [ ] **`$DIR/cover-prompt.md` 缺失或无 `cover_strategy` / `cover_effectiveness_scorecard`** → 步骤 6d 第 8 步必须原子写入
- [ ] `images.json` 缺少 `verification` 字段 → vision 校验未执行，回步骤 7b
- [ ] **Vision 校验通过率 < 80%** → 回到步骤 6e 检查 prompt 构建逻辑
- [ ] 配图提示词为通用描述（如"美丽风景"、"商务场景"）→ 需重写为章节具体内容
- [ ] 连续 3 张配图视觉雷同 → 需更换构图类型
- [ ] 封面图包含马赛克/播放标记 → 需重新生成
- [ ] 标题使用省略号隐藏关键信息 → 需补全信息
- [ ] 文章字数过短（<500 字）→ 需扩展内容
- [ ] AI 痕迹明显（33 类模式检测得分低）→ 需加强去痕
- [ ] 违禁词报告显示高风险词汇 → 需人工复核
- [ ] **HTML 用 `convert_markdown` 生成**（而非 `render_template`）→ 回到步骤 8 重新渲染
- [ ] **`layout_plan` JSON 中 `image_url` 未回填 CDN URL** → 步骤 7d 未完成
- [ ] HTML 文件过大（>1MB）→ 需精简内联样式
- [ ] `final-review.md` 未通过 → 不得发布草稿

---

## 分阶段交付策略

当文章较长时，按以下阶段独立交付：

- **阶段 1 - 选题与大纲**：完成选题分析、关键词提取、文章大纲（`01-research.md`, `02-outline.md`）
- **阶段 2 - 内容创作**：完成文章撰写、AI 去痕、合规检查（`03-article.md`, `04-article-final.md`）
- **阶段 3 - SEO 与视觉**：完成 SEO 优化、模板选择与节奏规划、封面图生成（带 vision 校验）、配图设计与生成
- **阶段 4 - 发布准备**：完成 HTML 渲染（`render_template`）、发布前总验收和草稿创建

每个阶段完成后可独立验证，配图生成可分批进行。
