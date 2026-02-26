---
name: visual-design
description: 微信公众号视觉设计工具，提供配图建议、封面设计和版式优化服务。Use when user mentions "封面"、"配图"、"主题"、"theme"、"图片生成"、"cover image"。
user-invocable: false
metadata:
  author: rick
  version: 2.3.0
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

## 使用示例

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

- 主题样式：[themes.md](references/themes.md)
- 图片语法：[image-syntax.md](references/image-syntax.md)
- 封面图平台规范：[cover-guidelines.md](references/cover-guidelines.md)
