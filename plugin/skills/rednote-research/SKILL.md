---
name: rednote-research
description: Analyzes Xiaohongshu (小红书) topics and scores engagement potential. Use when analyzing Xiaohongshu (小红书) topics, scoring engagement, or researching trending rednote content.
---

# 小红书选题研究知识库

## 选题前必做：查看已有选题

在开始选题研究前，调用 `list_topics(channel_id="$CHANNEL_ID")` 查看系统内已有选题列表，避免重复。

---

## xsec_token 工作流

**重要**：大多数 MCP 工具需要 `feed_id` 和 `xsec_token` 两个参数。这两个参数**只能**从 `search_feeds` 或 `list_feeds` 的返回结果中获取，不能凭空构造。

工作流程：
1. 先调用 `search_feeds(keyword=...)` 或 `list_feeds()` 获取 Feed 列表
2. 从返回结果中提取每个笔记的 `feed_id`（或 `id`）和 `xsecToken` 字段
3. 将这两个值传入后续工具调用（如 `get_feed_detail` 等）

---

## MCP 工具用法

```
# 搜索相关话题的热门笔记（返回结果包含 feed_id 和 xsecToken）
search_feeds(keyword="<话题关键词>")

# 获取推荐流（了解平台当前推广内容，返回结果包含 feed_id 和 xsecToken）
list_feeds()

# 获取具体笔记详情+评论数据（需从上面的结果提取 feed_id 和 xsec_token）
get_feed_detail(feed_id="<笔记ID>", xsec_token="<从列表结果提取的xsecToken>")
```

---

## 互动率评分模型

评分公式：

```
topic_score = engagement_rate × recency_weight × novelty_bonus
engagement_rate = (likes + favorites + 2×comments) / max(total, 1)
recency_weight: 24h→1.0, 7d→0.8, 30d→0.5, 更早→0.3
novelty_bonus: 同角度笔记<3 → 1.2, 否则 → 1.0
```

计算所有候选选题的 `topic_score`，自动选择得分最高者。评分明细与选题理由写入工作目录的 `topic-analysis.md`。

---

## 内容分析维度

分析热门笔记时，重点提取以下维度：

- **标题模板**：句式、情绪词、字数分布
- **封面模板**：信息层级、文字密度、配色规律
- **正文模板**：开场钩子、段落结构、结尾 CTA 形式
- **评论信号**：高频关键词、用户痛点、争议点
- **标签组合**：核心话题 + 垂直话题 + 长尾话题
