# 视觉节奏规划规范（visual-rhythm-plan.md）

## Contents

- [核心理念](#核心理念)
- [模板库](#模板库)
- [Slot 类型](#slot-类型)
- [visual-rhythm-plan.md 模板](#visual-rhythm-planmd-模板)
- [元信息](#元信息)
- [视觉风格锚点（Phase 1 产出）](#视觉风格锚点phase-1-产出)
- [Slot 分配表](#slot-分配表)
- [layout_plan（传给 render_template MCP 工具）](#layoutplan传给-rendertemplate-mcp-工具)
- [模板规则自检](#模板规则自检)
- [完整示例](#完整示例)
  - [示例 1：养生账号长文《慢下来的力量》](#示例-1养生账号长文慢下来的力量)
- [元信息](#元信息)

## 核心理念

**模板驱动节奏，节奏决定图片位置。** 每篇文章在生成任何图片之前，必须先确定每个 `##` 章节对应什么类型的图片 slot。这是把"图片与内容贴切"从偶然变成系统化的关键。

---

## 模板库

模板位于 `templates/article/`：

| 模板 | 适用 | rhythm 摘要 | image_count |
|------|------|------------|-------------|
| `long-form-essay` | 深度观点、评论、分析 | hero + section_opener×N + 可选 inline/footer | 4-6 |
| `listicle` | 清单、盘点、Top N | hero + section_opener×N + 必选 footer | 3-8 |
| `tutorial` | 教程、操作指南 | hero + section_opener×N + 可选 inline + 必选 footer | 5-8 |
| `story-narrative` | 故事、叙事、回忆 | hero + section_opener×N + 可选 inline/footer | 4-6 |

---

## Slot 类型

| slot_id | 含义 | image_size | 何时使用 |
|---------|------|------------|----------|
| `hero` | 文章开篇视觉 | full-bleed (2.35:1) | 每篇文章有且只有 1 张（即封面） |
| `section_opener` | 章节开头图 | full-width (16:9) | 每个主章节配一张，紧跟 `## 标题` |
| `inline_detail` | 章节内细节图 | inline (4:3 或 1:1) | 关键比喻、案例、特写需要时插入 |
| `footer` | 结尾图或装饰 | full-width 或 inline | 模板要求 footer 时使用，可为空（仅 module） |

---

## visual-rhythm-plan.md 模板

```markdown
# 视觉节奏规划

## 元信息

- 文章标题: {from 03-article.md H1 或 seo-result.md}
- 所选模板: {long-form-essay | listicle | tutorial | story-narrative}
- 模板路径: templates/article/{template}.yaml
- 选择理由: {1 句话说明为什么选这个模板}
- 章节数: {N}
- 计划配图数: {hero + section_opener×M + inline×K + footer?}

## 视觉风格锚点（Phase 1 产出）

- visual_style: {from cover.md 3D analysis}
- color_palette: {e.g., "warm earth tones with soft green and gold"}
- mood: {e.g., "contemplative and peaceful"}

---

## Slot 分配表

| slot_id | section_index | section_title | image_size | module | composition_type | chapter_anchor |
|---------|---------------|---------------|------------|--------|------------------|----------------|
| hero | 0 | — | full-bleed | hero | 留白主导 | 全篇核心隐喻：{1 句} |
| section_opener | 1 | {章节 1 标题} | full-width | quote | 三分法 | {章节 1 核心论点} |
| section_opener | 2 | {章节 2 标题} | full-width | null | 前景/背景 | {章节 2 核心论点} |
| inline_detail | 2 | {章节 2 标题} | inline | null | 特写 | {段落 N 的比喻：1 句} |
| section_opener | 3 | {章节 3 标题} | full-width | null | 中心聚焦 | {章节 3 核心论点} |
| footer | -1 | — | — | cta | — | CTA 文案：{1 句} |

---

## layout_plan（传给 render_template MCP 工具）

以下 JSON 是本节奏计划机器可读形式，渲染 HTML 时传给 `render_template` 的 `layout_plan` 参数：

```json
{
  "article_type": "{template_name}",
  "template_name": "{template_name}",
  "slots": [
    {
      "slot_id": "hero",
      "section_index": 0,
      "image_url": "<cover_cdn_url>",
      "image_size": "full-bleed",
      "module": "hero",
      "module_vars": { "label": "深度", "title": "...", "subtitle": "..." }
    },
    {
      "slot_id": "section_opener",
      "section_index": 1,
      "section_title": "...",
      "image_url": "<img_01_cdn_url>",
      "image_size": "full-width",
      "module": "quote",
      "module_vars": { "text": "..." }
    },
    {
      "slot_id": "section_opener",
      "section_index": 2,
      "section_title": "...",
      "image_url": "<img_02_cdn_url>",
      "image_size": "full-width",
      "module": "callout",
      "module_vars": { "title": "...", "body": "..." }
    },
    {
      "slot_id": "inline_detail",
      "section_index": 2,
      "after_paragraph_index": 3,
      "image_url": "<img_03_cdn_url>",
      "image_size": "inline",
      "module": null
    },
    {
      "slot_id": "footer",
      "section_index": -1,
      "image_url": null,
      "module": "cta",
      "module_vars": { "title": "...", "note": "..." }
    }
  ]
}
```

注意：`image_url` 在 Phase 0 写入时填占位符（如 `<img_01_cdn_url>`），Phase 4 配图生成 + 上传 CDN 后**会回头修改 visual-rhythm-plan.md 本文件**，把占位符替换为实际 CDN URL。传给 render_template 时所有 URL 必须已上传到 CDN。

---

## 模板规则自检

- [ ] hero slot 有且仅有 1 个
- [ ] section_opener 数量等于 `##` 章节数量（除非模板允许跳过）
- [ ] inline_detail 数量在模板允许范围内（listicle 模板禁止 inline_detail）
- [ ] footer 符合模板要求（listicle/tutorial 必选，其他可选）
- [ ] composition_type 在 3+ section_opener 时至少使用 3 种不同类型（listicle 模板可豁免）
- [ ] 每个 slot 都有 chapter_anchor（1 句话说明该 slot 服务哪个章节论点）
```

---

## 完整示例

### 示例 1：养生账号长文《慢下来的力量》

```markdown
# 视觉节奏规划

## 元信息

- 文章标题: 慢下来的力量
- 所选模板: long-form-essay
- 模板路径: templates/article/long-form-essay.yaml
- 选择理由: 文章为深度观点文，4 个主章节分析"慢"的哲学，无并列清单或步骤
- 章节数: 4
- 计划配图数: 5（hero + 4 section_opener）

## 视觉风格锚点

- visual_style: warm natural photography, soft morning light, organic textures
- color_palette: warm earth tones with soft sage green and golden accents
- mood: serene and meditative

---

## Slot 分配表

| slot_id | section_index | section_title | image_size | module | composition_type | chapter_anchor |
|---------|---------------|---------------|------------|--------|------------------|----------------|
| hero | 0 | — | full-bleed | hero | 留白主导 | 一朵晨光中缓缓绽放的莲花 |
| section_opener | 1 | 身体的智慧 | full-width | quote | 三分法 | 石板路裂缝里钻出的新芽 |
| section_opener | 2 | 时间的节奏 | full-width | null | 前景/背景 | 案头沙漏与一杯茶 |
| section_opener | 3 | 慢与效率的悖论 | full-width | callout | 中心聚焦 | 一只缓慢但持续前进的蜗牛 |
| section_opener | 4 | 练习慢下来 | full-width | summary | 特写 | 双手捧着一杯热茶 |
```

### 示例 2：科技账号清单《5 个让你效率翻倍的 Mac 工具》

```markdown
# 视觉节奏规划

## 元信息

- 文章标题: 5 个让你效率翻倍的 Mac 工具
- 所选模板: listicle
- 模板路径: templates/article/listicle.yaml
- 选择理由: 5 个并列工具盘点，标题含数字，结构典型清单
- 章节数: 5
- 计划配图数: 7（hero + 5 section_opener + footer）

## 视觉风格锚点

- visual_style: clean modern tech photography, minimalist desk scenes
- color_palette: cool grays with electric blue accents
- mood: focused and professional

---

## Slot 分配表

| slot_id | section_index | section_title | image_size | module | composition_type | chapter_anchor |
|---------|---------------|---------------|------------|--------|------------------|----------------|
| hero | 0 | — | full-bleed | hero | 俯拍 | 5 个 Mac 应用的图标平铺 |
| section_opener | 1 | 1. Raycast — 启动器之王 | full-width | metrics | 俯拍 | Mac 屏幕显示 Raycast 搜索框 |
| section_opener | 2 | 2. Rectangle — 窗口管理 | full-width | metrics | 俯拍 | 多窗口分屏排列的桌面 |
| section_opener | 3 | 3. Obsidian — 知识库 | full-width | metrics | 俯拍 | Obsidian 图谱视图 |
| section_opener | 4 | 4. Paste — 剪贴板历史 | full-width | metrics | 俯拍 | Paste 浮窗显示剪贴板条目 |
| section_opener | 5 | 5. Bartender — 菜单栏整理 | full-width | metrics | 俯拍 | 整洁的 Mac 菜单栏 |
| footer | -1 | — | — | summary | — | 5 个工具一句话总结 |
```

注意 listicle 模板：所有 section_opener 使用同一种构图（俯拍），强化"清单"感；composition_type 多样性规则豁免。

### 示例 3：美食教程《手冲咖啡的 4 个关键步骤》

```markdown
# 视觉节奏规划

## 元信息

- 文章标题: 手冲咖啡的 4 个关键步骤
- 所选模板: tutorial
- 模板路径: templates/article/tutorial.yaml
- 选择理由: 4 个步骤的操作指南，标题含"步骤"
- 章节数: 4
- 计划配图数: 7（hero + 4 section_opener + 2 inline_detail + footer）

## 视觉风格锚点

- visual_style: warm lifestyle photography, natural kitchen light
- color_palette: warm wood tones with copper and cream
- mood: cozy and focused

---

## Slot 分配表

| slot_id | section_index | section_title | image_size | module | composition_type | chapter_anchor |
|---------|---------------|---------------|------------|--------|------------------|----------------|
| hero | 0 | — | full-bleed | hero | 前景/背景 | 一杯手冲咖啡与咖啡豆 |
| section_opener | 1 | 步骤 1：研磨咖啡豆 | full-width | steps | 前景/背景 | 手持手摇磨豆器的动作 |
| inline_detail | 1 | 步骤 1：研磨咖啡豆 | inline | null | 特写 | 研磨后的咖啡粉粗细对比 |
| section_opener | 2 | 步骤 2：温杯与准备滤纸 | full-width | steps | 前景/背景 | 手撕滤纸贴合滤杯 |
| section_opener | 3 | 步骤 3：闷蒸与注水 | full-width | steps | 特写 | 水流从鹅颈壶缓慢注入咖啡粉 |
| inline_detail | 3 | 步骤 3：闷蒸与注水 | inline | null | 特写 | 闷蒸时咖啡粉膨胀的细节 |
| section_opener | 4 | 步骤 4：享用与品鉴 | full-width | steps | 留白主导 | 一杯冲好的咖啡置于木质桌面 |
| footer | -1 | — | — | checklist | — | 4 步操作要点 checklist |
```

---

## 与 image-plan.md 的关系

`visual-rhythm-plan.md` 决定**结构**（每个 slot 在哪里、什么尺寸、什么 module）。
`image-plan.md` 决定**内容**（每个 slot 的图必须画什么、必须包含哪些实体）。

两份文档通过 `section_index` 关联。Phase 3 写 image-plan.md 时必须为 rhythm-plan 中每个需要图片的 slot 提供内容规划。
