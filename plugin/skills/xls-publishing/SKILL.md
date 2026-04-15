---
name: xls-publishing
description: Creates and manages WeChat Xiaolvshu (newspic) image post drafts with up to 20 images. Use when creating or managing WeChat Xiaolvshu (newspic) image post drafts.
---

# 微信公众号小绿书发布

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `upload_image` (channel_id, file_path) | 上传图片到微信素材库 |
| `publish_xls` (channel_id, title, content, images) | 创建小绿书草稿 |

---

适用于：纯图片帖子、旅行图集、产品展示，最多 20 张图片。内容为**纯文本**（不支持 HTML）。

## 草稿管理

查看发布历史：调用 `list_drafts` 和 `list_published` MCP 工具。

## publish_xls 参数说明

| 参数 | 说明 | 必填 |
|------|------|------|
| `title` | 帖子标题 | 是 |
| `content` | 纯文本描述 | 否 |
| `images` | 图片列表（文件路径或 media_id，自动上传本地图片） | 是 |
| `open_comment` | 开启评论 | 否 |
| `fans_only` | 仅粉丝可评论（需同时 open_comment） | 否 |

## 响应格式

```json
{
  "success": true,
  "data": {
    "media_id": "draft_media_id_xxx",
    "draft_url": "https://mp.weixin.qq.com/...",
    "count": 3,
    "uploaded_ids": ["media_id_1", "media_id_2", "media_id_3"]
  }
}
```

## 完整工作流

### 直接使用本地图片

1. 调用 `publish_xls`，传入图片文件路径列表，工具会自动上传到微信素材库
2. 可同时传入 `content` 文字描述和评论设置

### AI 生成图片完整工作流

1. 调用 `generate_image` 生成封面图片
2. 调用 `generate_batch_images` 批量生成内容图片
3. 调用 `publish_xls`，传入生成的图片路径，工具会自动上传并创建草稿

## 注意事项

- 内容为纯文本（不支持 HTML）
- 最多 20 张图片
- 封面图自动取第一张
- 从 Markdown 提取图片时，自动跳过网络图片（http/https 开头）
- SDK（silenceper）不支持 newspic，需直接调用微信 API

## 参考文档

- 微信API参考：[wechat-api.md](references/wechat-api.md)
