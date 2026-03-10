# Anban Writer Plugin

Professional WeChat content creation toolkit for Claude Code with AI-powered writing, visual design, and publishing optimization.

## Installation

```bash
# Add marketplace
/plugin marketplace add royalrick/anbanwriter

# Install plugin
/plugin install anbanwriter@anbanwriter
```

## Features

- **Convert** — Markdown → WeChat HTML with inline CSS and theme support
- **Write** — Style-based AI writing assistance (dan-koe, cultural-depth, casual-science)
- **Humanize** — Remove AI writing traces from articles
- **Score** — Evaluate article viral potential (5-dimension quality scoring)
- **Outline** — Generate structured article outlines
- **Draft** — Manage WeChat draft box (图文文章 & 小绿书)
- **Image** — AI image generation (OpenAI DALL-E, Google Gemini, OpenRouter, Volcengine Seedream)

## Configuration

Run `anbanwriter account init` for guided setup, or create `.anbanwriter/settings.json` directly:

```json
{
  "wechat": {
    "appid": "your_appid",
    "secret": "your_secret"
  },
  "article": {
    "image": { "key": "your_api_key", "provider": "gemini" }
  },
  "post": {
    "image": { "key": "your_api_key", "provider": "gemini" }
  }
}
```

Supported image providers: `openai`, `gemini`, `openrouter`, `volcengine`

## Skills

| Skill | Description |
|-------|-------------|
| `/anbanwriter:config` | Initialize or view account configuration |
| `/anbanwriter:content-writing` | Writing style, humanization, HTML conversion |
| `/anbanwriter:visual-design` | Image generation and theme management |
| `/anbanwriter:topic-research` | Topic scoring and outline generation |
| `/anbanwriter:seo-optimization` | Title, keyword, and excerpt optimization |
| `/anbanwriter:article-publishing` | 图文文章 draft publishing |
| `/anbanwriter:post-publishing` | 小绿书 image post publishing |

## Agents

- **wechatarticle** — Full article creation pipeline: 选题研究 → 写作 → AI去痕 → SEO优化 → 封面配图 → HTML转换 → 草稿发布
- **wechatpost** — Image post creation pipeline: 选题研究 → 图片设计 → 草稿发布

## CLI Commands

| Command | Description |
|---------|-------------|
| `account init` | Initialize config with guided setup |
| `account info` | View current account information |
| `convert <file>` | Convert Markdown to WeChat HTML |
| `write` | Style-based AI writing assistance |
| `humanize <file>` | Remove AI writing traces |
| `score <file>` | Score article viral potential |
| `outline` | Generate structured article outline |
| `draft article <json>` | Create 图文文章 draft |
| `draft post` | Create 小绿书 image post (max 20 images) |
| `image generate <prompt>` | Generate AI images |
| `image upload <file>` | Upload image to WeChat CDN |
| `doctor` | Diagnose config and connection issues |

## License

MIT
