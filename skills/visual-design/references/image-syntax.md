# 图片语法说明

## 图片引用类型

在 Markdown 中，支持三种图片引用方式：

### 1. 本地图片

```markdown
![图片描述](./path/to/image.png)
![图片描述](/absolute/path/image.jpg)
```

处理流程：读取 → 压缩 → 上传微信素材库 → 替换为 CDN URL

### 2. 在线图片

```markdown
![图片描述](https://example.com/image.jpg)
```

处理流程：下载 → 压缩 → 上传微信素材库 → 替换为 CDN URL

### 3. AI 生成图片

```markdown
![图片描述](__generate:A cute cat sitting on a windowsill__)
```

**语法格式：** `![alt](__generate:prompt__)`

- `__generate:` 是固定前缀
- `prompt` 是图片生成提示词，支持中英文

处理流程：提取 prompt → 调用生成 API → 下载 → 压缩 → 上传微信素材库 → 替换为 CDN URL

**配置要求**（在 `.wechatwriter/settings.json` 中）：

```json
"article": {
  "image": { "key": "your_api_key", "provider": "gemini" }
}
```

## 图片生成提供者

| 提供者 | provider 值 | 默认模型 | 说明 |
|--------|-------------|----------|------|
| OpenAI | `openai`（默认） | dall-e-3 | DALL-E 系列，付费 API |
| Google Gemini | `gemini` / `google` | gemini-3-pro-image-preview | 需 Google API Key |
| OpenRouter | `openrouter` / `or` | google/gemini-3-pro-image-preview | 多模型网关 |
| 火山引擎 | `volcengine` | doubao-seedream-4-5-251128 | 国内可用 |

### Gemini 配置

```json
"article": {
  "image": {
    "key": "your_google_api_key",
    "provider": "gemini",
    "model": "gemini-3-pro-image-preview",
    "size": "2560x1440"
  }
}
```

其他可用模型：`gemini-2.5-flash-preview-image`、`gemini-2.0-flash-exp-image-generation`

### OpenRouter 配置

```json
"article": {
  "image": {
    "key": "your_openrouter_api_key",
    "provider": "openrouter",
    "model": "google/gemini-3-pro-image-preview",
    "size": "2560x1440"
  }
}
```

其他可用模型：`google/gemini-2.5-flash-image-preview`、`black-forest-labs/flux.2-pro`、`black-forest-labs/flux.2-flex`

## 图片生成方式

### 方式一：自然语言对话（推荐）

直接用自然语言告诉 Claude 生成图片：

```
"帮我在文章开头生成一张产品概念图"
"生成一张可爱的猫坐在窗台上的图片"
```

Claude 会自动读取上下文、创建提示词、调用生成命令。

### 方式二：CLI 命令

```bash
# 默认尺寸
wechatwriter image generate "prompt"

# 16:9 比例（推荐用于公众号封面）
wechatwriter image generate -s 4k "prompt"
```

### 方式三：Markdown 语法

在 Markdown 中直接写入：

```markdown
![图片描述](__generate:A cute cat sitting on a windowsill__)
```

## 图片占位符

生成 HTML 时，图片按出现顺序使用占位符：

```html
<!-- IMG:0 -->
<!-- IMG:1 -->
<!-- IMG:2 -->
```

## 图片处理命令

```bash
# 上传本地图片
wechatwriter image upload "/path/to/image.png"

# 下载并上传在线图片
wechatwriter image download "https://example.com/image.jpg"

# AI 生成图片
wechatwriter image generate "prompt"
```

## 图片压缩规则

| 条件 | 处理方式 |
|------|----------|
| 宽度 > 1920px | 等比缩放至 1920px |
| 文件大小 > 2MB | 压缩质量 |
| 格式不支持 | 转换为 JPG |

## 小绿书图片设计指南

小绿书（图片帖）的图片是内容主体，与图文文章的配图有本质区别。

### 推荐尺寸

| 场景 | 尺寸 | 说明 |
|------|------|------|
| 信息流展示 | 1728x2304 | 3:4 竖版，在 feed 流中占据更大面积 |
| 横向风景 | 2560x1440 | 16:9 横版，适合旅行、风景类 |
| 竖向人像/产品 | 1440x2560 | 9:16 竖版，适合产品展示、人物特写 |

### 系列图片叙事

- 第 1 张（封面）：最吸引眼球的画面，决定用户是否点击
- 第 2-4 张（展开）：核心内容，信息密度最高
- 最后 1 张（收尾）：总结或引导互动

### 与图文配图的区别

| | 图文文章配图 | 小绿书图片 |
|--|------------|-----------|
| 角色 | 辅助文字理解 | 图片本身就是内容 |
| 数量 | 按需插入 | 3-5 张最佳 |
| 独立性 | 依赖上下文 | 每张图需独立可理解 |
| 文字 | 可无 | 建议图上叠加关键文字 |
