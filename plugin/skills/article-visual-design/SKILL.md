---
name: article-visual-design
description: Manages images for WeChat article (公众号图文) content including AI generation, compression, and CDN upload. Use when generating or processing images for WeChat articles.
---

# 公众号图文图片管理

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `generate_image` (channel_id, prompt, image_type="cover"或"content", output_path) | 生成单张图片 |
| `batch_generate_from_markdown` (channel_id, file_path, mode) | 从 Markdown 批量提取+生成图片 |
| `upload_image` (channel_id, file_path) | 上传图片到微信素材库 |
| `compress_image` (file_path) | 压缩图片 |

---

## 三种使用场景

1. **文章内嵌图片**：Markdown 中用 `__generate:prompt__` 语法，批量提取并生成
2. **独立生成**：单张或批量生成图片，用于文章配图
3. **上传已有图片**：压缩后上传到微信 CDN

---

## 使用方式

通过 MCP 工具调用：

1. **文章内图片批量提取+生成（推荐）**：调用 `batch_generate_from_markdown`，传入 Markdown 文件路径
2. **单张生成**：调用 `generate_image`，指定 image_type
3. **上传已有图片**：调用 `upload_image`，传入文件路径
4. **下载在线图片**：调用 `download_image`，传入 URL

---

## 文章内图片语法

在 Markdown 中使用 AI 生成占位符：

```markdown
# 文章标题

正文段落...

![图片描述](__generate:详细的图片生成提示词__){width=100%}

继续正文...
```

`batch_generate_from_markdown` 工具提取所有 `__generate:...` 占位符，批量生成图片并替换。

---

## 技术规范

**微信图片限制**：
- 最大尺寸：10MB（超出会被自动压缩）
- 最大宽度：1920px（保持比例压缩）
- 支持格式：JPG、PNG、GIF、WebP

**公众号常用比例**：
- 正文配图：16:9 或 4:3 横版
- 封面图（公众号封面）：2.35:1（900×383px 标准）
- 正方形配图：1:1

见 [references/image-syntax.md](references/image-syntax.md) 查看完整图片语法参考。
