---
name: article-publishing
description: 微信公众号图文文章（news）草稿创建与管理。Use when user says "发布文章"、"创建草稿"、"draft"、"publish"、"推送"。
user-invocable: false
metadata:
  author: rick
  version: 2.3.0
---

# 微信公众号图文文章发布

适用于：带 HTML 排版的长文、深度文章。

## 草稿管理

查看现有草稿：`wechatwriter draft list`

## 命令

```bash
# 从 JSON 文件创建草稿
wechatwriter draft article /path/to/article.json

# 测试 HTML 草稿（验证内容）
wechatwriter draft test article.html cover.jpg -t "标题"

# 列出草稿箱
wechatwriter draft list
wechatwriter draft list --offset 20 --count 10
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
wechatwriter convert article.md --theme autumn-warm --draft

# 2. 生成封面图，记录 media_id
wechatwriter image generate -s 4k "封面图提示词"

# 3. 创建草稿
wechatwriter draft article ./draft.json
```

## 注意事项

- 内容格式为 HTML，所有 CSS 必须内联
- 封面图需通过 thumb_media_id 指定
- 内容大小限制：< 20,000 字符或 1MB
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

## 参考文档

- 微信API参考：[wechat-api.md](references/wechat-api.md)
