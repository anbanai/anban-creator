---
name: topic-research
description: Use when researching WeChat topics, selecting from a topic pool, checking historical duplication, scoring topic candidates, or generating article outlines.
---

# 微信公众号选题分析


## 案例库

遇到场景分支、产物格式或质量边界不确定时，先读 [references/examples.md](references/examples.md)。

## 图片比例固定规则

本 Skill 只要涉及生成、选择、裁切、校验或引用图片，必须按以下优先级决定画面比例：

1. 用户/任务明确指定的 `image_ratio`、`size` 或平台规格优先。
2. 项目/频道默认比例次之。
3. 业务默认比例只作兜底：微信文章封面/正文图默认 `16:9`；Seednote/XLS/移动信息流默认 `3:4`；电商、广告投放、视频封面按具体平台素材位要求执行。
4. 不得从模型路由、供应商默认 `size` 或模型能力反推业务比例；模型只决定能力和成本，比例属于创作场景约束。

## Intent Routing

Use this Skill for topic source selection, duplicate checks, candidate generation, candidate scoring, Top 1 choice, and outline creation. Topic research and outline writing happen inside the Skill; MCP is used only for controlled discovery such as topic pool, history, profile, and task progress.

## Discovery First

1. Read the user prompt and decide whether a concrete topic was specified.
2. Call `get_project_profile(project_id, scope="article", task_id?)` for positioning, keywords, audience, writer, theme, and task overrides.
3. If the task did not specify a concrete topic, call `claim_topic(project_id, task_id?)` first.
4. Always call `list_project_titles(project_id)`, `list_drafts(project_id)`, and `list_published_articles(project_id)` before finalizing a title or outline.
5. Build an exclusion list from existing titles, draft titles, published titles, and close keyword variants.

## Configuration Boundaries

- A user-specified topic wins over the pool and must not consume the pool again.
- A non-empty topic pool wins over Skill-generated candidates.
- Project keywords guide candidate generation but are not themselves a topic.
- History tools are the source of truth for duplication checks.
- The Skill chooses structure and scoring rubric itself; do not delegate creative judgment to a generation MCP endpoint.

## Output Contract

Write these file-backed artifacts:

1. `$DIR/01-research.md` with:
   - topic source: user prompt / claimed pool item / Skill-generated candidate;
   - existing titles and exclusion list;
   - candidates, angles, audience fit, freshness, risk, and score;
   - final Top 1 topic and reason.
2. `$DIR/02-outline.md` with:
   - final title;
   - hook;
   - `##` sections;
   - each section's claim, context anchor, supporting material, and reader takeaway;
   - CTA / ending direction;
   - SEO seed keywords.
3. `$DIR/context-brief.md` when the caller has not already created one, containing original user need, project positioning, historical avoidance, chosen topic reason, and section anchors.

## Candidate And Outline Protocol

When the pool is empty, generate 5-10 candidates directly from project positioning, user intent, keywords, and historical gaps. Score each candidate on:

- audience fit;
- novelty against historical titles;
- usefulness or emotional resonance;
- concrete writing material available;
- compliance and overclaiming risk;
- title potential.

Pick the highest-scoring non-duplicate candidate. If all candidates collide with history, generate a second batch with a narrower angle or a different reader problem.

Outline templates are chosen internally:

| Template | Use When |
|---|---|
| authoritative | analysis, judgment, expert explanation |
| comparison | choices, products, methods, tradeoffs |
| cultural | story, history, people, values |
| practical | tutorial, checklist, step-by-step action |

## Failure Handling

- `claim_topic` returns an item but it duplicates history: keep the claimed topic, adjust the wording/angle, and record the change in `$DIR/01-research.md`.
- History tools fail: record the failure and continue with an explicit risk note.
- Candidate confidence is low: choose the least risky topic and add a "low confidence" note instead of blocking the whole task.
- Outline lacks concrete anchors: return to research notes and add supporting material before writing `$DIR/02-outline.md`.

## 深入参考

- 大纲模板：[outline-templates.md](references/outline-templates.md)
