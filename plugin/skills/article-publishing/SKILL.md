---
name: article-publishing
description: Creates and manages WeChat news article drafts with HTML formatting. Use when creating or managing WeChat news article drafts.
---

# 微信公众号图文文章发布

适用于：带 HTML 排版的长文、深度文章。

## 草稿管理

查看发布历史：`abwriter account history`

## 命令

```bash
# 查看草稿箱和已发布文章
abwriter account history
abwriter account history --count 10

# 从 JSON 文件创建草稿
abwriter draft article /path/to/article.json

# 测试 HTML 草稿（验证内容）
abwriter draft test article.html cover.jpg -t "标题"
```

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

```bash
# 1. 转换 Markdown → WeChat HTML（同时保存 draft.json）
abwriter convert article.md --theme autumn-warm --draft

# 2. 生成封面图到本地
abwriter image generate -s 2k "封面图提示词" -o output/cover.jpg
# 3. 上传封面图到微信素材库，记录返回的 media_id
abwriter image upload output/cover.jpg

# 4. 创建草稿
abwriter draft article ./draft.json
```

## 注意事项

- 内容格式为 HTML，所有 CSS 必须内联
- 封面图需通过 thumb_media_id 指定
- 内容大小限制：< 20,000 字符或 1MB
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

## 参考文档

- 微信API参考：[wechat-api.md](references/wechat-api.md)
