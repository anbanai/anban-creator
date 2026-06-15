---
name: article-visual-design
description: Manages images for WeChat article (公众号图文) content including cover generation (封面), content illustration (配图), compression, and CDN upload. Use when generating or processing images for WeChat articles. Also use when user mentions '封面', '配图', '插图', '视觉设计', '图片上传', 'generate cover', or when the article pipeline calls for image planning, generation, or uploading.
---

# 公众号图文图片管理

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `generate_image` (channel_id, prompt, image_type, output_path, task_id, ref_image_path) | 生成单张图片，返回 download_url 和 file_path |
| `upload_image` (channel_id, file_path) | 上传图片到微信 CDN，返回 CDN URL |
| `download_image` (channel_id, url) | 下载在线图片 |
| `compress_image` (file_path) | 压缩图片 |

---

## 核心原则：账号驱动，非 Writer 驱动

**Writer YAML 定义文字风格（语气、结构、修辞），不定义图片风格。**

图片的视觉风格由账号定位、内容主题和目标受众三个维度独立确定，与 writer 选择无关。即使使用 dan-koe（犀利深刻写作风格）为养生账号写文章，图片也应使用温暖自然的视觉风格，而非维多利亚版画。

三维风格分析详见 [references/cover.md](references/cover.md)。

---

## 封面图生成

封面设计规范详见 [references/cover.md](references/cover.md)。

### 流程

1. 从 `get_channel_profile` 结果中读取账号定位、关键词、受众信息
2. 读取文章内容（`$DIR/04-article-final.md`）提取主题和关键意象
3. 执行三维风格分析（账号定位 + 内容主题 + 目标受众）→ 确定视觉风格、色彩基调、情绪氛围
4. 从零构建封面 prompt（**不使用 writer YAML 的 cover_prompt**）
5. 构建 prompt 时（如 MCP 工具可用）可调用 `list_resources(category="image_presets")` 获取封面预设模板列表，再用 `get_resource(category="image_presets", name="cover-default")` 等获取具体预设模板，将 `{{ARTICLE_TITLE}}`、`{{ARTICLE_SUMMARY}}`、`{{VISUAL_STYLE}}`、`{{ASPECT_RATIO}}` 等变量替换为实际内容。如果工具不可用，直接从零构建 prompt
6. 调用 `generate_image(channel_id="$CHANNEL_ID", prompt=封面提示词, image_type="cover", output_path="$DIR/cover.png", task_id="$TASK_ID", size="2.35:1")` 生成
7. 调用 `upload_image(channel_id="$CHANNEL_ID", file_path="$DIR/cover.png")` 上传，获取 `media_id`

### 产出

- `$DIR/cover.png` — 封面图文件
- `media_id` — 微信素材 ID
- `$VISUAL_STYLE` — 三维分析确定的视觉风格描述
- `$COLOR_PALETTE` — 色彩基调
- `$COVER_PATH` — 封面图路径（供配图参考链使用）

---

## 图片内容规划

在生成任何内容配图之前，必须先创建 `image-plan.md`，为每张配图规划具体内容和视觉方向。这是确保配图与文章内容关联性的核心环节。

设计规范详见 [references/content.md](references/content.md)。

### 规划流程

#### 步骤 1：提取章节内容

读取 `$DIR/04-article-final.md`，对每个 `##` 章节提取：

- **核心论点**：该章节要传达的关键信息（1 句话）
- **情感基调**：理性分析、温暖鼓励、犀利批判、诗意沉思、轻松幽默等
- **具体素材**：章节中使用的案例、比喻、场景描述、引用等
- **原文摘录**：可直接支撑配图 prompt 的章节原句或短段（写入 `source_excerpt`）

#### 步骤 2：分配构图类型

从 8 种构图类型中为每章配图选择不同类型（参见 [references/content.md](references/content.md)）：
- 中心聚焦、对角线流动、三分法、前景/背景、俯拍、特写、留白主导、重复图案
- 3 张以上配图时，必须使用 3 种以上不同构图

#### 步骤 3：写入 image-plan.md

按模板写入 `$DIR/image-plan.md`，包含总体策略和每章配图的详细规划。每张图必须包含 `chapter_title`、`core_point`、`source_excerpt`、`visual_subject`、`composition_type`、`prompt_strategy`。

### 产出

- `$DIR/image-plan.md` — 配图内容规划文档

---

## 内容配图设计与生成

设计规范详见 [references/content.md](references/content.md)。

### 参考链

**所有内容配图使用封面图作为参考锚点**：

