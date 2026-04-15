---
name: config
description: Initializes, views, and modifies anbanwriter configuration settings. Use when user wants to initialize, view, or modify anbanwriter configuration.
---

# anbanwriter 配置管理

## 分步初始化配置（Agent 引导流程）

通过 MCP 工具逐步写入配置，支持增量更新（已有字段不会被覆盖）。

### 第一步：微信账号凭证（必填）

向用户获取微信公众号 AppID 和 Secret，通过 MCP 工具设置。

### 第二步：公众号基本信息（可选）

通过 MCP 工具设置公众号名称和作者。

### 第三步：写作风格

可选值：`dan-koe`（简洁有力）、`cultural-depth`（文化深度）、`casual-science`（轻松科普）

### 第四步：文章主题

可选值：`default`、`apple`、`autumn-warm`、`spring-fresh`、`ocean-calm`

### 第五步：AI API 配置（可选）

通过 MCP 工具设置 AI API Key 和 Base URL。

### 第六步：图片生成服务（可选）

可选值：`gemini`、`openai`、`openrouter`、`volcengine`

## 查看账号信息

调用 `get_account_info` MCP 工具。
