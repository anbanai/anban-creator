# 公众号内容配图设计规范

## 参考链机制

所有内容配图使用封面图作为风格参考锚点：

```
封面图先生成 → $DIR/cover.png
所有内容配图使用 ref_image_path="$DIR/cover.png"（始终用封面，不用上一张）
```

**为什么始终用封面**：如果每张图引用上一张，风格漂移会累积放大（每张图的微小差异逐步叠加）。封面是风格锚点，确保所有配图保持一致的视觉基准。

---

## 图片内容规划（image-plan.md）

在生成任何配图之前，必须先创建 `$DIR/image-plan.md`，确保每张图都有明确的内容规划和视觉方向。

### 规划流程

1. 读取 `$DIR/04-article-final.md`，提取所有 `##` 章节
2. 逐章提取：核心论点、情感基调、具体素材（案例/比喻/场景描述）、可支撑配图的原文摘录
3. 为每章分配不同的构图类型（确保多样性）
4. 写入 `$DIR/image-plan.md`

### image-plan.md 模板

```markdown
# 图片内容规划

## 总体策略

- 账号定位: {from get_channel_profile}
- 视觉风格: {from 3-dimension analysis}
- 色彩基调: {determined by account + content}
- 情绪氛围: {from content analysis}

---

## cover 封面

- 视觉主体: {what's in the image}
- 隐喻关联: {how it connects to article theme}
- 情绪: {tone}

---

## img_01 章节：{chapter title}

- chapter_title: {chapter title}
- 核心论点: {1 sentence}
- core_point: {same as 核心论点, concise and reusable in prompt}
- 情感基调: {rational / warm / critical / poetic / humorous}
- 具体素材: {cases, metaphors, scenes extracted from chapter}
- source_excerpt: {exact sentence or short passage from this chapter that anchors the image}
- 推荐构图: {composition type from the 8 types below}
- composition_type: {one of 中心聚焦 / 对角线流动 / 三分法 / 前景/背景 / 俯拍 / 特写 / 留白主导 / 重复图案}
- 视觉主体: {concrete visual element from chapter content}
- visual_subject: {same visual subject, concise and reusable in images.json}
- prompt_strategy: {how to turn source_excerpt + visual_subject + composition_type into the final prompt}

---

## img_02 章节：{chapter title}

{same structure as img_01}

{repeat for each ## chapter}
```

---

## 8 种构图类型

为确保视觉多样性，从以下 8 种中为每章配图选择不同类型：

| # | 构图类型 | 适用场景 | 视觉特征 |
|---|----------|----------|----------|
| 1 | **中心聚焦** | 核心概念、重要观点 | 单一主体居中，视觉冲击力强 |
| 2 | **对角线流动** | 变化、过程、流动感 | 元素沿对角线分布，动态感 |
| 3 | **三分法** | 自然平衡、通用场景 | 主体位于三分线交点，和谐稳定 |
| 4 | **前景/背景** | 层次、上下文、环境 | 前后景分层，纵深感和空间感 |
| 5 | **俯拍** | 结构、细节、展示 | 鸟瞰或平铺视角，秩序感 |
| 6 | **特写** | 质感、情感、细节强调 | 微距聚焦纹理/图案，亲密感 |
| 7 | **留白主导** | 沉思、极简、意境 | 大面积负空间，主体精简，呼吸感 |
| 8 | **重复图案** | 节奏、重复、规律 | 图案重复排列，韵律感和秩序感 |

---

## 视觉多样性规则

当有 3 张以上配图时，必须遵守：

1. **构图多样性**：使用 3 种以上不同构图类型
2. **主体多样性**：每张图的视觉主体不同（从章节内容提取）
3. **情绪匹配**：配图情绪与章节情感基调一致（不是全部同一氛围）
4. **避免连续雷同**：不得出现连续 3 张视觉元素、构图、色调高度相似的配图
5. **审计字段完整**：`image-plan.md` 中每张图必须具备 `chapter_title`、`core_point`、`source_excerpt`、`visual_subject`、`composition_type`、`prompt_strategy`

