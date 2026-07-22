---
name: seednote-research
description: 'Use when analyzing Seednote topics, scoring engagement, researching trending seednote content, or fetching source note details for replicate mode. Also use when user mentions ''种草笔记选题'', ''热门笔记'', ''竞品分析'', ''笔记分析'', or when the seednote pipeline calls for topic discovery or source note fetching. Analyzes Seednote (种草笔记) topics, trending notes (热门笔记), and scores engagement potential (互动率评分).'
---

# 种草笔记选题研究

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 外部数据入口

Agent-Reach 是小红书真实外部数据的首选且唯一入口，但只是原创模式的可选增强能力。需要外部数据时先 using the `agent-reach` skill，并执行：

```bash
agent-reach doctor --json
```

同时读取 `xiaohongshu.status` 和 `xiaohongshu.active_backend`。只有 `status == "ok"` 且 `active_backend` 非空时，才按 Agent-Reach 选出的 backend 执行对应官方命令组；`warn` 状态即使带有 `active_backend` 也只是可修复候选，不可执行。不要在 Anban 内自行判断桌面、服务器、无头环境，也不要自行给 OpenCLI、xiaohongshu-mcp 或 xhs-cli 排优先级。

Agent-Reach 不可用、未安装、未登录或 doctor 没有健康 backend 时，原创模式不得失败、不得写 `failure-state.json`：改用用户明确主题、选题池、账号画像和已有标题完成保守选题，并如实记录外部数据不可用。不得把本地判断描述成热门数据、互动率证据或 Agent-Reach 结果。复刻模式若只提供外部笔记 ID/链接且无法取得任何源内容，才属于无法满足核心输入的可恢复失败。

本 skill 只读：允许搜索、笔记详情、评论、feed、用户公开数据；禁止发布、删除、关注、取关、点赞、收藏、评论写入等写操作。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。

## Anban MCP 工具

Anban MCP 只用于 Anban 产品能力，不用于小红书外部数据主路径。

| MCP 工具 | 说明 |
|----------|------|
| `claim_topic` (project_id, task_id?) | 从项目选题池认领下一个未用选题（原创模式选题**首选来源**，池非空必用） |
| `list_project_titles` (project_id) | 查看系统内已有标题（定标题前必调） |

兼容工具 `search_seednote_feeds`、`get_seednote_feed_detail`、`check_seednote_login_status` 只作为 legacy/server/internal fallback，不进入新 seednote 研究主路径。

## Agent-Reach backend 命令族

下列命令只能在 `agent-reach doctor --json` 返回 `status == "ok"` 和对应 `active_backend` 后使用。实际可用性、安装、登录和 fallback 顺序由 Agent-Reach 决定。

```bash
# active_backend: OpenCLI
opencli xiaohongshu search "query" -f json
opencli xiaohongshu note "NOTE_URL_FROM_SEARCH_OR_FEED" -f json
opencli xiaohongshu comments "NOTE_URL_FROM_SEARCH_OR_FEED" --with-replies --limit 20 -f json
opencli xiaohongshu feed -f json
opencli xiaohongshu user "USER_ID_OR_PROFILE_URL" -f json

# active_backend: xiaohongshu-mcp
mcporter call 'xiaohongshu.check_login_status()' --timeout 120000
mcporter call 'xiaohongshu.get_login_qrcode()' --timeout 120000
mcporter call 'xiaohongshu.search_feeds(keyword: "query")' --timeout 120000
mcporter call 'xiaohongshu.get_feed_detail(feed_id: "...", xsec_token: "...")' --timeout 120000

# active_backend: xhs-cli (xiaohongshu-cli)
xhs search "query"
xhs read "NOTE_URL_FROM_SEARCH_OR_FEED"
xhs comments "NOTE_URL_FROM_SEARCH_OR_FEED"
xhs feed
```

## xsec_token 工作流

`feed_id` 和 `xsec_token` 只能从 Agent-Reach backend 的 search/feed/note 返回结果或完整签名 URL 中提取，不能凭空构造。裸 note_id 不可靠；复刻模式必须保留 token 来源。

产物中必须记录：

```text
data_source=<agent-reach|task_topic|topic_pool|project_context>
channel_status=<ok|warn|off|error|missing>
active_backend=<Agent-Reach backend；不可用时写 none>
backend_command_family=<OpenCLI|xiaohongshu-mcp|xhs-cli (xiaohongshu-cli)|none>
token_source=<search|feed|signed_url|missing>
missing_fields=<缺失字段列表>
fallback_reason=<无降级则写 none>
```

## 完整研究流程

