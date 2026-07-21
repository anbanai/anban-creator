---
name: montage
description: Use when handling Anban Montage tasks that convert montage-input.json into an Montage adapter manifest, run the upstream Montage pipeline, and register normalized Anban deliverables.
---

# Montage Skill

Use this skill only for Anban `montage` tasks.

遇到素材映射、pipeline 选择或交付格式不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。

## Inputs

- `$TASK_ID`
- `$PROJECT_ID`
- `$ANBAN_MONTAGE_SUBMODULE_PATH` (preferred when set; production images set it to `/app/third_party/OpenMontage`)
- `montage-input.json`
- `montage-tool-policy.json`
- `montage-pipeline-defaults.json`
- project profile from Anban MCP
- configured Montage submodule path
- OpenMontage provider secrets from environment variables only

## Required Files

- `montage-project.json`: adapter manifest sent to Montage
- `montage-tool-policy.json`: non-secret capability preferences configured by the server
- `montage-pipeline-defaults.json`: non-secret pipeline defaults configured by the server
- `delivery-manifest.json`: normalized Anban delivery manifest
- `final.mp4`: final video when the pipeline succeeds
- `failure-diagnosis.md`: required when the pipeline cannot complete

## Rules

- Run all creative pipeline work through the bundled OpenMontage adapter and its provider registry.
- Do not expose raw Montage pipeline internals as Anban stable schema.
- Do not modify files under `third_party/OpenMontage`.
- Resolve the upstream runtime from `$ANBAN_MONTAGE_SUBMODULE_PATH` first, then fall back to the configured/default `third_party/OpenMontage` path.
- Secrets only arrive through environment variables. Never write provider keys to `montage-project.json`, task files, logs, MCP feedback, or failure diagnosis.
- Before production, run the OpenMontage registry capability check (`provider_menu_summary()` or the equivalent registry command) and compare the selected pipeline's required/optional tools with the configured provider envelope.
- Let OpenMontage selectors/registry choose concrete providers from the configured policy and real availability; do not hardcode Anban-side provider routing.
- Use Anban MCP tools for project profile, workspace preparation, progress, uploads, task files, and feedback.
- 托管任务自动批准常规 creative gate，但不得跳过 checkpoint；每个 checkpoint 仍执行，并在 Montage decision log 中记录 `anban_managed_task` 预授权来源、自动选择和结果。
- Authentication, required capability, hard budget, safety, source corruption, or impossible-delivery blockers must write `failure-diagnosis.md` and terminate. Full-run preauthorization never overrides these blockers.
- Do not expose the Backlot page directly. Retain only stable delivery files and structured checkpoint, timeline, and run-log artifacts required by Anban.

## Adapter Manifest

Write `montage-project.json` with:

```json
{
  "task_id": "$TASK_ID",
  "project_id": "$PROJECT_ID",
  "brief": "",
  "pipeline_key": "default",
  "assets": [],
  "preferences": {},
  "limits": {},
  "tool_policy": {},
  "pipeline_defaults": {},
  "env_keys": {},
  "approval_policy": {
    "mode": "auto",
    "source": "anban_managed_task",
    "scope": "full_run"
  },
  "output_dir": "output/montage/$TASK_ID"
}
```

The adapter maps this stable manifest into the current upstream Montage project format. Keep every upstream checkpoint and decision log, while using `approval_policy` as full-run authorization for ordinary creative choices.
