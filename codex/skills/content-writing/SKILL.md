---
name: content-writing
description: Writes WeChat articles with style guidance, removes AI traces (去AI痕), converts Markdown to WeChat HTML, and checks content compliance (合规检查). Use when writing articles, removing AI traces, converting Markdown to WeChat HTML, or checking content compliance. Also use when user mentions '写文章', '去痕', '去AI味', 'HTML转换', '违禁词检查', '合规检查', 'humanize', or when any step in the article pipeline calls for writing, decontaminating, or converting article content.
---

# 微信公众号内容写作

## MCP 工具

| MCP 工具 | 说明 |
|----------|------|
| `write_article` (channel_id, topic, input_type?, article_type?, length?) | 调用 LLM 按频道写作风格生成文章 |
| `convert_markdown` (channel_id, markdown, theme?) | 调用 LLM 将 Markdown 转为 WeChat HTML |
| `humanize_article` (channel_id, content, intensity?) | 调用 LLM 去除 AI 痕迹 |
| `list_resources` (category) | 获取可用排版模块列表（如可用） |
| `get_resource` (category, name) | 获取特定排版模块的详细语法和示例（如可用） |

---

## 写作流程

### 步骤 1：准备上下文

读取以下文件：
- `$DIR/context-brief.md` — 上下文锚点，每个章节绑定的核心信息和方向
- `$DIR/02-outline.md` — 结构化大纲

### 步骤 2：生成文章初稿

调用 `write_article`：

```
write_article(
  channel_id="$CHANNEL_ID",
  topic="大纲中选定的主题",
  input_type="outline",     // 从大纲扩展
  article_type="essay",     // 可选: essay, commentary, story, tutorial, review
  length="medium"           // 可选: short, medium, long
)
```

**input_type 选择**：

| 值 | 何时使用 | 传入内容 |
|----|----------|----------|
| `idea` | 从零开始 | 一句话观点或想法 |
| `outline` | 有大纲 | 完整大纲内容 |
| `fragment` | 润色现有内容 | 草稿或未完成的文章 |
| `title` | 围绕标题写作 | 文章标题 |

**写作要求**：
- 每个 `##` 章节必须绑定 `context-brief.md` 中至少 1 个上下文锚点
- 每个章节必须包含具体素材（案例、场景、比喻、数据、人物、冲突或操作细节）
- 不要在正文中插入配图占位符（配图由后续步骤专门处理）
- 句子长短交替，避免连续相同句式开头

保存为 `$DIR/03-article.md`。

### 步骤 3：AI 去痕

调用 `humanize_article`：

```
humanize_article(
  channel_id="$CHANNEL_ID",
  content="文章全文",
  intensity="gentle"        // 可选: gentle, medium, aggressive, authentic
)
```

**intensity 选择**：

| 强度 | 适用场景 |
|------|----------|
| `gentle` | 文章已比较自然，只去除明显 AI 痕迹 |
| `medium` | 大多数场景的默认选择 |
| `aggressive` | AI 味很重的文本 |
| `authentic` | 追求极致真人感，使用独立的六维规则重写 |

去痕后检查质量评分（总分 50，35+ 为良好）。如果分数偏低，可提高强度重试。

保存为 `$DIR/04-article-final.md`。

### 步骤 4：违禁词合规检查

对 `$DIR/04-article-final.md` 全文执行违禁词扫描。

词库详见 [prohibited-words.md](references/prohibited-words.md)。

**处理规则**：
- 高风险（政治敏感/色情/暴力/赌博）：删除相关内容
- 中风险（广告法绝对化用语/虚假承诺）：替换为合规近义词
- 低风险：替换或删除
- 禁止变相使用：不得通过谐音字、拼音、特殊符号规避

**报告格式**：
```
违禁词检查报告：
- [词汇] → 已替换为 [合规表述]（位置：第X段）
共处理 N 处违禁词，内容已达到平台合规标准。
```

### 步骤 5：内容质量报告

创建 `$DIR/content-quality-report.md`，检查以下 6 项：

| 检查项 | 通过标准 |
|--------|----------|
| 用户需求覆盖 | 文章回应了用户原始需求 |
| 账号定位一致 | 语气、深度与账号定位匹配 |
| 历史文章差异 | 与已有文章选题不重复，角度有差异 |
| 章节实质内容 | 每个 `##` 章节包含具体素材，不是泛泛而谈 |
| 研究结论引用 | 大纲中的核心观点在正文中得到展开 |
| AI 套话风险 | 无明显 AI 痕迹（参考 [humanizer.md](references/humanizer.md) 的 24 种模式） |

任一检查项不通过时，必须回到步骤 2/3 重写（最多重试 2 次，超过则记录问题继续后续流程）。

---

## 排版模块

通过 `list_resources(category="layouts")` 获取可用的排版模块列表，用 `get_resource(category="layouts", name="<模块名>")` 获取详细语法。（如果 MCP 工具不可用，跳过此步骤，直接写作。）

排版模块使用标准 Markdown 语法，由 `convert_markdown` 工具中的 LLM 自动渲染为微信 HTML。写作时按需选用 2-4 个模块增强视觉丰富度。

**使用原则**：
- 根据内容需要选用，不要为了用而用
- 优先保证内容质量，排版模块是锦上添花

---

## HTML 转换

将定稿 Markdown 转为微信 HTML：

```
convert_markdown(
  channel_id="$CHANNEL_ID",
  markdown="文章全文（含已插入的 CDN 图片链接）",
  theme="default"        // 可选，默认使用频道配置的主题
)
```

**微信 HTML 限制**：
- 所有 CSS 必须内联（style 属性）
- 禁止外部资源（无外部字体、图片、CSS）
- 安全标签：section, p, span, strong, em, h1-h6, ul, ol, li, blockquote, pre, code, table, img, br, hr

保存为 `$DIR/05-article.html`。

---

## 配图说明

配图由 `article-visual-design` skill 在文章定稿后专门设计并插入，写作步骤不需要处理配图。

写作时应确保每个 `##` 章节有足够的实质性内容（具体案例、比喻、场景描述等），为后续配图提供丰富的视觉素材。

---

## 写作风格

内置风格定义文件位于 `writers/` 目录：
- [dan-koe.yaml](../../writers/dan-koe.yaml) — 简洁有力、直击要点、哲理深度
- [cultural-depth.yaml](../../writers/cultural-depth.yaml) — 文化底蕴、文学修辞、深度思考
- [casual-science.yaml](../../writers/casual-science.yaml) — 通俗易懂、生动有趣、科学严谨

写作风格由频道配置决定，调用 `write_article` 时自动应用。

## 深入参考

- AI 去痕详细指南（24 种 AI 痕迹模式 + authentic 六维规则）：[humanizer.md](references/humanizer.md)
- 写作工具完整参数说明：[writing-guide.md](references/writing-guide.md)
- 内容合规详细规则：[content-compliance.md](references/content-compliance.md)
