---
name: article-cover-design
description: 'Use when 微信公众号封面图（公众号头图）专用设计——硬编码官方比例 900×383（2.35:1），中心安全区构图保证转发卡 1:1 主体完整，受控文字策略（按真实场景决定是否带短文字），从文章核心隐喻和标题钩子推导视觉概念，生成后用封面质量评分卡把关。用户提到「封面」「公众号封面」「头图」「cover」「封面设计」「封面图」时，或 article 流水线到达封面生成步骤时使用。'
---

# 公众号封面图设计（效果与用途保障方法论）

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

封面是否真的承担内容相关度和点击率，按 [references/cover-effectiveness.md](references/cover-effectiveness.md) 的方法论执行；主文件只保留流程与硬闸门。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## 跳过条件（图片开关：封面关）

公众号文章的封面可由用户在创建任务/计划时关闭。当结构化运行控制 `article_image_mode` 为 `content_only` 或 `text_only` 时，**整个本 skill 跳过**：

- 不调 `generate_image`、不生成 `$DIR/cover.png`、不写 `cover-prompt.md`、不取 `media_id`/`wechat_url`。
- 节奏规划 `visual-rhythm-plan.md` 的 hero slot `image_url=null`。
- 发布草稿（`article-publishing` skill）**不带 `thumb_media_id`**——即使有正文配图也**不复用**作封面。
- 两关（纯文字）时，在 `final-review.md` 记录「未生成封面，公众号后台可能不显示封面/需手动设置」。

无上述禁令（默认/封面开）→ 正常执行下文全流程。

---

本 skill 是公众号文章封面（`thumb_media_id`）的**唯一权威设计入口**，也是全篇内容配图的**风格锚点**（产物 `$DIR/cover.png` 供后续内容图 `ref_image_path` 继承）。它不是「随便生成一张横图」，而是一套多层方法论，确保封面达成：订阅号列表抓眼球（CTR）、转发卡主体完整、微信零裁剪、一眼传达主题、品牌调性统一。

## 核心规格（写死，不可改）

| 项 | 值 | 说明 |
|----|----|------|
| 大图比例 | **2.35:1 = 900×383px** | 订阅号列表、文章详情页展示 |
| 转发卡 | **1:1**（微信自动从封面**中心**裁切） | 群发通知、转发/分享卡片 |
| 文字 | **受控文字策略**（按场景决定是否带 2-8 个字短标题/关键词；禁止乱码/水印/logo） | 标题利益点强、教程/清单/杂志风可带短文字；普通真实场景/氛围图默认无字 |
| 生成比 | `size="21:9"`（≈2.333:1，Volcengine 支持的最近比） | 服务端强制中心裁剪到精确 900×383 |

**关键**：服务端 `generate_image` 对 `platform=article && image_type="cover"` 会把成品**精确裁剪到 900×383** 并做像素断言。因此你只负责「出一张宽银幕横图 + 主体居中 + 按受控文字策略决定是否带短文字」，最终比例由服务端兜底，微信**绝不会**再裁剪（告别「一张需要手动裁剪的纯图」）。

## 用途 ↔ 规则 ↔ 验证（方法论的纲）

| 封面用途 | 设计规则（硬性） | 验证方法 |
|----------|------------------|----------|
| 微信零裁剪、列表/详情完整 | 精确 900×383 | 服务端像素断言（不可绕过） |
| 转发卡 1:1 主体完整 | 主体置于**居中 1:1 安全区**（≈383×383），避开底部 20% | 评分卡 `safe_zone_centered` |
| 避免低质文字干扰 | 按受控文字策略：需要时只放短文字；不需要时无字；始终禁止乱码/水印/logo/密集排版 | 评分卡 `text_policy_ok` + `hard_no_watermark_logo`（硬维度） |
| 缩略图 0.5 秒抓眼球（CTR） | 主体大、高对比、强焦点 | 评分卡 `subject_thumbnail_readability` |
| 一眼传达主题 | 视觉概念 = 标题钩子 + 文章核心论点/最强隐喻的具象化 | 评分卡 `title_cover_digest_alignment` + `content_metaphor_relevance` + `required_entities` |
| 品牌调性统一 | 视觉风格 = `get_project_profile` 的 `visual_style`（配置优先） | 评分卡 `style_consistency` |
| 视觉质感达标 | 摄影级/绘画级；禁 3D 合成/卡通/纯色底/对称 PPT | 评分卡 `composition_quality` |

