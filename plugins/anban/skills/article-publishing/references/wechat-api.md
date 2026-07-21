# 微信公众号 API 参考（图文文章）

## Contents

- [SDK](#sdk)
- [API 1: 上传永久素材](#api-1-上传永久素材)
  - [MCP 工具](#mcp-工具)
  - [响应格式](#响应格式)
  - [错误码](#错误码)
- [API 2: 新建图文草稿](#api-2-新建图文草稿)
  - [MCP 工具](#mcp-工具)
  - [响应格式](#响应格式)
  - [文章字段说明](#文章字段说明)
  - [草稿错误码](#草稿错误码)
- [图片生成 API](#图片生成-api)
- [认证配置](#认证配置)

## SDK

使用 `github.com/silenceper/wechat/v2` SDK，支持素材上传和草稿创建。

## API 1: 上传永久素材

上传图片到微信素材库，返回 media_id 和 CDN URL。

### MCP 工具

调用 `upload_image` 上传本地图片，调用 `download_image` 下载在线图片。

### 响应格式

```json
{
  "success": true,
  "media_id": "media_id_xxx",
  "wechat_url": "https://mmbiz.qpic.cn/mmbiz_jpg/xxx/0?wx_fmt=jpeg"
}
```

### 错误码

| 错误码 | 说明 | 处理方式 |
|--------|------|----------|
| 40001 | AppID 错误 | 检查配置 |
| 40004 | 文件为空 | 检查文件路径 |
| 40005 | 文件类型不支持 | 检查图片格式 |
| 40006 | 文件大小超限 | 压缩图片 |
| 42001 | AppSecret 错误 | 检查配置 |

## API 2: 新建图文草稿

创建图文草稿到公众号草稿箱。

### MCP 工具

调用 `publish_draft`，传入 project_id 和 articles 数组。

### 响应格式

```json
{
  "success": true,
  "media_id": "draft_media_id_xxx",
  "draft_url": "https://mp.weixin.qq.com/..."
}
```

### 文章字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| Title | string | 是 | 标题，不超过 64 字符 |
| Author | string | 否 | 作者 |
| Digest | string | 否 | 摘要，不超过 120 字符 |
| Content | string | 是 | HTML 正文（< 2万字符或 1MB） |
| ThumbMediaID | string | 是 | 封面图 media_id |
| ShowCoverPic | int | 否 | 是否显示封面，0 或 1 |
| ContentSourceURL | string | 否 | 原文链接 |

### 草稿错误码

| 错误码 | 说明 | 处理方式 |
|--------|------|----------|
| 45002 | 内容超过 2 万字符或 1MB | 精简内容 |
| 45004 | 标题超过 64 字符 | 缩短标题 |
| 45005 | 摘要超过 120 字符 | 缩短摘要 |

## 图片生成 API

使用兼容 OpenAI 的图片生成接口，通过 `.anban-creator/settings.json` 配置 API key 和 base URL。

| 错误 | 处理方式 |
|------|----------|
| API Key 无效 | 返回错误，提示检查配置 |
| 配额超限 | 返回错误，提示稍后重试 |
| 生成失败 | 返回错误，跳过该图片 |
| 超时 | 重试 1 次，仍失败则跳过 |

## 认证配置

WeChat AppID 和 AppSecret 通过 `.anban-creator/settings.json` 配置文件管理。

## 速率限制

| API | 限制 |
|-----|------|
| 上传素材 | 100 次/天 |
| 创建草稿 | 100 次/天 |

## 最佳实践

- 缓存已上传图片的 media_id，避免重复上传
- 失败后等待 1 秒再重试，最多重试 3 次
- 批量处理时控制并发数，避免触发速率限制
- 使用 zap 结构化日志记录上传结果，敏感字段脱敏
