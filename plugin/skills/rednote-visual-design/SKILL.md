---
name: rednote-visual-design
description: Generates cover and content images for Xiaohongshu (小红书) posts with 3:4 ratio design norms. Use when creating rednote visual content including covers, content pages, and tail pages.
---

# 小红书图片生成

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `generate_image` (channel_id, prompt, image_type="cover", output_path) | 生成封面（单张） |
| `generate_batch_images` (channel_id, prompt, count, output_dir) | 批量生成内容图 |
| `upload_image` (channel_id, file_path) | 上传图片到微信素材库 |

---

## 平台 Gotcha

小红书图片是 **3:4 竖版**，强视觉驱动。封面决定点击率，内容图决定完读率，尾图决定互动率。三类图片目标不同，prompt 构建方式也不同。

---

## 视觉风格设计原则

风格无固定预设，每次根据账号定位和内容动态设计：

**三个维度定调**：
1. **账号定位** — 知识干货型（专业简洁，结构感强）/ 生活美学型（温暖氛围，情绪感强）/ 娱乐趣味型（活泼鲜艳，夸张对比）
2. **内容主题** — 美食/旅行/家居/时尚各有视觉惯例，参考主题的典型配色和构图
3. **目标受众** — 年龄层、消费力影响配色（年轻用户偏饱和鲜艳；成熟用户偏质感低饱和）

**封面与内容图的一致性**：
- 无配置参考图时：先生成封面确立基准风格 → 以封面作为参考图批量生成内容图
- 有配置参考图时：所有图片统一使用配置参考图

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
- 源笔记视觉结构（仅复刻模式，如适用）

### 步骤 1：内容分析

读取 `content.md`，提取：
- 核心主题和内容类型（干货/情感/测评/教程/...）
- 全部信息点（具体数据、方法、结论、场景描述）
- 正文总字数和段落数
- 目标受众和内容调性

### 步骤 2：信息点提取与分组

将提取的信息点按主题相关性分组：
- 每组 2-3 个信息点（对应一张内容图）
- 确保组间不重叠
- 标记每组的关键词和推荐布局类型（参考 [references/content.md](references/content.md) 的 6 种布局模式）
- 优先级排序：最重要的信息 → 首张内容图

### 步骤 3：图片数量与布局选择

**图片数量决策**：
1. 计算信息点组数 G
2. N = max(G + 2, 3)（加上封面和尾图）
3. N 取最近的奇数（3/5/7/9）
4. N ≤ 9

**内容类型 → 图片数量参考**：
- 情感/生活：3-5 张
- 干货/教程：5-7 张
- 合集/测评：5-7 张
- 旅行/探店：5-9 张

### 步骤 4：生成 image-plan.md

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

---

## image_01 [内容] 主题：{第一组信息点主题}

- 信息点1: （8-15 字）
- 信息点2: （8-15 字）
- 推荐布局: {编号清单/对比双栏/步骤流程/标注图解/数据卡片/Q&A 对话}

{重复 image_02 ... image_0{N-2}}

---

## tail [尾部] 类型：{follow|comment|traffic}

匹配依据：{根据内容类型自动判断——知识干货→follow, 测评对比→comment, 种草推荐→traffic}
- 内容点: （见 tail.md 规范，根据类型填充）
```

### 步骤 5：图片生成

按 image-plan.md 逐一生成：

1. **封面**：使用 [references/cover.md](references/cover.md) 的 Prompt 模板生成
2. **内容图**：使用 [references/content.md](references/content.md) 的 Prompt 模板批量生成（count = N-2）
3. **尾图**：使用 [references/tail.md](references/tail.md) 的 Prompt 模板单独生成

### 步骤 6：质量验证

生成后检查：
- [ ] 所有图片文件存在且可访问
- [ ] 封面、内容图、尾图视觉风格一致
- [ ] 内容图之间有视觉多样性（不同背景、构图、焦点位置）
- [ ] 每张内容图信息点与 image-plan.md 一致

### 复刻模式适配

当提供改写模式和源笔记视觉结构时：
- `style-only`：完全独立规划，不参考源笔记图片结构
- `medium`：参考源笔记信息结构重新设计内容图主题
- `tight`：参照源笔记视觉结构模板（图片张数、各页主题关键词），若标记"无法提取"则按 `medium` 处理

---

## 图片生成方式

通过 MCP 工具调用：

1. **生成封面（单张）**：调用 `generate_image`，image_type 设为 `"cover"`
2. **批量生成内容图（N-2 张，不含尾图）**：调用 `generate_batch_images`，指定 count
3. **单独生成尾图**：调用 `generate_image`，传入封面作为参考图
4. **带参考图（保持风格一致）**：提供参考图路径
5. **带风格描述**：在 prompt 中加入风格描述（如"手绘感，暖色调，小清新"）

**关键规则**：内容图必须用 `generate_batch_images` 批量生成，尾图单独生成（`tail.png`），封面单独生成（`cover.png`）。
