---
name: xls
description: 微信公众号小绿书（图片帖）全自动创作。用户提到"小绿书"、"图片帖"、"newspic"、"发图片"时使用此 skill。
---

# /xls 微信公众号小绿书（图片帖）创作命令

## 强制执行声明

**你正在执行微信公众号小绿书（图片帖）创作任务。你必须使用工具（MCP 工具、Write、Bash、TaskCreate 等）完成完整的创作流水线。**

**禁止直接用文字回答用户的主题问题。** 你不是在回答问题，你是在创作一组图片帖子。如果你直接输出文字回答而没有使用任何工具，说明你理解错了任务。

用户输入 `/xls` 后面的内容是创作主题，不是让你回答的问题。

---

## 必须执行的步骤

按顺序执行以下步骤。每一步都必须调用对应的工具，不能跳过。

### 步骤 1：获取账号信息

调用 MCP 工具：
- `list_channels()` → 找到 `platform` 为 `xls` 的 channel，记为 `$CHANNEL_ID`
- `get_account_info(channel_id="$CHANNEL_ID", scope="xls")` → 获取账号信息和图片数量配置（默认 4 张）
- `list_drafts(channel_id="$CHANNEL_ID")` 和 `list_published(channel_id="$CHANNEL_ID")` → 查看已有帖子标题，后续选题避开
- `list_topics(channel_id="$CHANNEL_ID")` → 查看系统内已有选题，后续选题避开

### 步骤 2：创建工作目录

调用 MCP 工具：
- `prepare_workspace(content_type="xls", task_id=TASK_ID)` → 获取工作目录路径，记为 `$DIR`

### 步骤 3：选题研究

使用 `topic-research` skill：
- 结合账号关键词和用户需求搜索热门话题
- 规划三个独立元素：
  - **帖子标题**：关键词 + 好奇缺口 + 数字钩子（≤32 字符）
  - **封面钩子**：视觉钩子优先，目标是让人想点进来
  - **内容页规划**：每页一个信息点，3 秒能懂

### 步骤 4：确定视觉风格

使用 `xls-visual-design` skill：
- 有参考图 → 用 `--ref`；无参考图 → 动态设计 `$STYLE`
- 确保封面与所有内容图视觉一致

### 步骤 5：生成图片

使用 `xls-visual-design` skill：
- 以封面确立基准风格，后续图片以封面为参考批量生成
- 输出模式 `--mode xls`
- 保存到 `$DIR/`：`cover.png`、`image_01.png` ... `image_0{N-2}.png`、`tail.png`

### 步骤 6：合规检查

使用 `content-writing` skill：
- 对标题和描述文案执行违禁词合规检查

### 步骤 7：上传图片

逐一上传图片到微信素材库：
- `image upload $DIR/cover.png` → 获取 media_id
- 同样上传所有内容图和尾部图
- 记录每张图的 media_id

### 步骤 8：发布草稿

使用 `xls-publishing` skill：
- 用素材 ID 发布到微信公众号草稿箱

---

## 三段式思维框架

- **帖子标题**：服务算法推荐和搜索发现
- **封面图**：服务 CTR（点击率），视觉钩子优先，可完全无文字
- **内容图**：服务完读率，每页一个信息点
- **尾部图**：服务转化，记忆点提炼 + 提问引导

---

## 质量标准

- 图片数量以 `get_account_info` 返回的配置为准（默认 4 张）
- 所有图片视觉风格一致
- 标题 ≤ 32 字符，包含关键词或钩子
- 描述文案为纯文本，无违禁词
- 发布前通过 `--dry-run` 验证
- 草稿创建成功

---

## 任务追踪要求

流程启动时用 `TaskCreate` 创建任务列表，每个步骤对应一个任务。开始前 `TaskUpdate status → in_progress`，完成后 `TaskUpdate status → completed`。报告进度示例：`[4/8] 图片生成完成 → $DIR/ (4张图片)`
