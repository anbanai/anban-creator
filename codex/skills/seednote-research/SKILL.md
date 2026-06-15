---
name: seednote-research
description: Analyzes Seednote (种草笔记) topics, trending notes (热门笔记), and scores engagement potential (互动率评分). Use when analyzing Seednote topics, scoring engagement, researching trending seednote content, or fetching source note details for replicate mode. Also use when user mentions '种草笔记选题', '热门笔记', '竞品分析', '笔记分析', or when the seednote pipeline calls for topic discovery or source note fetching.
---

# 种草笔记选题研究

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `list_channel_titles` (channel_id) | 查看系统内已有标题（定标题前必调） |
| `search_feeds` (keyword) | 搜索相关话题的热门笔记 |
| `list_feeds` () | 获取推荐流 |
| `get_feed_detail` (feed_id, xsec_token) | 获取具体笔记详情+评论数据 |

---

## 完整研究流程

### 步骤 1：查重

调用 `list_channel_titles(channel_id="$CHANNEL_ID")` 查看已有标题，后续标题避开。

### 步骤 2：搜索热门笔记

根据账号定位和用户需求，确定搜索关键词（2-3 个），调用：

```
search_feeds(keyword="咖啡馆选址")    // 返回 feed_id + xsecToken
list_feeds()                         // 了解平台当前推广内容
```

**xsec_token 工作流**：`feed_id` 和 `xsec_token` 只能从 `search_feeds`/`list_feeds` 的返回结果中提取，不能凭空构造。将这两个值传入 `get_feed_detail` 获取详细数据。

### 步骤 3：分析热门笔记

对搜索结果中的 Top 3-5 条笔记，调用 `get_feed_detail` 获取详情，提取：

- **标题模板**：句式、情绪词、字数分布
- **封面模板**：信息层级、文字密度、配色规律
- **正文模板**：开场钩子、段落结构、结尾 CTA 形式
- **评论信号**：高频关键词、用户痛点、争议点
- **标签组合**：核心话题 + 垂直话题 + 长尾话题

### 步骤 4：评分与选题

使用互动率评分模型：

```
topic_score = engagement_rate × recency_weight × novelty_bonus
engagement_rate = (likes + favorites + 2×comments) / max(total, 1)
recency_weight: 24h→1.0, 7d→0.8, 30d→0.5, 更早→0.3
novelty_bonus: 同角度笔记<3 → 1.2, 否则 → 1.0
```

计算所有候选选题的 `topic_score`，自动选择得分最高者。

**产出**：将评分明细与选题理由写入 `$DIR/topic-analysis.md`。

---

## 复刻模式源笔记获取

当用户提供笔记 ID 或链接时，本 skill 只负责获取源笔记详情，不做爆款模板分析：

1. 通过 `search_feeds` 或 `list_feeds` 获取真实返回的 `feed_id` 与 `xsec_token`
2. 调用 `get_feed_detail(feed_id, xsec_token)` 获取源笔记详情、互动数据和评论数据
3. 将原始详情、token 来源、互动数据、评论摘要和数据缺失项写入 `$DIR/source-note.md`
4. 后续由 `seednote-viral-analysis` skill 读取 `$DIR/source-note.md`，生成 `$DIR/source-analysis.md`、`$DIR/viral-template.json`、`$DIR/template-meta.json`

**边界**：不要在本 skill 中提取爆款模板，不要调用 `save_template`，不要生成改写正文。

---

## 产出要求

| 模式 | 产出文件 |
|------|----------|
| 原创模式 | `$DIR/topic-analysis.md`（候选话题列表、评分明细、最终选题理由） |
| 复刻模式 | `$DIR/source-note.md`（源笔记原始详情、互动数据、评论摘要、数据缺失项） |
