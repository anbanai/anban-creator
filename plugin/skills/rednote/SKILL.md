---
name: rednote
description: 小红书图文全自动创作。用户提到"小红书"、"红书"、"rednote"、"种草"、"复刻"、"仿写"、"改写笔记"、"爆款改写"、"克隆"、"clone"时使用此 skill。
---

# /rednote 小红书内容创作命令

## 强制执行声明

**你正在执行小红书内容创作任务。你必须使用工具（MCP 工具、Write、Bash、TaskCreate 等）完成完整的创作流水线。**

**禁止直接用文字回答用户的主题问题。** 你不是在回答问题，你是在创作一篇小红书笔记。如果你直接输出文字回答而没有使用任何工具，说明你理解错了任务。

用户输入 `/rednote` 后面的内容是创作主题，不是让你回答的问题。

---

## 必须执行的步骤

按顺序执行以下步骤。每一步都必须调用对应的工具，不能跳过。

### 步骤 1：获取账号信息

调用 MCP 工具：
- `list_channels()` → 找到 `platform` 为 `rednote` 的 channel，记为 `$CHANNEL_ID`
- `get_account_info(channel_id="$CHANNEL_ID", scope="rednote")` → 获取账号定位、关键词等信息
- `list_topics(channel_id="$CHANNEL_ID")` → 查看系统内已有选题，后续选题避开

### 步骤 2：创建工作目录

调用 MCP 工具：
- `prepare_workspace(content_type="rednote", task_id=TASK_ID)` → 获取工作目录路径，记为 `$DIR`

### 步骤 3：研究选题

使用 `rednote-research` skill：
- 采集热门笔记数据
- 自动选 Top 1 选题
- 评分结果写入 `$DIR/topic-analysis.md`

### 步骤 4：创作内容

使用 `rednote-writing` skill：
- 生成标题（≤20 字）、正文、话题标签
- 内容保存到 `$DIR/content.md`

### 步骤 5：生成图片

使用 `rednote-visual-design` skill：
- 传入 `$DIR/content.md`
- 生成封面 `$DIR/cover.png`、内容图 `$DIR/image_01.png` ... `$DIR/image_0{N-2}.png`、尾图 `$DIR/tail.png`
- 图片规划写入 `$DIR/image-plan.md`

### 步骤 6：合规检查（复刻模式）

如果是复刻模式（用户提供了笔记 ID 或链接）：
- 使用 `rednote-writing` skill 扫描标题与正文
- 生成 `$DIR/compliance-report.md`

### 步骤 7：归档

- 从 `$DIR/content.md` 提取最终标题
- 调用 `archive_workspace(content_type="rednote", name="{标题}")` 归档
- 报告成果目录路径

---

## 模式判断

- 用户提供笔记 ID 或链接 → **复刻模式**（步骤 3 改为获取源笔记并分析）
- 其他情况 → **原创模式**（按上述步骤执行）

---

## 质量标准

- 图片总数 ≥ 3 张（封面 + 至少 2 张内容图 + 尾图）
- 所有图片视觉风格一致
- `content.md` 包含标题、正文、话题标签三部分
- 标题 ≤ 20 字，关键词前置

---

## 任务追踪要求

流程启动时用 `TaskCreate` 创建任务列表，每个步骤对应一个任务。开始前 `TaskUpdate status → in_progress`，完成后 `TaskUpdate status → completed`。