---

## 内容配图 Prompt 模板

```
{VISUAL_STYLE_FROM_PLAN}. {COLOR_PALETTE}.
{CHAPTER_SPECIFIC_SUBJECT} — {VISUAL_METAPHOR_FROM_CHAPTER}.
{COMPOSITION_TYPE}. {MOOD_MATCHING_CHAPTER}.
16:9 horizontal, photographic quality.
```

### Prompt 构建要点

1. **VISUAL_STYLE_FROM_PLAN**：与封面一致的视觉风格描述
2. **COLOR_PALETTE**：与封面一致或互补的色彩基调
3. **CHAPTER_SPECIFIC_SUBJECT**：章节核心论点的视觉化表达
4. **VISUAL_METAPHOR_FROM_CHAPTER**：**优先使用章节中已有的比喻、案例或场景描述作为视觉主体**（不是泛泛的描述）
5. **COMPOSITION_TYPE**：从 8 种构图类型中选择
6. **MOOD_MATCHING_CHAPTER**：与章节情感基调匹配

### 好的配图 prompt 示例

假设养生账号、视觉风格为自然摄影，章节讨论"身体的自我修复能力"：

```
Warm natural photography, soft golden hour light. A small crack in a stone path with tender green shoots emerging, symbolizing natural healing and resilience. Rule of thirds composition, shoots at the right intersection. Warm earth tones with fresh green. 16:9 horizontal, photographic quality.
```

假设文化账号、视觉风格为传统美学，章节讨论"独处的力量"：

```
Traditional Chinese aesthetic, ink wash texture with warm photographic tones. A lone scholar's desk by a window, a single candle flame, scrolls and ink stones arranged with generous negative space. Negative space dominant composition, warm amber light. Ink black and warm brown palette. 16:9 horizontal, photographic quality.
```

---

## 生成方式

通过 MCP 工具逐张调用 `generate_image`：

1. 每张使用 image-plan.md 中对应章节的分析填充 prompt
2. `output_path` 设为 `$DIR/img_01.png`、`$DIR/img_02.png` ...
3. **`ref_image_path` 始终传入封面路径 `$DIR/cover.png`** 保持视觉一致性
4. 生成后调用 `upload_image` 上传获取 CDN URL
5. 将 `![描述](CDN_URL)` 插入到章节关键段落之后（不紧跟 `##` 标题，不在章节末尾）

> **关键规则**：每张配图必须使用章节具体内容构造独立 prompt，确保内容差异化。通过封面参考图保持视觉一致性。

---

## images.json 审计模板

`$DIR/images.json` 是视觉质量审计记录，不参与 `convert_markdown` 自动替换。每张内容图必须记录以下字段：

```json
[
  {
    "index": 1,
    "image_type": "content",
    "chapter_title": "章节标题",
    "composition_type": "三分法",
    "visual_subject": "从章节内容提取的具体视觉主体",
    "prompt": "Final image generation prompt",
    "prompt_source_excerpt": "支撑该 prompt 的章节原文摘录",
    "ref_image_path": "$DIR/cover.png",
    "file_path": "$DIR/img_01.png",
    "url": "https://cdn.example.com/img_01.png",
    "quality_status": "passed"
  }
]
```

### 质量状态

- `passed`：文件存在、已上传、使用封面参考图、prompt 与章节内容相关
- `retry_needed`：生成结果或审计字段不合格，需要重试
- `failed`：重试后仍失败，必须在最终报告中说明；超过一半章节失败时停止流程

### 审计规则

- `ref_image_path` 必须等于 `$DIR/cover.png`，不得为空，不得引用上一张内容图
- `prompt_source_excerpt` 必须来自对应章节，不得写成泛化概念
- `composition_type` 必须与 `image-plan.md` 中对应章节一致
- `quality_status` 不是 `passed` 时，不得在 `final-review.md` 判定视觉质量通过
