---
name: content-writing
description: Writes articles with style guidance, removes AI traces, converts Markdown to WeChat HTML, and checks content compliance. Use when writing articles, removing AI traces, converting Markdown to WeChat HTML, or checking content compliance.
---

# 微信公众号内容写作知识库

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `write_article` | server 端调用 LLM，按写作风格生成文章 |
| `convert_markdown` | server 端调用 LLM 转换 Markdown 为 WeChat HTML |
| `humanize_article` | server 端调用 LLM 去除 AI 痕迹 |

---

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
6. **风格一致性**：所有章节的配图提示词应保持相似的视觉风格描述

## AI 去痕参考

详见 [humanizer.md](references/humanizer.md)

## 写作指南

详见 [writing-guide.md](references/writing-guide.md)

## 平台内容合规

详见 [content-compliance.md](references/content-compliance.md)

## 违禁词合规检查

词库详见 [prohibited-words.md](references/prohibited-words.md)，涵盖微信平台违禁类和广告法违禁类。

**检查流程**：
1. **创作时**：主动规避词库中的高风险词汇，优先使用合规替代表述
2. **完稿后**：对全文执行违禁词扫描，按优先级处理：
   - 高风险（政治敏感/色情/暴力/赌博）：直接删除相关内容
   - 中风险（广告法绝对化用语/虚假承诺/医疗夸大）：用词库中的合规替代词替换
3. **禁止变相使用**：不得通过谐音字、拼音、特殊符号规避检查

**报告格式**（完成检查后输出）：
```
违禁词检查报告：
- [词汇] → 已替换为 [合规表述]（位置：第X段）
- [词汇] → 已删除相关句子（位置：第X段）
共处理 N 处违禁词，内容已达到平台合规标准。
```

## 微信 HTML 规范

- 所有 CSS 必须内联（style 属性）
- 禁止外部资源
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

## 相关工具

- 风格写作：调用 `write_article` MCP 工具
- Markdown 转微信 HTML：调用 `convert_markdown` MCP 工具
- AI 去痕：调用 `humanize_article` MCP 工具