```
所有内容配图：ref_image_path="$DIR/cover.png"（始终用封面，不用上一张）
```

为什么始终用封面：使用上一张会导致风格漂移（每张图的微小差异累积放大），封面是风格锚点。

### 生成流程

对 `image-plan.md` 中每个章节配图执行：

1. 根据 image-plan 中的分析构建 prompt（必须包含章节具体概念、比喻或案例）
2. 调用 `generate_image`（`channel_id="$CHANNEL_ID"`, `prompt=章节提示词`, `image_type="content"`, `output_path="$DIR/img_N.png"`, `task_id="$TASK_ID"`, `ref_image_path="$DIR/cover.png"`）
3. 调用 `upload_image`（`channel_id="$CHANNEL_ID"`, `file_path="$DIR/img_N.png"`）→ 获取 CDN URL
4. 将 `![描述](CDN_URL)` 插入到章节关键段落之后（不紧跟 `##` 标题，不在章节末尾）
5. 将生成信息写入 `$DIR/images.json`，用于后续视觉质量审计

### Prompt 要求

- 必须包含章节中的具体概念、比喻或案例作为视觉主体
- 必须引用 `image-plan.md` 的 `source_excerpt` 或等价章节原文信息
- 避免抽象通用描述（如"商务场景"、"科技背景"）
- 不同章节的 prompt 必须有明显区别
- 视觉风格和色彩与封面一致

---

## 质量验证

生成完成后执行 5 项检查：

- [ ] **文件完整性**：所有图片文件存在且可访问
- [ ] **风格一致性**：读取 `$DIR/images.json`，确认所有内容配图记录 `ref_image_path="$DIR/cover.png"`
- [ ] **视觉多样性**：读取 `$DIR/image-plan.md` 与 `$DIR/images.json`，确认 3 张以上配图使用 3 种以上不同 `composition_type`
- [ ] **内容关联性**：逐项对照 `image-plan.md`，确认每个 prompt 引用了 `source_excerpt` 或章节具体内容（非通用描述）
- [ ] **审计完整性**：每条图片记录必须包含 `chapter_title`、`composition_type`、`visual_subject`、`prompt_source_excerpt`、`ref_image_path`、`image_type`、`quality_status`

未通过检查时：重试对应配图（更换 prompt 措辞），仍失败则记录问题继续后续章节。

---

## 保存结果

- 将含 CDN 图片链接的文章覆盖写回 `$DIR/04-article-final.md`
- 将所有配图信息保存为 `$DIR/images.json`（含 index, image_type, chapter_title, composition_type, visual_subject, prompt, prompt_source_excerpt, ref_image_path, file_path, url, quality_status）

---

## 技术规范

**微信图片限制**：
- 最大尺寸：10MB（超出会被自动压缩）
- 最大宽度：1920px（保持比例压缩）
- 支持格式：JPG、PNG、GIF、WebP

**公众号常用比例**：
- 正文配图：16:9 或 4:3 横版
- 封面图（公众号封面）：2.35:1（900x383px 标准）
- 正方形配图：1:1

---

## 构图类型选择指南

根据章节内容主题推荐构图类型：

| 章节主题 | 推荐构图 | 原因 |
|----------|----------|------|
| 开篇引入 / 总述 | 中心聚焦 | 建立视觉焦点，统领全文 |
| 过程 / 变化 / 方法 | 对角线流动 | 表达动态感和方向性 |
| 平衡分析 / 多角度 | 三分法 | 自然和谐，适合并列观点 |
| 层次 / 上下文 / 环境 | 前景/背景 | 表达深度和空间关系 |
| 细节 / 数据 / 质感 | 特写 | 强调微观细节和真实感 |
| 沉思 / 哲理 / 极简 | 留白主导 | 营造意境和呼吸感 |
| 节奏 / 规律 / 重复 | 重复图案 | 表达秩序感和韵律感 |
| 总览 / 全貌 / 结构 | 俯拍 | 展现整体结构和关系 |

---

## 常见失败与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 配图与内容无关 | prompt 使用抽象通用描述 | 从章节中提取具体概念/比喻/案例作为视觉主体 |
| 风格不一致 | 未使用封面作为参考图 | 确保所有内容图传入 `ref_image_path="$DIR/cover.png"` |
| 所有配图构图雷同 | 未在 image-plan 中分配不同构图 | 3+ 张图时强制使用 3+ 种构图类型 |
| 封面与文章主题脱节 | 封面 prompt 缺少内容主题关联 | 在封面 prompt 中包含文章的视觉隐喻 |
