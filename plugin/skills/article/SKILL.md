---
name: article
description: 微信公众号图文文章全自动创作。用户提到"写文章"、"写一篇"、"发文章"、"公众号文章"、"推文"时使用此 skill。
---

# /article 微信公众号文章创作命令

## 强制执行声明

**你正在执行微信公众号文章创作任务。你必须使用工具（MCP 工具、Write、Bash、TaskCreate 等）完成完整的创作流水线。**

**禁止直接用文字回答用户的主题问题。** 你不是在回答问题，你是在创作一篇微信公众号文章。如果你直接输出文字回答而没有使用任何工具，说明你理解错了任务。

用户输入 `/article` 后面的内容是创作主题，不是让你回答的问题。

---

## 必须执行的步骤

按顺序执行以下步骤。每一步都必须调用对应的工具，不能跳过。

### 步骤 1：获取账号信息

调用 MCP 工具：
- `list_channels()` → 找到 `platform` 为 `article` 的 channel，记为 `$CHANNEL_ID`
- `get_account_info(channel_id="$CHANNEL_ID", scope="article")` → 获取账号定位、受众、写作风格
- `list_drafts(channel_id="$CHANNEL_ID")` 和 `list_published(channel_id="$CHANNEL_ID")` → 查看已有文章标题，后续选题避开
- `list_topics(channel_id="$CHANNEL_ID")` → 查看系统内已有选题，后续选题避开

### 步骤 2：创建工作目录

调用 MCP 工具：
- `prepare_workspace(content_type="articles", task_id=TASK_ID)` → 获取工作目录路径，记为 `$DIR`

### 步骤 3：选题研究

使用 `topic-research` skill：
- 结合账号关键词和用户需求搜索热门话题
- 生成文章大纲
- 保存为 `$DIR/01-research.md` 和 `$DIR/02-outline.md`

### 步骤 4：撰写文章

使用 `content-writing` skill：
- 基于大纲输出 Markdown 格式文章
- 每个章节至少一个配图占位符，提示词与章节内容强相关
- 保存为 `$DIR/03-article.md`

### 步骤 5：AI 去痕与合规

使用 `content-writing` skill：
- 检测并移除 AI 痕迹（`gentle` 模式）
- 执行违禁词合规检查
- 保存为 `$DIR/04-article-final.md`

### 步骤 6：SEO 优化

使用 `seo-optimization` skill：
- 优化标题、关键词、摘要

### 步骤 7：生成封面图

使用 `article-visual-design` skill：
- 生成文章封面，保存到 `$DIR/cover.png`
- 上传到微信素材库：`image upload $DIR/cover.png` → 获取 media_id

### 步骤 8：生成章节配图

使用 `article-visual-design` skill：
- 验证章节配图覆盖率，补充缺失的配图占位符
- 批量生成配图，输出到 `$DIR/images.json`

### 步骤 9：HTML 转换

使用 `content-writing` skill：
- 转换为 WeChat 兼容 HTML
- 图片链接自动替换为 CDN 链接
- 保存为 `$DIR/05-article.html`

### 步骤 10：发布草稿

使用 `article-publishing` skill：
- 创建 `draft.json`
- 发布到草稿箱：`draft article $DIR/draft.json`

---

## 质量标准

- 文章至少 3 个二级标题，结构清晰
- 封面图必须成功生成并上传
- **图文并茂**：每个 `##` 章节至少一张配图
- 同一篇文章内所有配图风格一致
- 无明显 AI 痕迹，无违禁词
- 草稿创建成功

---

## 任务追踪要求

流程启动时用 `TaskCreate` 创建任务列表，每个步骤对应一个任务。开始前 `TaskUpdate status → in_progress`，完成后 `TaskUpdate status → completed`。报告进度示例：`[5/10] AI去痕完成 → $DIR/04-article-final.md`
