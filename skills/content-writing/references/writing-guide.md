# 写作功能指南 (Writing Guide)

> **风格写作 (`write`)** 命令让你只需提供一个想法，AI 就能自动生成符合特定创作者风格的文章。

## 概述

- **零基础友好**：只需一个观点或想法，AI 自动扩展成完整文章
- **创作者风格**：内置 Dan Koe 等风格，支持自定义
- **封面自动生成**：根据文章内容自动匹配封面提示词
- **AI 模式**：返回结构化提示词，由 Claude 等大模型生成内容

## 命令格式

### 基本命令

```bash
# 交互式写作（最简单）
wechatwriter write

# 查看所有可用风格
wechatwriter write --list

# 指定风格写作
wechatwriter write --style dan-koe

# 指定标题写作（heredoc 输入）
wechatwriter write --style dan-koe --title "文章标题" <<EOF
你的内容
EOF

# 只生成封面提示词
wechatwriter write --style dan-koe --cover-only

# 同时生成文章和封面
wechatwriter write --style dan-koe --cover
```

### 输入类型

| 类型 | 参数 | 说明 | 示例 |
|------|------|------|------|
| **观点** | `--input-type idea` | 一个观点或想法 | "我觉得自律是个伪命题" |
| **片段** | `--input-type fragment` | 内容片段，需要润色扩展 | 现有的草稿或未完成的文章 |
| **大纲** | `--input-type outline` | 文章大纲，需要填充内容 | 有结构，需要填充内容 |
| **标题** | `--input-type title` | 仅标题，围绕标题写作 | "自律是个谎言" |

### 其他参数

| 参数 | 说明 |
|------|------|
| `--style` | 写作风格（默认: dan-koe） |
| `--length` | 文章长度：short/medium/long |
| `--title` | 文章标题 |
| `-o, --output` | 输出文件路径 |
| `--cover` | 同时生成封面提示词 |
| `--cover-only` | 仅生成封面提示词 |
| `--list` | 列出所有可用风格 |
| `--detail` | 显示详细风格信息 |

## AI 模式说明

`write` 命令默认使用 **AI 模式**：

1. 命令返回结构化的提示词（JSON 格式）
2. 由 Claude 等大模型处理提示词
3. 生成最终文章内容

**在 Claude Code 中使用时，这个流程是自动的。**

### AI 模式输出

```json
{
  "success": true,
  "mode": "ai",
  "action": "ai_write_request",
  "style": "Dan Koe",
  "prompt": "结构化的写作提示词..."
}
```

### 带封面的输出

```json
{
  "success": true,
  "prompt": "文章提示词...",
  "cover_prompt": "封面提示词...",
  "cover_explanation": "封面设计思路..."
}
```

## 内置风格

### Dan Koe 风格

**特点**：深刻但不晦涩、犀利但不刻薄、有哲学深度但接地气

**适合内容**：个人成长、观点评述、人生感悟、方法论分享

## 自定义风格

在 `writers/` 目录下创建 YAML 文件即可添加自定义风格：

```yaml
name: "我的风格"
english_name: "my-style"
description: "简洁有力"

writing_prompt: |
  你是一位简洁有力的写作者。
  用最少的字表达最清晰的观点。

cover_prompt: |
  为文章生成一个简洁有力的封面提示词。

cover_style: "minimalist"
cover_mood: "professional"
cover_color_scheme: ["#000000", "#FFFFFF", "#FF0000"]
```

详细格式参考 `writers/dan-koe.yaml`。

## 封面尺寸建议

| 用途 | 推荐尺寸 | 说明 |
|------|----------|------|
| **文章封面** | 2560x1440 | 16:9 横版，在微信 feed 流和文章列表显示效果更好 |
| **默认生成** | 2048x2048 (1:1) | 方形图片，在预览时会被裁剪 |

生成封面图：

```bash
# 生成 16:9 封面图（推荐）
wechatwriter image generate "封面提示词"
```
