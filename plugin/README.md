# 案板创作助手 (anbanwriter)

微信公众号 & 小红书 AI 内容创作套件，基于 Claude Code 的 Agent + Skill + MCP 体系，实现从选题到发布的端到端自动化创作。

## 目录

- [安装](#安装)
- [前置配置](#前置配置)
- [快速开始](#快速开始)
- [创作引擎 (Agents)](#创作引擎-agents)
- [技能 (Skills)](#技能-skills)
- [写作风格 (Writers)](#写作风格-writers)
- [排版主题 (Themes)](#排版主题-themes)
- [自动质量检查 (Hooks)](#自动质量检查-hooks)
- [项目结构](#项目结构)

## 安装

```bash
# 在 Claude Code 中安装
/install-plugin anbanai/anbanwriter
```

安装后通过 `/plugin` 确认版本（当前 2.4.0）。

## 前置配置

### 1. MCP 连接（必须）

插件通过 MCP 协议与平台通信，需要配置以下环境变量：

```bash
# 添加到 ~/.zshrc
export ANBANWRITER_API_KEY="你的平台API Key"       # 从平台后台获取
export ANBANWRITER_API_URL="https://你的域名"       # 远程部署需要，本地开发默认 http://localhost:18060
```

API Key 可在 Studio 后台的「设置」页面生成。

### 2. AI 图片生成（配图需要）

图片生成 Key 配置在 `~/.anbanwriter/settings.json` 中：

```json
{
  "image": {
    "provider": "volcengine",
    "key": "你的Key",
    "base_url": "https://ark.cn-beijing.volces.com/api/v3",
    "model": "doubao-seedream-5-0-260128"
  }
}
```

支持的提供商：

| 提供商 | provider 值 | 说明 |
|--------|------------|------|
| OpenAI (DALL-E) | `openai` | 同步生成，dall-e-2/dall-e-3 |
| Google Gemini | `gemini` | 内联图片数据 |
| OpenRouter | `openrouter` | 多模型网关 |
| 火山引擎 Seedream | `volcengine` | 异步轮询 |

> 仅做文字创作不配图可跳过此步。

### 3. 微信公众号（发布需要）

通过自然语言或 `/config` skill 配置 AppID 和 AppSecret：

```
请帮我配置微信公众号，AppID 是 xxx，Secret 是 yyy
```

> 只写文章不发布到微信可跳过此步。

## 快速开始

安装并配置好 MCP Key 后，直接用自然语言即可触发：

```
帮我写一篇关于 AI Agent 的文章                    # 微信公众号图文
小红书种草笔记，主题是降噪耳机                     # 小红书图文
小绿书图片帖，主题是春日穿搭                       # 微信图片帖
帮我生成一组郁金香的鲜花图片                       # 鲜花图片
```

Agent 会自动编排整个流程，无需手动调用各个步骤。

## 创作引擎 (Agents)

端到端自动化引擎，用自然语言触发即可。Agent 会自动编排研究、写作、配图、发布等全部流程。

| Agent | 触发关键词 | 说明 |
|-------|-----------|------|
| **wechatarticle** | "写文章"、"写一篇"、"发文章"、"公众号文章"、"推文" | 微信公众号图文，选题 → 写作 → 配图 → 发布 |
| **wechatxls** | "小绿书"、"图片帖"、"newspic"、"发图片" | 微信图片帖，选题 → 生成配图 → 发布 |
| **rednote** | "小红书"、"红书"、"rednote"、"种草"、"复刻"、"仿写"、"改写笔记"、"爆款改写" | 小红书图文，选题 → 写作 → 封面/内容图 → 发布 |
| **flower** | "花"、"鲜花"、"flower"、"花卉图片" | 鲜花图片生成，调研花卉 → 生成摄影级 prompt → 批量出图 |

### 使用示例

```
# 微信公众号
写一篇关于远程工作的文章，用文化深度的风格

# 小绿书
帮我发一组春日花草的小绿书

# 小红书
小红书爆款改写，这是原文：[粘贴原文]
仿写这篇小红书笔记：https://www.xiaohongshu.com/xxx

# 鲜花
帮我生成一组玫瑰和百合的鲜花图片
```

## 技能 (Skills)

独立技能，可单独调用以精细控制创作流程的某个环节。

### 微信公众号

| Skill | 说明 |
|-------|------|
| `topic-research` | 选题研究，评估互动潜力，生成内容大纲 |
| `content-writing` | AI 风格化写作，去 AI 痕迹，Markdown → 微信 HTML |
| `article-visual-design` | 文章配图管理，AI 生图、压缩、上传 CDN |
| `article-publishing` | 创建和管理微信公众号草稿 |
| `seo-optimization` | 标题、关键词、摘要优化，提升搜索排名 |

### 微信小绿书

| Skill | 说明 |
|-------|------|
| `xls-visual-design` | 生成封面和内容图（3:4 比例） |
| `xls-publishing` | 创建和管理小绿书图片帖草稿（最多 20 张图） |

### 小红书

| Skill | 说明 |
|-------|------|
| `rednote-research` | 选题调研，评估互动潜力 |
| `rednote-writing` | 小红书文案写作，标题优化，爆款改写 |
| `rednote-visual-design` | 封面图和内容图生成（3:4 比例） |

### 其他

| Skill | 说明 |
|-------|------|
| `config` | 配置管理（微信公众号账号、风格、主题等） |
| `writers` | 写作风格管理 |
| `flower-content-design` | 花卉调研和摄影级 prompt 生成 |
| `flower-visual-design` | 花卉系列图片生成，保持视觉一致性 |

### 分步创作示例

如果不想用 Agent 全自动，可以分步控制：

```
# 1. 先做选题研究
帮我研究一下 "AI 编程助手" 这个主题的选题潜力

# 2. 根据选题结果写作
根据上面的选题大纲，写一篇文章

# 3. 给文章配图
帮我给这篇文章生成配图

# 4. 发布到微信
帮我把这篇文章发布为草稿
```

## 写作风格 (Writers)

插件内置 3 种写作风格，文件位于 `writers/` 目录：

| 风格 | english_name | 特点 |
|------|-------------|------|
| Dan Koe | `dan-koe` | 简洁有力，个人品牌/知识分享 |
| 文化深度 | `cultural-depth` | 深度长文，文化视角 |
| 轻松科普 | `casual-science` | 通俗易懂，科学话题 |

### 使用

```
用 dan-koe 风格写一篇关于创业的文章
```

### 自定义风格

在 `writers/` 目录下创建 YAML 文件即可，详见 [writers/README.md](writers/README.md)。

## 排版主题 (Themes)

文章转换为微信 HTML 时的排版主题，文件位于 `themes/` 目录。

内置主题：`default`、`apple`、`autumn-warm`、`spring-fresh`、`ocean-calm`、`bold-red`、`bytedance`、`chinese`、`custom`、`cyber`、`elegant-gold`、`focus-green`、`minimal-blue`、`sports`。

### 使用

```
用 ocean-calm 主题排版这篇文章
```

### 自定义主题

在 `themes/` 目录下创建 YAML 文件，主题系统支持热加载。

## 自动质量检查 (Hooks)

插件内置生命周期钩子，自动在关键节点进行质量验证：

- **Agent 完成时** — 自动检查产出文件完整性，生成交付摘要（文件清单、文章概要、草稿状态）
- **任务完成时** — 验证产出文件存在性、命令执行是否报错、格式是否符合平台规范

无需手动触发，所有创作流程自动享受质量检查。

## 项目结构

```
plugin/
├── .claude-plugin/
│   ├── plugin.json          # 插件清单
│   └── marketplace.json     # 市场信息
├── .mcp.json                # MCP 服务器连接配置
├── agents/                  # 端到端创作引擎
│   ├── wechatarticle.md     # 微信公众号图文
│   ├── wechatxls.md         # 微信小绿书
│   ├── rednote.md           # 小红书图文
│   └── flower.md            # 鲜花图片生成
├── skills/                  # 独立技能
│   ├── topic-research/      # 选题研究
│   ├── content-writing/     # AI 写作
│   ├── article-visual-design/  # 文章配图
│   ├── article-publishing/  # 微信发布
│   ├── seo-optimization/    # SEO 优化
│   ├── xls-visual-design/   # 小绿书配图
│   ├── xls-publishing/      # 小绿书发布
│   ├── rednote-research/    # 小红书选题
│   ├── rednote-writing/     # 小红书写作
│   ├── rednote-visual-design/  # 小红书配图
│   ├── flower-content-design/  # 花卉调研
│   ├── flower-visual-design/   # 花卉出图
│   ├── config/              # 配置管理
│   └── writers/             # 风格管理
├── writers/                 # 写作风格定义 (YAML)
│   ├── dan-koe.yaml
│   ├── cultural-depth.yaml
│   ├── casual-science.yaml
│   └── README.md            # 自定义风格指南
├── themes/                  # 排版主题 (YAML)
├── hooks/
│   └── hooks.json           # 生命周期钩子配置
└── README.md
```
