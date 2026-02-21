---
name: content-writing
description: 微信公众号写作知识库。当用户需要写文章、去除AI痕迹、或转换格式时自动加载。提供风格参考、写作规范和质量标准。Do NOT use for SEO optimization (use seo-optimization) or topic scoring (use topic-research).
user-invocable: false
metadata:
  author: rick
  version: 2.3.0
---

# 微信公众号内容写作知识库

## 写作风格

内置风格定义文件位于 `writers/` 目录：

- [dan-koe.yaml](../../writers/dan-koe.yaml) - 简洁有力、直击要点、哲理深度
- [cultural-depth.yaml](../../writers/cultural-depth.yaml) - 文化底蕴、文学修辞、深度思考
- [casual-science.yaml](../../writers/casual-science.yaml) - 通俗易懂、生动有趣、科学严谨

## 质量标准

- **直接性**: 快速切入主题，避免冗长铺垫
- **节奏感**: 句子长短交替，避免单调
- **信任感**: 尊重读者智识，不过度解释
- **真实性**: 听起来像人写的，不像机器生成
- **精准性**: 无冗余内容，每句话都有价值

## AI 去痕参考

详见 [humanizer.md](references/humanizer.md)

## 写作指南

详见 [writing-guide.md](references/writing-guide.md)

## 微信 HTML 规范

- 所有 CSS 必须内联（style 属性）
- 禁止外部资源
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

## 命令帮助

```bash
# 风格写作
wechatwriter write --help

# Markdown 转微信 HTML
wechatwriter convert --help

# AI 去痕
wechatwriter humanize --help
```
