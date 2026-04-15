---
name: article-publishing
description: Creates and manages WeChat news article drafts with HTML formatting. Use when creating or managing WeChat news article drafts.
---

# 微信公众号图文文章发布

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `upload_image` (channel_id, file_path) | 上传图片到微信素材库 |
| `publish_draft` (channel_id, articles) | 创建图文文章草稿 |
| `list_drafts` (channel_id) | 查看已有草稿 |
| `list_published` (channel_id) | 查看已发布文章 |

---

适用于：带 HTML 排版的长文、深度文章。

## 草稿管理

查看发布历史：调用 `list_drafts` 和 `list_published` MCP 工具。

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

## 注意事项

- 内容格式为 HTML，所有 CSS 必须内联
- 封面图需通过 thumb_media_id 指定
- 内容大小限制：< 20,000 字符或 1MB
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

## 参考文档

- 微信API参考：[wechat-api.md](references/wechat-api.md)
