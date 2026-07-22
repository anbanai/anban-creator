---
name: article-publishing
description: 'Use when creating or managing WeChat news article drafts. Also use when user mentions ''发草稿'', ''发布文章'', ''创建草稿'', ''publish draft'', ''草稿箱'', or when the article pipeline reaches the draft publishing step. Creates and manages WeChat news article drafts (图文草稿) with HTML formatting.'
---

# 微信公众号图文文章发布

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。


## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `upload_image` (project_id, file_path) | 上传图片到微信素材库 |
| `publish_draft` (project_id, articles) | 创建图文文章草稿 |
| `list_drafts` (project_id) | 查看已有草稿 |
| `list_published_articles` (project_id) | 查看已发布文章 |

---

适用于：带 HTML 排版的长文、深度文章。

## 草稿管理

查看发布历史：调用 `list_drafts` 和 `list_published_articles` MCP 工具。

## 使用方式

通过 MCP 工具调用 `publish_draft`，传入 articles 数组创建草稿。

## draft.json 格式

```json
{
  "articles": [
    {
      "title": "文章标题",
      "content": "<p>HTML 正文...</p>",
      "author": "署名（仅取自 get_project_profile 顶层 author，详见「作者字段来源」）",
      "digest": "摘要（120字符以内）",
      "thumb_media_id": "封面图的 media_id",
      "show_cover_pic": 1,
      "content_source_url": "原文链接（可选）"
    }
  ]
}
```

## 作者字段来源（硬性）

`draft.json` 的 `author`（公众号署名）**必须且仅能**取自 `get_project_profile` 返回的**顶层 `author`** 字段（已按 task > project 两层解析；可看返回的 `author_source` 追溯来源）。

- 顶层 `author` 非空 → `articles[0].author = <顶层 author>`，**原样填入，不改写**。
- 顶层 `author` 为空 → **省略 `author` 字段**（或留空），由公众号后台用默认署名。
- **严禁**用下列任一字段顶替 `author`（它们都**不是**署名）：
  - `writer`：writer 资源 key（如 `dan-koe`），决定文风，不决定署名。
  - 写作风格头像/昵称：仅 Studio 展示元数据，不会出现在 `get_project_profile`，不得用于发布。

- **署名污染自检（防回归闸门）**：若顶层 `author` 命中 `get_project_profile` 返回的 `available_writers` 中任一写作风格的**人设名**（`name`，如 "Dan Koe"）或 **key**（`english_name`，如 `dan-koe`），几乎肯定是上游把写作风格误写进了署名字段——真实发布署名不该等于某个写作风格人设。此时**不要盲目发布**：按真实发布者署名修正后再发，并在 `submit_agent_feedback` 里标注「疑似署名污染：author 命中 writer 人设 X」。仅当署名确属真实同名（如作者本人就叫某人设名）才放行。

映射示例：

```
get_project_profile 返回            → draft.articles[0].author
author = "张三"                      → "张三"
writer = "dan-koe"                  → 不入 author（仅用于正文口吻）
author 为空                         → 省略 author 字段（切勿用 writer 顶替）
```

## thumb_media_id 来源（按图片开关，硬性）

公众号文章的封面可由用户在创建任务/计划时关闭。`thumb_media_id`（封面 media_id）的填法**严格取决于结构化运行控制 `article_image_mode`**：

| `article_image_mode` | 封面开关 | `thumb_media_id` 填法 |
|----------------------|----------|------------------------|
| `cover_and_content`（缺失时也按此处理） | 开 | 填步骤 6 封面的 `media_id` |
| `cover_only` | 开 | 填步骤 6 封面的 `media_id` |
| `content_only` | 关 | **省略 `thumb_media_id` 字段**（即使有正文配图也**不复用**作封面） |
| `text_only` | 关 | **省略 `thumb_media_id` 字段**；在 `final-review.md` 记录「未生成封面，公众号后台可能不显示封面/需手动设置」 |

**关键约束**：封面开关关闭 → 一律不设 `thumb_media_id`，**绝不**用任何正文配图的 `media_id` 顶替。服务端 `publish_draft` 对 `thumb_media_id` 是可选的，省略它不会导致发布失败——公众号后台只是不显示封面（用户已知情选择）。封面开关开启但封面 `media_id` 缺失（生成失败）才是真正的发布阻塞，需回到 `article-cover-design` skill 重新生成。

## 响应格式

```json
{
  "success": true,
  "data": {
    "media_id": "draft_media_id_xxx",
    "draft_url": "https://mp.weixin.qq.com/..."
  }
}
```

## 完整发布工作流

1. 调用 `render_template`（带 `layout_plan`）将 Markdown + 节奏计划确定性渲染为 WeChat HTML（替代旧的 `convert_markdown`）
2. 调用 `generate_image`（带 `verify_with_vision=true, upload_to_cdn=true`）生成封面图——**生成与上传原子化**，同一调用内完成生成→校验→压缩→上传微信 CDN，直接返回 `media_id` + `wechat_url`（无需单独 `upload_image`）。**流水线场景**：步骤 6d 已取得封面 `media_id`，直接复用即可，跳过本步
3. （仅当上一步返回 `upload_error` 时）调用 `upload_image(file_path="$DIR/cover.png")` 单独重传获取 `media_id`，不重新生成
4. 调用 `publish_draft` 创建草稿

## 流水线集成

本 skill 是 article 流水线的最后一步。前置条件：

| 前置产出 | 来源 | 用途 |
|----------|------|------|
| `$DIR/05-article.html` | content-writing skill（通过 `render_template` 生成） | 作为 articles[0].content |
| `$DIR/cover.png` 的 `media_id` | article-visual-design skill（已通过 vision 校验） | 作为 articles[0].thumb_media_id |
| `$DIR/seo-result.md` | seo-optimization skill | 提取优化后的标题和摘要 |
| `$DIR/visual-rhythm-plan.md` | article-visual-design skill | 渲染审计参考（HTML 应已按 plan 渲染） |
| `$DIR/images.json` | article-visual-design skill | 视觉审计参考（含 vision 校验记录） |

## 发布前验证

创建草稿前，确认以下所有项（封面 `media_id` 项**仅在封面开关开启时**要求；关闭时不带 `thumb_media_id`，见「thumb_media_id 来源」）：

- [ ] HTML 文件存在且内容完整
- [ ] 封面 `media_id` 已获取（非空，**仅封面开关开启时**）
- [ ] 标题使用 SEO 优化标题（来自 seo-result.md）
- [ ] 摘要使用 SEO 优化摘要（来自 seo-result.md）
- [ ] 内容大小 < 20,000 字符或 1MB

## 注意事项

- 内容格式为 HTML，所有 CSS 必须内联
- 封面图需通过 thumb_media_id 指定
- 内容大小限制：< 20,000 字符或 1MB
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

## 常见失败与修复

| 问题 | 原因 | 修复 |
|------|------|------|
| 草稿创建失败 | media_id 无效或过期 | 重新上传封面图获取新 media_id |
| 内容超限 | HTML 超过 20,000 字符 | 精简文章内容或拆分为多篇 |
| 标题过长 | 超过 64 字符 | 缩短标题 |
| 摘要过长 | 超过 120 字符 | 缩短摘要 |

## 参考文档

- 微信API参考：[wechat-api.md](references/wechat-api.md)