---

## 封面质量闸门

生成前先从 `$DIR/context-brief.md`、`$DIR/seo-result.md`、digest 和 `$DIR/04-article-final.md` 提炼 `cover_strategy` 并写入 `cover-prompt.md`：

- `final_title`：最终标题
- `digest_hook`：摘要前半句的利益点/悬念/反差
- `cover_strategy`：必须含 `target_reader`、`reader_pain_or_job`、`article_promise`、`content_proof_points`、`click_trigger`、`cover_concept_candidates`、`selected_cover_concept`
- `cover_hook`：封面唯一要放大的点击钩子
- `visual_metaphor`：能被画出来的核心隐喻
- `thumbnail_strategy`：缩到 200px 时靠什么被看见（主体大小、对比、色块、短文字）
- `anti_generic_constraints`：明确禁止的泛化画面，例如"通用养生水墨背景"、无主体山水、只放茶盏/莲花/药材摆拍、与标题无关的人像
- `cover_effectiveness_scorecard`：必须含 `information_scent_alignment`、`audience_motivation`、`content_specificity`、`thumbnail_attention`、`truthfulness_not_clickbait`、`brand_style_fit`、`visual_distinctiveness`、`safe_zone_text_policy`、`overall_pass`
- 三个硬测试：`generic_swap_test`、`promise_proof_test`、`audience_motivation_test`

封面必须先服务 `article_promise`、`target_reader` 和 `click_trigger`，再服务品牌风格。品牌统一不等于每篇都用同一张浅色水墨背景；同一批文章中，主体、隐喻、构图或色彩重心必须能区分。**仅有旧的 6 维 vision 全 high 不得通过**：缺 `cover_strategy` 或 `cover_effectiveness_scorecard.overall_pass=false` 时必须重构概念。

## 受控文字策略

封面不再一律纯图，也不能无脑加字。先判断真实场景与用户需求，再决定是否在图上生成文字：

- **应考虑带短文字**：最终标题利益点很强、系列栏目需要识别、教程/清单/方法论需要关键词、杂志编辑风封面、用户 prompt 或项目视觉风格明确要求图中文字。
- **默认无字**：真实场景摄影、氛围意象、人物/物件特写、自然/生活方式画面，以及模型不稳定时。
- **文字约束**：仅 2-8 个中文字或 1 个短标签；必须大而清晰，位于安全区内，避开底部 20%；禁止乱码、伪文字、水印、logo、密集排版、长段落。
- **prompt 要求**：带字时明确写出需要出现的精确文字；无字时写明 `NO text, NO watermark, NO logo`。
- **vision 校验**：必须检查文字是否短、清晰、准确、无乱码；文字失败时优先改为无字封面或重试。
- **反导流视觉禁区**：封面不得出现二维码、联系方式、外链 URL、扫码提示、跳转图标、加群、加微信、关注领资料或回复关键词等导流元素；出现即判定不通过并重试。
## 第一步：推导封面策略与三选一概念

封面**必须从文章内容推导**，不是随机图：

1. 读 `$DIR/context-brief.md`、`$DIR/seo-result.md` 和 `$DIR/04-article-final.md`，提取**最终标题**、digest 前半句、目标读者、读者痛点/任务、文章承诺、正文证据和最强视觉素材（文章已有的比喻/意象/案例/场景）。
2. 取视觉锚点 `$VISUAL_STYLE`：
   - `get_project_profile` 的 `visual_style` 非空 → 以它为**权威锚点**，三维分析只做**细化**（配色/情绪/构图），不得偏离。
   - 为空 → 按账号定位 + 内容主题 + 受众三维分析兜底（方向见下方「三维方向参考」）。
   - **绝不**从 writer YAML 推视觉（writer 只管文字；切断 dan-koe→维多利亚版画 bug）。
