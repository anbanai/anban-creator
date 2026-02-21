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
```

> 图片压缩由配置自动处理（`image.compress: true`），无需手动执行。

### 2. 封面图设计

生成专业封面图（WeChat 最低要求：≥3,686,400 像素）：

```bash
# 生成封面图，指定 4k 尺寸（约 2560x1440）
wechatwriter image generate "春茶品鉴指南封面，简约大气" -s 4k
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

# 2. 生成封面（4k 高清）
wechatwriter image generate "春茶品鉴指南封面" -s 4k

# 3. 上传本地图片到微信
wechatwriter image upload ./cover.jpg

# 4. 下载在线图片并上传到微信
wechatwriter image download https://example.com/photo.jpg
```

## 输出格式

```json
{
  "success": true,
  "data": {
    "prompt": "茶园清晨薄雾",
    "original_url": "https://generated.example.com/xxx.jpg",
    "wechat_url": "https://mmbiz.qpic.cn/mmbiz_jpg/xxx/0?wx_fmt=jpeg",
    "media_id": "media_id_xxx",
    "width": 1024,
    "height": 1024
  }
}
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
