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
| 小红书 | 视频封面 | [rednote-cover.md](references/rednote-cover.md) |
| 小红书 | 内容图 | [rednote-content.md](references/rednote-content.md) |

**通用 reference**（跨场景适用）：

- 主题样式（公众号 HTML）：[themes.md](references/themes.md)
- 图片语法：[image-syntax.md](references/image-syntax.md)
- 风格预设库（设计 `$STYLE` 时参考）：[styles/](references/styles/)
  - `cute-doodle`：可爱手绘涂鸦信息风（知识分享/生活方式）
  - `warm-healing`：温暖治愈手绘风（情感/读书/美食）
  - `hand-drawn-infographic`：手绘科普信息风（科普/干货/教程）
  - `pixel-art`：2D像素风格（科技/游戏/趣味）
  - `flat-vector`：扁平矢量插画风（职场/商业/效率）
  - `retro-poster`：复古海报风（文化/观点/推荐）
  - `watercolor-nature`：自然水彩风（旅行/自然/美食）
  - `scientific-diagram`：科学原理图风（科普/技术原理/数据）
  - `abstract-futurism`：抽象未来主义视觉（AI/科技前沿/概念）
  - `minimal-chinese`：极简国风海报（文化/国潮/传统）
  - `monet-impressionism`：莫奈印象派风格（自然/艺术/情感）
  - `heavy-color-freehand`：重彩写意风格（文化/国潮/艺术）

---

## 核心命令

```bash
# 生成图片（横版，公众号封面 2K）
anbanwriter image generate "{prompt}" -s 2k

# 生成图片（竖版，小绿书）
anbanwriter image generate "{prompt}" --mode xls --style "$STYLE"

# 生成图片（竖版，小红书，默认 3:4:1K）
anbanwriter image generate "{prompt}" --mode xhs --style "$STYLE"

# 【多图场景必须使用】组图模式：一次生成 N 张风格一致的图片（--count 指定张数）
anbanwriter image generate "{prompt}" --count 5 --mode xls --style "$STYLE" -o ./output_dir/

# 【多图场景必须使用】组图 + 参考图模式：基于封面风格批量生成内容图
anbanwriter image generate "{prompt}" --count 4 --mode xls --ref ./cover.png -o ./output_dir/

# 上传到微信素材库
anbanwriter image upload ./image.png

# 下载并上传在线图片
anbanwriter image download https://example.com/image.jpg

# 批量生成章节配图
anbanwriter image batch article.md --style "$STYLE" -o images.json

# 将多张图片组装成视频（需要 ffmpeg）
anbanwriter video assemble $DIR/ --duration 1.5 --transition 0.5 -o $DIR/video.mp4
```

> **多图生成规则**：需要生成 2 张及以上图片时，**必须使用组图模式**（`--count`），禁止逐张调用 `image generate`。理由：逐张生成会导致风格漂移（每次独立推理，配色/构图不一致），且效率低。组图模式在同一上下文中生成，风格一致性显著更高。输出文件自动命名为 `image_01.png`、`image_02.png` ... `image_0N.png`（存入 `-o` 指定目录）。

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

### image generate --count（组图模式，输出到目录）

```json
{
  "success": true,
  "data": {
    "count": 5,
    "output_dir": "./output_dir/",
    "files": [
      "output_dir/image_01.png",
      "output_dir/image_02.png",
      "output_dir/image_03.png",
      "output_dir/image_04.png",
      "output_dir/image_05.png"
    ]
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
- **小绿书封面/内容图**：竖版 3:4（2K），使用 `--mode xls` 或 `--size 3:4`
- **小红书封面/内容图**：竖版 3:4（1K），使用 `--mode xhs` 或 `--size 3:4:1K`
- **尺寸格式**：`比例` 或 `比例:档位`（如 `16:9`, `3:4:1K`, `3:4:4K`），默认档位 2K
- **文件大小**：单张不超过 10MB（上传前自动压缩）
- **格式支持**：JPG、PNG
- **色彩模式**：RGB，避免 CMYK

## 注意事项

- **不要读取图片文件内容**，这会消耗大量 token。
