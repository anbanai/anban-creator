---
name: article-visual-design
description: Manages images for WeChat article (公众号图文) content including AI generation, compression, and CDN upload. Use when generating or processing images for WeChat articles.
---

# 公众号图文图片管理

## 运行模式

当 anbanwriter MCP 服务器可用时（检查 MCP 工具列表），使用 MCP 工具代替 CLI 命令：

| CLI 命令 | MCP 工具 |
|----------|----------|
| `abwriter image generate "{prompt}" --mode article -o {path}` | `generate_image` (channel_id, prompt, image_type="cover"或"content", output_path) |
| `abwriter image batch {file} --mode article` | `generate_batch_images` (channel_id, prompt, count, output_dir) |
| `abwriter image upload {file}` | `upload_image` (channel_id, file_path) |
| `abwriter image compress {file}` | `compress_image` (file_path) |

当 MCP 不可用时，回退到 CLI 命令。

---

## 三种使用场景

1. **文章内嵌图片**：Markdown 中用 `__generate:prompt__` 语法，批量提取并生成
2. **独立生成**：单张或批量生成图片，用于文章配图
3. **上传已有图片**：压缩后上传到微信 CDN

---

## CLI 命令

```bash
# 文章内图片批量提取+生成（推荐）
abwriter image batch article.md --mode article

# 单张生成
abwriter image generate "{prompt}" --mode article -o ./image.png

# 生成并上传
abwriter image generate "{prompt}" --mode article --upload -o ./image.png

# 上传已有图片
abwriter image upload ./image.png

# 下载在线图片（+上传）
abwriter image download https://example.com/image.jpg
abwriter image download https://example.com/image.jpg --upload
```

---

## 文章内图片语法

在 Markdown 中使用 AI 生成占位符：

```markdown
# 文章标题

正文段落...

![图片描述](__generate:详细的图片生成提示词__){width=100%}

继续正文...
```

`image batch` 命令提取所有 `__generate:...` 占位符，批量生成图片并替换。

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
