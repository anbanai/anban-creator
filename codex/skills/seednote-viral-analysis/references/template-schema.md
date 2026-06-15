# 爆款模板 JSON 规范

## viral-template.json

供 `seednote-writing` 读取，必须是合法 JSON。

```json
{
  "title_template": "数字/人群/痛点/反差等标题结构，不复制原标题",
  "cover_template": "封面主视觉、文字层级、色彩方向和信息密度，不复制具体构图",
  "body_template": "开头钩子、段落结构、信息密度和收藏理由",
  "interaction_template": "合规互动机制，不包含评论区/点赞收藏等诱导互动",
  "tag_template": "核心大词 + 垂直词 + 长尾词策略",
  "audience_insight": "源笔记命中的用户需求或人群心理",
  "viral_mechanism": "源笔记可能获得点击/收藏/评论的核心机制",
  "rewrite_constraints": [
    "改写时必须替换的内容、案例、视角或表达"
  ],
  "do_not_copy": [
    "不得复用的源作者经历、原图元素、评论原话、品牌承诺或专属素材"
  ],
  "recommended_clone_depth": "style-only",
  "confidence": "medium"
}
```

字段要求：

| 字段 | 必填 | 说明 |
|------|------|------|
| `title_template` | 是 | 抽象结构，不是改写标题 |
| `cover_template` | 是 | 视觉方向和信息层级，不得要求照搬源图 |
| `body_template` | 是 | 可迁移结构，避免源作者专属经历 |
| `interaction_template` | 是 | 必须合规，禁止显性诱导互动 |
| `tag_template` | 是 | 标签组合策略 |
| `audience_insight` | 是 | 用户需求或心理动机 |
| `viral_mechanism` | 是 | 爆款机制摘要 |
| `rewrite_constraints` | 是 | 至少 2 条 |
| `do_not_copy` | 是 | 至少 2 条 |
| `recommended_clone_depth` | 是 | `style-only` / `medium` / `tight` |
| `confidence` | 是 | `high` / `medium` / `low` |

默认值：

- `recommended_clone_depth` 默认 `style-only`。
- 数据缺失较多时 `confidence` 必须是 `low` 或 `medium`。
- 相似度风险高时不得推荐 `tight`。

## template-meta.json

供 seednote 完成 hook 调用 `save_template`。

```json
{
  "type": "seednote",
  "name": "基于源笔记主题生成的模板名",
  "category": "viral_analysis",
  "source_feed_id": "源笔记 ID",
  "source_url": "源笔记链接",
  "tags": ["行业", "内容类型", "爆款机制"],
  "template_hash": "由 viral-template.json 的核心字段生成的稳定摘要",
  "save_eligible": true
}
```

字段要求：

- `type` 固定为 `seednote`。
- `category` 固定为 `viral_analysis`。
- `name` 必须是模板名，不能是源笔记标题原文。
- `template_hash` 用于幂等保存；如果无法计算，写空字符串并在报告中说明。
- `save_eligible=false` 用于相似度风险高、证据不足或用户明确不保存的情况。

## 合规要求

- JSON 中不得包含整段源笔记正文。
- 不得复制评论区原话作为互动模板。
- 不得把源图人物姿势、图标组合、文字框位置写成必须复用的模板。
- 不得承诺爆款结果，只能描述可迁移机制。
