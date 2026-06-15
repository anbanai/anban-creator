---
name: article-publishing
description: Creates and manages WeChat news article drafts (图文草稿) with HTML formatting. Use when creating or managing WeChat news article drafts. Also use when user mentions '发草稿', '发布文章', '创建草稿', 'publish draft', '草稿箱', or when the article pipeline reaches the draft publishing step.
---

# 微信公众号图文文章发布

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `upload_image` (channel_id, file_path) | 上传图片到微信素材库 |
| `publish_draft` (channel_id, articles) | 创建图文文章草稿 |
| `list_drafts` (channel_id) | 查看已有草稿 |
| `list_published_articles` (channel_id) | 查看已发布文章 |

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
      "author": "作者",
      "digest": "摘要（120字符以内）",
      "thumb_media_id": "封面图的 media_id",
      "show_cover_pic": 1,
      "content_source_url": "原文链接（可选）"
    }
  ]
}
```

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

1. 调用 `convert_markdown` 将 Markdown 转为 WeChat HTML
2. 调用 `generate_image` 生成封面图到本地
3. 调用 `upload_image` 上传封面图到微信素材库，记录返回的 media_id
4. 调用 `publish_draft` 创建草稿

## 流水线集成

本 skill 是 article 流水线的最后一步。前置条件：

| 前置产出 | 来源 | 用途 |
|----------|------|------|
| `$DIR/05-article.html` | content-writing skill | 作为 articles[0].content |
| `$DIR/cover.png` 的 `media_id` | article-visual-design skill | 作为 articles[0].thumb_media_id |
| `$DIR/seo-result.md` | seo-optimization skill | 提取优化后的标题和摘要 |

## 发布前验证

创建草稿前，确认以下所有项：

- [ ] HTML 文件存在且内容完整
- [ ] 封面 `media_id` 已获取（非空）
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