3. 生成至少 3 个 `cover_concept_candidates`，**先评审，不先画图**。每个候选必须写清：标题钩子、摘要承诺、正文证据、目标读者点击理由、可视化实体、误导风险、可替换性风险。
4. 对 3 个候选执行 `generic_swap_test`、`promise_proof_test`、`audience_motivation_test`，选择评分最高且三项全过的 `selected_cover_concept`。任何“换到其他方法论文章也成立”的概念必须失败。
5. 合成封面概念：`{selected_cover_concept} × {VISUAL_STYLE} × {COLOR_PALETTE} × {article_promise} × {click_trigger} × {thumbnail_strategy} × {宽银幕叙事构图 + 主体居中安全区}`。
6. 提炼 `required_entities`（封面必须出现的具体物体，vision 校验依据）和 `anti_generic_constraints`（必须避免的同质化画面）。

## 第二步：构建封面 prompt

```
A cinematic 2.35:1 wide banner for a WeChat article cover. {VISUAL_STYLE}.
Target reader: {TARGET_READER}. Reader pain/job: {READER_PAIN_OR_JOB}.
Article promise: {ARTICLE_PROMISE}. Click trigger: {CLICK_TRIGGER}. {COLOR_PALETTE}.
Selected cover concept: {SELECTED_COVER_CONCEPT}. {CONCRETE_PROOF_FROM_ARTICLE — 具象化正文证据}.
Thumbnail strategy: {THUMBNAIL_STRATEGY}. Avoid generic visuals: {ANTI_GENERIC_CONSTRAINTS}.
{MOOD_TONE}. Main subject centered within the middle safe zone (works for both
the 2.35:1 hero and the 1:1 forward-card crop), large and high-contrast for
thumbnail readability, generous negative space, avoid the bottom 20% (WeChat
overlays the article title there). Photographic quality, {TEXT_POLICY: exact short Chinese text when needed, otherwise NO text}, NO watermark,
NO logo.
```

构建要点：

- **主体居中安全区**：主体落在画面中央 ≈383px 宽区域，确保转发卡 1:1 裁切后仍完整。
- **避开底部 20%**：微信会在底部叠加标题，关键元素不放底部。
- **缩略图可读**：主体要大、对比强，列表里缩成 ≈200px 仍能一眼识别。
- **文字策略**：先按「受控文字策略」判断；需要文字时写出精确短文字，不需要文字时写死 `NO text, NO watermark, NO logo`。

## 第三步：构建公众号封面质量评分卡（生成前就绪）

服务端 `verify_with_vision=true` 时要求 `verification_prompt` 非空。封面用专用评分卡（对应上方映射表）：

