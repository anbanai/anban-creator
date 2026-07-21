---
name: seednote-viral-analysis
description: Use when analyzing or decomposing a Seednote (种草笔记) viral note, extracting reusable templates from a source note, or preparing evidence-backed clone guidance. Also use when the user mentions "拆解爆款", "爆款拆解", "分析爆款笔记", "提取爆款模板", "source note analysis", or when the seednote replicate pipeline needs source-note analysis.
---

# 种草笔记爆款拆解

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## 核心原则

爆款拆解必须是 **证据驱动**，不是主观点评。每个重要结论都要从源笔记内容、封面、互动数据、评论信号或账号定位中找到依据，并转化为可复用模板和下一步动作。

本 skill 只负责分析与模板产物，不写新笔记，不调用 `save_template`。模板持久化由 seednote agent 步骤 11 统一处理。

## 输入

- 源笔记详情：标题、正文、图片/封面描述、标签、发布时间、作者信息（有则用）
- 互动数据：点赞、收藏、评论、分享、浏览/曝光（有则用）
- 评论信号：高频词、追问、争议点、购买/行动意图（有则用）
- 当前账号画像：定位、受众、关键词、历史标题、内容边界
- 工作目录 `$DIR`

缺失任何数据时不要编造；在评分中降低 `confidence`，并写入 `missing_data`。

## 工作流

1. **收集证据**：从源笔记中摘出标题结构、封面元素、正文段落、标签、评论高频词、互动数据。
2. **7 维拆解**：按选题、标题、封面、正文、互动、标签、评论信号分析；每维都输出 `observation`、`mechanism`、`transferability`、`action`。
3. **评分与置信度**：整体输出 `score`、`confidence`、`evidence_count`、`missing_data`、`why_not_higher`；不要给每个维度裸分。
4. **提取模板**：生成 `$DIR/viral-template.json`，供 `seednote-writing` 直接消费。
5. **生成元数据**：生成 `$DIR/template-meta.json`，供 hook 判断是否保存模板。
6. **写分析报告**：生成 `$DIR/source-analysis.md`，让用户能看懂"为什么火"和"我怎么用"。

详细评分依据见 [breakdown-rubric.md](references/breakdown-rubric.md)。
报告格式见 [report-format.md](references/report-format.md)。
JSON 字段规范见 [template-schema.md](references/template-schema.md)。

## 7 维拆解

| 维度 | 关注点 |
|------|--------|
| 选题角度 | 强需求、热点、痛点、人群身份、差异化角度 |
| 标题 | 句式、关键词、情绪词、数字、信息缺口 |
| 封面 | 主视觉、文字层级、点击钩子、风格差异化 |
| 正文 | 开头钩子、信息密度、段落节奏、收藏理由 |
| 互动 | 评论触发点、争议点、低门槛参与机制 |
| 标签 | 大词、垂直词、长尾词组合 |
| 评论信号 | 真实需求、反对意见、追问、二创空间 |

每维必须使用以下结构：

```markdown
### 维度名

- observation: 观察到什么，绑定原文片段、图片描述或数据
- mechanism: 为什么可能有效
- transferability: 高/中/低，并说明是否适合当前账号复用
- action: 下一篇具体怎么改
```

评分统一放在报告的“评分与置信度”部分，不放进单个维度。

## 固定产物

必须在 `$DIR` 下生成：

- `source-analysis.md`：用户可读报告，重点是证据、机制、迁移判断和行动建议。
- `viral-template.json`：写作 skill 可直接读取的结构化模板。
- `template-meta.json`：hook 保存模板所需元数据。

## 复刻深度判断

> **原创性 & AI 内容红线（2026）**：2026 小红书持续严打搬运——高相似/搬运内容不仅本篇限流，还会**连累账号历史笔记降权**；同时 AI 生成合成内容须主动标识（未标识→限制分发/下架，恶意隐瞒→封号）。原创度体现在"内容质量分"和搬运降权里，不是独立的"原创性权重"——别和"用户画像匹配度"（内容是否精准命中某类人）混为一谈，那是不同维度。所以复刻的默认取向是"借机制不借形"——宁可原创度高、机制迁移，也不要追求和源笔记像。这条比"源笔记很火"更重要。

默认推荐 `style-only`。

- `style-only`：大多数情况。只迁移主题机制、信息层级、互动逻辑和视觉风格方向。
- `medium`：源结构可迁移、风险可控，但需要重做案例、表达和视觉元素。
- `tight`：仅当主题高度通用、源笔记无强个人经历/专属素材/品牌承诺，且 `do_not_copy` 风险低时使用。

不要因为源笔记很火就推荐 `tight`。相似度风险高时必须推荐 `style-only` 或 `medium`。证据不足（`confidence=low`）、视觉/文案相似度高、或命中较多 `do_not_copy` 项时，**一律降级**到 `style-only`——这是用 2026 搬运降权 + AI 内容标识监管倒推出的安全策略，不是保守，是保流量。

## 红旗

- 裸评分没有证据
- 把源作者经历当成可复用模板
- 把评论区原话复制为互动设计
- 复用源图构图、人物姿势、图标组合或文字框位置到不可区分
- 无评论/无封面/无发布时间仍输出 high confidence
- 生成可发布正文或直接调用 `save_template`
