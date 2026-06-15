---
name: topic-research
description: Researches WeChat topics (选题研究), scores engagement potential, and generates content outlines (大纲). Use when researching topics, scoring engagement potential, or generating content outlines. Also use when user mentions '选题', '话题研究', '大纲', '选题分析', '话题评分', or when any content pipeline calls for topic discovery, scoring, or outline generation.
---

# 微信公众号选题分析

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `list_channel_titles` (channel_id) | 查看系统内已有标题（定标题前必调） |
| `list_drafts` (channel_id) | 查看已有草稿 |
| `list_published_articles` (channel_id) | 查看已发布文章 |
| `research_topics` (channel_id, keywords?, domain?, count?) | 选题研究 |
| `score_article` (channel_id, content, title?, domain?) | 话题评分 |
| `generate_outline` (channel_id, topic, template?, domain?, style?, keywords?) | 内容框架生成 |

---

## 完整研究流程

按以下步骤执行选题研究，每一步都产出可追溯的文件。

### 步骤 1：查重——收集已有内容

定标题前必须先检查已有标题，避免重复。

调用以下三个工具：
```
list_channel_titles(channel_id="$CHANNEL_ID")
list_drafts(channel_id="$CHANNEL_ID")
list_published_articles(channel_id="$CHANNEL_ID")
```

从结果中提取所有已有标题和选题关键词，构建**排除列表**。后续标题必须避开这些已有标题和近似表达。

**产出**：在 `$DIR/01-research.md` 中记录已有内容摘要和排除列表。

### 步骤 2：选题研究——生成候选话题

调用 `research_topics` 获取话题建议：

```
research_topics(
  channel_id="$CHANNEL_ID",
  keywords=["关键词1", "关键词2"],  // 从 get_channel_profile 获取
  domain="general",                // 可选: general, tea, tech, lifestyle, culture, business, education
  count=5                          // 生成 5 个候选
)
```

**参数说明**：
- `keywords`：从频道 profile 中的 keywords 字段获取，结合用户输入的选题方向
- `domain`：根据账号领域选择，不确定时使用 `general`
- `count`：建议 5-10 个候选，以便对比筛选

**产出**：将所有候选话题及其 viral_score、angle、keywords 写入 `$DIR/01-research.md`。

### 步骤 3：话题评分——筛选最优选题

对每个候选话题调用 `score_article` 评估爆款潜力：

```
score_article(
  channel_id="$CHANNEL_ID",
  title="候选标题",
  content="话题的核心观点描述（2-3句话）",
  domain="general"
)
```

对比所有候选话题的评分，**自动选择得分最高且不在排除列表中的话题**。

**产出**：在 `$DIR/01-research.md` 中记录评分明细和最终选择理由。

### 步骤 4：生成大纲——构建内容框架

基于选定话题生成大纲：

```
generate_outline(
  channel_id="$CHANNEL_ID",
  topic="选定的话题",
  template="authoritative",    // 选择匹配的模板
  domain="general",
  style="dan-koe",             // 从频道 profile 获取
  keywords=["关键词1", "关键词2"]
)
```

**模板选择指南**：

| 模板 | 值 | 适合内容 | 特征 |
|------|----|----------|------|
| 权威 | `authoritative` | 深度分析、行业解读、专业观点 | 观点先行，论证支撑，结论有力 |
| 对比 | `comparison` | 产品对比、方案评估、选择指南 | 多维对比，优劣势分析，推荐结论 |
| 文化 | `cultural` | 文化解读、历史故事、人文关怀 | 叙事性强，情感共鸣，文化深度 |
| 实用 | `practical` | 教程攻略、方法论、操作指南 | 步骤清晰，可操作性强，场景具体 |

选择原则：知识科普类用 `authoritative`，产品/方案类用 `comparison`，文化情感类用 `cultural`，方法教程类用 `practical`。不确定时默认 `authoritative`。

**产出**：保存为 `$DIR/02-outline.md`。

---

## 产出要求

选题研究完成后，应产出以下文件：

| 文件 | 内容 |
|------|------|
| `$DIR/01-research.md` | 已有内容摘要、候选话题列表、评分明细、最终选题理由 |
| `$DIR/02-outline.md` | 结构化大纲（含标题、各章节标题、核心论点） |
| `$DIR/context-brief.md` | 上下文锚点文件，为后续写作提供章节级上下文（由调用方创建） |

## 注意事项

- 评分结果是参考值，结合账号定位和受众判断，不要盲目追高分
- 如果所有候选话题都在排除列表中，扩大 research_topics 的 count 并调整 keywords
- 模板选择应与文章类型匹配，不匹配的模板会导致大纲质量下降
- 大纲中的每个章节标题应具体、有信息量（好的："三种晨间习惯让你精力充沛" / 差的："关于习惯"）
