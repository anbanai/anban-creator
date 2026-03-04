---
name: content-writing
description: 微信公众号写作知识库。当用户需要写文章、去除AI痕迹、或转换格式时自动加载。提供风格参考、写作规范和质量标准。Do NOT use for SEO optimization (use seo-optimization) or topic scoring (use topic-research).
user-invocable: false
metadata:
  author: Rick
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

## 章节配图要求

写作时必须确保**图文并茂**，遵循以下规则：

1. **每个 ## 章节至少插入一个图片占位符**，使用语法：`![描述](__generate:提示词__)`
2. **提示词必须与章节内容强相关**：从该章节讨论的具体主题、场景、概念中提炼，禁止使用"美丽风景"等通用描述
3. **图片位置**：放在关键段落之后，不要紧接 ## 标题，不要放在章节末尾
4. **提示词差异化**：不同章节的配图提示词必须有明显区别，反映各章节的不同主题
5. **提示词格式**：`[章节核心主题] + [具体场景/物体] + [视觉风格] + [构图指导]`，长度 30-80 字
6. **风格一致性**：所有章节的配图提示词应保持相似的视觉风格描述（后续通过 `--style` 参数统一执行）

## AI 去痕参考

详见 [humanizer.md](references/humanizer.md)

## 写作指南

详见 [writing-guide.md](references/writing-guide.md)

## 平台内容合规

详见 [content-compliance.md](references/content-compliance.md)

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