```
这是文章《$ARTICLE_TITLE》的公众号封面（将用于订阅号列表 2.35:1 + 转发卡 1:1）。
digest 钩子：$DIGEST_HOOK；封面钩子：$COVER_HOOK。
目标读者：$TARGET_READER；读者痛点/任务：$READER_PAIN_OR_JOB；文章承诺：$ARTICLE_PROMISE；点击触发点：$CLICK_TRIGGER。
正文证据：$CONTENT_PROOF_POINTS；选中封面概念：$SELECTED_COVER_CONCEPT。
文章核心论点：$CORE_THESIS；核心隐喻：$METAPHOR；账号视觉风格锚点：$VISUAL_STYLE。
反同质化约束：$ANTI_GENERIC_CONSTRAINTS。
请按 JSON 评分（软维度 high/medium/low；hard_* 为硬性布尔）：
{
  "final_title": "$ARTICLE_TITLE",
  "digest_hook": "$DIGEST_HOOK",
  "cover_strategy": {
    "target_reader": "$TARGET_READER",
    "reader_pain_or_job": "$READER_PAIN_OR_JOB",
    "article_promise": "$ARTICLE_PROMISE",
    "content_proof_points": ["..."],
    "click_trigger": "$CLICK_TRIGGER",
    "cover_concept_candidates": ["candidate A", "candidate B", "candidate C"],
    "selected_cover_concept": "$SELECTED_COVER_CONCEPT"
  },
  "cover_hook": "$COVER_HOOK",
  "visual_metaphor": "$METAPHOR",
  "thumbnail_strategy": "$THUMBNAIL_STRATEGY",
  "anti_generic_constraints": "$ANTI_GENERIC_CONSTRAINTS",
  "cover_effectiveness_scorecard": {
    "information_scent_alignment": "...", // 标题、摘要、封面是否给出同一个清晰预期？
    "audience_motivation": "...",         // 目标读者 0.5 秒内是否知道这和自己的问题有关？
    "content_specificity": "...",         // 主体/冲突/短文字是否来自正文证据而非泛方法论？
    "thumbnail_attention": "...",         // 缩略图是否有大主体、强冲突或短文字承担打开率？
    "truthfulness_not_clickbait": "...",  // 是否准确但有吸引力，不夸大正文承诺？
    "brand_style_fit": "...",             // 风格是否符合账号，不喧宾夺主？
    "visual_distinctiveness": "...",      // 与同批/历史封面是否能区分？
    "safe_zone_text_policy": "...",       // 安全区与受控文字策略是否同时成立？
    "generic_swap_test": true/false,
    "promise_proof_test": true/false,
    "audience_motivation_test": true/false,
    "overall_pass": true/false            // 三个硬测试 true，且前 8 项不低于 medium
  },
  "visual_quality_scorecard": {
    "title_cover_digest_alignment": "...", // 标题、封面、digest 是否强化同一个利益点/悬念？
    "thumbnail_readability": "...",        // 缩成 200px 时主体是否仍大、清楚、高对比？
    "contrast_focus": "...",               // 主体与背景是否有明确明度/色彩/轮廓对比？
    "specificity_not_generic": "...",      // 是否不是通用养生水墨背景/无主体山水/摆拍素材？
    "series_distinctiveness": "...",       // 与同批文章是否能凭主体/隐喻/构图区分？
    "safe_zone_centered": "...",           // 主体是否落在居中 1:1 安全区？
    "text_policy_ok": true/false,          // 文字策略是否正确：短、清晰、准确或无字
    "hard_no_forbidden_cues": true/false,  // 无水印/logo/二维码/联系方式/外链 URL/扫码提示/加群/加微信
    "overall_pass": true/false             // 所有硬项 true，且 alignment/readability/contrast/specificity/distinctiveness 不低于 medium
  },
  "subject_thumbnail_readability": "...",   // 兼容旧字段：主体大且高对比，缩成 200px 列表缩略图能否 0.5s 抓住？
  "safe_zone_centered": "...",             // 主体是否落在居中 1:1 安全区？转发卡裁切后是否完整？
  "content_metaphor_relevance": "...",     // 与文章核心论点/隐喻的语义相关度
  "style_consistency": "...",              // 是否与账号视觉风格锚点一致
  "composition_quality": "...",            // 摄影级质感，无合成/卡通/纯色底/对称 PPT
  "text_policy_ok": true/false,          // 文字策略是否正确：需要文字时短且清晰；不需要时无字；无乱码/伪文字
  "hard_no_watermark_logo": true/false,  // 硬性：无水印/logo/密集排版
  "hard_aspect_ok": true/false,            // 硬性：宽银幕横版（非竖图/方图错比）
  "missing_or_forbidden": "...",           // 缺失实体或违禁元素（文字水印、二维码、联系方式、外链 URL、扫码提示、加群、加微信等）
  "overall_pass": true/false,              // visual_quality_scorecard.overall_pass=true 且 text_policy_ok=true、所有 hard_* 为 true
  "sharper_prompt_hint": "..."             // 不通过时的锐化建议
}
```

## 第四步：生成（生成与上传原子化）

```
generate_image(
  project_id=$PROJECT_ID,
  prompt=<第二步的封面 prompt>,
  image_type="cover",
  output_path="$DIR/cover.png",
  task_id=$TASK_ID,
  size="21:9",
  verify_with_vision=true,
  verification_prompt=<第三步的评分卡>,
  upload_to_cdn=true
)
```

- `size="21:9"` 是生成提示比；**服务端按 `platform=article + image_type=cover` 把成品精确裁到 900×383**（你无需管最终比例）。
- `upload_to_cdn=true`：vision 校验通过才上传，同一调用内完成「生成→裁剪→校验→上传」，直接返回 `media_id`（发布草稿的 thumb）+ `wechat_url`；校验未通过则**不上传**（不浪费微信素材位）。
- 返回 `upload_error`（生成成功但上传失败）→ 用 `upload_image(file_path="$DIR/cover.png")` 单独重传，**无需重新生成**。

## 第五步：迭代闭环

