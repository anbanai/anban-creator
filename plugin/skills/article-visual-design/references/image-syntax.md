# 公众号图文图片语法参考

## 三种图片引用类型

### 1. 本地图片

```markdown
![描述](./images/photo.jpg)
![描述](/absolute/path/to/image.png)
```

转换时自动压缩（≤1920px，≤10MB）并上传到微信 CDN。

### 2. 在线图片

```markdown
![描述](https://example.com/image.jpg)
```

转换时自动下载 → 压缩 → 上传到微信 CDN。

### 3. AI 生成图片

```markdown
![图片描述](__generate:详细的图片生成提示词__)
```

转换时自动生成 → 压缩 → 上传到微信 CDN。

---

## AI 图片批量提取

调用 `batch_generate_from_markdown` MCP 工具从 Markdown 文件提取所有 AI 生成占位符，批量生成并可选上传。

---

## 压缩规则

- 最大宽度：1920px（超出按比例缩小）
- 最大体积：10MB（超出自动降质）
- 格式保留原始格式（JPG/PNG/WebP）
- GIF 不压缩（动态图原样上传）

---

## 上传返回值

```json
{
  "media_id": "xxx",
  "url": "https://mmbiz.qpic.cn/..."
}
```

`url` 是微信 CDN 永久链接，替换 Markdown 中的原始图片引用。

---

## 错误处理

- 本地文件不存在：报错并跳过该图片
- 在线图片下载失败：自动重试 3 次，失败则跳过
- AI 生成失败：报错提示，保留占位符原样
- 上传超限：先压缩，压缩后仍超限则报错
