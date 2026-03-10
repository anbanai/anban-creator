---
name: visual-design
description: 微信公众号视觉设计工具，提供配图建议、封面设计和版式优化服务。Use when user mentions "封面"、"配图"、"主题"、"theme"、"图片生成"、"cover image"。
user-invocable: false
metadata:
  author: Rick
  version: 3.0.0
---

# 微信公众号视觉设计工具

专业的微信公众号视觉设计工具，提供配图方案推荐、封面设计指导和版式布局优化。

## 场景路由

根据平台和场景，加载对应 reference：

| 平台 | 场景 | Reference |
|------|------|-----------|
| 公众号 | 封面图 | [wechat-cover.md](references/wechat-cover.md) |
| 公众号 | 章节配图 | [wechat-section-images.md](references/wechat-section-images.md) |
| 小绿书 | 封面图 | [xiaolvshu-cover.md](references/xiaolvshu-cover.md) |
| 小绿书 | 内容图 | [xiaolvshu-content.md](references/xiaolvshu-content.md) |
| 小红书 | 封面图 | [rednote-cover.md](references/rednote-cover.md) |
| 小红书 | 内容图 | [rednote-content.md](references/rednote-content.md) |

**通用 reference**（跨场景适用）：

- 主题样式（公众号 HTML）：[themes.md](references/themes.md)
- 图片语法：[image-syntax.md](references/image-syntax.md)
- 风格预设库（设计 `$STYLE` 时参考）：[styles/](references/styles/)
  - `morandi-flat`：莫兰迪扁平（生活/知识/干货）
  - `tech-dark`：科技暗色（科技/AI/数码）
  - `warm-paper`：暖色纸质（美食/文化/读书）
  - `nature-watercolor`：自然水彩（旅行/美食/自然）
  - `minimal-white`：极简白色（职场/商业/效率）

---

## 核心命令

```bash
# 生成图片（横版，公众号封面 2K）
anbanwriter image generate "{prompt}" -s 2k

# 生成图片（竖版，小绿书/小红书）
anbanwriter image generate "{prompt}" --post --style "$STYLE"

# 上传到微信素材库
anbanwriter image upload ./image.png

# 下载并上传在线图片
anbanwriter image download https://example.com/image.jpg

# 批量生成章节配图
anbanwriter image batch article.md --style "$STYLE" -o images.json

# 将多张图片组装成视频（需要 ffmpeg）
anbanwriter video assemble $DIR/ --duration 3 --transition 1 -o $DIR/video.mp4
```

---

## 输出格式

### image generate（生成图片到本地）

```json
{
  "success": true,
  "data": {
    "prompt": "茶园清晨薄雾",
    "url": "https://generated.example.com/xxx.jpg",
    "file_path": "generated_image.png",
    "model": "gemini-3-pro-image-preview",
    "size": "16:9"
  }
}
```

### image upload / image download（上传到微信素材库）

```json
{
  "success": true,
  "data": {
    "media_id": "微信素材 ID",
    "wechat_url": "https://mmbiz.qpic.cn/... (微信素材库外部访问链接)"
  }
}
```

---

## 生成 + 上传工作流

```bash
# Step 1: 生成图片到本地（返回 file_path）
anbanwriter image generate "茶园清晨薄雾" -o cover.jpg

# Step 2: 上传到微信素材库（返回 media_id + wechat_url）
anbanwriter image upload cover.jpg
```

---

## 技术规格

- **公众号封面**：横版 16:9（2K），使用 `--size 16:9`
- **公众号章节配图**：横版 16:9（2K），使用 `--size 16:9`
- **小绿书封面/内容图**：竖版 3:4（2K），使用 `--post` flag 或 `--size 3:4`
- **小红书封面/内容图**：竖版 3:4（1K），使用 `--size 3:4:1K`
- **尺寸格式**：`比例` 或 `比例:档位`（如 `16:9`, `3:4:1K`, `3:4:4K`），默认档位 2K
- **文件大小**：单张不超过 10MB（上传前自动压缩）
- **格式支持**：JPG、PNG
- **色彩模式**：RGB，避免 CMYK