读 `generate_image` 返回的 `verification` 对象（服务端归一化字段 `passed`/`score`/`missing_entities`/`notes`/`raw`）：

- `passed=true` → 通过，进入第六步。
- `passed=false` → 按 `notes` / `sharper_prompt_hint` 锐化 prompt 重试，**最多 3 次**（共 3 次尝试）。锐化策略：
  - 在 prompt 开头加 `MAIN SUBJECT: <具体物体>`，强化主体权重；
  - 把隐喻改得更具体（材质/颜色/方位/数量）；
  - 若失败原因是"通用养生水墨背景"、低对比、标题/封面/digest 不协同、读者动机弱、正文证据不足、系列辨识度低，必须先改 `cover_strategy` / `selected_cover_concept` / `cover_hook` / `visual_metaphor` / `thumbnail_strategy`，不要只换风格形容词；
  - 收紧「主体居中安全区 + 避开底部 20%」；
  - 按受控文字策略修正：文字乱码/过密则改为无字或缩短为精确短词；始终强化 `NO watermark, NO logo`。
- vision JSON 类型不匹配、校验超时、缺 `cover_effectiveness_scorecard`、三个硬测试任一失败、或 `cover_effectiveness_scorecard.overall_pass=false` → 不得手动 `upload_image` 后继续发布；必须重新校验或回到第一步重构概念。
- 3 次仍不过 → **暂停并请求用户协助**，**不得**用未通过 vision 的封面发布。

## 第六步：落盘审计（cover-prompt.md，硬性）

封面构建完成后，**原子写入 `$DIR/cover-prompt.md`**（先写 `$DIR/.cover-prompt.md.tmp` → `fsync` → `rename` 覆盖），完整记录封面决策，便于复盘与风格漂移排查。内容必须含：

- **比例**：公众号 2.35:1（900×383px 标准；服务端强制裁剪）。
- **账号视觉风格来源**：`$VISUAL_STYLE` / `$COLOR_PALETTE` / `$MOOD` + 三维分析依据（账号定位/内容主题/受众），或配置锚点来源（`visual_style_source`）。
- **标题协同字段**：`final_title`、`digest_hook`、`cover_hook`。
- **封面策略**：`cover_strategy`，含 `target_reader`、`reader_pain_or_job`、`article_promise`、`content_proof_points`、`click_trigger`、`cover_concept_candidates`、`selected_cover_concept`。
- **文章核心隐喻**：`visual_metaphor`，即封面要表达的最强视觉隐喻。
- **缩略图策略与反同质化约束**：`thumbnail_strategy`、`anti_generic_constraints`。
- **`required_entities`**：封面必须出现的具体物体列表。
- **最终 prompt**：实际传给 `generate_image` 的完整 prompt。
- **封面质量评分卡**：`visual_quality_scorecard` + vision prompt + 结果（passed/score/各维度/missing_or_forbidden）。
- **封面有效性评分卡**：`cover_effectiveness_scorecard` + `generic_swap_test` / `promise_proof_test` / `audience_motivation_test` 结果；仅有旧的 6 维 vision 全 high 不得通过。

**产出**：`$DIR/cover.png`、`media_id`、`$COVER_CDN_URL`、`$DIR/cover-prompt.md`。

**注意**：封面仅用于 `thumb_media_id`，**不得复用为正文内容图**。正文每张图的 `wechat_url` 必须各自独立生成上 CDN——服务端 `publish_draft` 会硬拦截「正文 ≥2 图但唯一 URL==1」的草稿。

---

## 三维方向参考（仅 `visual_style` 为空时兜底）

| 账号领域 | 视觉方向 | 典型色彩 |
|----------|----------|----------|
| 中医/养生/健康 | 自然摄影、有机元素 | 大地色、柔绿、金色 |
| 文化/历史/艺术 | 传统美学、水墨韵味 | 墨黑、暖棕、青瓷 |
| 科普/科技/教育 | 干净现代、概念化 | 蓝、青、白 |
| 生活/美食/旅行 | 温暖生活方式摄影 | 橙、奶油、橄榄 |
| 育儿/家庭 | 温暖自然、柔和色调 | 柔粉、暖米、鼠尾草 |
| 金融/商业 | 专业简洁设计 | 藏蓝、金、白 |
