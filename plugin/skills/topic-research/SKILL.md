---
name: topic-research
description: Researches topics, scores engagement potential, and generates content outlines. Use when researching topics, scoring engagement potential, or generating content outlines.
---

# 微信公众号选题分析工具

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `list_topics` (channel_id) | 查看系统内已有选题（选题前必调） |
| `research_topics` (channel_id, keywords?, domain?, count?) | 选题研究 |
| `score_article` (channel_id, content, title?, domain?) | 话题评分 |
| `generate_outline` (channel_id, topic, template?, domain?, style?, keywords?) | 内容框架生成 |
| `list_drafts` (channel_id) | 查看已有草稿 |
| `list_published` (channel_id) | 查看已发布文章 |

---

## 核心功能

### 0. 查看已有内容（选题前必做）

在开始选题前，先检查已有内容，避免重复主题：

调用 `list_topics(channel_id)` 查看系统内已有选题列表。

调用 `list_drafts` 和 `list_published` 查看已有内容。

列出所有已有标题和选题后，选题应避开这些已有主题。

### 1. 话题评分

评估话题的爆款潜力：

调用 `score_article`，传入话题内容、标题和领域参数。

### 2. 内容框架生成

基于话题生成内容框架：

调用 `generate_outline`，传入话题、模板、领域、风格和关键词参数。

**可用模板**: authoritative(权威), comparison(对比), cultural(文化), practical(实用)
**可用领域**: general, tea, tech, lifestyle, culture, business, education

**模板详细说明**: 参见 [`references/outline-templates.md`](references/outline-templates.md)

## 注意事项

- 评分结果仅供参考，需结合实际情况判断
- 建议结合多个话题进行对比分析