### 步骤 0：确定选题来源（优先选题池，仅原创模式）

> 复刻模式（用户提供笔记 ID/链接）不做选题，直接看下方「复刻模式源笔记获取」专节。

原创模式下，先确定本次笔记选题，**不要凭空搜**。

**如何判断任务是否已指定主题**：检查本任务的 user prompt。
- 含 `create content about: <X>` → `<X>` 就是任务指定主题。
- 是 `research and create content ... choose the optimal theme` 这类让你自己选题的措辞 → 任务未指定主题。
- 项目 profile 的 keywords 不是主题。

1. 任务已指定主题：直接采用 `<X>`，禁止调用 `claim_topic`，把它作为 Agent-Reach 搜索关键词；评分仅作参考。
2. 任务未指定主题：先调用 `claim_topic(project_id="$PROJECT_ID", task_id="$TASK_ID")`。返回非空 `topic` 则采用；返回 `null` 且 Agent-Reach 健康时继续外部搜索 + 评分，Agent-Reach 不可用时基于账号画像与已有标题选择一个具体、可去重的保守主题。

### 步骤 1：查重

调用 `list_project_titles(project_id="$PROJECT_ID")` 查看已有标题，后续标题避开。

### 步骤 2：Agent-Reach 采集热门笔记

根据账号定位和用户需求确定 2-3 个搜索关键词。执行 `agent-reach doctor --json`；确认 `xiaohongshu.status == "ok"` 后再按 `active_backend` 调用对应命令族，采集搜索结果、feed 结果和 Top 3-5 条笔记详情/评论。

若命令不存在或 backend 不健康，跳过全部外部命令并继续原创流程。`topic-analysis.md` 必须记录 doctor 原始状态（命令不存在时写 `channel_status=missing`）、`active_backend=none`、`backend_command_family=none`、`missing_fields=external_hot_data` 和具体 `fallback_reason`。

### 步骤 3：分析热门笔记

只有取得真实外部数据时才提取：

- 标题模板：句式、情绪词、字数分布
- 封面模板：信息层级、文字密度、配色规律
- 正文模板：开场钩子、段落结构、结尾 CTA 形式
- 评论信号：高频关键词、用户痛点、争议点
- 标签组合：核心话题 + 垂直话题 + 长尾话题

### 步骤 4：评分与选题

只有取得真实互动字段时才使用 2026 小红书 CES 互动评分模型：

```text
topic_score = engagement_rate × recency_weight × novelty_bonus
engagement_rate = (like_count×1 + collect_count×1 + comment_count×4 + share_count×4) / max(total, 1)
recency_weight: 24h→1.0, 7d→0.8, 30d→0.5, 更早→0.3
novelty_bonus: 同角度笔记<3 → 1.2, 否则 → 1.0
```

字段缺失时按 0 计入并在 `missing_fields` 中记录；不要补造数据。无外部数据时不得套用 CES 或伪造互动率，改按“用户主题匹配度、账号定位匹配度、与已有标题差异度、内容具体性”记录定性依据。评分明细或降级依据、最终选题理由和数据来源字段写入 `$DIR/topic-analysis.md`。

## 复刻模式源笔记获取

当用户提供笔记 ID 或链接时，本 skill 只负责获取源笔记详情，不做爆款模板分析：

1. 通过 Agent-Reach backend 的 search/feed/signed URL 获取真实 `feed_id` 与 `xsec_token`。
2. 按 `active_backend` 获取笔记详情、互动数据和评论数据。
3. 将原始详情、`data_source=agent-reach`、`active_backend`、`backend_command_family`、`token_source`、互动数据、评论摘要、`missing_fields` 和 `fallback_reason` 写入 `$DIR/source-note.md`。
4. 后续由 `seednote-viral-analysis` skill 读取 `$DIR/source-note.md`，生成 `$DIR/source-analysis.md`、`$DIR/viral-template.json`、`$DIR/template-meta.json`。

**边界**：不要在本 skill 中提取爆款模板，不要调用 `save_template`，不要生成改写正文。仅有外部 ID/链接且无法取得源内容时，写结构化 `failure-state.json` 并从 `research` 恢复；这条失败规则不适用于原创模式。

## 产出要求

| 模式 | 产出文件 |
|------|----------|
| 原创模式 | `$DIR/topic-analysis.md`（候选话题、外部评分或降级依据、最终选题理由、数据来源字段） |
| 复刻模式 | `$DIR/source-note.md`（源笔记原始详情、互动数据、评论摘要、Agent-Reach 数据来源字段、数据缺失项） |
