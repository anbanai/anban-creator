---
name: agent-reach
description: Use when Seednote or Xiaohongshu research needs real external note/search data through Agent-Reach, including CLI availability checks, active backend selection, note detail retrieval, xsec_token handling, or unavailable-backend reporting without fabricated data.
---

# Agent-Reach

Use Agent-Reach as the only boundary for external Xiaohongshu data. This skill does not replace Anban MCP tools; use it only for real outside data collection that Seednote research needs.

## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。

## Required Flow

1. Run `agent-reach doctor --json` before any external research.
2. Read both `xiaohongshu.status` and `xiaohongshu.active_backend` from the doctor output.
3. Collect external data only when `xiaohongshu.status == "ok"` and `active_backend` is non-empty. Agent-Reach may expose a repairable backend while status is `warn`; `active_backend` alone never proves usability.
4. Treat the exact backend labels as `OpenCLI`, `xiaohongshu-mcp`, and `xhs-cli (xiaohongshu-cli)`. Unknown labels are unsupported for external collection and must use the unavailable-backend path.
5. Use only the command family exposed by the active Agent-Reach backend. Do not call OpenCLI, xiaohongshu-mcp, xhs-cli, browser scrapers, or custom HTTP clients directly unless Agent-Reach selected that backend.
6. Write provenance into research artifacts: `data_source`, `channel_status`, `active_backend`, `backend_command_family`, `token_source`, `missing_fields`, and `fallback_reason`.

## Source Notes

- Extract `feed_id` and `xsec_token` only from real URLs or backend results returned by Agent-Reach.
- Never construct `xsec_token` by guesswork or template.
- If a source note cannot be resolved after the Agent-Reach backend's retry path, stop and explain the recoverable step.

## Unavailable Backend

Agent-Reach is an optional enhancement for original Seednote research. When the CLI is missing or Xiaohongshu has no healthy backend, report the exact availability state to the parent workflow and do not run any backend command. An original task must continue from its explicit topic, topic pool, project profile, and existing-title deduplication; it must not create `failure-state.json` merely because external research is unavailable.

When the CLI exists but `xiaohongshu.status` is not `ok`, report the exact `status`, `active_backend`, and `message` from doctor. Do not describe local/project-context fallback as external trending data. When replicate mode depends only on an external note ID or URL and no source content can be resolved, report that recoverable source-input failure to the parent workflow.

Do not run `pip`, `pipx`, `npm`, `agent-reach install`, or mutate a managed task runtime. Only show the installation hint when the user explicitly asks to enable external research.

For an unmanaged local environment where the CLI itself is missing, use the official install flow:

```text
帮我安装 Agent Reach：https://raw.githubusercontent.com/Panniantong/agent-reach/main/docs/install.md
```
