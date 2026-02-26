---
name: post-publishing
description: 微信公众号小绿书（newspic）图片帖子的创建与管理。Use when user mentions "小绿书"、"图片帖"、"newspic"、"image post"。
user-invocable: false
metadata:
  author: rick
  version: 2.3.0
---

# 微信公众号小绿书发布

适用于：纯图片帖子、旅行图集、产品展示，最多 20 张图片。内容为**纯文本**（不支持 HTML）。

## 草稿管理

查看现查看现发布历史：`wechatwriter account history`

## 命令

```bash
# 从图片路径列表创建
wechatwriter draft post \
  -t "帖子标题" \
  --images "photo1.jpg,photo2.jpg,photo3.jpg"

# 从 Markdown 文件提取本地图片
wechatwriter draft post \
  -t "旅行日记" \
  -m article.md

# 使用已上传的 media_id（跳过重复上传，适合 image generate 后直接发布）
wechatwriter draft post \
  -t "AI 生成图集" \
  --media-ids "media_id_1,media_id_2,media_id_3"

# 混合使用：已有 media_id + 本地图片
wechatwriter draft post \
  -t "混合图集" \
  --media-ids "media_id_1" \
  --images "local_photo.jpg"

# 带描述文字和评论设置
wechatwriter draft post \
  -t "美食分享" \
  -c "今天的午餐，简单又美味" \
  --images food.jpg \
  --open-comment

# 仅粉丝可评论
wechatwriter draft post \
  -t "会员专享" \
  --images a.jpg,b.jpg \
  --open-comment --fans-only

# 预览模式（不实际创建）
wechatwriter draft post \
  -t "测试" --images a.jpg,b.jpg --dry-run

# 保存结果到文件
wechatwriter draft post \
  -t "标题" --images a.jpg -o output/02-result.json
```

## 参数说明

| 参数 | 说明 | 必填 |
|------|------|------|
| `-t` / `--title` | 帖子标题 | 是 |
| `-c` / `--content` | 纯文本描述 | 否 |
| `--images` | 本地图片文件路径，逗号分隔（将自动上传到微信素材库） | 三选一 |
| `--media-ids` | 微信素材 ID（media_id），逗号分隔（已上传到素材库的图片，跳过重复上传） | 三选一 |
| `-m` / `--from-markdown` | 从 Markdown 提取本地图片 | 三选一 |
| `--open-comment` | 开启评论 | 否 |
| `--fans-only` | 仅粉丝可评论（需同时 --open-comment） | 否 |
| `--dry-run` | 预览模式，不实际创建 | 否 |
| `-o` / `--output` | 保存结果到 JSON 文件 | 否 |

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

```bash
# 1. 预览（验证图片路径和数量）
wechatwriter draft post \
  -t "周末出游" --images p1.jpg,p2.jpg,p3.jpg --dry-run

# 2. 确认无误后正式发布
wechatwriter draft post \
  -t "周末出游" --images p1.jpg,p2.jpg,p3.jpg \
  -c "难得的好天气" --open-comment \
  -o output/02-result.json
```

### AI 生成图片完整工作流

```bash
# 1. 生成图片到本地
wechatwriter image generate "封面" --post --style "$STYLE" -o output/cover.jpg
wechatwriter image generate "内容" --post --style "$STYLE" -o output/page1.jpg

# 2. 上传到微信素材库，获取 media_id
wechatwriter image upload output/cover.jpg
# → data.media_id = "COVER_MID"
wechatwriter image upload output/page1.jpg
# → data.media_id = "PAGE1_MID"

# 3. 用 media_id 创建小绿书（跳过重复上传）
wechatwriter draft post -t "标题" --media-ids "COVER_MID,PAGE1_MID"
```

也可以直接用本地文件路径（自动上传，但无法复用 media_id）：

```bash
wechatwriter draft post -t "标题" --images "output/cover.jpg,output/page1.jpg"
```

## 注意事项

- 内容为纯文本（不支持 HTML）
- 最多 20 张图片
- 封面图自动取第一张
- 从 Markdown 提取图片时，自动跳过网络图片（http/https 开头）
- SDK（silenceper）不支持 newspic，需直接调用微信 API

## 参考文档

- 微信API参考：[wechat-api.md](references/wechat-api.md)
