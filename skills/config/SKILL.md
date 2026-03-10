---
name: config
description: 初始化或查看 anbanwriter 配置。Use when user says "配置"、"config"、"设置"、"账号信息"、"account"。
user-invocable: true
metadata:
  author: Rick
  version: 2.4.0
---

# anbanwriter 配置管理

## 分步初始化配置（Agent 引导流程）

通过 `account init --flag value` 逐步写入配置，支持增量更新（已有字段不会被覆盖）。

### 第一步：微信账号凭证（必填）

向用户获取微信公众号 AppID 和 Secret：

```bash
anbanwriter account init --appid <wx_appid> --secret <wx_secret>
```

### 第二步：公众号基本信息（可选）

```bash
anbanwriter account init --name <公众号名称> --author <作者名称>
```

### 第三步：写作风格

可选值：`dan-koe`（简洁有力）、`cultural-depth`（文化深度）、`casual-science`（轻松科普）

```bash
anbanwriter account init --style dan-koe
```

### 第四步：文章主题

可选值：`default`、`apple`、`autumn-warm`、`spring-fresh`、`ocean-calm`

```bash
anbanwriter account init --theme default
```

### 第五步：AI API 配置（可选）

```bash
anbanwriter account init --ai-key <api_key> --ai-base-url https://api.anthropic.com/v1
```

### 第六步：图片生成服务（可选）

可选值：`gemini`、`openai`、`openrouter`、`volcengine`

```bash
anbanwriter account init --provider gemini
```

> 多个 flags 可合并：`anbanwriter account init --appid wx123 --secret abc --style dan-koe`

---

## 无 flags：创建完整模板文件

```bash
anbanwriter account init
```

创建 `.anbanwriter/settings.json` 模板，手动编辑填入凭证。

## 查看账号信息

```bash
anbanwriter account info
```
