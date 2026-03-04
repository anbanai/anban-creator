---
name: visual-design
description: 微信公众号视觉设计工具，提供配图建议、封面设计、风格预设和版式优化服务。Use when user mentions "封面"、"配图"、"主题"、"theme"、"图片生成"、"cover image"、"风格预设"、"style preset"。
user-invocable: false
metadata:
  author: Rick
  version: 2.4.0
---

# 微信公众号视觉设计工具

专业的微信公众号视觉设计工具，提供配图方案推荐、封面设计指导和版式布局优化。

## 核心功能

### 1. 图片处理

处理文章配图，支持多种方式：

```bash
# 上传本地图片到微信素材库
wechatwriter image upload ./photo.jpg

# 下载并上传在线图片
wechatwriter image download https://example.com/image.jpg

# AI生成配图
wechatwriter image generate "茶园清晨薄雾"

# AI生成配图，指定统一风格（多图一致性）
wechatwriter image generate "封面标题" --style "扁平插画，莫兰迪色系，圆角卡片"
```

> 图片压缩由配置自动处理（`image.compress: true`），无需手动执行。

### 2. 封面图设计

生成专业封面图（WeChat 最低要求：≥3,686,400 像素）：

```bash
# 生成封面图，指定 2k 尺寸（约 2560x1440）
wechatwriter image generate "春茶品鉴指南封面，简约大气" -s 2k
```

**设计要素**:

- 视觉冲击力: 色彩对比和构图平衡
- 主题突出: 一眼识别文章核心内容
- 文字清晰: 标题文字可读性强
- 品牌一致: 符合整体视觉风格

### 3. 章节配图（图文并茂）

为文章各章节生成内容相关配图，确保图文并茂。

**验证流程**：

1. 读取文章 Markdown，按 `##` 标题拆分章节
2. 检查每个章节是否至少有一个 `![desc](__generate:prompt__)` 占位符
3. 对缺少配图的章节，根据章节内容创建提示词并插入

**风格一致性**：

- 在生成第一张配图前，根据文章主题确定统一风格描述（如"水彩插画，柔和暖色调，留白构图"）
- 所有配图使用同一 `--style` 参数
- 风格描述应与文章基调匹配（严肃文章用沉稳色调，轻松文章用明快色调）

**提示词要求**：

- 必须从章节具体内容中提炼，反映该章节讨论的核心主题
- 包含 2-3 个来自章节的关键词或意象
- 匹配章节的情感基调
- 禁止通用装饰性描述（如"美丽风景"、"抽象背景"）

**生成流程**（每张配图）：

```bash
# 确定统一风格（仅一次）
STYLE="水彩插画，柔和暖色调，留白构图"
# 生成每张配图都使用同一风格
wechatwriter image generate "从章节内容提炼的具体提示词" --style "$STYLE"
wechatwriter image upload <file_path>
```

详见：[章节配图规范](references/section-images.md)

### 4. 视觉风格预设

通过结构化预设确保多图视觉一致性，避免遗漏关键设计维度：

```bash
# 查看所有可用预设
wechatwriter style list

# 查看预设详情（配色、边框、背景、完整提示词）
wechatwriter style show morandi-flat

# 将预设名称直接用于 --style 参数
wechatwriter image generate "封面" --post --style morandi-flat
wechatwriter image generate "内容图" --post --style morandi-flat
```

**内置预设**（通过 `style list` 获取最新列表）：

| 预设名 | 分类 | 适用场景 |
|--------|------|---------|
| `morandi-flat` | 知识图卡 | 知识分享、教程 |
| `tech-dark` | 科技现代 | 科技、编程、AI |
| `warm-paper` | 文艺清新 | 生活、读书、文化 |
| `minimal-white` | 商务简约 | 职场、效率、商业 |
| `nature-watercolor` | 自然手绘 | 旅行、美食、自然 |

预设名称未命中时，`--style` 参数值视为自由文本（向后兼容）。

详见：[视觉风格预设](references/style-presets.md)

```bash
# 1. 生成配图（压缩自动进行）
wechatwriter image generate "茶园清晨薄雾，阳光透过茶树"

# 2. 生成封面（2k 高清）
wechatwriter image generate "春茶品鉴指南封面" -s 2k

# 3. 生成小绿书多图（统一风格）
STYLE="扁平信息图，低饱和莫兰迪配色，圆角卡片，无衬线字体"
wechatwriter image generate "封面" --post --style "$STYLE"
wechatwriter image generate "内容1" --post --style "$STYLE"
wechatwriter image generate "内容2" --post --style "$STYLE"

# 4. 上传本地图片到微信
wechatwriter image upload ./cover.jpg

# 5. 下载在线图片并上传到微信
wechatwriter image download https://example.com/photo.jpg
```

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

## 生成 + 上传工作流

图片生成和上传是两个独立步骤：

```bash
# Step 1: 生成图片到本地（返回 file_path）
wechatwriter image generate "茶园清晨薄雾" -o cover.jpg
# → data.file_path = "cover.jpg"

# Step 2: 上传到微信素材库（返回 media_id + wechat_url）
wechatwriter image upload cover.jpg
# → data.media_id = "xxx", data.wechat_url = "https://..."
```

## 设计标准

- **分辨率**: 封面图建议 900x500 像素（最低 3,686,400 总像素）
- **文件大小**: 单张图片不超过 10MB（上传前自动压缩）
- **格式支持**: JPG、PNG
- **色彩模式**: RGB，避免CMYK

## 命令帮助

```bash
# 图片处理（上传、下载、生成）
wechatwriter image --help

# AI 生成图片
wechatwriter image generate --help
```

## 参考文档

- 视觉风格预设：[style-presets.md](references/style-presets.md)
- 主题样式：[themes.md](references/themes.md)
- 图片语法：[image-syntax.md](references/image-syntax.md)
- 封面图平台规范：[cover-guidelines.md](references/cover-guidelines.md)
- 章节配图规范：[section-images.md](references/section-images.md)
