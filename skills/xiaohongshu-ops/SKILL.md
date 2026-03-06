---
name: xiaohongshu-ops
description: |
  小红书运营全流程技能——选题研究、图片生成、笔记发布、评论互动。
  Use when user mentions "小红书", "红书", "xiaohongshu", "xhs", "种草笔记", "小红书发布", "小红书评论"
user-invocable: false
metadata:
  author: Rick
  version: 2.1.0
---

# 小红书运营技能

端到端的小红书运营流程，覆盖选题研究、图片生成、笔记发布和评论互动。

## 前置条件

**MCP 服务要求**：`xiaohongshu` 必须处于运行状态。所有小红书平台操作均通过 MCP 工具执行，无需浏览器自动化。

使用前先验证登录状态：

```
check_login
```

若未登录，执行：

```
login
```

---

> **内容创作**：文案写作、标题公式、正文结构等创作知识已独立为 [xiaohongshu-writing](../xiaohongshu-writing/SKILL.md) 技能。

---

## 1) 选题研究

使用 MCP 工具分析平台热门内容：

```
# 搜索相关话题的热门笔记
search_posts(query="<话题关键词>", limit=20)

# 获取推荐流（了解平台当前推广内容）
list_feed(limit=20)

# 获取具体笔记详情+评论数据
get_feed_detail(post_id="<笔记ID>")
```

**分析维度**：
- 标题模板：句式、情绪词、字数
- 封面模板：信息层级、文字密度、配色
- 正文模板：开场钩子、段落结构、结尾 CTA
- 评论信号：高频关键词、用户痛点、争议点
- 标签组合：核心话题 + 长尾话题

**自动评分选 Top 1 选题**（无需用户确认），评分公式：

```
topic_score = engagement_rate × recency_weight × novelty_bonus
engagement_rate = (likes + favorites + 2×comments) / max(total, 1)
recency_weight: 24h→1.0, 7d→0.8, 30d→0.5, 更早→0.3
novelty_bonus: 同角度笔记<3 → 1.2, 否则 → 1.0
```

计算所有候选选题的 `topic_score`，自动选择得分最高者。评分明细与选题理由写入 `$DIR/topic-analysis.md`。

---

## 2) 图片生成

使用 `wechatwriter` CLI 生成小红书图片。详细规范和 Prompt 模板参见：

- 封面图：[xiaohongshu-cover.md](../visual-design/references/xiaohongshu-cover.md)
- 内容图：[xiaohongshu-content.md](../visual-design/references/xiaohongshu-content.md)

```bash
# 生成单张图片（指定风格描述）
wechatwriter image generate "内容描述" --post --style "$STYLE" -o ./image_01.png

# 封面图（第一张，确定基准风格）
wechatwriter image generate "封面：大标题卡片，主题XXX" --post --style "$STYLE" -o ./cover.png

# 后续图片用封面作参考图，保持风格一致
wechatwriter image generate "内容图：步骤列表信息图" --post --ref ./cover.png -o ./image_02.png
wechatwriter image generate "收尾图：总结CTA卡片" --post --ref ./cover.png -o ./image_03.png
```

**统一视觉风格**（参考图优先）：
- 有参考图：直接 `--ref <参考图路径>` 传入（封面生成后，后续图片用 `--ref cover.png`）
- 无参考图：设计风格描述作为 `$STYLE`（含设计风格、配色 hex 值、背景、边框、字体），传 `--style "$STYLE"`
- `$STYLE` 示例：`"扁平信息图风格，低饱和莫兰迪配色（主色蓝灰#8B9DAF，辅色暖棕#C4A882，强调色陶土橙#D4856B，米白背景#F5F0EB），圆角卡片，无衬线字体"`

---

## 3) 笔记发布

### 图文笔记

```
publish_post(
  title="笔记标题",
  content="正文内容（含话题标签）",
  images=["./cover.png", "./image_02.png", "./image_03.png"],
  tags=["话题1", "话题2", "话题3"]
)
```

**发布前自动检查与修正**（所有检查通过后直接发布）：
- 标题超过 20 字 → 自动截断并重新生成符合要求的标题
- 话题标签不足 5 个 → 自动补充至 5 个；超过 8 个 → 保留权重最高的 8 个
- 图片路径不可访问 → 自动排除，确保 ≥3 张可用图片
- 违禁词命中 → 按风险等级自动替换/删除（参照 [prohibited-words.md](references/prohibited-words.md)）

### 视频笔记

```
publish_video(
  title="视频标题",
  content="正文内容",
  video_path="./video.mp4",
  tags=["话题1", "话题2"]
)
```

---

## 4) 评论互动

### 查看评论

```
# 获取笔记详情和评论
get_feed_detail(post_id="<笔记ID>")
```

### 回复评论

```
# 在笔记下发表评论
post_comment(post_id="<笔记ID>", content="评论内容")

# 回复具体评论
reply_comment(post_id="<笔记ID>", comment_id="<评论ID>", content="回复内容")
```

**回复规范**：
- 短句换行，1-4 行
- 接梗/嘴硬开头 → 立场/结论 → 顺手一句有用的 → 轻飘飘收尾
- 默认 one-reply-per-turn，不连发

### 互动操作

```
# 点赞
like_post(post_id="<笔记ID>", action="like")

# 收藏
favorite_post(post_id="<笔记ID>", action="favorite")
```

---

## 5) 数据分析

发布后使用 MCP 追踪数据：

```
# 获取笔记详情（含点赞/收藏/评论数）
get_feed_detail(post_id="<已发布笔记ID>")

# 查看自己账号主页数据
get_user_profile(user_id="self")
```

分析维度：互动率（点赞+收藏/曝光）、评论质量、热门评论关键词。

---

## 6) Viral Copy 链路

输入爆款笔记，输出高贴合可发布新笔记：

1. 用 `get_feed_detail(post_id="<爆款ID>")` 获取原笔记完整内容
2. 拆解模板（标题句式/封面层级/正文节奏/互动机制/标签组合）
3. 保留：同主题、同互动机制、同内容结构
4. 替换：具体措辞、案例细节、账号人设口吻
5. 禁止：逐句照抄、原图二次使用
6. 参考 `xiaohongshu-writing` 技能创作内容，按第 2-3 节流程生成图片并发布

---

## 实操经验

- 话题标签优先用平台热门话题，发布时在 `tags` 字段传入
- 发布后 1 小时内回复前几条评论，有助于互动率
- 图片内容密度适中：封面文字大而醒目，内容图信息结构清晰
- 若出现新类型评论节奏问题，优先减少每小时回复频率而非提高

---

## 案例模块

具体垂直行业经验在 `examples/` 目录，先跑通用流程再加载对应案例：

- `examples/drama-watch/case.md`（陪你看剧账号）
- `examples/reply-examples.md`（回复文案范例）
